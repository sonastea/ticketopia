package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/models"
)

// Capacity gates (rollout P1.1/P1.2) run three in-process replica services,
// each with its own pool, against one isolated real-MariaDB schema. They measure
// server-side sharing, bounds and latency; the deployment fixture separately
// covers the production image, proxy and process limits.

var capacityCities = []activity.Location{
	{Mode: "city", City: "London", Country: "GB"}, {Mode: "city", City: "Berlin", Country: "DE"},
	{Mode: "city", City: "Paris", Country: "FR"}, {Mode: "city", City: "Madrid", Country: "ES"},
	{Mode: "city", City: "Rome", Country: "IT"}, {Mode: "city", City: "Tokyo", Country: "JP"},
	{Mode: "city", City: "São Paulo", Country: "BR"}, {Mode: "city", City: "Toronto", Country: "CA"},
	{Mode: "city", City: "Chicago", Country: "US"}, {Mode: "city", City: "Sydney", Country: "AU"},
	{Mode: "city", City: "Seoul", Country: "KR"}, {Mode: "city", City: "Mexico City", Country: "MX"},
	{Mode: "city", City: "Amsterdam", Country: "NL"}, {Mode: "city", City: "Kraków", Country: "PL"},
	{Mode: "city", City: "Zürich", Country: "CH"}, {Mode: "city", City: "Montréal", Country: "CA"},
}
var capacityCategories = []string{"Music", "Sports", "Arts", "Film", "Family", "Comedy", "Theatre", "Miscellaneous"}

const capacityArtists, capacityVenues = 512, 256

func capacitySizes(t *testing.T, name string) []int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		t.Skip("set " + name + " (for example 100,1000,10000) to run this capacity gate")
	}
	sizes := []int{}
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n < 1 || n > activity.MaxCandidates {
			t.Fatal(name + " must list sizes in 1..10000")
		}
		sizes = append(sizes, n)
	}
	return sizes
}

func capacityArtist(k int) models.Artist {
	id := fmt.Sprintf("CapArtist_%d", k%capacityArtists)
	return models.Artist{ID: "ticketmaster:" + id, Name: "Capacity artist " + id, Source: models.Source{Provider: "ticketmaster", ID: id}}
}

func capacityVenue(k int, l activity.Location) models.Venue {
	id := fmt.Sprintf("CapVenue_%d", k%capacityVenues)
	return models.Venue{ID: "ticketmaster:" + id, Source: models.Source{Provider: "ticketmaster", ID: id}, Place: models.Place{Name: "Capacity venue " + id, City: l.City, CountryCode: l.Country}}
}

// capacityTypical selects LIVE_ACTIVITY_CAPACITY_PROFILE=typical: 1-3 artists
// and one long title in sixteen. The default rich profile has 1-12 artists and
// a quarter long titles; on 2026-10-20 its 10,000-group window exceeded the
// index accounting budget (a recorded failed gate, not a reason to lighten it).
var capacityTypical = os.Getenv("LIVE_ACTIVITY_CAPACITY_PROFILE") == "typical"

// capacityDetail varies title length, lineup size, venue count, city and category.
func capacityDetail(i int) models.EventDetail {
	d := savedDetail(fmt.Sprintf("LiveCap_%d", i))
	d.Meta.DataAsOf = time.Now().UTC()
	l := capacityCities[i%len(capacityCities)]
	d.Item.Name = fmt.Sprintf("Capacity fixture %d", i)
	long, lineup := i%4 == 0, 1+i%12
	if capacityTypical {
		long, lineup = i%16 == 0, 1+i%3
	}
	if long {
		d.Item.Name = strings.Repeat("A long multilingual event title — 音楽 · Ñandú · Привет · ", 6) + strconv.Itoa(i)
	}
	category := capacityCategories[(i/16)%len(capacityCategories)]
	d.Item.Classifications = []models.Classification{{Primary: true, Segment: &models.NamedID{ID: category, Name: category}}}
	d.Item.Artists = nil
	for k := range lineup {
		d.Item.Artists = append(d.Item.Artists, capacityArtist(i*31+k*97))
	}
	d.Item.Venues = []models.Venue{capacityVenue(i*7, l)}
	if i%3 == 0 {
		d.Item.Venues = append(d.Item.Venues, capacityVenue(i*11+5, l))
	}
	return d
}

