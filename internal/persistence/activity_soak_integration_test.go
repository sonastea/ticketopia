package persistence

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
)

// soakStream is one browser tab's SSE connection. Rotation swaps its slot
// (filter, account or visible IDs) and reconnects, as a filter change does.
type soakStream struct {
	mu          sync.Mutex
	replica     int
	path        string
	credential  string
	ip          string
	cancel      context.CancelFunc
	lastCounts  time.Time
	frames      int // counts frames on the current connection
	intervals   []time.Duration
	early       [][2]time.Duration // previous and early interval, for diagnosis
	connections int
	refused     map[int]int
	unavailable int
	snapshots   int
}

func publicFilterQuery(f activity.Filters) string {
	data, _ := json.Marshal(map[string]any{"location": f.Location, "category_ids": f.CategoryIDs, "artist_ids": f.ArtistIDs, "venue_ids": f.VenueIDs})
	return "filter=" + url.QueryEscape(string(data))
}

func soakIDs(offset, count int) string {
	q := []string{}
	for i := range count {
		q = append(q, "event_id="+url.QueryEscape(fmt.Sprintf("ticketmaster:LiveCap_%d", offset+i)))
	}
	return strings.Join(q, "&")
}

func meanFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// Opt-in sustained mixed workload (rollout P1.3) on the deployment fixture:
// 704 SSE tabs unevenly spread 256/256/192 plus 64 fifteen-second fallback
// clients, visible-ID count subscriptions up to 101 IDs, rotating filters and
// accounts, continuous publication with natural ten-minute expiry, private
// membership writes and periodic in-network ordinary browse/write probes.
func TestMariaDBActivityDeploymentSoak(t *testing.T) {
	image, raw := os.Getenv("LIVE_ACTIVITY_LOAD_IMAGE"), os.Getenv("LIVE_ACTIVITY_SOAK")
	if image == "" || raw == "" {
		t.Skip("set LIVE_ACTIVITY_LOAD_IMAGE and LIVE_ACTIVITY_SOAK (for example 35m)")
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration < 5*time.Minute {
		t.Fatal("LIVE_ACTIVITY_SOAK must be a duration of at least 5m")
	}
	if duration < 30*time.Minute {
		t.Log("soak shorter than 30m: not a complete P1.3 gate result")
	}
	n := 1000
	if value := os.Getenv("LIVE_ACTIVITY_LOAD_GROUPS"); value != "" {
		n, err = strconv.Atoi(value)
		// Continuous replacement fluctuates around n; leave candidate headroom.
		if err != nil || n < 100 || n > 9000 {
			t.Fatal("LIVE_ACTIVITY_LOAD_GROUPS must be 100..9000 for the soak")
		}
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "soak-owner")
	_, token, err := auth.CreateToken(t.Context(), owner.ID, "soak")
	if err != nil {
		t.Fatal(err)
	}
	seedLiveFilterWindow(t, p, owner.ID, 1) // the probe's write target
	contributors := []string{}
	for i := range 8 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("soak-contributor-%d", i))
		contributors = append(contributors, account.ID)
	}
	seedCapacityWindow(t, f, p, contributors, n)
	// Natural expiry: spread the seeded window over the next ten minutes; the
	// publisher replaces groups at the same rate, crossing several windows.
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".live_recommendation_activity SET expires_at=UTC_TIMESTAMP(6)+INTERVAL MOD(CRC32(event_id),600000000) MICROSECOND")
	private := personalFilter([]string{"interested", "saved"})
	privateTokens := []string{}
	privateIDs := []string{}
	for i := range 64 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("soak-private-%d", i))
		_, raw, err := auth.CreateToken(t.Context(), account.ID, "soak")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Accounts().ReplaceLivePreferences(t.Context(), account.ID, 1, true, private); err != nil {
			t.Fatal(err)
		}
		privateTokens = append(privateTokens, raw)
		privateIDs = append(privateIDs, account.ID)
	}
	filters := distinctCapacityFilters(t, 0, 256, map[string]bool{})
	d := startActivityDeployment(t, f, image, "location = /nginx_status { stub_status; }")
	t.Logf("soak duration=%s groups=%d", duration, n)
	pooled := func(results []loadProbeResult) [2]time.Duration {
		var browse, write []time.Duration
		for _, r := range results {
			browse = append(browse, r.BrowseSamples...)
			write = append(write, r.WriteSamples...)
		}
		return [2]time.Duration{durationPercentile(browse, 95), durationPercentile(write, 95)}
	}
	baselineRuns := []loadProbeResult{}
	for i := range 3 {
		baselineRuns = append(baselineRuns, d.ordinary(fmt.Sprintf("baseline-%d", i), token))
	}
	baseline := pooled(baselineRuns)

	var ipSerial atomic.Uint32
	nextIP := func() string {
		v := ipSerial.Add(1)
		return fmt.Sprintf("198.%d.%d.%d", 30+v/65536, (v/256)%256, v%256)
	}
	slot := func(replica, k int) (string, string) {
		ids := ""
		switch k % 4 {
		case 0:
			ids = soakIDs((k*37)%n, min(101, n))
		case 1:
			ids = soakIDs((k*53)%n, 20)
		}
		join := func(parts ...string) string {
			kept := slices.DeleteFunc(parts, func(s string) bool { return s == "" })
			if len(kept) == 0 {
				return ""
			}
			return "?" + strings.Join(kept, "&")
		}
		if k%4 == 3 {
			return fmt.Sprintf("/r%d/api/v1/me/community/activity/stream%s", replica, join(ids)), privateTokens[k%len(privateTokens)]
		}
		return fmt.Sprintf("/r%d/api/v1/community/activity/stream%s", replica, join(publicFilterQuery(filters[k%len(filters)]), ids)), ""
	}
	ctx, stop := context.WithCancel(t.Context())
	// Live clients (SSE, fallback, rotation) stop before ordinary writers, so the
	// end baseline keeps the same write workload without live activity.
	live, stopLive := context.WithCancel(ctx)
	var running sync.WaitGroup
	defer func() { stop(); running.Wait() }()
	streams := []*soakStream{}
	for replica, count := range []int{256, 256, 192} {
		for k := range count {
			path, credential := slot(replica, k+replica*1000)
			streams = append(streams, &soakStream{replica: replica, path: path, credential: credential, ip: nextIP(), refused: map[int]int{}})
		}
	}
	run := func(s *soakStream) {
		for live.Err() == nil {
			session, cancel := context.WithCancel(live)
			s.mu.Lock()
			s.cancel, s.lastCounts, s.frames = cancel, time.Time{}, 0
			path, credential, ip := s.path, s.credential, s.ip
			s.mu.Unlock()
			resp, err := d.request(session, "GET", path, ip, credential, "")
			if err != nil || resp.StatusCode != 200 {
				s.mu.Lock()
				if resp != nil {
					s.refused[resp.StatusCode]++
					resp.Body.Close()
				} else {
					s.refused[0]++
				}
				s.mu.Unlock()
				cancel()
				sleepContext(live, 250*time.Millisecond)
				continue
			}
			s.mu.Lock()
			s.connections++
			s.mu.Unlock()
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 4096), 2<<20)
			event := ""
			for scanner.Scan() {
				line := scanner.Text()
				if value, ok := strings.CutPrefix(line, "event: "); ok {
					event = value
					continue
				}
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				now := time.Now()
				s.mu.Lock()
				switch event {
				case "counts.snapshot":
					// The first counts frame on a connection is its join and the
					// next follows the replica's shared cadence, so the interval
					// between them is legitimately shorter. Only later intervals
					// measure the fifteen-second cadence.
					s.frames++
					if s.frames >= 3 {
						interval := now.Sub(s.lastCounts)
						if interval < 13500*time.Millisecond && len(s.intervals) > 0 && len(s.early) < 8 {
							s.early = append(s.early, [2]time.Duration{s.intervals[len(s.intervals)-1], interval})
						}
						s.intervals = append(s.intervals, interval)
					}
					s.lastCounts = now
				case "activity.snapshot":
					s.snapshots++
				case "unavailable":
					s.unavailable++
				}
				s.mu.Unlock()
			}
			resp.Body.Close()
			cancel()
			sleepContext(live, time.Second) // quiet reconnect
		}
	}
	starts := make(chan struct{}, 24)
	for _, s := range streams {
		running.Go(func() {
			starts <- struct{}{}
			go func() {
				deadline := time.Now().Add(30 * time.Second)
				for time.Now().Before(deadline) {
					s.mu.Lock()
					connected := s.connections > 0
					s.mu.Unlock()
					if connected {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				<-starts
			}()
			run(s)
		})
	}
	deadline := time.Now().Add(90 * time.Second)
	for {
		connected := 0
		for _, s := range streams {
			s.mu.Lock()
			if s.connections > 0 {
				connected++
			}
			s.mu.Unlock()
		}
		if connected == len(streams) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d/%d soak streams connected", connected, len(streams))
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Logf("704 SSE streams connected (256/256/192)")

	// Fifteen-second fallback clients: snapshot plus 101-ID counts, balanced.
	var fallbackRequests, fallbackFailures atomic.Int64
	for k := range 64 {
		ip := nextIP()
		running.Go(func() {
			sleepContext(live, time.Duration(k)*15*time.Second/64)
			for live.Err() == nil {
				path, credential := "/api/v1/community/activity?"+publicFilterQuery(filters[(k*5)%len(filters)]), ""
				if k%4 == 3 {
					path, credential = "/api/v1/me/community/activity", privateTokens[k%len(privateTokens)]
				}
				for _, target := range []string{path, "/api/v1/events/activity-counts?" + soakIDs((k*41)%n, min(101, n))} {
					resp, err := d.request(live, "GET", target, ip, credential, "")
					fallbackRequests.Add(1)
					if err != nil {
						if live.Err() == nil {
							fallbackFailures.Add(1)
						}
						continue
					}
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode != 200 {
						fallbackFailures.Add(1)
					}
				}
				sleepContext(live, 15*time.Second)
			}
		})
	}
	// Continuous writes: publication at the expiry rate and private membership.
	every := func(scope context.Context, interval time.Duration, op func(int)) {
		running.Go(func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for i := 0; ; i++ {
				select {
				case <-scope.Done():
					return
				case <-ticker.C:
					op(i)
				}
			}
		})
	}
	writer := p.Recommendations()
	writer.EnableLiveActivity(true)
	var published atomic.Int64
	every(ctx, 10*time.Minute/time.Duration(n), func(i int) {
		if _, _, err := writer.Set(ctx, contributors[i%len(contributors)], capacityDetail(n+i), ""); err != nil && ctx.Err() == nil {
			t.Error("publication", err)
		}
		published.Add(1)
	})
	var membershipWrites atomic.Int64
	every(ctx, 2*time.Second, func(i int) {
		k := rand.IntN(len(privateTokens))
		event := fmt.Sprintf("ticketmaster:LiveCap_%d", n+max(0, int(published.Load())-rand.IntN(50)-1))
		method, body := "PUT", `{"visibility":"private"}`
		if i%2 == 1 {
			method, body = "DELETE", ""
		}
		resp, err := d.request(ctx, method, "/api/v1/me/event-interests/"+event, nextIP(), privateTokens[k], body)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			membershipWrites.Add(1)
		}
	})
	// Rotation: sixteen tabs change filter, account or visible IDs every 10s.
	var rotations atomic.Int64
	every(live, 10*time.Second, func(i int) {
		for j := range 16 {
			s := streams[rand.IntN(len(streams))]
			path, credential := slot(s.replica, i*16+j+7)
			s.mu.Lock()
			s.path, s.credential, s.ip = path, credential, nextIP()
			cancel := s.cancel
			s.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			rotations.Add(1)
		}
	})
	// Samples: proxy connections, database sessions and container memory.
	type sample struct {
		at                 time.Time
		proxy, db          float64
		memory             [3]float64
		hasMemory          bool
		streams, connected int
	}
	var samplesMu sync.Mutex
	samples := []sample{}
	active := regexp.MustCompile(`Active connections: (\d+)`)
	usage := regexp.MustCompile(`^([0-9.]+)(KiB|MiB|GiB)`)
	every(ctx, 15*time.Second, func(i int) {
		s := sample{at: time.Now()}
		if resp, err := d.request(ctx, "GET", "/nginx_status", "198.51.100.9", "", ""); err == nil {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if m := active.FindSubmatch(data); m != nil {
				s.proxy, _ = strconv.ParseFloat(string(m[1]), 64)
			}
		}
		var sessions int
		if err := f.admin.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER=?", f.runtime.User).Scan(&sessions); err == nil {
			s.db = float64(sessions)
		}
		if i%2 == 0 {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			out, err := execOutput(ctx, "docker", append([]string{"stats", "--no-stream", "--format", "{{.MemUsage}}"}, d.containers[:3]...)...)
			cancel()
			if err == nil {
				for r, line := range strings.Split(strings.TrimSpace(out), "\n")[:3] {
					if m := usage.FindStringSubmatch(line); m != nil {
						v, _ := strconv.ParseFloat(m[1], 64)
						s.memory[r] = v * map[string]float64{"KiB": 1.0 / 1024, "MiB": 1, "GiB": 1024}[m[2]]
					}
				}
				s.hasMemory = true
			}
		}
		samplesMu.Lock()
		samples = append(samples, s)
		samplesMu.Unlock()
	})

	soakStart := time.Now()
	logsSince := soakStart.UTC().Format(time.RFC3339)
	loadedRuns := []loadProbeResult{}
	for time.Since(soakStart) < duration {
		sleepContext(t.Context(), min(5*time.Minute, duration-time.Since(soakStart)))
		if time.Since(soakStart) >= time.Minute {
			result := d.ordinary(fmt.Sprintf("soak-%s", time.Since(soakStart).Round(time.Minute)), token)
			loadedRuns = append(loadedRuns, result)
			t.Logf("soak %s: probe browse p95=%s write p95=%s rotations=%d publications=%d membership_writes=%d", time.Since(soakStart).Round(time.Second),
				result.Browse, result.Write, rotations.Load(), published.Load(), membershipWrites.Load())
		}
	}
	// Same-volume baseline: live clients stop, ordinary writers continue.
	stopLive()
	liveStopped := time.Now()
	time.Sleep(20 * time.Second)
	afterRuns := []loadProbeResult{}
	for i := range 3 {
		afterRuns = append(afterRuns, d.ordinary(fmt.Sprintf("after-%d", i), token))
	}
	stop()
	running.Wait()

	// Ordinary traffic. Continuous writes grow the data that ordinary pages read,
	// so the gate compares the final loaded runs with a no-live baseline at the
	// same data volume and write workload. The start-of-soak ratio is context.
	after := pooled(afterRuns)
	final := pooled(loadedRuns[max(0, len(loadedRuns)-2):])
	all := pooled(loadedRuns)
	for i, label := range []string{"browse", "write"} {
		t.Logf("ordinary %s p95: start no-live=%s, soak pooled=%s (%.3f×), final loaded=%s vs end no-live=%s ratio=%.3f", label,
			baseline[i], all[i], float64(all[i])/float64(baseline[i]), final[i], after[i], float64(final[i])/float64(after[i]))
		if final[i] > after[i]*11/10 {
			t.Errorf("ordinary %s exceeds the 10%% soak gate against the same-volume no-live baseline", label)
		}
	}
	// Count cadence: established connections receive counts every fifteen
	// seconds regardless of other tabs joining or rotating filters.
	intervals := []time.Duration{}
	refused := map[int]int{}
	reconnects, unavailable := 0, 0
	for _, s := range streams {
		s.mu.Lock()
		intervals = append(intervals, s.intervals...)
		for status, count := range s.refused {
			refused[status] += count
		}
		reconnects += s.connections - 1
		unavailable += s.unavailable
		s.mu.Unlock()
	}
	// A delayed frame (previous interval long, this one short, summing to about
	// 30s) is a late delivery, not an extra count; record both shapes.
	delayed, extra := 0, 0
	shown := 0
	for _, s := range streams {
		s.mu.Lock()
		for _, pair := range s.early {
			if sum := pair[0] + pair[1]; sum > 28*time.Second && sum < 33*time.Second {
				delayed++
			} else {
				extra++
			}
			if shown < 12 {
				t.Logf("early count interval on replica %d: previous=%s early=%s", s.replica, pair[0].Round(time.Millisecond), pair[1].Round(time.Millisecond))
				shown++
			}
		}
		s.mu.Unlock()
	}
	t.Logf("early intervals: delayed-delivery shape=%d extra-count shape=%d (sampled up to 8 per stream)", delayed, extra)
	early, late := 0, 0
	for _, interval := range intervals {
		if interval < 13500*time.Millisecond {
			early++
		}
		if interval > 16500*time.Millisecond {
			late++
		}
	}
	t.Logf("count cadence: intervals=%d p1=%s p50=%s p99=%s max=%s early(<13.5s)=%d late(>16.5s)=%d", len(intervals),
		durationPercentile(intervals, 1), durationPercentile(intervals, 50), durationPercentile(intervals, 99), durationPercentile(intervals, 100), early, late)
	t.Logf("rotations=%d reconnects=%d unavailable_frames=%d refused_by_status=%v fallback_requests=%d fallback_failures=%d publications=%d membership_writes=%d",
		rotations.Load(), reconnects, unavailable, refused, fallbackRequests.Load(), fallbackFailures.Load(), published.Load(), membershipWrites.Load())
	if len(intervals) < 1000 || early > 0 || late > len(intervals)/100 {
		t.Error("count cadence was not independent of joins and filter changes")
	}
	if fallbackFailures.Load() > 0 {
		t.Error("fallback clients received failures")
	}
	// Growth: compare the last third with the middle third after warmup.
	warmup := max(5*time.Minute, duration/5)
	// peak compares per-third maxima: for sawtooth gauges such as cached views,
	// which every generation change clears and active tabs then rebuild.
	growth := func(label string, series []float64, relative, absolute float64, peak ...bool) {
		t.Helper()
		if len(series) < 6 {
			t.Errorf("%s: only %d post-warmup samples", label, len(series))
			return
		}
		k := len(series) / 3
		middle, last := meanFloat(series[k:2*k]), meanFloat(series[2*k:])
		if len(peak) > 0 && peak[0] {
			middle, last = slices.Max(series[k:2*k]), slices.Max(series[2*k:])
		}
		t.Logf("growth %-34s first=%.1f middle=%.1f last=%.1f max=%.1f", label, meanFloat(series[:k]), middle, last, slices.Max(series))
		if last > middle*(1+relative)+absolute {
			t.Errorf("%s continued growing: %.1f → %.1f", label, middle, last)
		}
	}
	samplesMu.Lock()
	var proxySeries, dbSeries []float64
	memorySeries := [3][]float64{}
	for _, s := range samples {
		if s.at.Sub(soakStart) < warmup || s.at.After(liveStopped) {
			continue
		}
		proxySeries = append(proxySeries, s.proxy)
		dbSeries = append(dbSeries, s.db)
		if s.hasMemory {
			for r := range 3 {
				memorySeries[r] = append(memorySeries[r], s.memory[r])
			}
		}
	}
	samplesMu.Unlock()
	growth("proxy active connections", proxySeries, 0.05, 16)
	growth("database sessions", dbSeries, 0.10, 2)
	for r := range 3 {
		growth(fmt.Sprintf("replica %d container memory MiB", r), memorySeries[r], 0.10, 4)
	}
	for r, container := range d.containers[:3] {
		logs := d.docker("logs", "--since", logsSince, container)
		if strings.Contains(logs, token) || slices.ContainsFunc(privateTokens, func(c string) bool { return strings.Contains(logs, c) }) {
			t.Fatal("credential in operational logs")
		}
		var goroutines, heap, connections, cohorts, owners []float64
		for _, line := range strings.Split(logs, "\n") {
			if !strings.Contains(line, "Live activity metrics") {
				continue
			}
			var record struct {
				Activity activity.Stats        `json:"activity"`
				Process  activity.ProcessStats `json:"process"`
			}
			if err := json.Unmarshal([]byte(line), &record); err != nil {
				t.Fatal("invalid metric record", err)
			}
			s := record.Activity
			if s.Bytes > activity.MemoryBudget || s.SQLJobs > 2 || s.Pending > 64 || s.Cohorts > 384 || s.Owners > 384 || s.Streams > 256 {
				t.Errorf("replica %d resource bound exceeded: %+v", r, s)
			}
			if record.Activity.AsOf.Sub(soakStart) < warmup || record.Activity.AsOf.After(liveStopped) {
				continue
			}
			goroutines = append(goroutines, float64(record.Process.Goroutines))
			heap = append(heap, float64(record.Process.HeapBytes)/(1<<20))
			connections = append(connections, float64(record.Process.SQLOpenConnections))
			cohorts = append(cohorts, float64(s.Cohorts))
			owners = append(owners, float64(s.Owners))
		}
		growth(fmt.Sprintf("replica %d goroutines", r), goroutines, 0.10, 20)
		growth(fmt.Sprintf("replica %d heap MiB", r), heap, 0.15, 4)
		growth(fmt.Sprintf("replica %d SQL open connections", r), connections, 0.10, 2)
		// Generation changes clear cached views, which active tabs rebuild:
		// compare peaks. The absolute bounds are checked above.
		growth(fmt.Sprintf("replica %d cached cohorts (peak)", r), cohorts, 0.10, 16, true)
		growth(fmt.Sprintf("replica %d cached owners (peak)", r), owners, 0.10, 4, true)
	}
}

func execOutput(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}
