package persistence

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

// Opt-in deployment fixture: actual production image, three separate processes,
// 2 GiB/one CPU each, a real nginx proxy and the isolated real-MariaDB schema.
// No limiter bypass: nginx supplies controlled client IPs on its loopback-only
// test listener. Never copy that header policy into a deployment proxy.
func TestMariaDBActivityDeploymentLoad(t *testing.T) {
	image := os.Getenv("LIVE_ACTIVITY_LOAD_IMAGE")
	if image == "" {
		t.Skip("set LIVE_ACTIVITY_LOAD_IMAGE to a locally built production image")
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "load-owner")
	_, token, err := auth.CreateToken(t.Context(), owner.ID, "load")
	if err != nil {
		t.Fatal(err)
	}
	n := 100
	if value := os.Getenv("LIVE_ACTIVITY_LOAD_GROUPS"); value != "" {
		n, err = strconv.Atoi(value)
		if err != nil || n < 1 || n > activity.MaxCandidates {
			t.Fatal("LIVE_ACTIVITY_LOAD_GROUPS must be 1..10000")
		}
	}
	seedLiveFilterWindow(t, p, owner.ID, n)
	contributorID := ""
	if os.Getenv("LIVE_ACTIVITY_LOAD_RICH") == "true" {
		contributor, _ := loginAccount(t, auth, "load-second-contributor")
		contributorID = contributor.ID
		writer := p.Recommendations()
		writer.EnableLiveActivity(true)
		for i := range n {
			d := savedDetail(fmt.Sprintf("LiveFilter_%d", i))
			d.Meta.DataAsOf = time.Now().UTC()
			d.Item.Name = strings.Repeat("A long multilingual event title — 音楽 · ", 8) + strconv.Itoa(i)
			for artist := range 8 {
				id := fmt.Sprintf("Lineup_%d", (i+artist)%128)
				d.Item.Artists = append(d.Item.Artists, models.Artist{ID: "ticketmaster:" + id, Name: "Fixture artist", Source: models.Source{Provider: "ticketmaster", ID: id}})
			}
			if _, _, err := writer.Set(t.Context(), contributor.ID, d, ""); err != nil {
				t.Fatal(err)
			}
		}
	}
	name := "Loadname"
	if _, err := auth.UpdateProfile(t.Context(), owner.ID, accounts.ProfilePatch{ActivityFirstName: &name}); err != nil {
		t.Fatal(err)
	}
	// A private view whose membership can be changed through production HTTP.
	recipient, _ := loginAccount(t, auth, "load-recipient")
	_, privateToken, err := auth.CreateToken(t.Context(), recipient.ID, "load")
	if err != nil {
		t.Fatal(err)
	}
	filter := activity.DefaultPreferences().Filters
	filter.Connections = activity.Connections{Mode: "any", Sources: []string{"interested"}}
	if _, err := p.Accounts().ReplaceLivePreferences(t.Context(), recipient.ID, 1, true, filter); err != nil {
		t.Fatal(err)
	}
	privateTokens := make([]string, 256)
	for i := range privateTokens {
		privateTokens[i] = privateToken
	}
	distinct := os.Getenv("LIVE_ACTIVITY_LOAD_DISTINCT") == "true"
	if distinct {
		for i := range privateTokens {
			account, _ := loginAccount(t, auth, fmt.Sprintf("load-recipient-%d", i))
			_, raw, err := auth.CreateToken(t.Context(), account.ID, "load")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Accounts().ReplaceLivePreferences(t.Context(), account.ID, 1, true, filter); err != nil {
				t.Fatal(err)
			}
			privateTokens[i] = raw
		}
	}

	// Use the repository's Docker-shared local directory (macOS /var temporary
	// paths need not be mounted into the Docker VM).
	if err := os.MkdirAll("../../.local", 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("../../.local", "activity-load-")
	if err != nil {
		t.Fatal(err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	docker := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "docker", args...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker %s: %v\n%s", args[0], err, out)
		}
		return strings.TrimSpace(string(out))
	}
	network := os.Getenv("LIVE_ACTIVITY_LOAD_NETWORK")
	if network == "" {
		network = "ticketopia-db_default"
	}
	dbhost := os.Getenv("LIVE_ACTIVITY_LOAD_DB_HOST")
	if dbhost == "" {
		dbhost = "mariadb"
	}
	ca, err := os.ReadFile(f.runtime.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), ca, 0644); err != nil {
		t.Fatal(err)
	}
	env := fmt.Sprintf("PERSISTENCE_MODE=mariadb\nDB_HOST=%s\nDB_PORT=3306\nDB_NAME=%s\nDB_USER=%s\nDB_PASSWORD=%s\nDB_TLS_CA_FILE=/load/ca.crt\nAUTH_ENABLED=true\nAUTH_BASE_URL=http://localhost\nGOOGLE_CLIENT_ID=fixture\nGOOGLE_CLIENT_SECRET=fixture\nLIVE_ACTIVITY_ENABLED=true\nIP_GEOLOCATION_ENABLED=false\nTRUSTED_PROXY_CIDRS=172.16.0.0/12,192.168.0.0/16\n", dbhost, f.runtime.Database, f.runtime.User, f.runtime.Password)
	if err := os.WriteFile(filepath.Join(dir, "app.env"), []byte(env), 0600); err != nil {
		t.Fatal(err)
	}
	prefix := strings.ReplaceAll(f.runtime.Database, "_", "-")
	profile := newLoadProfile(t, f, "ticketopia-db-mariadb-1")
	containers := []string{}
	t.Cleanup(func() {
		for i := len(containers) - 1; i >= 0; i-- {
			cmd := exec.Command("docker", "rm", "-f", containers[i])
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("container cleanup: %v %s", err, out)
			}
		}
	})
	for i := range 3 {
		id := prefix + "-" + strconv.Itoa(i)
		containers = append(containers, id)
		docker("run", "-d", "--name", id, "--network", network, "--memory", "2g", "--cpus", "1", "--env-file", filepath.Join(dir, "app.env"), "-v", filepath.Join(dir, "ca.crt")+":/load/ca.crt:ro", image)
	}
	config := "worker_rlimit_nofile 8192; events { worker_connections 4096; }\nhttp { access_log off; upstream apps {"
	for i := range 3 {
		config += fmt.Sprintf("server %s-%d:8080;", prefix, i)
	}
	config += "} server { listen 8080; proxy_http_version 1.1; proxy_set_header Connection ''; proxy_set_header X-Forwarded-For $http_x_load_ip; proxy_buffering off; proxy_read_timeout 40s;"
	for i := range 3 {
		config += fmt.Sprintf("location /r%d/ { proxy_pass http://%s-%d:8080/; }", i, prefix, i)
	}
	config += "location / { proxy_pass http://apps; } } }"
	if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	proxy := prefix + "-proxy"
	containers = append(containers, proxy)
	docker("run", "-d", "--name", proxy, "--network", network, "--memory", "128m", "--cpus", "0.5", "--ulimit", "nofile=8192:8192", "-p", "127.0.0.1::8080", "-v", filepath.Join(dir, "nginx.conf")+":/etc/nginx/nginx.conf:ro", "nginx:1.28-alpine")
	base := "http://" + docker("port", proxy, "8080/tcp")
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		// Capture bounded, identity-free diagnostics before removing containers,
		// including failures which abort before the final telemetry assertions.
		for _, container := range containers[:3] {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out, err := exec.CommandContext(ctx, "docker", "logs", container).CombinedOutput()
			cancel()
			if err != nil {
				t.Log("load diagnostics unavailable", err)
				continue
			}
			lines := []string{}
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "Live activity metrics") || strings.Contains(line, "Live activity generation unavailable") {
					lines = append(lines, line)
				}
			}
			for _, line := range lines[max(0, len(lines)-3):] {
				t.Logf("%s %s", container, line)
			}
		}
	})
	client := &http.Client{Transport: &http.Transport{MaxIdleConns: 1024, MaxIdleConnsPerHost: 1024, ResponseHeaderTimeout: 10 * time.Second}}
	t.Cleanup(client.CloseIdleConnections)
	request := func(ctx context.Context, method, path, ip, credential, body string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("X-Load-IP", ip)
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return client.Do(req)
	}
	for i := range 3 {
		deadline := time.Now().Add(30 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			resp, err := request(ctx, "GET", fmt.Sprintf("/r%d/readyz", i), "198.51.100.1", "", "")
			cancel()
			ready := err == nil && resp.StatusCode == 200
			if resp != nil {
				resp.Body.Close()
			}
			if ready {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("replica not ready: %s", docker("logs", containers[i]))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("image=%s groups=%d app_limits=3x2GiB/1CPU proxy=128MiB/0.5CPU docker=%s", image, n, docker("info", "--format", "{{.NCPU}} CPUs {{.MemTotal}} bytes"))
	t.Logf("image_id=%s distinct_accounts=%t rich=%s", docker("image", "inspect", image, "--format", "{{.Id}}"), distinct, os.Getenv("LIVE_ACTIVITY_LOAD_RICH"))
	t.Logf("database=%s", docker("inspect", "ticketopia-db-mariadb-1", "--format", "{{.Config.Image}} memory={{.HostConfig.Memory}} nanocpus={{.HostConfig.NanoCpus}}"))
	profile.containers = []string{containers[0], proxy}
	// Probe ordinary traffic from its own container on the deployment network,
	// through the proxy. A host-side probe shares the test process and Docker's
	// host port forwarding with the 768 load-generator streams; on 2026-10-20
	// that path alone added ~2ms to write p95, even for an unmodified image.
	probe := filepath.Join(dir, "loadprobe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", probe, "./testdata/loadprobe")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+docker("version", "--format", "{{.Server.Arch}}"), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("load probe build: %v\n%s", err, out)
	}
	ordinary := func(label string) [2]time.Duration {
		profile.begin()
		phase := time.Now()
		defer func() { profile.end(label, time.Since(phase)) }()
		var result struct {
			Browse  time.Duration `json:"browse_p95"`
			Write   time.Duration `json:"write_p95"`
			Samples int           `json:"samples"`
		}
		out := docker("run", "--rm", "--network", network, "--cpus", "1", "--memory", "128m", "-e", "LOAD_PROBE_TOKEN="+token, "-v", probe+":/loadprobe:ro", "gcr.io/distroless/static-debian12:nonroot", "/loadprobe", "http://"+proxy+":8080/r0")
		// The result is the final line; a first run may precede it with pull output.
		if err := json.Unmarshal([]byte(out[strings.LastIndex(out, "\n")+1:]), &result); err != nil || result.Samples != 100 {
			t.Fatal("invalid load probe result", err)
		}
		return [2]time.Duration{result.Browse, result.Write}
	}
	baseline := ordinary("baseline")

	type frame struct {
		event    string
		snapshot activity.Snapshot
		at       time.Time
	}
	type stream struct {
		frames  chan frame
		cancel  context.CancelFunc
		started time.Time
		path    string
	}
	open := func(path, ip, credential string) (stream, error) {
		started := time.Now()
		ctx, cancel := context.WithCancel(t.Context())
		resp, err := request(ctx, "GET", path, ip, credential, "")
		if err != nil {
			cancel()
			return stream{}, err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			cancel()
			return stream{}, fmt.Errorf("stream status %d", resp.StatusCode)
		}
		ch := make(chan frame, 8)
		go func() {
			defer resp.Body.Close()
			defer close(ch)
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 4096), 2<<20)
			event := ""
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "event: ") {
					event = strings.TrimPrefix(line, "event: ")
				}
				if strings.HasPrefix(line, "data: ") {
					f := frame{event: event, at: time.Now()}
					if event == "activity.snapshot" {
						if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &f.snapshot) != nil {
							return
						}
					}
					select {
					case ch <- f:
					default:
					}
				}
			}
		}()
		return stream{frames: ch, cancel: cancel, started: started, path: path}, nil
	}
	wait := func(s stream, timeout time.Duration, match func(frame) bool) frame {
		t.Helper()
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		for {
			select {
			case f, ok := <-s.frames:
				if !ok {
					t.Fatal("stream closed before expected frame")
				}
				if match(f) {
					return f
				}
			case <-timer.C:
				t.Fatal("proxy frame deadline exceeded")
			}
		}
	}
	stats := func() {
		t.Log(docker(append([]string{"stats", "--no-stream", "--format", "{{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}}"}, containers...)...))
	}
	var all []stream
	var probes [3]stream
	defer func() {
		for _, s := range all {
			s.cancel()
		}
	}()
	var mutex sync.Mutex
	var wg sync.WaitGroup
	starts := make(chan struct{}, 24)
	for replica := range 3 {
		for tab := range 256 {
			starts <- struct{}{}
			wg.Go(func() {
				defer func() { <-starts }()
				s, err := open(fmt.Sprintf("/r%d/api/v1/community/activity/stream", replica), fmt.Sprintf("198.18.%d.%d", replica, tab), "")
				if err != nil {
					t.Error(err)
					return
				}
				mutex.Lock()
				all = append(all, s)
				if probes[replica].cancel == nil {
					probes[replica] = s
				}
				mutex.Unlock()
			})
		}
	}
	wg.Wait()
	if len(all) != 768 {
		t.Log(docker("logs", proxy))
		t.Fatalf("opened %d/768 streams", len(all))
	}
	initialLatencies := []time.Duration{}
	for _, s := range all {
		f := wait(s, 5*time.Second, func(f frame) bool { return f.event == "activity.snapshot" && len(f.snapshot.Items) == min(20, n) })
		initialLatencies = append(initialLatencies, f.at.Sub(s.started))
	}
	slices.Sort(initialLatencies)
	t.Logf("initial public snapshot p95=%s max=%s (cold and warm handshakes)", initialLatencies[(len(initialLatencies)*95-1)/100], initialLatencies[len(initialLatencies)-1])
	// Capacity rejection is distinct from shared-IP start rejection.
	resp, err := request(t.Context(), "GET", "/r0/api/v1/community/activity/stream", "203.0.113.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("skewed full replica returned %d", resp.StatusCode)
	}
	t.Log("768 concurrent streams and skewed replica rejection passed")
	// Compare steady-state browsing after the handshake burst has settled. The
	// heartbeat also verifies actual proxy flushing on each replica.
	for _, s := range probes {
		wait(s, 20*time.Second, func(f frame) bool { return f.event == "heartbeat" })
	}
	loaded := ordinary("loaded")
	for i, label := range []string{"browse", "write"} {
		t.Logf("ordinary %s p95 baseline=%s loaded=%s ratio=%.3f samples=100 (in-network probe via proxy)", label, baseline[i], loaded[i], float64(loaded[i])/float64(baseline[i]))
		if loaded[i] > baseline[i]*11/10 {
			t.Errorf("ordinary %s exceeds 10%% regression gate", label)
		}
	}
	if os.Getenv("LIVE_ACTIVITY_LOAD_PHASE") == "ordinary" {
		// Profiling iterations stop after the regression gate; this is never a
		// complete scenario result.
		t.Log("stopping after ordinary-traffic gate (LIVE_ACTIVITY_LOAD_PHASE=ordinary)")
		return
	}
	for range 60 {
		resp, err := request(t.Context(), "GET", "/r0/api/v1/community/activity", "203.0.113.2", "", "")
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("shared IP prematurely limited: %d", resp.StatusCode)
		}
	}
	resp, err = request(t.Context(), "GET", "/r0/api/v1/community/activity", "203.0.113.2", "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "60" {
		t.Fatal("shared-IP limit not enforced")
	}
	t.Log("shared-IP 60/minute and Retry-After passed")
	stats()
	// Record conservative write-start-to-visible time, including HTTP commit.
	latencies := []time.Duration{}
	for i := range 5 {
		started := time.Now()
		detail := savedDetail(fmt.Sprintf("LoadPublication_%d", i))
		writer := p.Recommendations()
		writer.EnableLiveActivity(true)
		if n == activity.MaxCandidates && i == 0 {
			// Keep the healthy workload within the cap while adding a new group.
			// The separate overflow regression exercises cap+1 explicitly.
			if err := writer.Remove(t.Context(), owner.ID, fmt.Sprintf("ticketmaster:LiveFilter_%d", n-1)); err != nil {
				t.Fatal(err)
			}
			if contributorID != "" {
				if err := writer.Remove(t.Context(), contributorID, fmt.Sprintf("ticketmaster:LiveFilter_%d", n-1)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, _, err := writer.Set(t.Context(), owner.ID, detail, ""); err != nil {
			t.Fatal(err)
		}
		for _, s := range all {
			f := wait(s, 5*time.Second, func(f frame) bool {
				return f.event == "activity.snapshot" && len(f.snapshot.Items) > 0 && f.snapshot.Items[0].EventID == detail.Item.ID
			})
			latencies = append(latencies, f.at.Sub(started))
		}
		if err := writer.Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
			t.Fatal(err)
		}
		started = time.Now()
		for _, s := range all {
			wait(s, 5*time.Second, func(f frame) bool {
				if f.event != "activity.snapshot" {
					return false
				}
				for _, item := range f.snapshot.Items {
					if item.EventID == detail.Item.ID {
						return false
					}
				}
				return true
			})
		}
		if time.Since(started) > 5*time.Second {
			t.Error("withdrawal exceeded five seconds")
		}
	}
	slices.Sort(latencies)
	p95 := latencies[(len(latencies)*95-1)/100]
	t.Logf("publication p95=%s observations=%d", p95, len(latencies))
	if p95 > 5*time.Second {
		t.Error("publication latency gate failed")
	}
	// Deliberately skew starts toward a full replica while another has spare
	// capacity. A deployment-wide total below 768 must not bypass a local cap.
	freed := 0
	for _, s := range all {
		if freed < 64 && strings.HasPrefix(s.path, "/r2/") {
			s.cancel()
			freed++
		}
	}
	if freed != 64 {
		t.Fatal("missing skew fixture streams")
	}
	for i := range 64 {
		resp, err := request(t.Context(), "GET", "/r0/api/v1/community/activity/stream", fmt.Sprintf("198.20.0.%d", i), "", "")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 503 {
			t.Fatalf("skewed full replica returned %d", resp.StatusCode)
		}
	}
	for i := range 64 {
		deadline := time.Now().Add(2 * time.Second)
		for {
			s, err := open("/r2/api/v1/community/activity/stream", fmt.Sprintf("198.21.0.%d", i), "")
			if err == nil {
				all = append(all, s)
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("spare replica did not accept replacement stream", err)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	t.Log("64 skewed starts rejected on full replica while spare replica accepted 64 replacements")
	for _, s := range all {
		s.cancel()
	}
	all = nil
	// Duplicate private tabs share one account's source bitmaps on each replica.
	for i := range 3 {
		for tab := range 256 {
			s, err := open(fmt.Sprintf("/r%d/api/v1/me/community/activity/stream", i), fmt.Sprintf("198.19.%d.%d", i, tab), privateTokens[tab])
			if err != nil {
				t.Fatal(err)
			}
			all = append(all, s)
			wait(s, 5*time.Second, func(f frame) bool { return f.event == "activity.snapshot" && len(f.snapshot.Items) == 0 })
		}
	}
	for _, method := range []string{"PUT", "DELETE"} {
		body := ""
		want := 0
		if method == "PUT" {
			body = `{"visibility":"private"}`
			want = 1
		}
		started := time.Now()
		writes := 1
		if distinct {
			writes = 256
		}
		for tab := range writes {
			starts <- struct{}{}
			wg.Go(func() {
				defer func() { <-starts }()
				resp, err := request(t.Context(), method, "/r0/api/v1/me/event-interests/ticketmaster:LiveFilter_0", "203.0.113.20", privateTokens[tab], body)
				if err != nil {
					t.Error(err)
					return
				}
				data, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 300 {
					t.Errorf("membership write %d %s", resp.StatusCode, data)
				}
			})
		}
		wg.Wait()
		// Local invalidation intentionally clears and closes the stream. Exercise
		// the same quiet reconnect as the browser; remote replicas use revisions.
		for tab := range 256 {
			starts <- struct{}{}
			wg.Go(func() {
				defer func() { <-starts }()
				all[tab].cancel()
				s, err := open("/r0/api/v1/me/community/activity/stream", fmt.Sprintf("198.19.0.%d", tab), privateTokens[tab])
				if err != nil {
					t.Error(err)
					return
				}
				all[tab] = s
			})
		}
		wg.Wait()
		membershipLatencies := []time.Duration{}
		for _, s := range all {
			f := wait(s, 5*time.Second, func(f frame) bool { return f.event == "activity.snapshot" && len(f.snapshot.Items) == want })
			membershipLatencies = append(membershipLatencies, f.at.Sub(started))
		}
		slices.Sort(membershipLatencies)
		p95 := membershipLatencies[(len(membershipLatencies)*95-1)/100]
		t.Logf("membership %s p95=%s all replicas=%s", method, p95, time.Since(started))
		if p95 > 5*time.Second {
			t.Error("membership latency gate failed")
		}
	}
	// Fail SQL for this isolated fixture only. Failure must clear private views,
	// and a later fallback/reconnect must rebuild rather than reuse old output.
	execSQL(t, f.admin, fmt.Sprintf("REVOKE SELECT ON %s.* FROM '%s'@'%%'", f.runtime.Database, f.runtime.User))
	// MariaDB caches database-level grants on established sessions. Disconnect
	// only this fixture's runtime sessions so new reads observe the revoked grant.
	disconnect := func() {
		rows, err := f.admin.QueryContext(t.Context(), "SELECT ID FROM information_schema.PROCESSLIST WHERE USER=?", f.runtime.User)
		if err != nil {
			t.Fatal(err)
		}
		connections := []uint64{}
		for rows.Next() {
			var id uint64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			connections = append(connections, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		for _, id := range connections {
			execSQL(t, f.admin, fmt.Sprintf("KILL CONNECTION %d", id))
		}
	}
	disconnect()
	for _, s := range all {
		wait(s, 5*time.Second, func(f frame) bool { return f.event == "unavailable" })
	}
	execSQL(t, f.admin, fmt.Sprintf("GRANT SELECT ON %s.* TO '%s'@'%%'", f.runtime.Database, f.runtime.User))
	disconnect()
	for i := range len(all) {
		all[i].cancel()
		deadline := time.Now().Add(5 * time.Second)
		for {
			s, err := open(fmt.Sprintf("/r%d/api/v1/me/community/activity/stream", i/256), fmt.Sprintf("198.19.%d.%d", i/256, i%256), privateTokens[i%256])
			if err == nil {
				all[i] = s
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("SQL recovery failed", err)
			}
			time.Sleep(100 * time.Millisecond)
		}
		wait(all[i], 5*time.Second, func(f frame) bool { return f.event == "activity.snapshot" && len(f.snapshot.Items) == 0 })
	}
	t.Log("SQL failure clears views; quiet reconnect rebuilds all replicas")
	// Drain an actual process; the proxy must deliver EOF rather than hang until
	// its read timeout. Other replicas remain usable.
	docker("stop", "--time", "10", containers[0])
	drain := time.NewTimer(5 * time.Second)
draining:
	for {
		select {
		case _, ok := <-all[0].frames:
			if !ok {
				break draining
			}
		case <-drain.C:
			t.Fatal("drain did not close stream")
		}
	}
	drain.Stop()
	resp, err = request(t.Context(), "GET", "/r1/api/v1/me/community/activity", "203.0.113.21", privateToken, "")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("healthy replica unavailable after drain")
	}
	for i := range 3 {
		logs := docker("logs", containers[i])
		if strings.Contains(logs, token) || strings.Contains(logs, privateToken) || strings.Contains(logs, name) {
			t.Fatal("sensitive data in operational logs")
		}
		for _, credential := range privateTokens {
			if strings.Contains(logs, credential) {
				t.Fatal("private credential in operational logs")
			}
		}
		for _, line := range strings.Split(logs, "\n") {
			if strings.Contains(line, "Live activity metrics") {
				var record struct {
					Activity activity.Stats `json:"activity"`
				}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal("invalid metric record", err)
				}
				s := record.Activity
				if s.Bytes > activity.MemoryBudget || s.SQLJobs > 2 || s.Pending > 64 || s.Cohorts > 384 {
					t.Fatal("resource bound exceeded", s)
				}
				if s.Streams == 256 && s.Owners == 0 && s.SQLJobs == 0 && s.Pending == 0 {
					// Public duplicate tabs cannot multiply projection SQL. Counts
					// have no subscriptions in this phase; only cleanup adds a job.
					maxGroups := min(n+1, activity.MaxCandidates)
					maxSQL := s.Operations["projection"].Count*uint64(2+(maxGroups+255)/256) + s.Operations["cleanup"].Count
					if s.SQLStatements > maxSQL {
						t.Error("duplicate tabs multiplied SQL", s.SQLStatements, maxSQL)
					}
					if s.CacheHits < 256 {
						t.Error("public cohort was not shared")
					}
				}
				t.Logf("replica=%d %s", i, line)
			}
		}
	}
}
