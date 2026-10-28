package persistence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/kv"
)

func migrateBeforeLivePreferences(t *testing.T, f mariaFixture) {
	t.Helper()
	older := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() >= "00017" {
			continue
		}
		data, _ := migrations.ReadFile("migrations/" + entry.Name())
		older[entry.Name()] = &fstest.MapFile{Data: data}
	}
	if err = migrateFS(t.Context(), f.migration, older); err != nil {
		t.Fatal(err)
	}
}

func TestMariaDBLivePreferenceUpgrade(t *testing.T) {
	f := newMaria(t)
	migrateBeforeLivePreferences(t, f)
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := range 2 {
		execSQL(t, db, `INSERT INTO accounts(account_id,email,notifications_paused,email_reminders,weekly_digest) VALUES(?,?,?,?,?)`, fmt.Sprintf("%032d", i+1), fmt.Sprintf("fixture%d@example.test", i), i == 0, i == 0, i == 1)
	}
	// Seed real subscriptions, an existing reply envelope and an accepted email
	// receipt under v16. Compare every persisted field across the upgrade.
	oldPool := &Pool{db: db, config: f.migration}
	detail := savedDetail("LivePreferenceUpgrade")
	posts := discussions.New(oldPool.Discussions(), discussionDetailFixture{detail})
	root, _, err := posts.Create(t.Context(), fmt.Sprintf("%032d", 1), detail.Item.ID, "", "", "Upgrade discussion", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = oldPool.DiscussionFollows().Set(t.Context(), fmt.Sprintf("%032d", 2), root.EventID, root.ID, "daily"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = posts.Create(t.Context(), fmt.Sprintf("%032d", 1), root.EventID, root.ID, "", "Existing reply", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	// Seed historical rows directly: today's writers require schema 19's
	// membership revision and schema 20's reminder enrollment, absent here.
	execSQL(t, db, `INSERT INTO saved_events(account_id,event_id) VALUES (?,?)`, fmt.Sprintf("%032d", 1), detail.Item.ID)
	execSQL(t, db, `INSERT INTO event_reminders(reminder_id,account_id,event_id,lead_minutes) VALUES (?,?,?,60)`, accounts.ID(), fmt.Sprintf("%032d", 1), detail.Item.ID)
	execSQL(t, db, `INSERT INTO reminder_jobs(job_id,reminder_id,sale_at,scheduled_at,state,attempts,attempted_at,message_id) SELECT ?,reminder_id,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),'accepted',1,UTC_TIMESTAMP(6),'existing-receipt' FROM event_reminders`, accounts.ID())
	tables := []string{"discussion_follows", "discussion_notifications", "discussion_notification_posts", "event_reminders", "reminder_jobs", "reminder_dispatch"}
	dump := func(table string) string {
		t.Helper()
		columns := "*"
		if table == "event_reminders" {
			// Later additive columns (schema 20 enrolled_at) are not reset data.
			columns = "reminder_id,account_id,event_id,lead_minutes,active,state,sale_at,scheduled_at,next_check,lease_until,token,created_at"
		}
		rows, e := db.QueryContext(t.Context(), `SELECT `+columns+` FROM `+table+` ORDER BY 1`)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		cols, _ := rows.Columns()
		result := [][]any{}
		for rows.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if e = rows.Scan(dest...); e != nil {
				t.Fatal(e)
			}
			result = append(result, values)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		if len(result) == 0 {
			t.Fatal("empty preservation fixture", table)
		}
		data, _ := json.Marshal(result)
		return string(data)
	}
	before := map[string]string{}
	for _, table := range tables {
		before[table] = dump(table)
	}
	// Existing independent permission values must not be rewritten by the reset.
	f.migrate(t)
	for _, table := range tables {
		if before[table] != dump(table) {
			t.Fatal("migration changed independent delivery data", table)
		}
	}
	p := f.open(t)
	for i := range 2 {
		id := fmt.Sprintf("%032d", i+1)
		a, e := p.Accounts().Get(t.Context(), id)
		if e != nil || a.Preferences.NotificationsPaused || a.Preferences.EmailReminders != (i == 0) || a.Preferences.WeeklyDigest != (i == 1) {
			t.Fatal(a, e)
		}
		pref, e := p.Accounts().LivePreferences(t.Context(), id)
		if e != nil || !pref.Enabled || pref.Revision != 1 {
			t.Fatal(pref, e)
		}
		var provenance string
		if e = db.QueryRow(`SELECT notification_pause_source FROM accounts WHERE account_id=?`, id).Scan(&provenance); e != nil || provenance != "migration_policy" {
			t.Fatal(provenance, e)
		}
	}
	owner := fmt.Sprintf("%032d", 1)
	paused := true
	if _, err = p.Accounts().UpdatePreferences(t.Context(), owner, accounts.PreferencesPatch{NotificationsPaused: &paused}); err != nil {
		t.Fatal(err)
	}
	pref, _ := p.Accounts().LivePreferences(t.Context(), owner)
	if _, err = p.Accounts().ReplaceLivePreferences(t.Context(), owner, pref.Revision, false); err != nil {
		t.Fatal(err)
	}
	f.migrate(t)
	restarted := f.open(t)
	pref, err = restarted.Accounts().LivePreferences(t.Context(), owner)
	if err != nil || !pref.Paused || pref.Enabled || pref.Revision != 3 {
		t.Fatal("rerun reset explicit choices", pref, err)
	}
	auth := accounts.New(restarted.Accounts(), accountProviderFixture{})
	fresh, _ := loginAccount(t, auth, "new-default")
	pref, err = auth.LivePreferences(t.Context(), fresh.ID)
	if err != nil || pref.Paused || !pref.Enabled || fresh.Preferences.NotificationsPaused {
		t.Fatal("new account defaults", pref, err)
	}
}

func TestMariaDBLivePreferenceHTTPAndLeases(t *testing.T) {
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	otherPool := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, raw := loginAccount(t, auth, "live-http")
	_, otherRaw := loginAccount(t, auth, "live-http-other")
	principal, err := auth.Authenticate(t.Context(), raw, "session")
	if err != nil {
		t.Fatal(err)
	}
	service := activity.New(t.Context(), p.Activity(), 8)
	defer service.Close()
	server := httptest.NewUnstartedServer(nil)
	config := accounts.Config{Enabled: true, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
	app, err := api.NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), api.WithAccounts(config, auth), api.WithLiveActivity(service, 60))
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = app.Routes()
	server.Start()
	defer server.Close()
	request := func(method, path, token, body, match string) *http.Response {
		t.Helper()
		r, _ := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if token != "" {
			r.AddCookie(&http.Cookie{Name: config.CookieName("session"), Value: token})
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", principal.CSRF)
		if match != "" {
			r.Header.Set("If-Match", match)
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	check := func(res *http.Response, status int) string {
		t.Helper()
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != status {
			t.Fatalf("status %d want %d: %s", res.StatusCode, status, body)
		}
		if !strings.Contains(res.Header.Get("Cache-Control"), "private, no-store") {
			t.Fatal("cacheable private response")
		}
		return string(body)
	}
	route := "/api/v1/me/live-activity-preferences"
	check(request("GET", route, "", "", ""), 401)
	initial := request("GET", route, raw, "", "")
	etag := initial.Header.Get("ETag")
	check(initial, 200)
	data, _ := json.Marshal(activity.DefaultPreferences())
	body := string(data)
	check(request("PUT", route, raw, body, ""), 428)
	check(request("PUT", route, raw, body, `"stale"`), 412)
	noop := request("PUT", route, raw, body, etag)
	if noop.Header.Get("ETag") != etag {
		t.Fatal("no-op changed revision")
	}
	check(noop, 200)
	other := request("GET", route, otherRaw, "", "")
	otherETag := other.Header.Get("ETag")
	check(other, 200)
	check(request("PUT", route, raw, body, otherETag), 412)
	check(request("PUT", route, raw, strings.Replace(body, `"mode":"all"`, `"mode":"radius"`, 1), etag), 400)
	check(request("GET", "/api/v1/me/community/activity?preferences_revision=999", raw, "", ""), 409)
	check(request("GET", "/api/v1/me/community/activity?account_id="+owner.ID, otherRaw, "", ""), 400)
	check(request("GET", "/api/v1/me/community/activity", "", "", ""), 401)
	stream := request("GET", "/api/v1/me/community/activity/stream?preferences_revision=1", raw, "", "")
	if stream.StatusCode != 200 {
		t.Fatal(stream.Status)
	}
	if _, err = otherPool.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 1, false); err != nil {
		t.Fatal(err)
	}
	frames := make(chan string, 1)
	go func() { b, _ := io.ReadAll(stream.Body); frames <- string(b) }()
	select {
	case frame := <-frames:
		if !strings.Contains(frame, "settings.changed") || strings.Contains(frame, owner.ID) {
			t.Fatal(frame)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cross-replica settings did not close stream")
	}
	stream.Body.Close()
	check(request("PUT", route, raw, body, etag), 412)
	paused := check(request("GET", "/api/v1/me/community/activity", raw, "", ""), 200)
	if !strings.Contains(paused, `"state":"paused"`) {
		t.Fatal(paused)
	}
	if _, err = otherPool.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 2, true); err != nil {
		t.Fatal(err)
	}
	stream = request("GET", "/api/v1/me/community/activity/stream", raw, "", "")
	reader := bufio.NewScanner(stream.Body)
	for reader.Scan() {
		if strings.HasPrefix(reader.Text(), "data:") {
			break
		}
	}
	if err = auth.Revoke(t.Context(), owner.ID, principal.Credential.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan bool, 1)
	go func() { _, _ = io.Copy(io.Discard, stream.Body); done <- true }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("local revocation not immediate")
	}
	stream.Body.Close()
	check(request("GET", "/api/v1/me/community/activity", raw, "", ""), 401)
	for _, mode := range []string{"remote revocation", "known expiry"} {
		account, token := loginAccount(t, auth, mode)
		credential, e := auth.Authenticate(t.Context(), token, "session")
		if e != nil {
			t.Fatal(e)
		}
		if mode == "known expiry" {
			execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.account_credentials SET expires_at=UTC_TIMESTAMP(6)+INTERVAL 3 SECOND WHERE credential_id=?`, credential.Credential.ID)
		}
		response := request("GET", "/api/v1/me/community/activity/stream", token, "", "")
		if response.StatusCode != 200 {
			t.Fatal(mode, response.Status)
		}
		if mode == "remote revocation" {
			if e = otherPool.Accounts().Revoke(t.Context(), account.ID, credential.Credential.ID); e != nil {
				t.Fatal(e)
			}
		}
		closed := make(chan string, 1)
		go func() { data, _ := io.ReadAll(response.Body); closed <- string(data) }()
		select {
		case data := <-closed:
			if !strings.Contains(data, "event: unavailable") {
				t.Fatal(mode, data)
			}
		case <-time.After(5 * time.Second):
			t.Fatal(mode, "did not close stream")
		}
		response.Body.Close()
		check(request("GET", "/api/v1/me/community/activity", token, "", ""), 401)
	}
}
