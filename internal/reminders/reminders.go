// Package reminders owns explicitly configured, private public-on-sale emails.
package reminders

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

var ErrNotFound = errors.New("reminder not found")
var ErrUnavailable = errors.New("reminders unavailable")

type Item struct {
	EventID     string     `json:"event_id"`
	Name        string     `json:"event_name"`
	LeadMinutes int        `json:"lead_minutes"`
	State       string     `json:"state"`
	SaleAt      *time.Time `json:"sale_at"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	CreatedAt   time.Time  `json:"created_at"`
}
type Page struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type Claim struct {
	ID, Owner, EventID, Token string
	LeadMinutes               int
	Detail                    models.EventDetail
	Paused                    bool
}
type Delivery struct {
	JobID, To, Name, URL, Timezone, UnsubscribeToken string
	SaleAt                                           time.Time
}
type Repository interface {
	Get(context.Context, string, string) (Item, error)
	Set(context.Context, string, string, int) (Item, error)
	Remove(context.Context, string, string) error
	List(context.Context, string, saved.Query) ([]Item, error)
	Claim(context.Context) (Claim, error)
	Prepare(context.Context, Claim, *models.EventDetail) (*Delivery, error)
	Finish(context.Context, string, string, string) error
	Unsubscribe(context.Context, string) error
}
type Service struct{ repo Repository }

func New(r Repository) *Service { return &Service{r} }
func validate(owner, event string) error {
	if !accounts.ValidID(owner) {
		return accounts.ErrUnauthenticated
	}
	return saved.ValidateID(event)
}
func Lead(value int) error {
	if value < 1 || value > 10080 {
		return &accounts.ValidationError{Field: "lead_minutes", Message: "choose 1–10080 minutes (up to seven days)"}
	}
	return nil
}
func (s *Service) Get(ctx context.Context, owner, event string) (Item, error) {
	if err := validate(owner, event); err != nil {
		return Item{}, err
	}
	return s.repo.Get(ctx, owner, event)
}
func (s *Service) Set(ctx context.Context, owner, event string, lead int) (Item, error) {
	if err := validate(owner, event); err != nil {
		return Item{}, err
	}
	if err := Lead(lead); err != nil {
		return Item{}, err
	}
	return s.repo.Set(ctx, owner, event, lead)
}
func (s *Service) Remove(ctx context.Context, owner, event string) error {
	if err := validate(owner, event); err != nil {
		return err
	}
	return s.repo.Remove(ctx, owner, event)
}
func (s *Service) List(ctx context.Context, owner string, values url.Values) (Page, error) {
	if !accounts.ValidID(owner) {
		return Page{}, accounts.ErrUnauthenticated
	}
	q, err := saved.ParseQuery(values, "reminders:"+owner)
	if err != nil {
		return Page{}, err
	}
	items, err := s.repo.List(ctx, owner, q)
	if err != nil {
		return Page{}, err
	}
	p := Page{Items: items}
	if p.Items == nil {
		p.Items = []Item{}
	}
	if len(items) > q.Limit {
		p.Items = items[:q.Limit]
		last := p.Items[q.Limit-1]
		p.NextCursor = saved.EncodeCursor("reminders:"+owner, last.CreatedAt, last.EventID)
	}
	return p, nil
}
func (s *Service) Unsubscribe(ctx context.Context, token string) error {
	if !accounts.ValidID(token) {
		return ErrNotFound
	}
	return s.repo.Unsubscribe(ctx, token)
}

// Schedule never guesses an unknown time or turns absence into cancellation.
func Schedule(e models.Event, lead int, now time.Time) (string, *time.Time, *time.Time) {
	if e.Status == "cancelled" || e.Status == "canceled" || e.Status == "postponed" {
		return "cancelled", nil, nil
	}
	if e.PublicSale.StartTBD || e.PublicSale.Start == nil {
		return "pending_date", nil, nil
	}
	sale := e.PublicSale.Start.UTC()
	if !sale.After(now) {
		return "expired", &sale, nil
	}
	due := sale.Add(-time.Duration(lead) * time.Minute)
	return "scheduled", &sale, &due
}

type Provider interface {
	RefreshEvent(context.Context, string) (models.EventDetail, error)
}
type Sender interface {
	Send(context.Context, Delivery) (string, string)
}
type Worker struct {
	repo     Repository
	provider Provider
	sender   Sender
	logger   zerolog.Logger
	interval time.Duration
}

func NewWorker(r Repository, p Provider, s Sender, l zerolog.Logger, interval time.Duration) *Worker {
	return &Worker{r, p, s, l, interval}
}
func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := w.Step(ctx); err != nil && ctx.Err() == nil {
			w.logger.Warn().Err(err).Msg("On-sale reminder processing failed")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *Worker) Step(ctx context.Context) error {
	c, err := w.repo.Claim(ctx)
	if err != nil || c.ID == "" {
		return err
	}
	detail := c.Detail
	if !c.Paused && (detail.Meta.DataAsOf.IsZero() || time.Since(detail.Meta.DataAsOf) > 5*time.Minute) {
		fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		detail, err = w.provider.RefreshEvent(fetchCtx, c.EventID)
		cancel()
	}
	var fresh *models.EventDetail
	if err == nil && detail.Item.ID == c.EventID && !detail.Meta.Stale && !detail.Meta.DataAsOf.IsZero() {
		fresh = &detail
	}
	job, err := w.repo.Prepare(ctx, c, fresh)
	if err != nil || job == nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	state, messageID := w.sender.Send(sendCtx, *job)
	cancel()
	// Never log recipients, email content, credentials or raw provider errors.
	w.logger.Info().Str("state", state).Msg("On-sale email attempt completed")
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	return w.repo.Finish(finishCtx, job.JobID, state, messageID)
}
