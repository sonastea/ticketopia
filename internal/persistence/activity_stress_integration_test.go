package persistence

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// stallListener models frozen tabs: while stalled, writes block until the
// handler's write deadline, as with a peer that stopped reading.
type stallListener struct {
	net.Listener
	stalled *atomic.Bool
}

func (l stallListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &stallConn{Conn: c, stalled: l.stalled}, nil
}

type stallConn struct {
	net.Conn
	stalled  *atomic.Bool
	mu       sync.Mutex
	deadline time.Time
}

func (c *stallConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return c.Conn.SetDeadline(t)
}
func (c *stallConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(t)
}
func (c *stallConn) Write(p []byte) (int, error) {
	for c.stalled.Load() {
		c.mu.Lock()
		deadline := c.deadline
		c.mu.Unlock()
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return 0, os.ErrDeadlineExceeded
		}
		time.Sleep(5 * time.Millisecond)
	}
	return c.Conn.Write(p)
}

// Ground truth uses database-clock bounds around each committed change. A
// projection's read view starts after its as_of, so a change committed before
// as_of must be reflected; any frame received five seconds after it must too.
type stressState struct {
	value         string
	before, after time.Time // after is zero while the write is in flight
}
type stressTruth struct {
	mu        sync.Mutex
	owner     map[string]string         // event -> sole contributor
	names     map[string][]stressState  // contributor -> first-name states
	versions  map[string][]stressState  // event -> "title|city" states
	expiries  map[string][]stressState  // event -> shortened expiry (RFC3339Nano)
	withdrawn map[string]time.Time      // event -> withdrawal committed
	interests map[string][][2]time.Time // event -> recipient [added before, removed after]
	live      []string                  // published, not withdrawn
	violation []string
}

const stressPrompt = 5 * time.Second

func (s *stressTruth) fail(format string, args ...any) {
	if len(s.violation) < 40 {
		s.violation = append(s.violation, fmt.Sprintf(format, args...))
	}
}

// allowed reports whether a displayed state is no older than the newest state
// committed by threshold, and was not written only after the frame's snapshot.
func allowed(states []stressState, shown string, asOf, threshold time.Time) bool {
	required := 0
	for i, state := range states {
		if !state.after.IsZero() && !state.after.After(threshold) {
			required = i
		}
	}
	for _, state := range states[required:] {
		if state.value == shown && !state.before.After(asOf.Add(time.Second)) {
			return true
		}
	}
	return false
}

func (s *stressTruth) check(client string, private, berlin bool, snapshot activity.Snapshot, received time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	threshold := snapshot.AsOf
	if prompt := received.Add(-stressPrompt); prompt.After(threshold) {
		threshold = prompt
	}
	if len(snapshot.Items) > 20 {
		s.fail("%s: %d items", client, len(snapshot.Items))
	}
	for _, item := range snapshot.Items {
		id := item.EventID
		owner, ok := s.owner[id]
		if !ok {
			s.fail("%s: unknown event %s", client, id)
			continue
		}
		if !item.ExpiresAt.After(snapshot.AsOf) {
			s.fail("%s: %s expired within its own generation", client, id)
		}
		// Receipt is converted to database time; allow delivery jitter only.
		if !received.Before(item.ExpiresAt.Add(250 * time.Millisecond)) {
			s.fail("%s: %s delivered %s after expiry", client, id, received.Sub(item.ExpiresAt))
		}
		if expiries := s.expiries[id]; len(expiries) > 0 {
			for _, e := range expiries {
				at, _ := time.Parse(time.RFC3339Nano, e.value)
				if !e.after.IsZero() && !e.after.After(threshold) && item.ExpiresAt.After(at) {
					s.fail("%s: %s kept pre-expiry %s", client, id, item.ExpiresAt)
				}
			}
		}
		if at, ok := s.withdrawn[id]; ok && !at.After(threshold) {
			s.fail("%s: withdrawn %s still visible (as_of %s, withdrawn %s)", client, id, snapshot.AsOf, at)
		}
		name := ""
		if item.ActorFirstName != nil {
			name = *item.ActorFirstName
		}
		if !allowed(s.names[owner], name, snapshot.AsOf, threshold) {
			s.fail("%s: %s attributed to stale or unknown name %q", client, id, name)
		}
		version := item.EventTitle + "|" + item.City
		if !allowed(s.versions[id], version, snapshot.AsOf, threshold) {
			s.fail("%s: %s shows stale metadata %q", client, id, version)
		}
		if berlin && item.City != "Berlin" {
			// The city fact itself was checked above; it must match the filter.
			s.fail("%s: %s outside Berlin filter (%s)", client, id, item.City)
		}
		if private {
			// Membership is read after the candidate generation, so it may be
			// newer than as_of; it can never precede the relationship's write.
			member := false
			for _, interval := range s.interests[id] {
				if !interval[0].After(received) && (interval[1].IsZero() || received.Before(interval[1].Add(stressPrompt))) {
					member = true
				}
			}
			if !member {
				s.fail("%s: %s outside recipient membership (as_of %s, received %s, intervals %v)", client, id,
					snapshot.AsOf.Format("15:04:05.000"), received.Format("15:04:05.000"), s.interests[id])
			}
		}
	}
}

