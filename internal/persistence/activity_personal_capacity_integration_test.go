package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
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

// observedActivity records membership batch shapes from the production
// repository: owner/candidate bounds and whether every candidate belongs to
// the live window rather than an account's historical collections.
type observedActivity struct {
	*ActivityRepository
	mu                       sync.Mutex
	calls, owners, maxOwners int
	maxCandidates            int
	outside                  []string
	latencies                []time.Duration
}

func (o *observedActivity) Memberships(ctx context.Context, owners []activity.MembershipOwner, candidates []activity.MembershipCandidate) (map[string]activity.Membership, error) {
	started := time.Now()
	result, err := o.ActivityRepository.Memberships(ctx, owners, candidates)
	elapsed := time.Since(started)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	o.owners += len(owners)
	o.maxOwners = max(o.maxOwners, len(owners))
	o.maxCandidates = max(o.maxCandidates, len(candidates))
	o.latencies = append(o.latencies, elapsed)
	for _, c := range candidates {
		if !strings.HasPrefix(c.ID, "ticketmaster:LiveCap_") && !strings.HasPrefix(c.ID, "ticketmaster:LiveProbe") && len(o.outside) < 8 {
			o.outside = append(o.outside, c.ID)
		}
	}
	return result, err
}

func (o *observedActivity) reset() {
	o.mu.Lock()
	o.calls, o.owners, o.maxOwners, o.maxCandidates, o.outside, o.latencies = 0, 0, 0, 0, nil, nil
	o.mu.Unlock()
}

type personalReplicas struct {
	pools    []*Pool
	repos    []*observedActivity
	services []*activity.Service
}

// Cold replicas: fresh pools and services created together, as after a
// coordinated restart. close releases their connections before the next phase.
func newPersonalReplicas(t *testing.T, f mariaFixture, n int) *personalReplicas {
	t.Helper()
	r := &personalReplicas{}
	for range n {
		p, err := Open(t.Context(), f.runtime)
		if err != nil {
			t.Fatal(err)
		}
		r.pools = append(r.pools, p)
		r.repos = append(r.repos, &observedActivity{ActivityRepository: p.Activity()})
	}
	for _, repo := range r.repos {
		r.services = append(r.services, activity.New(t.Context(), repo, 256))
	}
	t.Cleanup(r.close)
	return r
}

func (r *personalReplicas) close() {
	for _, s := range r.services {
		s.Close()
	}
	for _, p := range r.pools {
		p.Close()
	}
	r.services, r.pools = nil, nil
}

var personalSources = [][]string{{"saved"}, {"interested"}, {"followed_artists"}, {"followed_venues"}, {"saved", "interested", "followed_artists", "followed_venues"}}

func personalFilter(sources []string) activity.Filters {
	f := activity.DefaultPreferences().Filters
	f.Connections = activity.Connections{Mode: "any", Sources: slices.Clone(sources)}
	return f
}

