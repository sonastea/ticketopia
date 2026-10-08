package persistence

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/moderation"
)

func TestMariaDBModerationWorkflowPrivacyAndRevocation(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	operator, err := Open(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, authorRaw := loginAccount(t, auth, "moderation-author")
	reporter, reporterRaw := loginAccount(t, auth, "moderation-reporter")
	mod, modRaw := loginAccount(t, auth, "moderation-moderator")
	outsider, _ := loginAccount(t, auth, "moderation-outsider")
	if _, err = auth.UpdateProfile(t.Context(), reporter.ID, accounts.ProfilePatch{DisplayName: pointer("Private reporter")}); err != nil {
		t.Fatal(err)
	}
	posts := discussions.New(p.Discussions(), discussionDetailFixture{savedDetail("Moderation")})
	root, _, err := posts.Create(t.Context(), author.ID, "ticketmaster:Moderation", "", "", "Reported text <script>plain</script>", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	reply, _, err := posts.Create(t.Context(), reporter.ID, root.EventID, root.ID, "", "A useful reply", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = posts.React(t.Context(), reporter.ID, root.ID, true); err != nil {
		t.Fatal(err)
	}
	s := moderation.New(p.Moderation())
	if _, err = s.Queue(t.Context(), author.ID, nil); !errors.Is(err, moderation.ErrForbidden) {
		t.Fatal("default role", err)
	}
	if changed, err := p.SetModerator(t.Context(), mod.ID, "fixture", true); err == nil || changed {
		t.Fatal("runtime assigned moderator", err)
	}
	if changed, err := operator.SetModerator(t.Context(), mod.ID, "test operator", true); err != nil || !changed {
		t.Fatal("grant", changed, err)
	}
	if changed, err := operator.SetModerator(t.Context(), mod.ID, "test operator", true); err != nil || changed {
		t.Fatal("grant retry", changed, err)
	}
	if ok, err := s.IsModerator(t.Context(), author.ID); err != nil || ok {
		t.Fatal("email granted authority", ok, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	ids := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			r, _, err := s.Report(t.Context(), reporter.ID, root.ID, "private_information", "Private case context")
			if err != nil {
				errs <- err
			} else {
				ids <- r.ID
			}
		})
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		t.Fatal(err)
	}
	reportID := ""
	for id := range ids {
		if reportID == "" {
			reportID = id
		}
		if id != reportID {
			t.Fatal("duplicate report")
		}
	}
	if _, _, err = s.Report(t.Context(), author.ID, root.ID, "other", ""); err == nil {
		t.Fatal("self report")
	}
	c, err := s.Case(t.Context(), mod.ID, root.ID, nil)
	if err != nil || len(c.Reports) != 1 || c.CurrentBody != root.Body {
		t.Fatal("case", c, err)
	}
	stale := moderation.Review{Action: "hide", Reason: "Remove private information before posting.", Notes: "PRIVATE MODERATOR NOTES", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}
	if _, _, err = s.Report(t.Context(), outsider.ID, root.ID, "other", "Another private concern"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(t.Context(), mod.ID, root.ID, stale); !errors.Is(err, moderation.ErrConflict) {
		t.Fatal("closed an unseen report", err)
	}
	c, err = s.Case(t.Context(), mod.ID, root.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	review := stale
	review.ExpectedVersion = &c.Version
	review.ExpectedUpdatedAt = c.Post.UpdatedAt
	o, err := s.Decide(t.Context(), mod.ID, root.ID, review)
	if err != nil {
		t.Fatal("hide", err)
	}
	if _, err = s.Decide(t.Context(), mod.ID, root.ID, review); !errors.Is(err, moderation.ErrConflict) {
		t.Fatal("duplicate decision", err)
	}
	public, err := posts.Thread(t.Context(), "", root.EventID, root.ID)
	if err != nil || !public.Hidden || public.Body != "" || public.Profile != nil || public.HelpfulCount != 0 || public.Helpful || public.ReplyCount != 1 || !public.UpdatedAt.Equal(root.UpdatedAt) {
		t.Fatal("hidden projection", public, err)
	}
	if _, err = posts.Edit(t.Context(), author.ID, root.ID, "Bypass hiding"); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("hidden edit", err)
	}
	if _, err = posts.React(t.Context(), reporter.ID, root.ID, true); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("hidden Helpful", err)
	}
	if _, err = posts.Parent(t.Context(), "", root.EventID, root.ID, reply.ID); err != nil {
		t.Fatal("lost reply", err)
	}
	for _, who := range []string{author.ID, outsider.ID, mod.ID} {
		reports, err := s.Reports(t.Context(), who, nil)
		if err != nil || (who != outsider.ID && len(reports.Items) != 0) {
			t.Fatal("private reports", who, reports, err)
		}
	}
	reports, err := s.Reports(t.Context(), reporter.ID, nil)
	if err != nil || len(reports.Items) != 1 || reports.Items[0].Outcome == nil || reports.Items[0].Outcome.Reason != review.Reason {
		t.Fatal("report acknowledgment", reports, err)
	}
	if err = s.Appeal(t.Context(), reporter.ID, o.ID, "Not my contribution"); !errors.Is(err, moderation.ErrNotFound) {
		t.Fatal("wrong author appeal", err)
	}
	if err = s.Appeal(t.Context(), author.ID, o.ID, "Please reconsider; this was venue information."); err != nil {
		t.Fatal(err)
	}
	if err = s.Appeal(t.Context(), author.ID, o.ID, "Retry"); err != nil {
		t.Fatal("appeal retry", err)
	}
	queue, err := s.Queue(t.Context(), mod.ID, nil)
	if err != nil || len(queue.Items) != 1 || queue.Items[0].PendingReports != 0 || queue.Items[0].PendingAppeals != 1 {
		t.Fatal("appeal queue", queue, err)
	}
	cache := kv.NewMemory()
	defer cache.Close()
	app, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, api.WithAccounts(accounts.Config{Enabled: true, BaseURL: "https://events.example", ClientID: "fixture", ClientSecret: "fixture"}, auth), api.WithEventDiscussions(p.Discussions()), api.WithModeration(p.Moderation()))
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, raw string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if raw != "" {
			req.AddCookie(&http.Cookie{Name: "__Host-ticketopia_session", Value: raw})
		}
		w := httptest.NewRecorder()
		app.Routes().ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		path, raw string
		status    int
	}{{"/api/v1/me/reports", "", 401}, {"/api/v1/moderation", authorRaw, 403}, {"/api/v1/moderation/posts/" + root.ID, reporterRaw, 403}, {"/moderation/posts/" + root.ID, authorRaw, 403}, {"/api/v1/me/reports", reporterRaw, 200}, {"/api/v1/me/moderation", authorRaw, 200}} {
		w := request(tc.path, tc.raw)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		if tc.status == 200 {
			for _, secret := range []string{reporter.ID, "Private reporter", "PRIVATE MODERATOR NOTES", "reported_body", "moderator_id", "same-email@example.com"} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatal("private review data leaked", secret, w.Body.String())
				}
			}
		}
	}
	w := request("/api/v1/moderation/posts/"+root.ID, modRaw)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PRIVATE MODERATOR NOTES") {
		t.Fatal("moderator case", w.Code, w.Body.String())
	}
	// A later role grant must not make an affected author a reader of their own
	// evidence or reveal case counts through either the pending or all queue.
	if _, err = operator.SetModerator(t.Context(), author.ID, "test operator", true); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/moderation/posts/" + root.ID, "/moderation/posts/" + root.ID} {
		w = request(path, authorRaw)
		if w.Code != 403 || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("moderator author read own case", w.Code, w.Body.String())
		}
		for _, secret := range []string{reporter.ID, "Private case context", "PRIVATE MODERATOR NOTES", reportID} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("self case leaked evidence", w.Body.String())
			}
		}
	}
	if denied, err := p.Moderation().Case(t.Context(), author.ID, root.ID, moderation.CaseQuery{}); !errors.Is(err, moderation.ErrForbidden) || len(denied.Reports) != 0 || denied.CurrentBody != "" {
		t.Fatal("repository own-case privacy", denied, err)
	}
	for _, state := range []string{"pending", "all"} {
		ownQueue, err := s.Queue(t.Context(), author.ID, url.Values{"state": {state}})
		if err != nil || len(ownQueue.Items) != 0 {
			t.Fatal("own queue leaked case activity", ownQueue, err)
		}
	}
	for _, path := range []string{"/me/reports", "/me/moderation", "/api/v1/me/moderation"} {
		w = request(path, authorRaw)
		if w.Code != 200 {
			t.Fatal("self access lost own outcomes", path, w.Code, w.Body.String())
		}
		if path != "/me/reports" && !strings.Contains(w.Body.String(), review.Reason) {
			t.Fatal("missing own shared reason", w.Body.String())
		}
		for _, secret := range []string{reporter.ID, "Private case context", "PRIVATE MODERATOR NOTES"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("own outcome leaked evidence", w.Body.String())
			}
		}
	}
	c, err = s.Case(t.Context(), mod.ID, root.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := operator.SetModerator(t.Context(), mod.ID, "test operator", false); err != nil || !changed {
		t.Fatal("revoke", changed, err)
	}
	w = request("/api/v1/moderation/posts/"+root.ID, modRaw)
	if w.Code != 403 {
		t.Fatal("revocation needed restart", w.Code, w.Body.String())
	}
	if _, err = p.Moderation().Decide(t.Context(), mod.ID, root.ID, moderation.Review{Action: "restore", Reason: "Restored", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}); !errors.Is(err, moderation.ErrForbidden) {
		t.Fatal("repository used stale role", err)
	}
	if _, err = operator.SetModerator(t.Context(), mod.ID, "test operator", true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(t.Context(), mod.ID, root.ID, moderation.Review{Action: "restore", Reason: "Restored after reviewing context.", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}); err != nil {
		t.Fatal("restore", err)
	}
	public, err = posts.Thread(t.Context(), "", root.EventID, root.ID)
	if err != nil || public.Hidden || public.Body != root.Body || public.HelpfulCount != 1 {
		t.Fatal("restore projection", public, err)
	}
	queue, err = s.Queue(t.Context(), mod.ID, nil)
	if err != nil || len(queue.Items) != 0 {
		t.Fatal("unresolved case", queue, err)
	}
	// Private state and roles remain usable across a fresh pool, without provider.
	restarted := f.open(t)
	s = moderation.New(restarted.Moderation())
	outcomes, err := s.Outcomes(t.Context(), author.ID, nil)
	if err != nil || len(outcomes.Items) != 2 {
		t.Fatal("durable outcomes", outcomes, err)
	}
	if ok, err := s.IsModerator(t.Context(), mod.ID); err != nil || !ok {
		t.Fatal("durable role", err)
	}
	var events int
	if err = p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM account_role_events WHERE account_id=?`, mod.ID).Scan(&events); err != nil || events != 3 {
		t.Fatal("role audit", events, err)
	}
	data, _ := json.Marshal(outcomes)
	if strings.Contains(string(data), "PRIVATE MODERATOR NOTES") {
		t.Fatal("private notes persisted into author projection")
	}
	if outcomes.Items[0].EventName != root.Event.Name || !outcomes.Items[0].PostCreatedAt.Equal(root.CreatedAt) || reports.Items[0].EventName != root.Event.Name {
		t.Fatal("missing safe personal record identity", outcomes, reports)
	}
	// Select a reply outside the current page and retain only its public
	// placeholder after hiding. Never duplicate an element ID on an on-page target.
	selected, _, err := posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Selected reply beyond first page", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	path := "/events/" + root.EventID + "/discussions/" + root.ID + "?limit=1&post_id=" + selected.ID
	w = request(path, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Selected contribution") || strings.Count(w.Body.String(), `id="post-`+selected.ID+`"`) != 1 || !strings.Contains(w.Body.String(), selected.Body) {
		t.Fatal("off-page target", w.Code, w.Body.String())
	}
	w = request("/events/"+root.EventID+"/discussions/"+root.ID+"?limit=1&post_id="+reply.ID, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), ">Selected contribution<") || strings.Count(w.Body.String(), `id="post-`+reply.ID+`"`) != 1 {
		t.Fatal("duplicated on-page target", w.Code, w.Body.String())
	}
	c, err = s.Case(t.Context(), mod.ID, selected.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(t.Context(), mod.ID, selected.ID, moderation.Review{Action: "hide", Reason: "Hide selected reply", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	w = request(path, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), selected.Body) || strings.Count(w.Body.String(), `id="post-`+selected.ID+`"`) != 1 {
		t.Fatal("selected hidden target", w.Code, w.Body.String())
	}
	foreign, _, err := posts.Create(t.Context(), author.ID, root.EventID, "", "", "Unrelated conversation", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	w = request("/events/"+root.EventID+"/discussions/"+root.ID+"?post_id="+foreign.ID, "")
	if w.Code != 404 {
		t.Fatal("cross-thread target", w.Code, w.Body.String())
	}
	w = request(path+"&post_id="+reply.ID, "")
	if w.Code != 400 {
		t.Fatal("duplicate selection", w.Code, w.Body.String())
	}
}

func TestMariaDBModerationSchema7UpgradePreservesPostsAndActivity(t *testing.T) {
	f := newMaria(t)
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_event_identity.sql", "00002_accounts.sql", "00003_saved_events.sql", "00004_event_interests.sql", "00005_event_recommendations.sql", "00006_event_discussions.sql", "00007_recommendation_observation.sql"} {
		data, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(t.Context(), f.migration, previous); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := &Pool{db: db, config: f.migration}
	auth := accounts.New(old.Accounts(), accountProviderFixture{})
	author, raw := loginAccount(t, auth, "moderation-upgrade")
	detail := savedDetail("ModerationUpgrade")
	if _, _, err := old.Saved().Save(t.Context(), author.ID, detail); err != nil {
		t.Fatal(err)
	}
	post, key := accounts.ID(), accounts.ID()
	hash := accounts.Hash("retained retry fingerprint")
	execSQL(t, db, `INSERT INTO discussion_posts (post_id,account_id,event_id,body,idempotency_key,request_hash) VALUES (?,?,?,?,?,?)`, post, author.ID, detail.Item.ID, "Retained public text", key, hash[:])
	execSQL(t, db, `INSERT INTO post_helpful (account_id,post_id) VALUES (?,?)`, author.ID, post)
	if _, err = Open(t.Context(), f.migration); err == nil {
		t.Fatal("accepted old schema")
	}
	f.migrate(t)
	p := f.open(t)
	if principal, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), raw, "session"); err != nil || principal.Account.ID != author.ID {
		t.Fatal("lost session", err)
	}
	if _, err := p.Saved().Get(t.Context(), author.ID, detail.Item.ID); err != nil {
		t.Fatal("lost bookmark", err)
	}
	root, err := p.Discussions().Get(t.Context(), author.ID, post)
	if err != nil || root.Hidden || root.Removed || root.Body != "Retained public text" || root.HelpfulCount != 1 {
		t.Fatal("changed existing post", root, err)
	}
	if _, err := p.Discussions().Retry(t.Context(), author.ID, key, hash); err != nil {
		t.Fatal("lost retry identity", err)
	}
	if ok, err := p.Moderation().IsModerator(t.Context(), author.ID); err != nil || ok {
		t.Fatal("invented default moderator", ok, err)
	}
	var version int64
	if err := p.db.QueryRowContext(t.Context(), `SELECT review_version FROM discussion_posts WHERE post_id=?`, post).Scan(&version); err != nil || version != 0 {
		t.Fatal("invalid initial case", version, err)
	}
}

func TestMariaDBModerationDecisionRaceAndRollback(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	operator, err := Open(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "decision-author")
	reporter, _ := loginAccount(t, auth, "decision-reporter")
	mods := []accounts.Account{}
	for _, subject := range []string{"decision-mod-a", "decision-mod-b"} {
		who, _ := loginAccount(t, auth, subject)
		mods = append(mods, who)
		if _, err := operator.SetModerator(t.Context(), who.ID, "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	posts := discussions.New(p.Discussions(), discussionDetailFixture{savedDetail("DecisionRace")})
	root, _, err := posts.Create(t.Context(), author.ID, "ticketmaster:DecisionRace", "", "", "Public until a committed decision", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	s := moderation.New(p.Moderation())
	if _, _, err := s.Report(t.Context(), reporter.ID, root.ID, "spam", ""); err != nil {
		t.Fatal(err)
	}
	c, err := s.Case(t.Context(), mods[0].ID, root.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	review := moderation.Review{Action: "hide", Reason: "Reviewed concern", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}
	execSQL(t, f.admin, "REVOKE INSERT ON "+f.runtime.Database+".moderation_decisions FROM '"+f.runtime.User+"'@'%'")
	if _, err = s.Decide(t.Context(), mods[0].ID, root.ID, review); err == nil {
		t.Fatal("decision write did not fail")
	}
	public, err := posts.Thread(t.Context(), "", root.EventID, root.ID)
	if err != nil || public.Hidden {
		t.Fatal("failed transaction hid content", public, err)
	}
	reports, err := s.Reports(t.Context(), reporter.ID, nil)
	if err != nil || reports.Items[0].Outcome != nil {
		t.Fatal("failed transaction resolved report", reports, err)
	}
	execSQL(t, f.admin, "GRANT INSERT ON "+f.runtime.Database+".moderation_decisions TO '"+f.runtime.User+"'@'%'")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, who := range mods {
		wg.Go(func() { _, err := s.Decide(t.Context(), who.ID, root.ID, review); errs <- err })
	}
	wg.Wait()
	close(errs)
	committed, conflicted := 0, 0
	for err := range errs {
		if err == nil {
			committed++
		} else if errors.Is(err, moderation.ErrConflict) {
			conflicted++
		} else {
			t.Fatal(err)
		}
	}
	if committed != 1 || conflicted != 1 {
		t.Fatal("review race", committed, conflicted)
	}
	outcomes, err := s.Outcomes(t.Context(), author.ID, nil)
	if err != nil || len(outcomes.Items) != 1 || !outcomes.Items[0].CanAppeal {
		t.Fatal("author appeal eligibility", outcomes, err)
	}
	// Case snapshots expose real appeal flags, while reporter resolutions never do.
	if err = s.Appeal(t.Context(), author.ID, outcomes.Items[0].ID, "Please review again"); err != nil {
		t.Fatal(err)
	}
	c, err = s.Case(t.Context(), mods[0].ID, root.ID, nil)
	if err != nil || !c.Decisions[0].Appealed || c.Decisions[0].CanAppeal {
		t.Fatal("case appeal flags", c, err)
	}
	if _, err = s.Decide(t.Context(), mods[0].ID, root.ID, moderation.Review{Action: "restore", Reason: "Corrected decision", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	outcomes, err = s.Outcomes(t.Context(), author.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outcomes.Items {
		if o.CanAppeal {
			t.Fatal("outdated action offered appeal")
		}
	}
}

func TestMariaDBModerationPaginationRemovalAndConflicts(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	operator, err := Open(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "review-author")
	reporter, _ := loginAccount(t, auth, "review-reporter")
	other, _ := loginAccount(t, auth, "review-other")
	mod, _ := loginAccount(t, auth, "review-mod")
	for _, who := range []string{mod.ID, author.ID} {
		if _, err := operator.SetModerator(t.Context(), who, "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	posts := discussions.New(p.Discussions(), discussionDetailFixture{savedDetail("ReviewPages")})
	s := moderation.New(p.Moderation())
	roots := []discussions.Post{}
	for range 3 {
		root, _, err := posts.Create(t.Context(), author.ID, "ticketmaster:ReviewPages", "", "", "Reviewable contribution", accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
		if _, _, err = s.Report(t.Context(), reporter.ID, root.ID, "spam", "Private concern"); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".moderation_reports SET created_at='2026-10-07 12:00:00.000001'")
	values := url.Values{"limit": {"1"}}
	seen := map[string]bool{}
	firstCursor := ""
	for {
		list, err := s.Reports(t.Context(), reporter.ID, values)
		if err != nil || len(list.Items) != 1 || seen[list.Items[0].ID] {
			t.Fatal("report pagination", list, err)
		}
		seen[list.Items[0].ID] = true
		if list.NextCursor == nil {
			break
		}
		if firstCursor == "" {
			firstCursor = *list.NextCursor
		}
		values.Set("cursor", *list.NextCursor)
	}
	if len(seen) != 3 {
		t.Fatal("skipped reports", seen)
	}
	if _, err = s.Reports(t.Context(), other.ID, url.Values{"cursor": {firstCursor}}); err == nil {
		t.Fatal("cross-viewer cursor")
	}
	root := roots[0]
	if _, _, err = s.Report(t.Context(), other.ID, root.ID, "other", "Second concern"); err != nil {
		t.Fatal(err)
	}
	public, err := posts.Thread(t.Context(), "", root.EventID, root.ID)
	if err != nil || public.Hidden {
		t.Fatal("reports hid content", public, err)
	}
	c, err := s.Case(t.Context(), mod.ID, root.ID, url.Values{"limit": {"1"}})
	if err != nil || len(c.Reports) != 1 || c.NextReportsCursor == nil {
		t.Fatal("bounded case", c, err)
	}
	next, err := s.Case(t.Context(), mod.ID, root.ID, url.Values{"limit": {"1"}, "reports_cursor": {*c.NextReportsCursor}})
	if err != nil || len(next.Reports) != 1 || next.Reports[0].ID == c.Reports[0].ID || next.NextReportsCursor != nil {
		t.Fatal("case next page", next, err)
	}
	if _, err = s.Case(t.Context(), mod.ID, root.ID, url.Values{"appeals_cursor": {*c.NextReportsCursor}}); err == nil {
		t.Fatal("cross-history cursor")
	}
	review := moderation.Review{Action: "hide", Reason: "Spam", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}
	if _, err = s.Decide(t.Context(), author.ID, root.ID, review); !errors.Is(err, moderation.ErrForbidden) {
		t.Fatal("self moderation", err)
	}
	if _, err = posts.Edit(t.Context(), author.ID, root.ID, "Corrected text"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(t.Context(), mod.ID, root.ID, review); !errors.Is(err, moderation.ErrConflict) {
		t.Fatal("stale edited text", err)
	}
	c, err = s.Case(t.Context(), mod.ID, root.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	review.ExpectedVersion = &c.Version
	review.ExpectedUpdatedAt = c.Post.UpdatedAt
	if _, err = s.Decide(t.Context(), mod.ID, root.ID, review); err != nil {
		t.Fatal(err)
	}
	if err = posts.Remove(t.Context(), author.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	c, err = s.Case(t.Context(), mod.ID, root.ID, nil)
	if err != nil || c.CurrentBody != "" || c.Reports[0].ReportedBody == "" {
		t.Fatal("removed evidence", c, err)
	}
	review.Action = "restore"
	review.ExpectedVersion = &c.Version
	review.ExpectedUpdatedAt = c.Post.UpdatedAt
	if _, err = s.Decide(t.Context(), mod.ID, root.ID, review); err == nil {
		t.Fatal("resurrected owner removal")
	}
	if _, fresh, err := s.Report(t.Context(), reporter.ID, root.ID, "spam", "Retry after removal"); err != nil || fresh {
		t.Fatal("removed report retry", fresh, err)
	}
	public, err = posts.Thread(t.Context(), "", root.EventID, root.ID)
	if err != nil || !public.Removed || public.Hidden || public.Body != "" || public.Profile != nil {
		t.Fatal("removed projection", public, err)
	}
	// Hiding a reply suppresses target attribution without losing its children.
	root = roots[1]
	parent, _, err := posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Hidden target", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := posts.Create(t.Context(), other.ID, root.EventID, root.ID, parent.ID, "Retained child", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	c, err = s.Case(t.Context(), mod.ID, parent.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Decide(t.Context(), mod.ID, parent.ID, moderation.Review{Action: "hide", Reason: "Hide target", ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt}); err != nil {
		t.Fatal(err)
	}
	child, err = posts.Parent(t.Context(), "", root.EventID, root.ID, child.ID)
	if err != nil || child.ParentName != "Hidden contribution" {
		t.Fatal("hidden target identity", child, err)
	}
}
