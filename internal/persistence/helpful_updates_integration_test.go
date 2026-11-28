package persistence

import (
	"errors"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/saved"
)

// "Your answer helped": one private daily update per post for its author,
// counting that day's marks from other people that still stand, naming only
// people who opted into a first name, withheld while paused or withheld posts,
// and never caused by the author's own mark.
func TestMariaDBHelpfulUpdates(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	login := func(subject, first string, paused bool) accounts.Account {
		t.Helper()
		a, _ := loginAccount(t, auth, subject)
		if _, err := auth.UpdatePreferences(t.Context(), a.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(paused)}); err != nil {
			t.Fatal(err)
		}
		if first != "" {
			if _, err := auth.UpdateProfile(t.Context(), a.ID, accounts.ProfilePatch{ActivityFirstName: &first}); err != nil {
				t.Fatal(err)
			}
		}
		return a
	}
	author, quiet := login("helpful-author", "", false), login("helpful-paused-author", "", true)
	ada, ben, cy := login("helpful-ada", "Ada", true), login("helpful-ben", "", true), login("helpful-cy", "Cy", true)
	legacy := login("helpful-legacy", "Lee", true)
	talk := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	inbox := discussionfollows.New(p.DiscussionFollows())
	event := savedDetail("HelpfulUpdates")
	if _, _, err := p.Saved().Save(t.Context(), author.ID, event); err != nil {
		t.Fatal(err)
	}
	post := func(who, thread, body string) discussions.Post {
		t.Helper()
		created, _, err := talk.Create(t.Context(), who, event.Item.ID, thread, "", body, accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		return created
	}
	mark := func(who, id string, set bool) {
		t.Helper()
		if _, err := talk.React(t.Context(), who, id, set); err != nil {
			t.Fatal(err)
		}
	}
	envelopes := func(who string) int {
		t.Helper()
		var n int
		if err := p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM helpful_notifications WHERE account_id=?`, who).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	// Deliver today's windows now, as if the UTC day had ended.
	deliver := func() {
		t.Helper()
		execSQL(t, p.db, `UPDATE post_helpful SET created_at=created_at-INTERVAL 1 DAY`)
		execSQL(t, p.db, `UPDATE helpful_notifications SET window_start=window_start-INTERVAL 1 DAY,available_at=available_at-INTERVAL 1 DAY ORDER BY available_at`)
	}
	helpful := func(who string, values url.Values) []discussionfollows.Notification {
		t.Helper()
		page, err := inbox.Notifications(t.Context(), who, values)
		if err != nil {
			t.Fatal(err)
		}
		out := []discussionfollows.Notification{}
		for _, n := range page.Items {
			if n.Kind == discussionfollows.KindHelpful {
				out = append(out, n)
			}
		}
		return out
	}

	question := post(author.ID, "", "Which entrance is quickest?")
	reply := post(author.ID, question.ID, "The side entrance, it opened first.")
	quietPost := post(quiet.ID, "", "Paused author's tip")

	// A mark from before schema 29 has no time: it keeps the public count but
	// never counts toward, or is named in, an update.
	execSQL(t, p.db, `INSERT INTO post_helpful (account_id,post_id) VALUES (?,?)`, legacy.ID, question.ID)
	mark(author.ID, question.ID, true) // the author's own mark never notifies
	if n := envelopes(author.ID); n != 0 {
		t.Fatalf("an author's own mark queued %d updates", n)
	}
	for _, who := range []string{ada.ID, ben.ID, cy.ID} {
		mark(who, question.ID, true)
	}
	mark(ada.ID, question.ID, true) // unchanged retry
	mark(ben.ID, reply.ID, true)
	mark(ada.ID, quietPost.ID, true)
	if n := envelopes(author.ID); n != 2 {
		t.Fatalf("want one envelope per post per day, got %d", n)
	}
	if n := envelopes(quiet.ID); n != 0 {
		t.Fatalf("a paused author got %d queued updates", n)
	}
	if got := helpful(author.ID, nil); len(got) != 0 {
		t.Fatalf("updates delivered before their UTC day ended: %+v", got)
	}

	deliver()
	// Unmarking before delivery cancels that mark; Ben's is gone.
	mark(ben.ID, question.ID, false)
	got := helpful(author.ID, nil)
	if len(got) != 2 {
		t.Fatalf("delivered updates: %+v", got)
	}
	byPost := map[string]discussionfollows.Notification{}
	for _, n := range got {
		byPost[n.PostID] = n
	}
	q, r := byPost[question.ID], byPost[reply.ID]
	// Newest opted-in names first; Ben is never named.
	if q.HelpfulCount != 2 || !slices.Equal(q.HelpfulNames, []string{"Cy", "Ada"}) || q.Reply || q.ThreadID != question.ID || q.EventName == "" {
		t.Fatalf("question update: %+v", q)
	}
	if r.HelpfulCount != 1 || len(r.HelpfulNames) != 0 || !r.Reply || r.ThreadID != question.ID || r.URL == "" {
		t.Fatalf("reply update: %+v", r)
	}

	if public, err := talk.Thread(t.Context(), author.ID, event.Item.ID, question.ID); err != nil || public.HelpfulCount != 4 {
		t.Fatalf("public Helpful count with the legacy mark: %d %v", public.HelpfulCount, err)
	}

	// A reply update tied with the Helpful updates' delivery time merges into
	// one page order under one cursor, with no repeats or gaps.
	follows := discussionfollows.New(p.DiscussionFollows())
	if _, err := follows.Set(t.Context(), author.ID, event.Item.ID, question.ID, "immediate"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := talk.Create(t.Context(), ada.ID, event.Item.ID, question.ID, "", "Thanks, that helped.", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	execSQL(t, p.db, `UPDATE discussion_notifications SET available_at=(SELECT MAX(available_at) FROM helpful_notifications WHERE account_id=?) WHERE account_id=?`, author.ID, author.ID)
	all, err := inbox.Notifications(t.Context(), author.ID, nil)
	if err != nil || len(all.Items) != 3 {
		t.Fatalf("merged inbox: %+v %v", all.Items, err)
	}
	var paged []string
	values := url.Values{"limit": {"1"}}
	for range 5 {
		page, err := inbox.Notifications(t.Context(), author.ID, values)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range page.Items {
			paged = append(paged, n.ID)
		}
		if page.NextCursor == nil {
			break
		}
		values.Set("cursor", *page.NextCursor)
	}
	want := []string{}
	kinds := map[string]bool{}
	for _, n := range all.Items {
		want = append(want, n.ID)
		kinds[n.Kind] = true
	}
	if !slices.Equal(paged, want) || len(kinds) != 2 {
		t.Fatalf("paged %v, want %v (kinds %v)", paged, want, kinds)
	}
	for _, n := range all.Items {
		if n.Kind == discussionfollows.KindReplies {
			if err := inbox.Read(t.Context(), author.ID, n.ID); err != nil {
				t.Fatalf("read a reply update: %v", err)
			}
		}
	}

	if err := inbox.Read(t.Context(), author.ID, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Read(t.Context(), ada.ID, q.ID); !errors.Is(err, discussionfollows.ErrNotificationNotFound) {
		t.Fatalf("another account read someone's update: %v", err)
	}
	for _, n := range helpful(author.ID, nil) {
		if n.Kind == discussionfollows.KindHelpful && n.Read != (n.ID == r.ID) {
			t.Fatalf("read state: %+v", n)
		}
	}

	// A hidden post's update disappears, and can't be marked read.
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, question.ID)
	if got := helpful(author.ID, nil); len(got) != 1 || got[0].PostID != reply.ID {
		// (The reply update stays: it is about Ada's reply, not the question.)
		t.Fatalf("a hidden post's update stayed: %+v", got)
	}
	if err := inbox.Read(t.Context(), author.ID, q.ID); !errors.Is(err, discussionfollows.ErrNotificationNotFound) {
		t.Fatalf("read a hidden post's update: %v", err)
	}
	// Removing every mark leaves nothing to announce.
	mark(ben.ID, reply.ID, false)
	if got := helpful(author.ID, nil); len(got) != 0 {
		t.Fatalf("an update with no marks left: %+v", got)
	}
	// Pausing withholds the inbox.
	mark(ben.ID, reply.ID, true)
	deliver()
	if got := helpful(author.ID, nil); len(got) != 1 {
		t.Fatalf("re-marked reply: %+v", got)
	}
	if _, err := auth.UpdatePreferences(t.Context(), author.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(true)}); err != nil {
		t.Fatal(err)
	}
	if page, err := inbox.Notifications(t.Context(), author.ID, nil); err != nil || len(page.Items) != 0 {
		// Pause withholds both kinds.
		t.Fatalf("a paused inbox delivered: %+v %v", page.Items, err)
	}
}

// A Helpful mark locks the post, then the author's account (for the update);
// an author's reply locks them the other way round. Both must succeed when
// they race. This is a smoke test: the deadlock window is narrow and rarely
// hit here, so React's retry (as publication's) is what makes it survivable.
func TestMariaDBHelpfulRacesAuthorReplies(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "race-author")
	if _, err := auth.UpdatePreferences(t.Context(), author.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	talk := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	event := savedDetail("HelpfulRace")
	if _, _, err := p.Saved().Save(t.Context(), author.ID, event); err != nil {
		t.Fatal(err)
	}
	const rounds = 12
	markers := make([]accounts.Account, rounds)
	for i := range markers {
		markers[i], _ = loginAccount(t, auth, "race-marker-"+strconv.Itoa(i))
	}
	for i := range rounds {
		root, _, err := talk.Create(t.Context(), author.ID, event.Item.ID, "", "", "Race question "+strconv.Itoa(i), accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, errs[0] = talk.React(t.Context(), markers[i].ID, root.ID, true)
		}()
		go func() {
			defer wg.Done()
			_, _, errs[1] = talk.Create(t.Context(), author.ID, event.Item.ID, root.ID, "", "Author follow-up "+strconv.Itoa(i), accounts.ID())
		}()
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	var n int
	if err := p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM helpful_notifications WHERE account_id=?`, author.ID).Scan(&n); err != nil || n != rounds {
		t.Fatalf("queued %d Helpful updates, want %d: %v", n, rounds, err)
	}
}
