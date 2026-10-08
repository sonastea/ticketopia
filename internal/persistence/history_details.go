package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

func (r *HistoryRepository) ClaimDetail(ctx context.Context) (string, string, error) {
	token, err := newToken()
	if err != nil {
		return "", "", err
	}
	var id string
	err = historyTransaction(ctx, r.db, r.timeout, "detail claim", func(ctx context.Context, tx *sql.Tx) error {
		id = ""
		err := tx.QueryRowContext(ctx, `SELECT event_id FROM event_detail_tasks WHERE next_refresh<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6)) ORDER BY next_refresh,event_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE event_detail_tasks SET token=?,lease_until=TIMESTAMPADD(SECOND,60,UTC_TIMESTAMP(6)),last_attempt=UTC_TIMESTAMP(6) WHERE event_id=?`, token, id)
		return err
	})
	return id, token, err
}

func (r *HistoryRepository) FinishDetail(ctx context.Context, id, token string, detail *models.EventDetail, delay time.Duration) error {
	if delay < time.Second || delay > 7*24*time.Hour {
		return fmt.Errorf("invalid detail retry interval")
	}
	return historyTransaction(ctx, r.db, r.timeout, "detail completion", func(ctx context.Context, tx *sql.Tx) error {
		var owned string
		err := tx.QueryRowContext(ctx, `SELECT token FROM event_detail_tasks WHERE event_id=? AND token=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE`, id, token).Scan(&owned)
		if errors.Is(err, sql.ErrNoRows) {
			return history.ErrLeaseLost
		}
		if err != nil {
			return err
		}
		if detail != nil {
			if detail.Item.ID != id || detail.Meta.Stale {
				return history.ErrLeaseLost
			}
			args, data, err := prepareSnapshot(*detail)
			if err != nil {
				return err
			}
			if err := upsertSnapshotTx(ctx, tx, *detail, args, data); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `DELETE FROM event_detail_tasks WHERE event_id=?`, id)
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE event_detail_tasks SET token=NULL,lease_until=NULL,last_failure='provider_unavailable',next_refresh=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE event_id=?`, delay.Microseconds(), id)
		return err
	})
}