type stressClient struct {
	label           string
	base, path      string
	token           string
	private, berlin bool
	stalled         *atomic.Bool
	mu              sync.Mutex
	latest          *activity.Snapshot
	frames, status  int
	unavailable     []time.Time
	refused         []time.Time
	ended           []time.Time
	session         context.CancelFunc
}

func (c *stressClient) record(list *[]time.Time, at time.Time) {
	c.mu.Lock()
	*list = append(*list, at)
	c.mu.Unlock()
}
func (c *stressClient) count(list *[]time.Time, after time.Time) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, at := range *list {
		if at.After(after) {
			n++
		}
	}
	return n
}

func TestMariaDBLiveActivityResilienceStress(t *testing.T) {
	if testing.Short() {
		t.Skip("stress journey")
	}
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	churn := 5 * time.Second
	if v := os.Getenv("LIVE_ACTIVITY_STRESS_SECONDS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatal("LIVE_ACTIVITY_STRESS_SECONDS must be a positive integer")
		}
		churn = time.Duration(n) * time.Second
	}
	pa, pb := f.open(t), f.open(t)
	authA := accounts.New(pa.Accounts(), accountProviderFixture{})
	authB := accounts.New(pb.Accounts(), accountProviderFixture{})
	dbNow := func() time.Time {
		var at time.Time
		if err := f.admin.QueryRowContext(t.Context(), "SELECT UTC_TIMESTAMP(6)").Scan(&at); err != nil {
			t.Error(err)
		}
		return at
	}
	// Map receipt times to the database clock (same host; RTT midpoint).
	started := time.Now()
	db := dbNow()
	offset := db.Sub(started.Add(time.Since(started) / 2))
	clock := func() time.Time { return time.Now().Add(offset).UTC() }

	truth := &stressTruth{owner: map[string]string{}, names: map[string][]stressState{}, versions: map[string][]stressState{},
		expiries: map[string][]stressState{}, withdrawn: map[string]time.Time{}, interests: map[string][][2]time.Time{}}
	contributors := []accounts.Account{}
	secrets := []string{}
	for i := range 4 {
		account, raw := loginAccount(t, authA, fmt.Sprintf("stress-contributor-%d", i))
		contributors = append(contributors, account)
		secrets = append(secrets, account.ID, account.Email, raw)
		name := []string{"Ava", "Ben", "Cyd", "Dee"}[i]
		if _, err := authA.UpdateProfile(t.Context(), account.ID, accounts.ProfilePatch{ActivityFirstName: &name}); err != nil {
			t.Fatal(err)
		}
		truth.names[account.ID] = []stressState{{value: name, after: dbNow()}}
	}
	recipient, _ := loginAccount(t, authA, "stress-recipient")
	_, recipientToken, err := authA.CreateToken(t.Context(), recipient.ID, "stress")
	if err != nil {
		t.Fatal(err)
	}
	privateFilter := activity.DefaultPreferences().Filters
	privateFilter.Connections = activity.Connections{Mode: "any", Sources: []string{"interested"}}
	if _, err := pa.Accounts().ReplaceLivePreferences(t.Context(), recipient.ID, 1, true, privateFilter); err != nil {
		t.Fatal(err)
	}
	corrector, _ := loginAccount(t, authA, "stress-corrector")
	secrets = append(secrets, recipient.ID, recipient.Email, recipientToken, corrector.ID)

	writer := pa.Recommendations()
	writer.EnableLiveActivity(true)
	var gate sync.RWMutex // writes pause during the SQL outage
	var serial atomic.Int64
	detailFor := func(id string, version int, city string) models.EventDetail {
		d := savedDetail(strings.TrimPrefix(id, "ticketmaster:"))
		d.Item.Name = fmt.Sprintf("Stress %s v%d", strings.TrimPrefix(id, "ticketmaster:"), version)
		country := map[string]string{"Berlin": "DE", "London": "GB"}[city]
		d.Item.Venues = []models.Venue{{Place: models.Place{City: city, CountryCode: country}}}
		return d
	}
	versionCount := map[string]int{}
	publish := func() {
		n := serial.Add(1)
		id := fmt.Sprintf("ticketmaster:Stress_%d", n)
		city := []string{"London", "Berlin"}[n%2]
		owner := contributors[n%4].ID
		d := detailFor(id, 0, city)
		truth.mu.Lock()
		truth.owner[id] = owner
		truth.versions[id] = []stressState{{value: d.Item.Name + "|" + city, before: dbNow()}}
		truth.mu.Unlock()
		if _, _, err := writer.Set(t.Context(), owner, d, ""); err != nil {
			t.Error("publish", err)
			return
		}
		after := dbNow()
		truth.mu.Lock()
		truth.versions[id][0].after = after
		truth.live = append(truth.live, id)
		truth.mu.Unlock()
	}
	pick := func() string {
		truth.mu.Lock()
		defer truth.mu.Unlock()
		if len(truth.live) == 0 {
			return ""
		}
		return truth.live[rand.IntN(len(truth.live))]
	}
	for range 40 {
		publish()
	}

	// Two replicas with separate pools; a third front end shares replica A's
	// service through stallable connections.
	sa := activity.New(t.Context(), pa.Activity(), 64)
	defer sa.Close()
	sb := activity.New(t.Context(), pb.Activity(), 64)
	defer sb.Close()
	var stalled atomic.Bool
	serve := func(auth *accounts.Service, service *activity.Service, stall bool) string {
		server := httptest.NewUnstartedServer(nil)
		if stall {
			server.Listener = stallListener{server.Listener, &stalled}
		}
		config := accounts.Config{Enabled: true, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
		app, err := api.NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), api.WithAccounts(config, auth), api.WithLiveActivity(service, 1<<20))
		if err != nil {
			t.Fatal(err)
		}
		server.Config.Handler = app.Routes()
		server.Start()
		t.Cleanup(server.Close)
		return server.URL
	}
	baseA, baseB, baseStall := serve(authA, sa, false), serve(authB, sb, false), serve(authA, sa, true)
	berlin := activity.DefaultPreferences().Filters
	berlin.Location = activity.Location{Mode: "city", City: "Berlin", Country: "DE"}
	berlinQuery := "?filter=" + url.QueryEscape(`{"location":{"mode":"city","city":"Berlin","country":"DE"}}`)

	httpClient := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 64}}
	t.Cleanup(httpClient.CloseIdleConnections)
	get := func(ctx context.Context, base, path, token string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		return httpClient.Do(req)
	}
	leak := func(label string, data []byte) {
		for _, secret := range secrets {
			if strings.Contains(string(data), secret) {
				truth.mu.Lock()
				truth.fail("%s: frame leaked account data", label)
				truth.mu.Unlock()
			}
		}
	}
	ctx, stop := context.WithCancel(t.Context())
	var running sync.WaitGroup
	defer func() { stop(); running.Wait() }()
	stream := func(c *stressClient) {
		for ctx.Err() == nil {
			for c.stalled != nil && c.stalled.Load() && ctx.Err() == nil {
				time.Sleep(20 * time.Millisecond) // a frozen tab does not reconnect
			}
			session, cancel := context.WithCancel(ctx)
			c.mu.Lock()
			c.session = cancel
			c.mu.Unlock()
			resp, err := get(session, c.base, c.path, c.token)
			if err == nil {
				c.mu.Lock()
				c.status = resp.StatusCode
				c.mu.Unlock()
			}
			if err == nil && resp.StatusCode != 200 {
				resp.Body.Close()
				c.record(&c.refused, clock())
				err = fmt.Errorf("status %d", resp.StatusCode)
			}
			if err != nil {
				cancel()
				time.Sleep(100 * time.Millisecond)
				continue
			}
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 4096), 1<<20)
			event := ""
			for scanner.Scan() {
				line := scanner.Text()
				if value, ok := strings.CutPrefix(line, "event: "); ok {
					event = value
				}
				data, ok := strings.CutPrefix(line, "data: ")
				if !ok {
					continue
				}
				received := clock()
				leak(c.label, []byte(data))
				switch event {
				case "unavailable":
					c.record(&c.unavailable, received)
					c.mu.Lock()
					c.latest = nil
					c.mu.Unlock()
				case "activity.snapshot":
					var snapshot activity.Snapshot
					if err := json.Unmarshal([]byte(data), &snapshot); err != nil {
						t.Error(c.label, err)
						continue
					}
					truth.check(c.label, c.private, c.berlin, snapshot, received)
					c.mu.Lock()
					c.latest, c.frames = &snapshot, c.frames+1
					c.mu.Unlock()
				}
			}
			resp.Body.Close()
			cancel()
			c.record(&c.ended, clock())
			c.mu.Lock()
			c.latest = nil
			c.mu.Unlock()
			time.Sleep(100 * time.Millisecond) // quiet reconnect
		}
	}
	// Fallback readers poll the coalesced snapshot route instead of streaming.
	fallback := func(c *stressClient) {
		for ctx.Err() == nil {
			resp, err := get(ctx, c.base, c.path, c.token)
			if err == nil {
				data, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				received := clock()
				leak(c.label, data)
				c.mu.Lock()
				c.status = resp.StatusCode
				c.mu.Unlock()
				if resp.StatusCode != 200 {
					c.record(&c.refused, received)
				} else {
					var snapshot activity.Snapshot
					if err := json.Unmarshal(data, &snapshot); err != nil {
						t.Error(c.label, err)
					} else {
						truth.check(c.label, c.private, c.berlin, snapshot, received)
						c.mu.Lock()
						c.latest, c.frames = &snapshot, c.frames+1
						c.mu.Unlock()
					}
				}
			}
			time.Sleep(400 * time.Millisecond)
		}
	}
	var streams, fallbacks, stallClients []*stressClient
	for _, replica := range []struct {
		label, base string
	}{{"A", baseA}, {"B", baseB}} {
		for i := range 4 {
			streams = append(streams, &stressClient{label: fmt.Sprintf("%s-public-%d", replica.label, i), base: replica.base, path: "/api/v1/community/activity/stream"})
		}
		for i := range 3 {
			streams = append(streams, &stressClient{label: fmt.Sprintf("%s-berlin-%d", replica.label, i), base: replica.base, path: "/api/v1/community/activity/stream" + berlinQuery, berlin: true})
		}
		for i := range 3 {
			streams = append(streams, &stressClient{label: fmt.Sprintf("%s-private-%d", replica.label, i), base: replica.base, path: "/api/v1/me/community/activity/stream", token: recipientToken, private: true})
		}
		fallbacks = append(fallbacks,
			&stressClient{label: replica.label + "-fallback-public", base: replica.base, path: "/api/v1/community/activity"},
			&stressClient{label: replica.label + "-fallback-berlin", base: replica.base, path: "/api/v1/community/activity" + berlinQuery, berlin: true},
			&stressClient{label: replica.label + "-fallback-private", base: replica.base, path: "/api/v1/me/community/activity", token: recipientToken, private: true})
	}
	for i := range 4 {
		stallClients = append(stallClients, &stressClient{label: fmt.Sprintf("stall-%d", i), base: baseStall, path: "/api/v1/community/activity/stream", stalled: &stalled})
	}
	for _, c := range append(append([]*stressClient{}, streams...), stallClients...) {
		running.Go(func() { stream(c) })
	}
	for _, c := range fallbacks {
		running.Go(func() { fallback(c) })
	}
	all := append(append(append([]*stressClient{}, streams...), stallClients...), fallbacks...)
	// waitAll waits until each listed client has had a current view satisfying
	// ok. Private views on the mutating replica are closed for a quiet reconnect
	// by design, so convergence is latched per client; every later frame is
	// still checked against the withdrawal/redaction/membership invariants.
	waitAll := func(label string, clients []*stressClient, timeout time.Duration, ok func(*stressClient, *activity.Snapshot) bool) {
		t.Helper()
		started := clock()
		deadline := time.Now().Add(timeout)
		converged := map[*stressClient]bool{}
		for {
			pending := []string{}
			for _, c := range clients {
				c.mu.Lock()
				latest, status := c.latest, c.status
				c.mu.Unlock()
				if !converged[c] && latest != nil && ok(c, latest) {
					converged[c] = true
				}
				if !converged[c] {
					pending = append(pending, fmt.Sprintf("%s(status %d, unavailable %d, refused %d, ended %d)", c.label, status,
						c.count(&c.unavailable, started), c.count(&c.refused, started), c.count(&c.ended, started)))
				}
			}
			if len(pending) == 0 {
				return
			}
			if time.Now().After(deadline) {
				a, b := sa.Stats(), sb.Stats()
				t.Fatalf("%s: %d clients did not converge within %s: %v\nreplica A failures=%v\nreplica B failures=%v", label, len(pending), timeout, pending, a.Failures, b.Failures)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	ready := func(*stressClient, *activity.Snapshot) bool { return true }
	waitAll("initial views", all, 10*time.Second, ready)
	healthyFrom := clock()

	// Continuous mutations: publications, approaching expiry, first-name
	// redaction/renaming, metadata and city corrections, and private interest.
	var mutators sync.WaitGroup
	mutating, stopMutating := context.WithCancel(ctx)
	every := func(interval time.Duration, op func()) {
		mutators.Go(func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-mutating.Done():
					return
				case <-ticker.C:
					gate.RLock()
					op()
					gate.RUnlock()
				}
			}
		})
	}
	every(300*time.Millisecond, publish)
	every(350*time.Millisecond, func() {
		id := pick()
		if id == "" {
			return
		}
		ms := 400 + rand.IntN(2500)
		var at time.Time
		if err := f.admin.QueryRowContext(t.Context(), "SELECT UTC_TIMESTAMP(6)+INTERVAL ? MICROSECOND", ms*1000).Scan(&at); err != nil {
			t.Error(err)
			return
		}
		if _, err := f.admin.ExecContext(t.Context(), "UPDATE "+f.runtime.Database+".live_recommendation_activity SET expires_at=LEAST(expires_at,?) WHERE event_id=?", at, id); err != nil {
			t.Error("expiry", err)
			return
		}
		after := dbNow()
		truth.mu.Lock()
		truth.expiries[id] = append(truth.expiries[id], stressState{value: at.Format(time.RFC3339Nano), after: after})
		truth.live = slices.DeleteFunc(truth.live, func(v string) bool { return v == id })
		truth.mu.Unlock()
	})
	renamed := 0
	every(250*time.Millisecond, func() {
		renamed++
		account := contributors[renamed%4]
		name := ""
		if renamed%3 != 0 {
			// Letters only: encode a unique generation without digits.
			name = []string{"Ava", "Ben", "Cyd", "Dee"}[renamed%4] + " " + strings.Map(func(r rune) rune { return 'a' + (r - '0') }, strconv.Itoa(renamed))
		}
		truth.mu.Lock()
		truth.names[account.ID] = append(truth.names[account.ID], stressState{value: name, before: dbNow()})
		truth.mu.Unlock()
		if _, err := authA.UpdateProfile(t.Context(), account.ID, accounts.ProfilePatch{ActivityFirstName: &name}); err != nil {
			t.Error("rename", err)
			return
		}
		after := dbNow()
		truth.mu.Lock()
		states := truth.names[account.ID]
		states[len(states)-1].after = after
		truth.mu.Unlock()
	})
	every(300*time.Millisecond, func() {
		id := pick()
		if id == "" {
			return
		}
		versionCount[id]++
		city := []string{"London", "Berlin"}[rand.IntN(2)]
		d := detailFor(id, versionCount[id], city)
		d.Meta.DataAsOf = time.Now().UTC()
		truth.mu.Lock()
		truth.versions[id] = append(truth.versions[id], stressState{value: d.Item.Name + "|" + city, before: dbNow()})
		truth.mu.Unlock()
		if _, _, err := pa.Saved().Save(t.Context(), corrector.ID, d); err != nil {
			t.Error("correction", err)
			return
		}
		after := dbNow()
		truth.mu.Lock()
		states := truth.versions[id]
		states[len(states)-1].after = after
		truth.mu.Unlock()
	})
	interested := map[string]bool{}
	every(400*time.Millisecond, func() {
		id := pick()
		if id == "" || interested[id] {
			return
		}
		interested[id] = true
		truth.mu.Lock()
		truth.interests[id] = append(truth.interests[id], [2]time.Time{dbNow()})
		truth.mu.Unlock()
		if _, _, err := pa.Interests().Set(t.Context(), recipient.ID, savedDetail(strings.TrimPrefix(id, "ticketmaster:")), "private"); err != nil {
			t.Error("interest", err)
		}
	})
	time.Sleep(2 * time.Second)

	// Frozen readers must release their subscriptions through the write
	// deadline without delaying other recipients on the same replica.
	stallStart := clock()
	stalled.Store(true)
	deadline := time.Now().Add(10 * time.Second)
	for {
		released := 0
		for _, c := range stallClients {
			released += c.count(&c.ended, stallStart)
		}
		if released >= len(stallClients) && sa.Stats().Streams <= 10 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stalled streams not released: %d/%d ended, %d active", released, len(stallClients), sa.Stats().Streams)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("stalled readers released after %s", clock().Sub(stallStart))
	stalled.Store(false)

	// Mass withdrawal plus mass private-interest removal, concurrently.
	gate.RLock()
	truth.mu.Lock()
	victims := slices.Clone(truth.live[:min(15, len(truth.live))])
	truth.mu.Unlock()
	var mass sync.WaitGroup
	for _, id := range victims {
		truth.mu.Lock()
		owner := truth.owner[id]
		truth.mu.Unlock()
		mass.Go(func() {
			if err := writer.Remove(t.Context(), owner, id); err != nil {
				t.Error("withdraw", err)
				return
			}
			after := dbNow()
			truth.mu.Lock()
			truth.withdrawn[id] = after
			truth.live = slices.DeleteFunc(truth.live, func(v string) bool { return v == id })
			truth.mu.Unlock()
		})
	}
	truth.mu.Lock()
	removals := []string{}
	for id, intervals := range truth.interests {
		if intervals[len(intervals)-1][1].IsZero() {
			removals = append(removals, id)
		}
	}
	truth.mu.Unlock()
	for _, id := range removals {
		mass.Go(func() {
			if err := pa.Interests().Remove(t.Context(), recipient.ID, id); err != nil {
				t.Error("interest removal", err)
				return
			}
			after := dbNow()
			truth.mu.Lock()
			intervals := truth.interests[id]
			intervals[len(intervals)-1][1] = after
			truth.mu.Unlock()
		})
	}
	mass.Wait()
	gate.RUnlock()
	massAt := clock()
	waitAll("mass withdrawal", all, stressPrompt+time.Second, func(c *stressClient, s *activity.Snapshot) bool {
		for _, item := range s.Items {
			if slices.Contains(victims, item.EventID) {
				return false
			}
		}
		return true
	})
	t.Logf("mass withdrawal of %d groups and %d private interests converged after %s", len(victims), len(removals), clock().Sub(massAt))

	// Reconnect burst: every stream reconnects at once and recovers.
	burstAt := clock()
	for _, c := range append(append([]*stressClient{}, streams...), stallClients...) {
		c.mu.Lock()
		cancel := c.session
		c.latest = nil
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	waitAll("reconnect burst", all, stressPrompt, ready)
	t.Logf("reconnect burst of %d streams recovered after %s", len(streams)+len(stallClients), clock().Sub(burstAt))

	// SQL outage for this isolated fixture only: every view becomes unavailable,
	// and recovery rebuilds rather than reusing the failed generation.
	gate.Lock()
	kill := func(marker uint64) {
		rows, err := f.admin.QueryContext(t.Context(), "SELECT ID FROM information_schema.PROCESSLIST WHERE USER=? AND ID<=?", f.runtime.User, marker)
		if err != nil {
			t.Fatal(err)
		}
		ids := []uint64{}
		for rows.Next() {
			var id uint64
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			_, _ = f.admin.ExecContext(t.Context(), fmt.Sprintf("KILL CONNECTION %d", id))
		}
	}
	// MariaDB caches database grants per session. Connection IDs are monotonic,
	// so kill only sessions at or below a marker taken after the grant change.
	disconnect := func() {
		var marker uint64
		if err := f.admin.QueryRowContext(t.Context(), "SELECT MAX(ID) FROM information_schema.PROCESSLIST").Scan(&marker); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			kill(marker)
			time.Sleep(300 * time.Millisecond) // catch handshakes straddling the change
		}
	}
	outageAt := dbNow()
	execSQL(t, f.admin, fmt.Sprintf("REVOKE SELECT ON %s.* FROM '%s'@'%%'", f.runtime.Database, f.runtime.User))
	disconnect()
	deadline = time.Now().Add(stressPrompt + time.Second)
	for {
		pending := []string{}
		for _, c := range all {
			if c.count(&c.unavailable, outageAt)+c.count(&c.refused, outageAt)+c.count(&c.ended, outageAt) == 0 {
				pending = append(pending, c.label)
			}
		}
		if len(pending) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("outage not surfaced to %v", pending)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, c := range all {
		c.mu.Lock()
		c.latest = nil
		c.mu.Unlock()
	}
	execSQL(t, f.admin, fmt.Sprintf("GRANT SELECT ON %s.* TO '%s'@'%%'", f.runtime.Database, f.runtime.User))
	disconnect()
	restoredAt := dbNow()
	gate.Unlock()
	waitAll("SQL recovery", all, stressPrompt+3*time.Second, func(c *stressClient, s *activity.Snapshot) bool {
		return s.AsOf.After(restoredAt)
	})
	t.Logf("SQL outage surfaced to all %d clients and recovered after %s", len(all), clock().Sub(restoredAt))
	recoveredAt := clock()

	time.Sleep(churn)
	stopMutating()
	mutators.Wait()
	// Let shortened expiries pass, then compare every client with a fresh
	// complete-window reference from a separate pool.
	time.Sleep(3 * time.Second)
	reference := activity.New(t.Context(), f.open(t).Activity(), 4)
	defer reference.Close()
	public, err := reference.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	berlinView, err := reference.FilteredSnapshot(t.Context(), berlin)
	if err != nil {
		t.Fatal(err)
	}
	privateView, err := reference.OwnerSnapshot(t.Context(), recipient.ID, privateFilter)
	if err != nil {
		t.Fatal(err)
	}
	content := func(s *activity.Snapshot) string { data, _ := json.Marshal(s.Items); return string(data) }
	waitAll("final convergence", all, stressPrompt, func(c *stressClient, s *activity.Snapshot) bool {
		want := &public
		if c.berlin {
			want = &berlinView
		} else if c.private {
			want = &privateView
		}
		return content(s) == content(want)
	})

	// A healthy, continuously changing window must not interrupt public views:
	// only the SQL outage may surface unavailable/refused public reads.
	for _, c := range all {
		if c.private || c.stalled != nil {
			continue
		}
		c.mu.Lock()
		unexpected := []string{}
		for kind, list := range map[string][]time.Time{"unavailable": c.unavailable, "refused": c.refused} {
			for _, at := range list {
				if (at.After(healthyFrom) && at.Before(outageAt)) || at.After(recoveredAt) {
					unexpected = append(unexpected, fmt.Sprintf("%s at outage%+v/recovered%+v", kind, at.Sub(outageAt).Round(time.Millisecond), at.Sub(recoveredAt).Round(time.Millisecond)))
				}
			}
		}
		c.mu.Unlock()
		if len(unexpected) > 0 {
			t.Errorf("%s: healthy-period interruptions: %v", c.label, unexpected)
		}
	}
	truth.mu.Lock()
	defer truth.mu.Unlock()
	frames := 0
	for _, c := range all {
		c.mu.Lock()
		frames += c.frames
		c.mu.Unlock()
	}
	expired := 0
	for _, e := range truth.expiries {
		expired += len(e)
	}
	t.Logf("checked %d frames/snapshots: %d publications, %d expiries, %d renames, %d withdrawals", frames, serial.Load(), expired, renamed, len(truth.withdrawn))
	for _, v := range truth.violation {
		t.Error(v)
	}
}
