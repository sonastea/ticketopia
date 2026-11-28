package persistence

import (
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

// Venue tips are visible contributions on past events at the page's venue:
// never the event's own, a future or unannounced event's, another venue's, or
// a withheld post; most Helpful first, ties to the newest, at most three.
// Facets are seeded as collection writes them; saving doesn't.
func TestMariaDBVenueTips(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	auth := accounts.New(pool.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "venue-tip-author")
	fan, _ := loginAccount(t, auth, "venue-tip-fan")
	other, _ := loginAccount(t, auth, "venue-tip-other")
	svc := discussions.New(pool.Discussions(), saved.New(pool.Saved(), nil))
	current, pastA, pastB, upcoming, elsewhere := futureDetail("TipNow", 48*time.Hour), futureDetail("TipPastA", 49*time.Hour), futureDetail("TipPastB", 50*time.Hour), futureDetail("TipSoon", 51*time.Hour), futureDetail("TipElsewhere", 52*time.Hour)
	post := func(d models.EventDetail, thread, body string) discussions.Post {
		t.Helper()
		p, _, err := svc.Create(t.Context(), author.ID, d.Item.ID, thread, "", body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, d := range []models.EventDetail{current, pastA, pastB, upcoming, elsewhere} {
		if _, _, err := pool.Saved().Save(t.Context(), author.ID, d); err != nil {
			t.Fatal(err)
		}
	}
	own := post(current, "", "This event's own question")
	oldTip := post(pastA, "", "Use the side entrance")
	reply := post(pastA, oldTip.ID, "The side entrance opens at six")
	newer := post(pastB, "", "Bag check is strict")
	newest := post(pastB, "", "Parking fills up early")
	hidden := post(pastB, "", "Hidden by a moderator")
	removed := post(pastB, "", "Removed by its author")
	soon := post(upcoming, "", "Upcoming show's question")
	away := post(elsewhere, "", "Another venue's tip")

	execSQL(t, pool.db, `INSERT INTO venues (venue_id,snapshot,first_seen,last_seen) VALUES ('ticketmaster:TipHall','{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)),('ticketmaster:OtherHall','{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`)
	for _, d := range []models.EventDetail{current, pastA, pastB, upcoming} {
		execSQL(t, pool.db, `INSERT INTO event_search_facets (event_id,kind,value) VALUES (?,'venue','ticketmaster:TipHall')`, d.Item.ID)
	}
	execSQL(t, pool.db, `INSERT INTO event_search_facets (event_id,kind,value) VALUES (?,'venue','ticketmaster:OtherHall')`, elsewhere.Item.ID)
	execSQL(t, pool.db, `UPDATE events SET start_utc=UTC_TIMESTAMP(6)-INTERVAL 30 DAY WHERE event_id IN (?,?,?)`, pastA.Item.ID, pastB.Item.ID, elsewhere.Item.ID)
	execSQL(t, pool.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, hidden.ID)
	if err := svc.Remove(t.Context(), author.ID, removed.ID); err != nil {
		t.Fatal(err)
	}
	for _, who := range []string{fan.ID, other.ID} {
		if _, err := svc.React(t.Context(), who, reply.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.React(t.Context(), fan.ID, oldTip.ID, true); err != nil {
		t.Fatal(err)
	}

	event := current.Item
	event.Venues = []models.Venue{{ID: "ticketmaster:TipHall"}}
	ids := func() []string {
		t.Helper()
		tips, err := svc.VenueTips(t.Context(), fan.ID, event)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, p := range tips {
			if p.Event.ID == "" || p.Profile == nil || p.Body == "" {
				t.Fatalf("venue tip lacks its event or author: %+v", p)
			}
			out = append(out, p.ID)
		}
		return out
	}
	// Two Helpful, then one, then the newest of the unmarked.
	if got, want := ids(), []string{reply.ID, oldTip.ID, newest.ID}; !slices.Equal(got, want) {
		t.Fatalf("venue tips: got %v want %v (own %s, newer %s, hidden %s, removed %s, upcoming %s, elsewhere %s)", got, want, own.ID, newer.ID, hidden.ID, removed.ID, soon.ID, away.ID)
	}
	// Hiding a tip withdraws it at once; the next one takes its place.
	execSQL(t, pool.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, reply.ID)
	if got, want := ids(), []string{oldTip.ID, newest.ID, newer.ID}; !slices.Equal(got, want) {
		t.Fatalf("after hiding: got %v want %v", got, want)
	}
	// A date-only event is past only once its local day has ended.
	// MariaDB offsets stop at +13:00, so the zone's dates come from Go.
	kiritimati, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Fatal(err)
	}
	day := func(offset int) string { return time.Now().In(kiritimati).AddDate(0, 0, offset).Format(time.DateOnly) }
	execSQL(t, pool.db, `UPDATE events SET start_utc=NULL,timezone='Pacific/Kiritimati',local_date=? WHERE event_id=?`, day(0), pastB.Item.ID)
	if got, want := ids(), []string{oldTip.ID}; !slices.Equal(got, want) {
		t.Fatalf("today's date-only event counted as past: got %v want %v", got, want)
	}
	// Yesterday in Kiritimati (UTC+14) is past there when the server has named
	// time zones; without them, the unknown-zone fallback (a day later than
	// UTC's) still waits, so the event never counts early.
	execSQL(t, pool.db, `UPDATE events SET local_date=? WHERE event_id=?`, day(-1), pastB.Item.ID)
	var named bool
	if err := pool.db.QueryRowContext(t.Context(), `SELECT CONVERT_TZ(UTC_TIMESTAMP(),'+00:00','Pacific/Kiritimati') IS NOT NULL`).Scan(&named); err != nil {
		t.Fatal(err)
	}
	want := []string{oldTip.ID}
	if named {
		want = []string{oldTip.ID, newest.ID, newer.ID}
	}
	if got := ids(); !slices.Equal(got, want) {
		t.Fatalf("yesterday in the event's own zone (named zones %v): got %v want %v", named, got, want)
	}
	// An unannounced date never counts as past, whatever placeholder it keeps.
	execSQL(t, pool.db, `UPDATE events SET date_tbd=TRUE WHERE event_id=?`, pastB.Item.ID)
	execSQL(t, pool.db, `UPDATE events SET date_tba=TRUE WHERE event_id=?`, pastA.Item.ID)
	if got := ids(); len(got) != 0 {
		t.Fatalf("an unannounced date counted as a past show: %v", got)
	}
	execSQL(t, pool.db, `UPDATE events SET date_tba=FALSE WHERE event_id=?`, pastA.Item.ID)
	execSQL(t, pool.db, `UPDATE events SET date_tbd=FALSE WHERE event_id=?`, pastB.Item.ID)
	// A past current event still never lists its own posts.
	execSQL(t, pool.db, `UPDATE events SET start_utc=UTC_TIMESTAMP(6)-INTERVAL 2 DAY WHERE event_id=?`, current.Item.ID)
	if got := ids(); slices.Contains(got, own.ID) {
		t.Fatalf("a past event listed its own post as a venue tip: %v", got)
	}
	// Only the venue the page names counts, not a secondary one.
	event.Venues = []models.Venue{{ID: "ticketmaster:OtherHall"}, {ID: "ticketmaster:TipHall"}}
	if got, want := ids(), []string{away.ID}; !slices.Equal(got, want) {
		t.Fatalf("secondary venue matched: got %v want %v", got, want)
	}
	// Without venue IDs there is nothing to match.
	event.Venues = nil
	if got := ids(); len(got) != 0 {
		t.Fatalf("no venue, still tips: %v", got)
	}
}
