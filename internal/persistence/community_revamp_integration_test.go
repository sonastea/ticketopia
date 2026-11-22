package persistence

import (
	"errors"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

// The First post label is derived from visible history: a person's earliest
// visible question or tip, only while they have fewer than three visible
// contributions, and never a reply.
func TestMariaDBFirstPostFollowsVisibleHistory(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	auth := accounts.New(pool.Accounts(), accountProviderFixture{})
	who, _ := loginAccount(t, auth, "first-post-author")
	other, _ := loginAccount(t, auth, "first-post-other")
	svc := discussions.New(pool.Discussions(), saved.New(pool.Saved(), nil))
	one, two := savedDetail("FirstPostOne"), savedDetail("FirstPostTwo")
	if _, _, err := pool.Saved().Save(t.Context(), who.ID, one); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pool.Saved().Save(t.Context(), who.ID, two); err != nil {
		t.Fatal(err)
	}
	post := func(event, thread, body string) discussions.Post {
		t.Helper()
		p, _, err := svc.Create(t.Context(), who.ID, event, thread, "", body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	read := func(id string) discussions.Post {
		t.Helper()
		p, err := svc.Post(t.Context(), other.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	first := post(one.Item.ID, "", "Is there a cloakroom?")
	if !first.FirstPost {
		t.Fatal("a person's first question isn't marked")
	}
	second := post(two.Item.ID, "", "Where do people park?")
	if !read(first.ID).FirstPost || second.FirstPost {
		t.Fatal("only the earliest visible root should be marked")
	}
	reply := post(one.Item.ID, first.ID, "Found it, by the stairs.")
	if reply.FirstPost || read(first.ID).FirstPost {
		t.Fatal("a reply was marked, or the label outlived three visible contributions")
	}
	if err := svc.Remove(t.Context(), who.ID, reply.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Remove(t.Context(), who.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if read(first.ID).FirstPost || !read(second.ID).FirstPost {
		t.Fatal("after removal the earliest visible root should carry the label")
	}
}

func futureDetail(id string, in time.Duration) models.EventDetail {
	d := savedDetail(id)
	start := time.Now().UTC().Add(in).Truncate(time.Second)
	date := start.Format("2006-01-02")
	d.Item.Start.DateTime, d.Item.Start.LocalDate = &start, &date
	return d
}

// Open questions are visible roots with no visible reply on events that have
// not started, newest started first; "mine" stays the viewer's own private
// connections; Same question annotates without reordering and is withheld or
// deleted with the post.
func TestMariaDBOpenQuestionsAndSameQuestion(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	auth := accounts.New(pool.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "open-author")
	saver, _ := loginAccount(t, auth, "open-saver")
	helper, _ := loginAccount(t, auth, "open-helper")
	svc := discussions.New(pool.Discussions(), saved.New(pool.Saved(), nil))
	mine, theirs, past := futureDetail("OpenMine", 72*time.Hour), futureDetail("OpenTheirs", 96*time.Hour), savedDetail("OpenPast")
	for _, d := range []models.EventDetail{mine, theirs, past} {
		if _, _, err := pool.Saved().Save(t.Context(), author.ID, d); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := pool.Saved().Save(t.Context(), saver.ID, mine); err != nil {
		t.Fatal(err)
	}
	post := func(who, event, thread, body string) discussions.Post {
		t.Helper()
		p, _, err := svc.Create(t.Context(), who, event, thread, "", body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	ids := func(l discussions.List) []string {
		out := []string{}
		for _, p := range l.Items {
			out = append(out, p.ID)
		}
		return out
	}
	open := func(viewer string, values url.Values) discussions.List {
		t.Helper()
		l, err := svc.OpenQuestions(t.Context(), viewer, values)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	q1 := post(author.ID, mine.Item.ID, "", "Is the balcony seated?")
	q2 := post(author.ID, theirs.Item.ID, "", "Any parking nearby?")
	post(author.ID, past.Item.ID, "", "Was the sound good?")
	if got := open(saver.ID, nil); !slices.Equal(ids(got), []string{q2.ID, q1.ID}) || got.Count != 2 {
		t.Fatalf("open questions should be upcoming roots, newest first: %v (%d)", ids(got), got.Count)
	}
	if got := open(saver.ID, url.Values{"connections": {"mine"}}); !slices.Equal(ids(got), []string{q1.ID}) {
		t.Fatalf("mine should keep only the viewer's saved event: %v", ids(got))
	}
	if _, err := svc.OpenQuestions(t.Context(), "", url.Values{"connections": {"mine"}}); !errors.Is(err, accounts.ErrUnauthenticated) {
		t.Fatal("a guest asked for their own events", err)
	}
	page := open(saver.ID, url.Values{"limit": {"1"}})
	if page.NextCursor == nil || !slices.Equal(ids(open(saver.ID, url.Values{"limit": {"1"}, "cursor": {*page.NextCursor}})), []string{q1.ID}) {
		t.Fatal("open questions lost their place across pages")
	}
	if _, err := svc.OpenQuestions(t.Context(), saver.ID, url.Values{"connections": {"mine"}, "cursor": {*page.NextCursor}}); err == nil {
		t.Fatal("a cursor crossed from all open questions into mine")
	}
	reply := post(helper.ID, mine.Item.ID, q1.ID, "Yes, every row is seated.")
	if got := ids(open(saver.ID, nil)); !slices.Equal(got, []string{q2.ID}) {
		t.Fatalf("an answered question stayed open: %v", got)
	}
	execSQL(t, pool.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, reply.ID)
	if got := ids(open(saver.ID, nil)); !slices.Equal(got, []string{q2.ID, q1.ID}) {
		t.Fatalf("hiding the only reply should reopen the question in place: %v", got)
	}

	if _, err := svc.SameQuestion(t.Context(), author.ID, q2.ID, true); !errors.Is(err, discussions.ErrOwnQuestion) {
		t.Fatal("an author marked their own question", err)
	}
	if _, err := svc.SameQuestion(t.Context(), saver.ID, reply.ID, true); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("a reply accepted Same question", err)
	}
	for range 2 {
		marked, err := svc.SameQuestion(t.Context(), saver.ID, q2.ID, true)
		if err != nil || !marked.SameQuestion || marked.SameQuestionCount != 1 {
			t.Fatal("Same question isn't one idempotent mark", marked.SameQuestionCount, err)
		}
	}
	if seen, err := svc.Post(t.Context(), helper.ID, q2.ID); err != nil || seen.SameQuestion || seen.SameQuestionCount != 1 {
		t.Fatal("another viewer should see the count, not the mark", err)
	}
	if got := ids(open(saver.ID, nil)); !slices.Equal(got, []string{q2.ID, q1.ID}) {
		t.Fatalf("Same question reordered the list: %v", got)
	}
	execSQL(t, pool.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, q2.ID)
	if seen, _ := svc.Post(t.Context(), helper.ID, q2.ID); seen.SameQuestionCount != 0 || seen.SameQuestion {
		t.Fatal("a hidden question exposed its marks")
	}
	if got := ids(open(saver.ID, nil)); !slices.Equal(got, []string{q1.ID}) {
		t.Fatalf("a hidden question stayed open: %v", got)
	}
	execSQL(t, pool.db, `UPDATE discussion_posts SET hidden_at=NULL WHERE post_id=?`, q2.ID)
	if err := svc.Remove(t.Context(), author.ID, q2.ID); err != nil {
		t.Fatal(err)
	}
	var marks int
	if err := pool.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM discussion_same_question WHERE post_id=?`, q2.ID).Scan(&marks); err != nil || marks != 0 {
		t.Fatal("removal kept Same question marks", marks, err)
	}
	if unmarked, err := svc.SameQuestion(t.Context(), saver.ID, q2.ID, false); err != nil || unmarked.SameQuestion {
		t.Fatal("clearing a mark on a removed question should succeed quietly", err)
	}
}

// "Mine" matches each private connection kind for its owner only, and event
// eligibility follows the clock, not the cursor: an event that starts
// mid-pagination leaves later pages, while a date-only event stays listed
// through its own local day.
func TestMariaDBOpenQuestionsConnectionsAndEligibility(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pool := f.open(t)
	auth := accounts.New(pool.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "eligible-author")
	fan, _ := loginAccount(t, auth, "eligible-fan")
	stranger, _ := loginAccount(t, auth, "eligible-stranger")
	svc := discussions.New(pool.Discussions(), saved.New(pool.Saved(), nil))
	interested, artist, venue, unrelated := futureDetail("MineInterest", 48*time.Hour), futureDetail("MineArtist", 49*time.Hour), futureDetail("MineVenue", 50*time.Hour), futureDetail("MineNone", 51*time.Hour)
	roots := map[string]string{}
	for _, d := range []models.EventDetail{unrelated, venue, artist, interested} {
		if _, _, err := pool.Saved().Save(t.Context(), author.ID, d); err != nil {
			t.Fatal(err)
		}
		p, _, err := svc.Create(t.Context(), author.ID, d.Item.ID, "", "", "Question about "+d.Item.ID, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		roots[d.Item.ID] = p.ID
	}
	if _, _, err := pool.Interests().Set(t.Context(), fan.ID, interested, "private"); err != nil {
		t.Fatal(err)
	}
	execSQL(t, pool.db, `INSERT INTO artists (artist_id,snapshot,first_seen,last_seen) VALUES ('ticketmaster:MineBand','{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`)
	execSQL(t, pool.db, `INSERT INTO venues (venue_id,snapshot,first_seen,last_seen) VALUES ('ticketmaster:MineHall','{}',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6))`)
	execSQL(t, pool.db, `INSERT INTO event_search_facets (event_id,kind,value) VALUES (?,'artist','ticketmaster:MineBand'),(?,'venue','ticketmaster:MineHall')`, artist.Item.ID, venue.Item.ID)
	execSQL(t, pool.db, `INSERT INTO artist_follows (account_id,artist_id) VALUES (?,'ticketmaster:MineBand')`, fan.ID)
	execSQL(t, pool.db, `INSERT INTO venue_follows (account_id,venue_id) VALUES (?,'ticketmaster:MineHall')`, fan.ID)
	mine := func(viewer string) []string {
		t.Helper()
		l, err := svc.OpenQuestions(t.Context(), viewer, url.Values{"connections": {"mine"}})
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, p := range l.Items {
			out = append(out, p.ID)
		}
		return out
	}
	if got, want := mine(fan.ID), []string{roots[interested.Item.ID], roots[artist.Item.ID], roots[venue.Item.ID]}; !slices.Equal(got, want) {
		t.Fatalf("Interested, artist and venue follows should each match: got %v want %v", got, want)
	}
	if got := mine(stranger.ID); len(got) != 0 {
		t.Fatalf("another viewer's mine exposed someone else's connections: %v", got)
	}

	page, err := svc.OpenQuestions(t.Context(), fan.ID, url.Values{"limit": {"2"}})
	if err != nil || page.NextCursor == nil {
		t.Fatal("first page", err)
	}
	// The oldest question's event starts now; it must not appear on page two.
	execSQL(t, pool.db, `UPDATE events SET start_utc=UTC_TIMESTAMP(6)-INTERVAL 1 MINUTE WHERE event_id=?`, unrelated.Item.ID)
	next, err := svc.OpenQuestions(t.Context(), fan.ID, url.Values{"limit": {"2"}, "cursor": {*page.NextCursor}})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != roots[venue.Item.ID] {
		t.Fatalf("an event that started mid-pagination stayed listed: %+v %v", next.Items, err)
	}
	// Date-only: listed through its local day, gone once that day is past.
	execSQL(t, pool.db, `UPDATE events SET start_utc=NULL,timezone='Pacific/Kiritimati',local_date=DATE_FORMAT(CONVERT_TZ(UTC_TIMESTAMP(),'+00:00','+14:00'),'%Y-%m-%d') WHERE event_id=?`, unrelated.Item.ID)
	execSQL(t, pool.db, `UPDATE events SET start_utc=NULL,timezone=NULL,local_date=DATE_FORMAT(UTC_DATE()-INTERVAL 3 DAY,'%Y-%m-%d') WHERE event_id=?`, venue.Item.ID)
	all, err := svc.OpenQuestions(t.Context(), fan.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, p := range all.Items {
		ids[p.ID] = true
	}
	if !ids[roots[unrelated.Item.ID]] || ids[roots[venue.Item.ID]] {
		t.Fatalf("date-only eligibility: today's local event listed=%v, a past date listed=%v", ids[roots[unrelated.Item.ID]], ids[roots[venue.Item.ID]])
	}
}