// seedCapacityWindow publishes n groups through the production writer, then
// extends every contribution so the window is stable for the measurement. Soak
// tests exercise natural ten-minute expiry instead.
func seedCapacityWindow(t *testing.T, f mariaFixture, p *Pool, contributors []string, n int) {
	t.Helper()
	started := time.Now()
	w := p.Recommendations()
	w.EnableLiveActivity(true)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range jobs {
				if _, _, err := w.Set(t.Context(), contributors[i%len(contributors)], capacityDetail(i), ""); err != nil {
					t.Error("seed", err)
				}
			}
		})
	}
	for i := range n {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".live_recommendation_activity SET expires_at=UTC_TIMESTAMP(6)+INTERVAL 3 HOUR")
	profile := "rich (1-12 artists, 1 in 4 long titles)"
	if capacityTypical {
		profile = "typical (1-3 artists, 1 in 16 long titles)"
	}
	t.Logf("seeded %d %s groups, 1-2 venues, 16 cities, 8 categories in %s", n, profile, time.Since(started).Round(time.Millisecond))
}

// capacityFilter mixes Everywhere/city, zero-two categories, zero-three artists
// and an optional venue. Callers dedupe canonical forms.
func capacityFilter(i int) activity.Filters {
	f := activity.DefaultPreferences().Filters
	if c := i % 17; c > 0 {
		f.Location = capacityCities[c-1]
	}
	switch (i / 17) % 3 {
	case 1:
		f.CategoryIDs = []string{capacityCategories[(i/3+i)%8]}
	case 2:
		f.CategoryIDs = []string{capacityCategories[(i/3+i)%8], capacityCategories[(i+3)%8]}
	}
	for k := range (i / 7) % 4 {
		f.ArtistIDs = append(f.ArtistIDs, capacityArtist(i*13+k*41).ID)
	}
	if (i/5)%3 == 1 {
		f.VenueIDs = []string{capacityVenue(i*7, activity.Location{}).ID}
	}
	return f
}

func canonicalKey(t *testing.T, f activity.Filters) string {
	t.Helper()
	n, err := activity.NormalizeFilters(f)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(n)
	return string(data)
}

func distinctCapacityFilters(t *testing.T, start, count int, used map[string]bool) []activity.Filters {
	t.Helper()
	result := []activity.Filters{}
	for i := start; len(result) < count; i++ {
		f := capacityFilter(i)
		if key := canonicalKey(t, f); !used[key] {
			used[key] = true
			result = append(result, f)
		}
	}
	return result
}

// noncanonical spells the same filter differently: reversed and duplicated
// lists, irregular city whitespace/case and a lower-case country.
func noncanonical(f activity.Filters, spelling int) activity.Filters {
	v := f
	for _, ids := range []*[]string{&v.CategoryIDs, &v.ArtistIDs, &v.VenueIDs} {
		list := slices.Clone(*ids)
		slices.Reverse(list)
		if len(list) > 0 {
			list = append(list, list[len(list)-1])
		}
		*ids = list
	}
	if v.Location.Mode == "city" {
		v.Location.Country = strings.ToLower(v.Location.Country)
		v.Location.City = "  " + strings.ReplaceAll(v.Location.City, " ", "   ") + " "
		if spelling%2 == 1 {
			v.Location.City = strings.ToUpper(v.Location.City)
		}
	}
	return v
}

type capacityFrame struct {
	at       time.Time
	snapshot activity.Snapshot
	counts   bool
}

// capacityTab follows the browser contract: snapshot to coalesce cold work,
// subscribe, and quietly reconnect after an unavailable update.
type capacityTab struct {
	replica int
	owner   string
	filter  activity.Filters
	ids     []string
	mu      sync.Mutex
	latest  *activity.Snapshot
	frames  []capacityFrame
	opened  time.Time
	first   time.Time

	unavailable, refused int
}