// seedPersonalHistory gives every account large collections, nearly all outside
// the live window, plus zero/sparse/dense window matches by density class.
func seedPersonalHistory(t *testing.T, f mariaFixture, p *Pool, archive string, owners []string, density func(int) int, n int) {
	t.Helper()
	started := time.Now()
	db := f.runtime.Database
	const events, artists, venues = 20000, 2000, 1000
	const saved, interested, followedArtists, followedVenues = 2000, 1000, 500, 200
	template := savedDetail("HistTemplate")
	if _, _, err := p.Saved().Save(t.Context(), archive, template); err != nil {
		t.Fatal(err)
	}
	columns := func(table string) string {
		rows, err := f.admin.QueryContext(t.Context(), "SELECT column_name FROM information_schema.columns WHERE table_schema=? AND table_name=? AND column_name<>'event_id' AND extra NOT LIKE '%GENERATED%' ORDER BY ordinal_position", db, table)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		names := []string{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			names = append(names, name)
		}
		return strings.Join(names, ",")
	}
	long := func(query string, args ...any) {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		defer cancel()
		if _, err := f.admin.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(safeError("history seed", err))
		}
	}
	for _, table := range []string{"events", "event_snapshots"} {
		cols := columns(table)
		long(fmt.Sprintf("INSERT INTO %s.%s (event_id,%s) SELECT CONCAT('ticketmaster:Hist_',s.seq),%s FROM %s.%s t JOIN %s.seq_1_to_%d s WHERE t.event_id=?", db, table, cols, cols, db, table, db, events), template.Item.ID)
	}
	long(fmt.Sprintf("INSERT INTO %s.artists (artist_id,snapshot,first_seen,last_seen) SELECT CONCAT('ticketmaster:HistArtist_',seq),JSON_OBJECT('id',CONCAT('ticketmaster:HistArtist_',seq),'name','Historical artist'),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6) FROM %s.seq_1_to_%d", db, db, artists))
	long(fmt.Sprintf("INSERT INTO %s.venues (venue_id,snapshot,first_seen,last_seen) SELECT CONCAT('ticketmaster:HistVenue_',seq),JSON_OBJECT('id',CONCAT('ticketmaster:HistVenue_',seq),'name','Historical venue'),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6) FROM %s.seq_1_to_%d", db, db, venues))
	// Six historical lineup facets and one venue facet per historical event.
	long(fmt.Sprintf("INSERT IGNORE INTO %s.event_search_facets (event_id,kind,value) SELECT CONCAT('ticketmaster:Hist_',e.seq),'artist',CONCAT('ticketmaster:HistArtist_',MOD(e.seq*7+k.seq*131,%d)+1) FROM %s.seq_1_to_%d e JOIN %s.seq_1_to_6 k", db, artists, db, events, db))
	long(fmt.Sprintf("INSERT IGNORE INTO %s.event_search_facets (event_id,kind,value) SELECT CONCAT('ticketmaster:Hist_',seq),'venue',CONCAT('ticketmaster:HistVenue_',MOD(seq*3,%d)+1) FROM %s.seq_1_to_%d", db, venues, db, events))
	for a, owner := range owners {
		long(fmt.Sprintf("INSERT INTO %s.saved_events (account_id,event_id) SELECT ?,CONCAT('ticketmaster:Hist_',MOD(?*37+seq,%d)+1) FROM %s.seq_1_to_%d", db, events, db, saved), owner, a)
		long(fmt.Sprintf("INSERT INTO %s.event_interests (account_id,event_id) SELECT ?,CONCAT('ticketmaster:Hist_',MOD(?*53+seq+7000,%d)+1) FROM %s.seq_1_to_%d", db, events, db, interested), owner, a)
		long(fmt.Sprintf("INSERT INTO %s.artist_follows (account_id,artist_id) SELECT ?,CONCAT('ticketmaster:HistArtist_',MOD(?*11+seq,%d)+1) FROM %s.seq_1_to_%d", db, artists, db, followedArtists), owner, a)
		long(fmt.Sprintf("INSERT INTO %s.venue_follows (account_id,venue_id) SELECT ?,CONCAT('ticketmaster:HistVenue_',MOD(?*13+seq,%d)+1) FROM %s.seq_1_to_%d", db, venues, db, followedVenues), owner, a)
		// Window matches: zero, sparse (about 1%) or dense (about half).
		switch density(a) {
		case 1:
			long(fmt.Sprintf("INSERT INTO %s.saved_events (account_id,event_id) SELECT ?,CONCAT('ticketmaster:LiveCap_',seq-1) FROM %s.seq_1_to_%d WHERE MOD(seq-1+?,97)=0", db, db, n), owner, a)
			long(fmt.Sprintf("INSERT INTO %s.event_interests (account_id,event_id) SELECT ?,CONCAT('ticketmaster:LiveCap_',seq-1) FROM %s.seq_1_to_%d WHERE MOD(seq-1+?,89)=0", db, db, n), owner, a)
			long(fmt.Sprintf("INSERT INTO %s.artist_follows (account_id,artist_id) VALUES (?,?)", db), owner, capacityArtist(a).ID)
			long(fmt.Sprintf("INSERT INTO %s.venue_follows (account_id,venue_id) VALUES (?,?),(?,?)", db), owner, capacityVenue(a, activity.Location{}).ID, owner, capacityVenue(a+128, activity.Location{}).ID)
		case 2:
			long(fmt.Sprintf("INSERT INTO %s.saved_events (account_id,event_id) SELECT ?,CONCAT('ticketmaster:LiveCap_',seq-1) FROM %s.seq_1_to_%d WHERE MOD(seq-1+?,2)=0", db, db, n), owner, a)
			long(fmt.Sprintf("INSERT INTO %s.event_interests (account_id,event_id) SELECT ?,CONCAT('ticketmaster:LiveCap_',seq-1) FROM %s.seq_1_to_%d WHERE MOD(seq-1+?,3)=0", db, db, n), owner, a)
			long(fmt.Sprintf("INSERT IGNORE INTO %s.artist_follows (account_id,artist_id) SELECT ?,CONCAT('ticketmaster:CapArtist_',MOD(?*64+seq,%d)) FROM %s.seq_1_to_64", db, capacityArtists, db), owner, a)
			long(fmt.Sprintf("INSERT IGNORE INTO %s.venue_follows (account_id,venue_id) SELECT ?,CONCAT('ticketmaster:CapVenue_',MOD(?*96+seq,%d)) FROM %s.seq_1_to_96", db, capacityVenues, db), owner, a)
		}
	}
	var rows int
	if err := f.admin.QueryRowContext(t.Context(), fmt.Sprintf("SELECT (SELECT COUNT(*) FROM %[1]s.saved_events)+(SELECT COUNT(*) FROM %[1]s.event_interests)+(SELECT COUNT(*) FROM %[1]s.artist_follows)+(SELECT COUNT(*) FROM %[1]s.venue_follows)", db)).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d relationship rows for %d accounts (%d historical events with %d facet rows) in %s", rows, len(owners), events, events*7, time.Since(started).Round(time.Millisecond))
}

