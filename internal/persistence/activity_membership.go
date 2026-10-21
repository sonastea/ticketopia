package persistence

import (
	"context"
	"database/sql"
	"strings"
	"sync"

	"github.com/sonastea/ticketopia/internal/activity"
)

var membershipWatches = struct {
	sync.Mutex
	entries map[*sql.DB]map[*func(string)]bool
}{entries: map[*sql.DB]map[*func(string)]bool{}}

func (r *ActivityRepository) WatchMembership(fn func(string)) func() {
	membershipWatches.Lock()
	if membershipWatches.entries[r.db] == nil {
		membershipWatches.entries[r.db] = map[*func(string)]bool{}
	}
	membershipWatches.entries[r.db][&fn] = true
	membershipWatches.Unlock()
	return func() {
		membershipWatches.Lock()
		defer membershipWatches.Unlock()
		delete(membershipWatches.entries[r.db], &fn)
		if len(membershipWatches.entries[r.db]) == 0 {
			delete(membershipWatches.entries, r.db)
		}
	}
}
func notifyActivityMembership(db *sql.DB, owner string) {
	membershipWatches.Lock()
	defer membershipWatches.Unlock()
	for fn := range membershipWatches.entries[db] {
		(*fn)(owner)
	}
}

func bumpActivityMembership(ctx context.Context, tx *sql.Tx, owner string) error {
	_, err := tx.ExecContext(ctx, `UPDATE accounts SET activity_membership_revision=activity_membership_revision+1 WHERE account_id=?`, owner)
	return err
}

func membershipRevisions(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, owners []string) (map[string]uint64, error) {
	result := map[string]uint64{}
	if len(owners) == 0 {
		return result, nil
	}
	if len(owners) > 128 {
		return nil, activity.ErrUnavailable
	}
	args := make([]any, len(owners))
	for i, id := range owners {
		args[i] = id
	}
	rows, err := q.QueryContext(ctx, `SELECT account_id,activity_membership_revision FROM accounts WHERE account_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var revision uint64
		if err := rows.Scan(&id, &revision); err != nil {
			return nil, err
		}
		result[id] = revision
	}
	return result, rows.Err()
}

func (r *ActivityRepository) MembershipRevisions(ctx context.Context, owners []string) (map[string]uint64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if len(owners) > 0 && len(owners) <= 128 {
		r.statements.Add(1)
	}
	result, err := membershipRevisions(ctx, r.db, owners)
	if err != nil {
		return nil, safeError("activity membership revisions", err)
	}
	return result, nil
}

// Bounded candidate joins and revisions share a consistent metadata snapshot.
func (r *ActivityRepository) Memberships(ctx context.Context, owners []activity.MembershipOwner, candidates []activity.MembershipCandidate) (map[string]activity.Membership, error) {
	if len(owners) > 128 || len(candidates) > 256 {
		return nil, activity.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, safeError("activity membership read", err)
	}
	defer tx.Rollback()
	ids := []string{}
	for _, o := range owners {
		ids = append(ids, o.ID)
	}
	if len(ids) > 0 {
		r.statements.Add(1)
	}
	revisions, err := membershipRevisions(ctx, tx, ids)
	if err != nil {
		return nil, safeError("activity membership read", err)
	}
	result := map[string]activity.Membership{}
	for _, o := range owners {
		if revisions[o.ID] == 0 {
			return nil, activity.ErrUnavailable
		}
		result[o.ID] = activity.Membership{Revision: revisions[o.ID], Matches: map[string]uint8{}}
	}
	if len(candidates) == 0 {
		return result, nil
	}
	eventArgs := []any{}
	expected := map[string]activity.MembershipCandidate{}
	for _, c := range candidates {
		eventArgs = append(eventArgs, c.ID)
		expected[c.ID] = c
	}
	facets := false
	for _, o := range owners {
		facets = facets || o.Sources&(activity.SourceBit("followed_artists")|activity.SourceBit("followed_venues")) != 0
	}
	in := strings.TrimSuffix(strings.Repeat("?,", len(candidates)), ",")
	// Only artist/venue joins read event facets, so only they can combine newer
	// relationship matches with older generation facts. Saved/interested match
	// by event ID: verifying metadata there made every assembly fail (and back
	// off) while corrections arrived faster than the projection interval.
	if facets {
		r.statements.Add(1)
		rows, err := tx.QueryContext(ctx, `SELECT event_id,data_as_of FROM event_snapshots WHERE event_id IN (`+in+`)`, eventArgs...)
		if err != nil {
			return nil, safeError("activity membership metadata", err)
		}
		for rows.Next() {
			var c activity.MembershipCandidate
			if err := rows.Scan(&c.ID, &c.Revision); err != nil {
				rows.Close()
				return nil, err
			}
			if !expected[c.ID].Revision.Equal(c.Revision) {
				rows.Close()
				return nil, activity.ErrUnavailable
			}
			delete(expected, c.ID)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || len(expected) != 0 {
			return nil, activity.ErrUnavailable
		}
	}
	parts := []string{}
	args := []any{}
	for _, source := range []struct {
		bit         uint8
		table, join string
	}{
		{1, "saved_events m", ""}, {2, "event_interests m", ""},
		{4, "artist_follows m", ` JOIN event_search_facets f ON f.kind='artist' AND f.value=m.artist_id`},
		{8, "venue_follows m", ` JOIN event_search_facets f ON f.kind='venue' AND f.value=m.venue_id`},
	} {
		ownerArgs := []any{}
		for _, o := range owners {
			if o.Sources&source.bit != 0 {
				ownerArgs = append(ownerArgs, o.ID)
			}
		}
		if len(ownerArgs) == 0 {
			continue
		}
		event := "m.event_id"
		if source.join != "" {
			event = "f.event_id"
		}
		parts = append(parts, `SELECT DISTINCT m.account_id,`+event+`,? FROM `+source.table+source.join+` WHERE m.account_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ownerArgs)), ",")+`) AND `+event+` IN (`+in+`)`)
		args = append(args, source.bit)
		args = append(args, ownerArgs...)
		args = append(args, eventArgs...)
	}
	if len(parts) > 0 {
		r.statements.Add(1)
		rows, err := tx.QueryContext(ctx, strings.Join(parts, ` UNION ALL `), args...)
		if err != nil {
			return nil, safeError("activity membership matches", err)
		}
		for rows.Next() {
			var owner, event string
			var bit uint8
			if err := rows.Scan(&owner, &event, &bit); err != nil {
				rows.Close()
				return nil, err
			}
			result[owner].Matches[event] |= bit
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, safeError("activity membership matches", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, safeError("activity membership snapshot", err)
	}
	return result, nil
}