func (c *capacityTab) record(u activity.Update, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	snapshot := u.Snapshot
	c.latest = &snapshot
	if c.first.IsZero() {
		c.first = at
	}
	if len(c.frames) < 4096 {
		c.frames = append(c.frames, capacityFrame{at, snapshot, u.Counts != nil})
	}
}

func (c *capacityTab) run(ctx context.Context, s *activity.Service) {
	for ctx.Err() == nil {
		if _, err := s.OwnerSnapshot(ctx, c.owner, c.filter); err != nil {
			c.mu.Lock()
			c.refused++
			c.mu.Unlock()
			sleepContext(ctx, 100*time.Millisecond)
			continue
		}
		updates, release, err := s.SubscribeOwnerContext(ctx, c.owner, c.ids, c.filter)
		if err != nil {
			c.mu.Lock()
			c.refused++
			c.mu.Unlock()
			sleepContext(ctx, 100*time.Millisecond)
			continue
		}
	reading:
		for {
			select {
			case <-ctx.Done():
				break reading
			case <-s.Done():
				break reading
			case u := <-updates:
				if u.Unavailable {
					c.mu.Lock()
					c.unavailable++
					c.latest = nil
					c.mu.Unlock()
					break reading
				}
				c.record(u, time.Now())
			}
		}
		release()
		sleepContext(ctx, 100*time.Millisecond)
	}
}

func sleepContext(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (c *capacityTab) mark() {
	c.mu.Lock()
	c.frames = c.frames[:0]
	c.unavailable, c.refused = 0, 0
	c.mu.Unlock()
}

// firstMatch returns the first frame at or after since that satisfies ok.
func (c *capacityTab) firstMatch(since time.Time, ok func(activity.Snapshot) bool) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, frame := range c.frames {
		if !frame.at.Before(since) && ok(frame.snapshot) {
			return frame.at, true
		}
	}
	return time.Time{}, false
}

type capacityGroup struct {
	services []*activity.Service
	tabs     []*capacityTab
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func (c *capacityTab) ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.first.IsZero()
}

// startTabs opens tabs with bounded concurrent handshakes (24 matches the
// deployment fixture); a start slot is held until the tab's first view.
func startTabs(ctx context.Context, services []*activity.Service, tabs []*capacityTab, concurrency int) *capacityGroup {
	ctx, cancel := context.WithCancel(ctx)
	g := &capacityGroup{services: services, tabs: tabs, cancel: cancel}
	starts := make(chan struct{}, concurrency)
	for _, tab := range tabs {
		g.wg.Go(func() {
			starts <- struct{}{}
			tab.mu.Lock()
			tab.opened = time.Now()
			tab.mu.Unlock()
			done := make(chan struct{})
			go func() {
				defer close(done)
				tab.run(ctx, services[tab.replica])
			}()
			for ctx.Err() == nil && !tab.ready() {
				time.Sleep(5 * time.Millisecond)
			}
			<-starts
			<-done
		})
	}
	return g
}

func (g *capacityGroup) stop() { g.cancel(); g.wg.Wait() }

func (g *capacityGroup) failures() []map[string]uint64 {
	result := []map[string]uint64{}
	for _, s := range g.services {
		result = append(result, s.Stats().Failures)
	}
	return result
}

func (g *capacityGroup) waitReady(t *testing.T, timeout time.Duration) []time.Duration {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		pending := 0
		for _, tab := range g.tabs {
			tab.mu.Lock()
			if tab.first.IsZero() {
				pending++
			}
			tab.mu.Unlock()
		}
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d/%d tabs had no view within %s; replica failures: %v", pending, len(g.tabs), timeout, g.failures())
		}
		time.Sleep(10 * time.Millisecond)
	}
	ready := []time.Duration{}
	for _, tab := range g.tabs {
		ready = append(ready, tab.first.Sub(tab.opened))
	}
	return ready
}

func durationPercentile(values []time.Duration, p int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	values = slices.Clone(values)
	slices.Sort(values)
	return values[(len(values)*p-1)/100]
}