func TestMariaDBActivityPersonalizationCapacity(t *testing.T) {
	for _, n := range capacitySizes(t, "LIVE_ACTIVITY_PERSONAL_GROUPS") {
		t.Run(fmt.Sprintf("groups=%d", n), func(t *testing.T) { personalizationCapacity(t, n) })
	}
}

func personalizationCapacity(t *testing.T, n int) {
	f := newMaria(t)
	f.migrate(t)
	seed := f.open(t)
	auth := accounts.New(seed.Accounts(), accountProviderFixture{})
	contributors := []string{}
	for i := range 8 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("personal-contributor-%d", i))
		contributors = append(contributors, account.ID)
	}
	seedCapacityWindow(t, f, seed, contributors, n)
	archive, _ := loginAccount(t, auth, "personal-archive")
	shared, _ := loginAccount(t, auth, "personal-shared")
	distinct := []string{}
	for i := range 256 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("personal-account-%d", i))
		distinct = append(distinct, account.ID)
	}
	// Account 256 (shared) is dense; distinct accounts cycle zero/sparse/dense.
	owners := append(slices.Clone(distinct), shared.ID)
	density := func(a int) int {
		if a == 256 {
			return 2
		}
		return a % 3
	}
	seedPersonalHistory(t, f, seed, archive.ID, owners, density, n)
	reference := activity.New(t.Context(), f.open(t).Activity(), 4)
	defer reference.Close()
	verify := func(label string, tabs []*capacityTab) {
		t.Helper()
		wrong, empty := 0, 0
		cache := map[string]string{}
		for _, tab := range tabs {
			key := tab.owner + canonicalKey(t, tab.filter)
			want, ok := cache[key]
			if !ok {
				snapshot, err := reference.OwnerSnapshot(t.Context(), tab.owner, tab.filter)
				if err != nil {
					t.Fatal(label, "reference", err)
				}
				want = itemsJSON(snapshot)
				cache[key] = want
			}
			tab.mu.Lock()
			got := tab.latest
			tab.mu.Unlock()
			if got == nil || itemsJSON(*got) != want {
				wrong++
			}
			if want == "[]" {
				empty++
			}
		}
		t.Logf("%s: %d/%d views match the complete-window reference (%d expected empty)", label, len(tabs)-wrong, len(tabs), empty)
		if wrong > 0 {
			t.Errorf("%s: %d views differ from the reference", label, wrong)
		}
	}
	batches := (n + 255) / 256
	report := func(label string, r *personalReplicas, ready []time.Duration) {
		t.Helper()
		for i, repo := range r.repos {
			repo.mu.Lock()
			stats := r.services[i].Stats()
			t.Logf("%s replica=%d membership_calls=%d owner_rows=%d max_owners=%d max_candidates=%d call_p95=%s call_max=%s owners=%d accounted=%.1fMiB failures=%v",
				label, i, repo.calls, repo.owners, repo.maxOwners, repo.maxCandidates, durationPercentile(repo.latencies, 95), durationPercentile(repo.latencies, 100),
				stats.Owners, float64(stats.Bytes)/(1<<20), stats.Failures)
			if repo.maxOwners > 128 || repo.maxCandidates > 256 {
				t.Errorf("%s: membership batch exceeded 128 owners x 256 events", label)
			}
			if len(repo.outside) > 0 {
				t.Errorf("%s: membership query included non-window candidates %v", label, repo.outside)
			}
			repo.mu.Unlock()
		}
		if ready != nil {
			t.Logf("%s: cold membership-to-visible p95=%s max=%s (768 tabs, replicas started together)", label, durationPercentile(ready, 95), durationPercentile(ready, 100))
			if durationPercentile(ready, 95) > 5*time.Second {
				t.Errorf("%s: cold membership-to-visible p95 exceeded 5s", label)
			}
		}
	}

	// Probe event: newest publication, matched by no one until a mutation.
	writer := seed.Recommendations()
	writer.EnableLiveActivity(true)
	probe := capacityDetail(0)
	probe.Item.ID, probe.Item.Source.ID = "ticketmaster:LiveProbe", "LiveProbe"
	probe.Item.Name = "Membership probe"
	probe.Item.Artists = []models.Artist{{ID: "ticketmaster:ProbeArtist", Name: "Probe artist", Source: models.Source{Provider: "ticketmaster", ID: "ProbeArtist"}}}
	probe.Item.Venues = []models.Venue{{ID: "ticketmaster:ProbeVenue", Source: models.Source{Provider: "ticketmaster", ID: "ProbeVenue"}, Place: models.Place{Name: "Probe venue", City: "London", CountryCode: "GB"}}}
	if _, _, err := writer.Set(t.Context(), contributors[0], probe, ""); err != nil {
		t.Fatal(err)
	}
	if n == activity.MaxCandidates {
		if err := writer.Remove(t.Context(), contributors[(n-1)%len(contributors)], capacityDetail(n-1).Item.ID); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".live_recommendation_activity SET expires_at=UTC_TIMESTAMP(6)+INTERVAL 3 HOUR")
	hasProbe := func(s activity.Snapshot) bool {
		return slices.ContainsFunc(s.Items, func(i activity.Item) bool { return i.EventID == probe.Item.ID })
	}
	// mutate adds or removes the probe relationship for one account through the
	// production writer that the source uses, on replica 0's pool.
	mutate := func(p *Pool, owner string, sources []string, add bool) {
		t.Helper()
		var err error
		source := sources[0]
		if len(sources) > 1 {
			source = "interested"
		}
		target := models.CatalogDetail{Meta: models.Freshness{DataAsOf: time.Now().UTC()}}
		switch {
		case source == "saved" && add:
			_, _, err = p.Saved().Save(t.Context(), owner, probe)
		case source == "saved":
			err = p.Saved().Remove(t.Context(), owner, probe.Item.ID)
		case source == "interested" && add:
			_, _, err = p.Interests().Set(t.Context(), owner, probe, "private")
		case source == "interested":
			err = p.Interests().Remove(t.Context(), owner, probe.Item.ID)
		case source == "followed_artists" && add:
			target.Item = models.ArtistTarget(probe.Item.Artists[0])
			_, _, err = p.Follows().Follow(t.Context(), owner, target)
		case source == "followed_artists":
			err = p.Follows().Remove(t.Context(), owner, "artist", probe.Item.Artists[0].ID)
		case add:
			target.Item = models.VenueTarget(probe.Item.Venues[0])
			_, _, err = p.Follows().Follow(t.Context(), owner, target)
		default:
			err = p.Follows().Remove(t.Context(), owner, "venue", probe.Item.Venues[0].ID)
		}
		if err != nil {
			t.Fatal("membership mutation", source, add, err)
		}
	}
	// propagate writes concurrently and measures commit → first matching frame
	// on every tab of each mutated account, across all replicas.
	propagate := func(label string, r *personalReplicas, tabs []*capacityTab, changed map[string][]string, add bool) {
		t.Helper()
		for _, tab := range tabs {
			tab.mark()
		}
		committed := map[string]time.Time{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for owner, sources := range changed {
			wg.Go(func() {
				mutate(r.pools[0], owner, sources, add)
				mu.Lock()
				committed[owner] = time.Now()
				mu.Unlock()
			})
		}
		wg.Wait()
		latencies := []time.Duration{}
		deadline := time.Now().Add(15 * time.Second)
		for _, tab := range tabs {
			at, ok := committed[tab.owner]
			if !ok {
				continue
			}
			for {
				if seen, ok := tab.firstMatch(at, func(s activity.Snapshot) bool { return hasProbe(s) == add }); ok {
					latencies = append(latencies, seen.Sub(at))
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("%s: tab on replica %d did not observe the change within 15s", label, tab.replica)
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
		closed := [3]int{}
		for _, tab := range tabs {
			tab.mu.Lock()
			closed[tab.replica] += tab.unavailable
			tab.mu.Unlock()
		}
		t.Logf("%s: membership commit-to-visible p95=%s max=%s observations=%d; unavailable frames by replica %v", label, durationPercentile(latencies, 95), durationPercentile(latencies, 100), len(latencies), closed)
		if durationPercentile(latencies, 95) > 5*time.Second {
			t.Errorf("%s: membership-to-visible p95 exceeded 5s", label)
		}
	}

	// One account across 768 tabs: every replica assembles it once.
	func() {
		r := newPersonalReplicas(t, f, 3)
		monitor := monitorCapacity(t.Context(), r.services)
		defer monitor.finish(t)
		tabs := []*capacityTab{}
		for replica := range 3 {
			for range 256 {
				tabs = append(tabs, &capacityTab{replica: replica, owner: shared.ID, filter: personalFilter(personalSources[4])})
			}
		}
		g := startTabs(t.Context(), r.services, tabs, 24)
		defer g.stop()
		report("shared account", r, g.waitReady(t, 60*time.Second))
		for i, repo := range r.repos {
			repo.mu.Lock()
			calls := repo.calls
			repo.mu.Unlock()
			// One owner needs ceil(window/256) candidate batches per assembly;
			// allow one repeated assembly if a publication arrived meanwhile.
			if calls > 2*batches || r.services[i].Stats().Owners != 1 {
				t.Errorf("shared account: duplicate tabs did not reuse membership on replica %d (%d calls for %d batches)", i, calls, batches)
			}
		}
		time.Sleep(3 * time.Second)
		verify("shared account", tabs)
		for _, repo := range r.repos {
			repo.reset()
		}
		change := map[string][]string{shared.ID: {"interested"}}
		propagate("shared account addition", r, tabs, change, true)
		propagate("shared account removal", r, tabs, change, false)
		report("shared account mutations", r, nil)
	}()

	// 256 distinct accounts per replica: every source alone and combined, with
	// zero/sparse/dense window matches and large historical collections.
	func() {
		r := newPersonalReplicas(t, f, 3)
		monitor := monitorCapacity(t.Context(), r.services)
		defer monitor.finish(t)
		tabs := []*capacityTab{}
		for replica := range 3 {
			for a, owner := range distinct {
				tabs = append(tabs, &capacityTab{replica: replica, owner: owner, filter: personalFilter(personalSources[a%5])})
			}
		}
		g := startTabs(t.Context(), r.services, tabs, 24)
		defer g.stop()
		// Wait long enough to record the whole cold distribution; the gate is p95.
		ready := g.waitReady(t, 5*time.Minute)
		report("distinct accounts", r, ready)
		classes := map[string][]time.Duration{}
		for i := range tabs {
			a := i % 256
			key := []string{"zero", "sparse", "dense"}[density(a)] + "/" + strings.Join(personalSources[a%5], "+")
			classes[key] = append(classes[key], ready[i])
		}
		keys := []string{}
		for key := range classes {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			t.Logf("distinct accounts cold ready %-60s p95=%s", key, durationPercentile(classes[key], 95))
		}
		time.Sleep(3 * time.Second)
		verify("distinct accounts", tabs)
		for _, repo := range r.repos {
			repo.reset()
		}
		// LIVE_ACTIVITY_PERSONAL_BURST (default 64, at most 256) accounts change at once.
		burst := 64
		if v := os.Getenv("LIVE_ACTIVITY_PERSONAL_BURST"); v != "" {
			var err error
			if burst, err = strconv.Atoi(v); err != nil || burst < 1 || burst > 256 {
				t.Fatal("LIVE_ACTIVITY_PERSONAL_BURST must be 1..256")
			}
		}
		change := map[string][]string{}
		for a := range burst {
			change[distinct[a]] = personalSources[a%5]
		}
		propagate(fmt.Sprintf("%d distinct accounts addition", burst), r, tabs, change, true)
		propagate(fmt.Sprintf("%d distinct accounts removal", burst), r, tabs, change, false)
		report("distinct accounts mutations", r, nil)
		for _, tab := range tabs {
			tab.mark()
		}
		time.Sleep(2 * time.Second)
		verify("distinct accounts after mutations", tabs)
	}()

	// Overload: 400 cold distinct owners at once on one replica. Every answer is
	// either the correct view or explicit unavailability; gauges stay bounded.
	func() {
		extra := []string{}
		for i := range 144 {
			account, _ := loginAccount(t, auth, fmt.Sprintf("personal-overload-%d", i))
			extra = append(extra, account.ID)
		}
		r := newPersonalReplicas(t, f, 1)
		monitor := monitorCapacity(t.Context(), r.services)
		defer monitor.finish(t)
		s := r.services[0]
		all := append(slices.Clone(distinct), extra...)
		filter := personalFilter(personalSources[4])
		var mu sync.Mutex
		served, unavailable, wrong := 0, 0, 0
		var wg sync.WaitGroup
		for _, owner := range all {
			wg.Go(func() {
				view, err := s.OwnerSnapshot(t.Context(), owner, filter)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case errors.Is(err, activity.ErrUnavailable):
					unavailable++
				case err != nil:
					t.Error("overload returned a non-explicit error", err)
				default:
					want, err := reference.OwnerSnapshot(t.Context(), owner, filter)
					if err != nil || itemsJSON(want) != itemsJSON(view) {
						wrong++
					}
					served++
				}
			})
		}
		wg.Wait()
		stats := s.Stats()
		t.Logf("overload: 400 concurrent cold owners → served=%d unavailable=%d wrong=%d owners=%d failures=%v", served, unavailable, wrong, stats.Owners, stats.Failures)
		if wrong > 0 {
			t.Error("overload served an incorrect personal view")
		}
		if served+unavailable != len(all) || unavailable == 0 {
			t.Error("overload was not bounded with explicit unavailability")
		}
		// Recovery: an admitted owner is served promptly once the burst ends.
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := s.OwnerSnapshot(t.Context(), distinct[0], filter); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("replica did not recover after overload")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
}
