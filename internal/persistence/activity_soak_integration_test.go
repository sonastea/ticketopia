package persistence

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
	"syscall"
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
	open        bool // a connection is currently streaming
	epoch       int  // connections in the current live-client epoch
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
// membership writes and periodic in-network ordinary browse/write probes. After
// the sustained soak, an interleaved ABBA×4 phase measures the ordinary-traffic
// gate: live clients stop and restart per period while writers keep running.
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
	retained := []soakSample{}
	baselineRuns := []loadProbeResult{}
	for i := range 3 {
		label := fmt.Sprintf("baseline-%d", i)
		result := d.ordinary(label, token)
		baselineRuns = append(baselineRuns, result)
		retained = append(retained, soakSamples("baseline", 0, label, soakNoLive, result)...)
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
	// Live clients (SSE, fallback, rotation) run in epochs that the interleaved
	// phase stops and restarts; ordinary writers run on ctx throughout, so
	// no-live periods keep the same write workload without live activity.
	var running, liveRunning sync.WaitGroup
	stopLive := func() {}
	defer func() { stop(); liveRunning.Wait(); running.Wait() }()
	// Diagnostics retained beside the latency samples.
	var eventsMu sync.Mutex
	events := []soakEvent{}
	r0Frames := map[time.Time]int{}
	r0Frame := func(at time.Time) {
		eventsMu.Lock()
		r0Frames[at.Truncate(100*time.Millisecond)]++
		eventsMu.Unlock()
	}
	// Raw samples are saved even when the test fails before analysis.
	samplesPath := os.Getenv("LIVE_ACTIVITY_SOAK_SAMPLES")
	if samplesPath == "" {
		samplesPath = fmt.Sprintf("../../.local/activity-soak-%s.csv", time.Now().UTC().Format("20060102T150405Z"))
	}
	saved := false
	requests := []soakRequest{}
	save := func() {
		if saved {
			return
		}
		saved = true
		digest, err := writeSoakCSV(samplesPath, retained)
		if err != nil {
			t.Error(err)
			return
		}
		if len(requests) > 0 {
			if err := writeSoakRequests(strings.TrimSuffix(samplesPath, ".csv")+"-requests.csv", requests); err != nil {
				t.Error(err)
			}
		}
		eventsMu.Lock()
		for at, frames := range r0Frames {
			events = append(events, soakEvent{"r0_count_frames", at, float64(frames)})
		}
		slices.SortStableFunc(events, func(a, b soakEvent) int { return a.At.Compare(b.At) })
		err = writeSoakEvents(strings.TrimSuffix(samplesPath, ".csv")+"-events.csv", events)
		eventsMu.Unlock()
		if err != nil {
			t.Error(err)
		}
		t.Logf("raw samples: %s (%d rows, sha256 %s); diagnostics: %s", samplesPath, len(retained), digest, strings.TrimSuffix(samplesPath, ".csv")+"-events.csv")
	}
	t.Cleanup(save)
	streams := []*soakStream{}
	for replica, count := range []int{256, 256, 192} {
		for k := range count {
			path, credential := slot(replica, k+replica*1000)
			streams = append(streams, &soakStream{replica: replica, path: path, credential: credential, ip: nextIP(), refused: map[int]int{}})
		}
	}
	run := func(live context.Context, s *soakStream) {
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
				} else if live.Err() == nil { // not an epoch shutdown
					s.refused[0]++
				}
				s.mu.Unlock()
				cancel()
				sleepContext(live, 250*time.Millisecond)
				continue
			}
			s.mu.Lock()
			s.connections++
			s.epoch++
			s.open = true
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
					if s.replica == 0 && s.frames >= 2 {
						r0Frame(now) // r0's shared delivery ticks, for burst analysis
					}
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
			s.mu.Lock()
			s.open = false
			s.mu.Unlock()
			cancel()
			sleepContext(live, time.Second) // quiet reconnect
		}
	}
	var fallbackRequests, fallbackFailures, rotations atomic.Int64
	fallbackIPs := []string{}
	for range 64 {
		fallbackIPs = append(fallbackIPs, nextIP())
	}
	epochs := 0
	var fallback func(live context.Context, k int, ip string)
	// startLive starts one epoch of live clients: 704 SSE tabs (bounded to 24
	// concurrent handshakes), 64 fallback clients and rotation. It returns once
	// every tab has connected in this epoch.
	startLive := func() {
		live, cancelLive := context.WithCancel(ctx)
		epochs++
		for _, s := range streams {
			s.mu.Lock()
			s.epoch = 0
			s.mu.Unlock()
		}
		starts := make(chan struct{}, 24)
		for _, s := range streams {
			liveRunning.Go(func() {
				starts <- struct{}{}
				go func() {
					deadline := time.Now().Add(30 * time.Second)
					for time.Now().Before(deadline) {
						s.mu.Lock()
						connected := s.epoch > 0
						s.mu.Unlock()
						if connected {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
					<-starts
				}()
				run(live, s)
			})
		}
		deadline := time.Now().Add(90 * time.Second)
		for {
			connected := 0
			for _, s := range streams {
				s.mu.Lock()
				if s.epoch > 0 {
					connected++
				}
				s.mu.Unlock()
			}
			if connected == len(streams) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("epoch %d: only %d/%d soak streams connected", epochs, connected, len(streams))
			}
			time.Sleep(100 * time.Millisecond)
		}
		// Fifteen-second fallback clients: snapshot plus 101-ID counts, balanced.
		// These poll over short requests; they are not SSE connections.
		for k, ip := range fallbackIPs {
			liveRunning.Go(func() { fallback(live, k, ip) })
		}
		// Rotation: sixteen tabs change filter, account or visible IDs every 10s.
		liveRunning.Go(func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-live.Done():
					return
				case <-ticker.C:
				}
				for range 16 {
					s := streams[rand.IntN(len(streams))]
					path, credential := slot(s.replica, int(rotations.Add(1)-1)+7)
					s.mu.Lock()
					s.path, s.credential, s.ip = path, credential, nextIP()
					cancel := s.cancel
					s.mu.Unlock()
					if cancel != nil {
						cancel()
					}
				}
			}
		})
		stopLive = func() {
			cancelLive()
			liveRunning.Wait()
			// Idle keep-alive connections would otherwise remain at the proxy.
			d.client.CloseIdleConnections()
			stopLive = func() {}
		}
	}
	fallback = func(live context.Context, k int, ip string) {
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
	}
	startLive()
	t.Logf("704 SSE streams connected (256/256/192); 64 fallback polling clients started")
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
	// CPU diagnostics every 5s, identical in both states: the Docker VM's busy
	// cores (from /proc/stat in an idle helper container), this test process
	// (the load generator) and, when present, the Colima/Lima host processes
	// (QEMU VM, limactl host agents and SSH mux, which forward the host port).
	helper := d.proxy + "-cpu"
	d.docker("run", "-d", "--name", helper, "--network", "none", "--memory", "16m", "--cpus", "0.1", "alpine:3", "sleep", "86400")
	t.Cleanup(func() { exec.Command("docker", "rm", "-f", helper).Run() })
	cpuSeconds := func(value string) float64 {
		total := 0.0
		for _, part := range strings.Split(value, ":") {
			v, _ := strconv.ParseFloat(part, 64)
			total = total*60 + v
		}
		return total
	}
	var lastAt time.Time
	var lastBusy, lastTotal, lastSelf float64
	lastHost := map[string]float64{}
	every(ctx, 5*time.Second, func(int) {
		at := time.Now()
		sampled := map[string]float64{}
		vmCPUs, busy, total := 0.0, 0.0, 0.0
		probe, cancel := context.WithTimeout(ctx, 4*time.Second)
		if out, err := execOutput(probe, "docker", "exec", helper, "cat", "/proc/stat"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if len(fields) > 0 && strings.HasPrefix(fields[0], "cpu") && fields[0] != "cpu" {
					vmCPUs++
				}
				if len(fields) < 6 || fields[0] != "cpu" {
					continue
				}
				for k, field := range fields[1:] {
					v, _ := strconv.ParseFloat(field, 64)
					total += v
					if k != 3 && k != 4 { // idle, iowait
						busy += v
					}
				}
			}
		}
		cancel()
		var usage syscall.Rusage
		self := 0.0
		if syscall.Getrusage(syscall.RUSAGE_SELF, &usage) == nil {
			self = time.Duration(usage.Utime.Nano() + usage.Stime.Nano()).Seconds()
		}
		probe, cancel = context.WithTimeout(ctx, 4*time.Second)
		if out, err := execOutput(probe, "ps", "-A", "-o", "cputime=,command="); err == nil {
			for _, line := range strings.Split(out, "\n") {
				fields := strings.Fields(line)
				if len(fields) < 2 {
					continue
				}
				command := strings.Join(fields[1:], " ")
				kind := ""
				switch {
				case strings.Contains(command, "qemu-system"):
					kind = "host_qemu_vm"
				case strings.Contains(command, "limactl"):
					kind = "host_limactl"
				case strings.HasPrefix(command, "ssh") && strings.Contains(command, "colima"):
					kind = "host_lima_ssh"
				}
				if kind != "" {
					sampled[kind] += cpuSeconds(fields[0])
				}
			}
		}
		cancel()
		if !lastAt.IsZero() {
			elapsed := at.Sub(lastAt).Seconds()
			eventsMu.Lock()
			if total > lastTotal && lastTotal > 0 {
				events = append(events, soakEvent{"vm_busy", at, (busy - lastBusy) / (total - lastTotal) * vmCPUs})
			}
			events = append(events, soakEvent{"test_process", at, (self - lastSelf) / elapsed})
			for kind, value := range sampled {
				if previous, ok := lastHost[kind]; ok {
					events = append(events, soakEvent{kind, at, (value - previous) / elapsed})
				}
			}
			eventsMu.Unlock()
		}
		lastAt, lastBusy, lastTotal, lastSelf, lastHost = at, busy, total, self, sampled
	})

	soakStart := time.Now()
	logsSince := soakStart.UTC().Format(time.RFC3339)
	loadedRuns := []loadProbeResult{}
	for time.Since(soakStart) < duration {
		sleepContext(t.Context(), min(5*time.Minute, duration-time.Since(soakStart)))
		if time.Since(soakStart) >= time.Minute {
			label := fmt.Sprintf("soak-%s", time.Since(soakStart).Round(time.Minute))
			result := d.ordinary(label, token)
			loadedRuns = append(loadedRuns, result)
			retained = append(retained, soakSamples("soak", 0, label, soakLoaded, result)...)
			t.Logf("soak %s: probe browse p95=%s write p95=%s rotations=%d publications=%d membership_writes=%d", time.Since(soakStart).Round(time.Second),
				result.Browse, result.Write, rotations.Load(), published.Load(), membershipWrites.Load())
		}
	}
	liveStopped := time.Now()
	all := pooled(loadedRuns)
	for i, label := range []string{"browse", "write"} {
		t.Logf("sustained soak context, ordinary %s p95: start no-live=%s, soak pooled=%s (%.3f×; data volume differs, not the gate)", label,
			baseline[i], all[i], float64(all[i])/float64(baseline[i]))
	}

	// Interleaved gate measurement. Continuous writes grow the data that ordinary
	// pages read, so loaded and no-live periods alternate in a fixed ABBA×4
	// order and drift cancels within each adjacent pair. Before every period live
	// clients stop and teardown is verified; loaded periods then start a fresh
	// client epoch and wait for every tab to reconnect. Every period then settles
	// for the same fixed interval before the same probe samples it. Publication,
	// membership writes and resource sampling run unchanged throughout.
	const settle, periodIterations = 60 * time.Second, 300
	proxyActive := func() int {
		resp, err := d.request(ctx, "GET", "/nginx_status", "198.51.100.9", "", "")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		m := active.FindSubmatch(data)
		if m == nil {
			t.Fatal("no proxy status")
		}
		value, _ := strconv.Atoi(string(m[1]))
		return value
	}
	openStreams := func() int {
		open := 0
		for _, s := range streams {
			s.mu.Lock()
			if s.open {
				open++
			}
			s.mu.Unlock()
		}
		return open
	}
	// verifyNoLive waits for every replica to report zero subscriptions in a
	// metrics record taken after shutdown, and for the proxy to drain.
	verifyNoLive := func(since time.Time) {
		deadline := time.Now().Add(60 * time.Second)
		for {
			drained := 0
			for _, container := range d.containers[:3] {
				for _, line := range strings.Split(d.docker("logs", "--since", since.UTC().Format(time.RFC3339), container), "\n") {
					var record struct {
						Activity activity.Stats `json:"activity"`
					}
					if strings.Contains(line, "Live activity metrics") && json.Unmarshal([]byte(line), &record) == nil &&
						record.Activity.AsOf.After(since) && record.Activity.Streams == 0 {
						drained++
						break
					}
				}
			}
			proxy := proxyActive()
			if drained == 3 && proxy <= 8 && openStreams() == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("live clients did not shut down: %d/3 replicas at zero streams, proxy active=%d, open=%d", drained, proxy, openStreams())
			}
			time.Sleep(2 * time.Second)
		}
	}
	type periodRecord struct {
		state                                    string
		ready                                    time.Duration
		open, proxy                              int
		fallback, rotated, published, membership int64
		start, end                               time.Time
	}
	interleavedErrors := 0
	type window struct {
		state      string
		start, end time.Time
	}
	periodWindows := []window{}
	// Diagnostics: every period's probe requests are joined to nginx's probe
	// log. With LIVE_ACTIVITY_LOAD_TRACE, r0 is also traced in pairs 2, 3, 6
	// and 7 (both pair orders), leaving the other pairs to measure tracing
	// overhead; traces are analyzed after sampling ends, off the measured path.
	type tracedPeriod struct {
		period     int
		state      string
		path       string
		result     loadProbeResult
		logged     []probeLogEntry
		iterations int
	}
	tracedPeriods := []tracedPeriod{}
	tracing := d.traceDir != ""
	for i, state := range soakOrder {
		transition := time.Now()
		stopLive()
		stopped := time.Now()
		verifyNoLive(stopped)
		if state == soakLoaded {
			startLive()
		}
		record := periodRecord{state: state, ready: time.Since(transition)}
		sleepContext(t.Context(), settle)
		record.open, record.proxy = openStreams(), proxyActive()
		fallbackBefore, rotatedBefore, publishedBefore, membershipBefore := fallbackRequests.Load(), rotations.Load(), published.Load(), membershipWrites.Load()
		record.start = time.Now()
		label := fmt.Sprintf("interleaved-%d-%s", i+1, state)
		d.probeLog() // discard earlier probe runs' lines
		traced := tracing && slices.Contains([]int{2, 3}, (i/2)%4+1)
		if traced {
			d.traceStart()
		}
		result := d.probeRun(label, token, periodIterations)
		record.end = time.Now()
		logged := d.probeLog()
		if traced {
			path := fmt.Sprintf("%s-trace-%d.trace", strings.TrimSuffix(samplesPath, ".csv"), i+1)
			d.traceStop(path)
			tracedPeriods = append(tracedPeriods, tracedPeriod{i + 1, state, path, result, logged, periodIterations})
		} else {
			requests = append(requests, joinSoakRequests(t, i+1, state, periodIterations, result, logged, nil)...)
		}
		periodWindows = append(periodWindows, window{state, record.start, record.end})
		record.fallback, record.rotated = fallbackRequests.Load()-fallbackBefore, rotations.Load()-rotatedBefore
		record.published, record.membership = published.Load()-publishedBefore, membershipWrites.Load()-membershipBefore
		if state == soakNoLive && (record.open != 0 || record.fallback != 0 || record.rotated != 0) {
			t.Fatalf("period %d: live clients active during a no-live period: %+v", i+1, record)
		}
		if state == soakLoaded && (record.open < len(streams)*9/10 || record.fallback == 0) {
			t.Fatalf("period %d: live clients not running during a loaded period: %+v", i+1, record)
		}
		interleavedErrors += result.Errors
		retained = append(retained, soakSamples("interleaved", i+1, label, state, result)...)
		t.Logf("period %d %-7s ready=%s settle=%s sampled %s–%s open_sse=%d/%d proxy_active=%d fallback_requests=%d rotations=%d publications=%d membership_writes=%d probe_errors=%d",
			i+1, state, record.ready.Round(time.Second), settle, record.start.UTC().Format("15:04:05"), record.end.UTC().Format("15:04:05"),
			record.open, len(streams), record.proxy, record.fallback, record.rotated, record.published, record.membership, result.Errors)
	}
	stopLive()
	stop()
	liveRunning.Wait()
	running.Wait()
	for _, p := range tracedPeriods {
		summary, err := analyzeTrace(t.Context(), p.path)
		if err != nil {
			t.Errorf("period %d trace: %v", p.period, err)
			continue
		}
		logTraceRuntime(t, p.period, p.state, summary, traceFileSize(p.path))
		requests = append(requests, joinSoakRequests(t, p.period, p.state, p.iterations, p.result, p.logged, &summary)...)
	}

	// Retain raw samples for reanalysis (TestActivitySoakSampleAnalysis).
	save()
	logSoakRequestDiagnostics(t, requests)
	// Per-state CPU during sampling: is the loaded shift VM/host contention?
	eventsMu.Lock()
	cpu := map[string]map[string][]float64{}
	for _, e := range events {
		if e.Kind == "r0_count_frames" {
			continue
		}
		for _, p := range periodWindows {
			if e.At.After(p.start) && !e.At.After(p.end.Add(5*time.Second)) {
				if cpu[e.Kind] == nil {
					cpu[e.Kind] = map[string][]float64{}
				}
				cpu[e.Kind][p.state] = append(cpu[e.Kind][p.state], e.Value)
			}
		}
	}
	eventsMu.Unlock()
	kinds := slices.Sorted(maps.Keys(cpu))
	for _, kind := range kinds {
		t.Logf("cpu %-22s loaded mean=%.2f cores (n=%d) no-live mean=%.2f cores (n=%d)", kind,
			meanFloat(cpu[kind][soakLoaded]), len(cpu[kind][soakLoaded]), meanFloat(cpu[kind][soakNoLive]), len(cpu[kind][soakNoLive]))
	}
	if tracing || d.profile.logFile != "" {
		t.Log("instrumented run (trace build on r0 and/or statement log): a diagnostic result, not a P1.3 gate result")
	}
	for _, r := range logSoakAnalysis(t, retained) {
		if r.ratio > 1.10 {
			t.Errorf("ordinary %s exceeds the 10%% gate: pooled loaded/no-live p95 ratio %.3f", r.operation, r.ratio)
		}
	}
	if interleavedErrors > 0 {
		t.Errorf("ordinary traffic had %d probe errors in the interleaved phase", interleavedErrors)
	}
	// Count cadence: established connections receive counts every fifteen
	// seconds regardless of other tabs joining or rotating filters.
	intervals := []time.Duration{}
	refused := map[int]int{}
	connections, reconnects, unavailable := 0, 0, 0
	for _, s := range streams {
		s.mu.Lock()
		intervals = append(intervals, s.intervals...)
		for status, count := range s.refused {
			refused[status] += count
		}
		connections += s.connections
		reconnects += s.connections - epochs // beyond each epoch's first connection
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
	t.Logf("SSE: %d tabs, %d client epochs, %d connections established (%d rotation/drop reconnects); fallback: %d polling clients (not SSE), %d requests, %d failures",
		len(streams), epochs, connections, reconnects, len(fallbackIPs), fallbackRequests.Load(), fallbackFailures.Load())
	t.Logf("rotations=%d unavailable_frames=%d refused_by_status=%v publications=%d membership_writes=%d",
		rotations.Load(), unavailable, refused, published.Load(), membershipWrites.Load())
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