// capacityMonitor samples every service's gauges throughout a scenario.
type capacityMonitor struct {
	mu                          sync.Mutex
	bytes, cohorts, owners      int64
	pending, jobs, streams      int
	heap                        uint64
	stop                        context.CancelFunc
	done                        chan struct{}
	samples                     int
	streamLimitHits, queueLimit uint64
}

func monitorCapacity(ctx context.Context, services []*activity.Service) *capacityMonitor {
	ctx, cancel := context.WithCancel(ctx)
	m := &capacityMonitor{stop: cancel, done: make(chan struct{})}
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		var mem runtime.MemStats
		for i := 0; ; i++ {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			m.mu.Lock()
			for _, s := range services {
				stats := s.Stats()
				m.bytes = max(m.bytes, stats.Bytes)
				m.cohorts = max(m.cohorts, stats.Cohorts)
				m.owners = max(m.owners, stats.Owners)
				m.pending = max(m.pending, stats.Pending)
				m.jobs = max(m.jobs, stats.SQLJobs)
				m.streams = max(m.streams, stats.Streams)
			}
			m.samples++
			m.mu.Unlock()
			if i%10 == 0 {
				runtime.ReadMemStats(&mem)
				m.mu.Lock()
				m.heap = max(m.heap, mem.HeapAlloc)
				m.mu.Unlock()
			}
		}
	}()
	return m
}

func (m *capacityMonitor) finish(t *testing.T) {
	t.Helper()
	m.stop()
	<-m.done
	m.mu.Lock()
	defer m.mu.Unlock()
	t.Logf("bounds over %d samples: max accounted=%.1f MiB cohorts=%d owners=%d pending=%d sql_jobs=%d streams=%d; test-process heap max=%.1f MiB (three replicas plus load generator)",
		m.samples, float64(m.bytes)/(1<<20), m.cohorts, m.owners, m.pending, m.jobs, m.streams, float64(m.heap)/(1<<20))
	if m.bytes > activity.MemoryBudget {
		t.Errorf("cache accounting %d exceeded %d", m.bytes, activity.MemoryBudget)
	}
	if m.cohorts > 384 || m.owners > 384 || m.pending > 64 || m.jobs > 2 || m.streams > 256 {
		t.Error("replica resource bound exceeded")
	}
}

type statsDelta struct {
	statements, projections, locations, cleanups, membership, hits, misses uint64
}

func deltaStats(before, after activity.Stats) statsDelta {
	op := func(name string) uint64 { return after.Operations[name].Count - before.Operations[name].Count }
	return statsDelta{after.SQLStatements - before.SQLStatements, op("projection"), op("location"), op("cleanup"), op("membership"),
		after.CacheHits - before.CacheHits, after.CacheMisses - before.CacheMisses}
}

func newCapacityReplicas(t *testing.T, f mariaFixture, n int) ([]*Pool, []*activity.Service) {
	t.Helper()
	pools := make([]*Pool, n)
	services := make([]*activity.Service, n)
	for i := range n {
		pools[i] = f.open(t)
	}
	for i := range n {
		services[i] = activity.New(t.Context(), pools[i].Activity(), 256)
		t.Cleanup(services[i].Close)
	}
	return pools, services
}

func itemsJSON(s activity.Snapshot) string {
	data, _ := json.Marshal(s.Items)
	return string(data)
}

func TestMariaDBActivityPublicFilterCapacity(t *testing.T) {
	for _, n := range capacitySizes(t, "LIVE_ACTIVITY_CAPACITY_GROUPS") {
		t.Run(fmt.Sprintf("groups=%d", n), func(t *testing.T) { publicFilterCapacity(t, n) })
	}
}

