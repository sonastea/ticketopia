package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/models"
)

var _ events.Repository = (*EventRepository)(nil)

type EventRepository struct {
	db      *sql.DB
	timeout time.Duration
}

const eventColumns = `event_id, name, source_url, start_utc, local_date, local_time, timezone,
date_tba, date_tbd, time_tba, no_specific_time, status, venues, artists, classifications, place`

var providerIdentity = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,31}$`)
var sourceIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

func validateEvent(e models.Event) error {
	if !providerIdentity.MatchString(e.Source.Provider) || !sourceIdentity.MatchString(e.Source.ID) || e.ID != e.Source.Provider+":"+e.Source.ID {
		return fmt.Errorf("event requires a matching provider-scoped identity")
	}
	if e.Name == "" || !utf8.ValidString(e.Name) || utf8.RuneCountInString(e.Name) > 1024 || utf8.RuneCountInString(e.Status) > 64 || len(e.Source.URL) > 65535 {
		return fmt.Errorf("event metadata exceeds supported limits")
	}
	if e.Start.DateTime != nil && (e.Start.DateTime.Year() < 1000 || e.Start.DateTime.Year() > 9999) {
		return fmt.Errorf("event instant is outside MariaDB's supported range")
	}
	if e.Start.LocalDate != nil {
		if _, err := time.Parse("2006-01-02", *e.Start.LocalDate); err != nil {
			return fmt.Errorf("event local date must be YYYY-MM-DD")
		}
	}
	if e.Start.LocalTime != nil {
		if _, err := time.Parse("15:04:05", *e.Start.LocalTime); err != nil {
			return fmt.Errorf("event local time must be HH:MM:SS")
		}
	}
	if e.Start.Timezone != nil && utf8.RuneCountInString(*e.Start.Timezone) > 64 {
		return fmt.Errorf("event time zone exceeds supported limits")
	}
	return nil
}

// Upsert changes metadata, never identity. No absence/deletion reconciliation is
// provided. Unique keys and one InnoDB transaction coordinate independent pools.
func (r *EventRepository) Upsert(ctx context.Context, event models.Event) (models.Event, error) {
	if err := validateEvent(event); err != nil {
		return models.Event{}, err
	}
	if event.Start.DateTime != nil {
		instant := event.Start.DateTime.UTC().Truncate(time.Microsecond)
		event.Start.DateTime = &instant
	}
	args := []any{event.ID, event.Name, event.Source.URL, event.Start.DateTime, event.Start.LocalDate, event.Start.LocalTime, event.Start.Timezone,
		event.Start.DateTBA, event.Start.DateTBD, event.Start.TimeTBA, event.Start.NoSpecificTime, event.Status}
	for _, value := range []any{event.Venues, event.Artists, event.Classifications, event.Place} {
		data, err := json.Marshal(value)
		if err != nil || len(data) > 1024*1024 {
			return models.Event{}, fmt.Errorf("event metadata cannot be stored")
		}
		args = append(args, string(data))
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		retry, err := r.upsertOnce(ctx, event, args)
		if err == nil {
			return event, nil
		}
		if !retry || attempt == 2 {
			return models.Event{}, safeError("event upsert", err)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return models.Event{}, safeError("event upsert", ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func retryable(err error) bool {
	var server *mysql.MySQLError
	return errors.As(err, &server) && (server.Number == 1213 || server.Number == 1205)
}

func (r *EventRepository) upsertOnce(ctx context.Context, event models.Event, args []any) (bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return retryable(err), err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO events (`+eventColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE name=VALUES(name), source_url=VALUES(source_url), start_utc=VALUES(start_utc),
local_date=VALUES(local_date), local_time=VALUES(local_time), timezone=VALUES(timezone), date_tba=VALUES(date_tba),
date_tbd=VALUES(date_tbd), time_tba=VALUES(time_tba), no_specific_time=VALUES(no_specific_time), status=VALUES(status),
venues=VALUES(venues), artists=VALUES(artists), classifications=VALUES(classifications), place=VALUES(place), updated_at=UTC_TIMESTAMP(6)`, args...)
	if err != nil {
		return retryable(err), err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO event_providers (provider, source_id, event_id) VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE event_id=event_id`, event.Source.Provider, event.Source.ID, event.ID)
	if err != nil {
		return retryable(err), err
	}
	var mapped string
	if err := tx.QueryRowContext(ctx, `SELECT event_id FROM event_providers WHERE provider=? AND source_id=?`, event.Source.Provider, event.Source.ID).Scan(&mapped); err != nil {
		return retryable(err), err
	}
	if mapped != event.ID {
		return false, fmt.Errorf("provider mapping cannot change identity")
	}
	// A transport failure during COMMIT is uncertain. Do not automatically retry;
	// callers can resolve the stable identity before deciding to repeat this upsert.
	return false, tx.Commit()
}

func (r *EventRepository) Get(ctx context.Context, id string) (models.Event, error) {
	provider, source, ok := strings.Cut(id, ":")
	if !ok || !providerIdentity.MatchString(provider) || !sourceIdentity.MatchString(source) {
		return models.Event{}, fmt.Errorf("invalid durable event identity")
	}
	return r.GetByProvider(ctx, provider, source)
}

func (r *EventRepository) GetByProvider(ctx context.Context, provider, source string) (models.Event, error) {
	if !providerIdentity.MatchString(provider) || !sourceIdentity.MatchString(source) {
		return models.Event{}, fmt.Errorf("invalid provider identity")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row := r.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE event_id =
(SELECT event_id FROM event_providers WHERE provider=? AND source_id=?)`, provider, source)
	var event models.Event
	var instant sql.NullTime
	var localDate, localTime, timezone sql.NullString
	var venues, artists, classifications, place []byte
	err := row.Scan(&event.ID, &event.Name, &event.Source.URL, &instant, &localDate, &localTime, &timezone,
		&event.Start.DateTBA, &event.Start.DateTBD, &event.Start.TimeTBA, &event.Start.NoSpecificTime, &event.Status,
		&venues, &artists, &classifications, &place)
	if errors.Is(err, sql.ErrNoRows) {
		return models.Event{}, events.ErrNotFound
	}
	if err != nil {
		return models.Event{}, safeError("event read", err)
	}
	event.Source.Provider, event.Source.ID, _ = strings.Cut(event.ID, ":")
	if instant.Valid {
		value := instant.Time.UTC()
		event.Start.DateTime = &value
	}
	if localDate.Valid {
		event.Start.LocalDate = &localDate.String
	}
	if localTime.Valid {
		event.Start.LocalTime = &localTime.String
	}
	if timezone.Valid {
		event.Start.Timezone = &timezone.String
	}
	for _, item := range []struct {
		data   []byte
		target any
	}{
		{venues, &event.Venues}, {artists, &event.Artists}, {classifications, &event.Classifications}, {place, &event.Place},
	} {
		if err := json.Unmarshal(item.data, item.target); err != nil {
			return models.Event{}, fmt.Errorf("durable event metadata is invalid")
		}
	}
	return event, nil
}