func publicFilterCapacity(t *testing.T, n int) {
	f := newMaria(t)
	f.migrate(t)
	seed := f.open(t)
	auth := accounts.New(seed.Accounts(), accountProviderFixture{})
	contributors := []string{}
	for i := range 8 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("capacity-contributor-%d", i))
		contributors = append(contributors, account.ID)
	}
	seedCapacityWindow(t, f, seed, contributors, n)
	_, services := newCapacityReplicas(t, f, 3)
	monitor := monitorCapacity(t.Context(), services)
	defer monitor.finish(t)

	const window = 12 * time.Second
	used := map[string]bool{}
	identical := capacityFilter(1) // London, no other dimensions: a broad public cohort
	identical.CategoryIDs = []string{"Music"}
	used[canonicalKey(t, identical)] = true
	distinct := distinctCapacityFilters(t, 0, 256, used)
	measure := func(label string, filterFor func(replica, tab int) activity.Filters) (statsDelta, *capacityGroup) {
		t.Helper()
		tabs := []*capacityTab{}
		for replica := range 3 {
			for tab := range 256 {
				tabs = append(tabs, &capacityTab{replica: replica, filter: filterFor(replica, tab)})
			}
		}
		g := startTabs(t.Context(), services, tabs, 24)
		ready := g.waitReady(t, 30*time.Second)
		// Let first-time location keys and cold cohorts settle, then measure a
		// steady window spanning about six shared projections.
		time.Sleep(3 * time.Second)
		for _, tab := range tabs {
			tab.mark()
		}
		before := make([]activity.Stats, 3)
		for i, s := range services {
			before[i] = s.Stats()
		}
		time.Sleep(window)
		var total statsDelta
		for i, s := range services {
			d := deltaStats(before[i], s.Stats())
			t.Logf("%s replica=%d statements=%d projections=%d location=%d cleanup=%d hits=%d misses=%d cohorts=%d accounted=%.1fMiB",
				label, i, d.statements, d.projections, d.locations, d.cleanups, d.hits, d.misses, s.Stats().Cohorts, float64(s.Stats().Bytes)/(1<<20))
			total.statements += d.statements
			total.projections += d.projections
			total.locations += d.locations
			total.cleanups += d.cleanups
			total.hits += d.hits
			total.misses += d.misses
		}
		interrupted := 0
		for _, tab := range tabs {
			tab.mu.Lock()
			interrupted += tab.unavailable + tab.refused
			tab.mu.Unlock()
		}
		if interrupted > 0 {
			t.Errorf("%s: %d healthy-window interruptions", label, interrupted)
		}
		t.Logf("%s: 768 tabs ready p95=%s max=%s", label, durationPercentile(ready, 95), durationPercentile(ready, 100))
		return total, g
	}
	perProjection := func(d statsDelta) float64 {
		return float64(d.statements-d.locations-d.cleanups) / float64(max(d.projections, 1))
	}

	same, g := measure("identical", func(int, int) activity.Filters { return identical })
	g.stop()
	spread, g := measure("distinct", func(_, tab int) activity.Filters { return distinct[tab] })
	t.Logf("projection SQL per generation: identical=%.2f distinct=%.2f (projections %d vs %d)", perProjection(same), perProjection(spread), same.projections, spread.projections)
	if perProjection(spread) > perProjection(same)+0.01 || spread.projections > same.projections+3 {
		t.Error("shared projection SQL multiplied with distinct filters")
	}
	if spread.locations > 0 {
		t.Error("warm location keys were reread", spread.locations)
	}
	if spread.misses > 0 {
		// An unchanged generation keeps every active view cached.
		t.Error("active distinct views were reselected without a generation change", spread.misses)
	}

	// Every distinct view matches a complete-window reference on a separate pool.
	reference := activity.New(t.Context(), f.open(t).Activity(), 4)
	defer reference.Close()
	mismatches := 0
	for _, tab := range g.tabs {
		want, err := reference.FilteredSnapshot(t.Context(), tab.filter)
		if err != nil {
			t.Fatal(err)
		}
		tab.mu.Lock()
		got := tab.latest
		tab.mu.Unlock()
		if got == nil || itemsJSON(*got) != itemsJSON(want) {
			mismatches++
		}
	}
	if mismatches > 0 {
		t.Errorf("%d distinct views differ from the reference", mismatches)
	}

	// Warm selection: cached active reads, then uncached selections of new
	// filters on the same warm generation (these also become extra cohorts).
	timed := func(s *activity.Service, filter activity.Filters) (time.Duration, bool) {
		before := s.Stats().Operations["projection"].Count
		started := time.Now()
		_, err := s.FilteredSnapshot(t.Context(), filter)
		elapsed := time.Since(started)
		if err != nil {
			t.Error("warm read", err)
		}
		return elapsed, s.Stats().Operations["projection"].Count == before
	}
	var hits, misses []time.Duration
	for range 3 {
		for _, filter := range distinct {
			for _, s := range services {
				if d, warm := timed(s, filter); warm {
					hits = append(hits, d)
				}
			}
		}
	}
	extra := distinctCapacityFilters(t, 1000, 512, used)
	for _, filter := range extra {
		for _, s := range services {
			if d, warm := timed(s, filter); warm {
				misses = append(misses, d)
			}
		}
	}
	selection := services[0].Stats().Operations["selection"]
	t.Logf("warm cached read p95=%s max=%s (n=%d); warm uncached selection p95=%s max=%s (n=%d); replica0 selection buckets=%v",
		durationPercentile(hits, 95), durationPercentile(hits, 100), len(hits), durationPercentile(misses, 95), durationPercentile(misses, 100), len(misses), selection.Buckets)
	if durationPercentile(hits, 95) > 10*time.Millisecond || durationPercentile(misses, 95) > 10*time.Millisecond {
		t.Error("warm selection p95 exceeded 10ms")
	}
	if len(hits) < 2000 || len(misses) < 1000 {
		t.Error("too few warm samples; projections overlapped the measurement", len(hits), len(misses))
	}
	// After 512 extra cohorts, active views remain cached and the total stays
	// within 256 active + 128 extra.
	for i, s := range services {
		stats := s.Stats()
		if stats.Cohorts > 384 {
			t.Errorf("replica %d retained %d cohorts", i, stats.Cohorts)
		}
		before := stats.CacheMisses
		for _, filter := range distinct {
			if _, err := s.FilteredSnapshot(t.Context(), filter); err != nil {
				t.Fatal(err)
			}
		}
		if after := s.Stats(); after.CacheMisses != before && after.Operations["projection"].Count == stats.Operations["projection"].Count {
			t.Errorf("replica %d evicted %d active views for extra cohorts", i, after.CacheMisses-before)
		}
	}
	g.stop()

	// Canonical reuse: reordered/duplicate/irregular spellings share one view.
	variants := distinctCapacityFilters(t, 3000, 128, used)
	s := services[0]
	for _, filter := range variants {
		before := s.Stats()
		want, err := s.FilteredSnapshot(t.Context(), filter)
		if err != nil {
			t.Fatal(err)
		}
		for spelling := range 3 {
			got, err := s.FilteredSnapshot(t.Context(), noncanonical(filter, spelling))
			if err != nil {
				t.Fatal(err)
			}
			if data, _ := json.Marshal(got); string(data) != func() string { data, _ := json.Marshal(want); return string(data) }() {
				t.Fatal("noncanonical spelling produced a different view")
			}
		}
		after := s.Stats()
		if after.Operations["projection"].Count == before.Operations["projection"].Count && after.CacheMisses-before.CacheMisses != 1 {
			t.Fatalf("noncanonical spellings missed the cache: %d misses for one canonical filter", after.CacheMisses-before.CacheMisses)
		}
	}
	tabs := []*capacityTab{}
	for tab := range 256 {
		tabs = append(tabs, &capacityTab{replica: 1, filter: noncanonical(variants[tab/2], tab)})
		if tab%2 == 0 {
			tabs[tab].filter = variants[tab/2]
		}
	}
	g = startTabs(t.Context(), services, tabs, 24)
	g.waitReady(t, 30*time.Second)
	time.Sleep(3 * time.Second)
	if cohorts := services[1].Stats().Cohorts; cohorts > 128+128 {
		t.Errorf("128 canonical filters spelled two ways retained %d cohorts", cohorts)
	}
	g.stop()
	t.Logf("canonical reuse: 128 filters x 4 spellings → one cached view each; 256 mixed-spelling tabs share 128 cohorts")
}
