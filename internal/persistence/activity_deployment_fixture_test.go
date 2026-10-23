package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// activityDeployment is the opt-in deployment fixture shared by the load and
// soak gates: the production image in three containers (2 GiB/one CPU each), a
// real nginx proxy and the isolated real-MariaDB schema. nginx supplies
// controlled client IPs on its loopback-only test listener; never copy that
// header policy into a deployment proxy.
type activityDeployment struct {
	t          *testing.T
	network    string
	proxy      string
	base       string
	containers []string // three apps, then the proxy
	client     *http.Client
	profile    *loadProfile
	probe      string
	traceDir   string // set when r0 runs the trace-capable build
	traces     int
	// ablationDir is mounted into every replica (LIVE_ACTIVITY_ABLATION only).
	ablationDir string
	ablationSeq int
	probeLines  int // probe log lines already returned by probeLog
}

// loadProbeIP is testdata/loadprobe's X-Load-IP; nginx logs only its requests.
const loadProbeIP = "203.0.113.90"

const probeLogPath = "/tmp/probe.log"

// probeLogEntry is one probe request as nginx saw it. Times have nginx's
// millisecond resolution; upstream values are -1 when not reported.
type probeLogEntry struct {
	End                                    time.Time
	Method                                 string
	Status                                 int
	Request, Connect, Header, ResponseTime time.Duration
}

// probeLog returns probe requests logged since the previous call, in order.
func (d *activityDeployment) probeLog() []probeLogEntry {
	d.t.Helper()
	out := d.docker("exec", d.proxy, "cat", probeLogPath)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if out == "" {
		lines = nil
	}
	entries := []probeLogEntry{}
	seconds := func(value string) time.Duration {
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return -1 // "-" when nginx did not reach the upstream
		}
		return time.Duration(v * float64(time.Second))
	}
	for _, line := range lines[min(d.probeLines, len(lines)):] {
		fields := strings.Fields(line)
		if len(fields) != 7 {
			d.t.Fatalf("unexpected probe log line %q", line)
		}
		end, _ := strconv.ParseFloat(fields[0], 64)
		status, _ := strconv.Atoi(fields[2])
		entries = append(entries, probeLogEntry{time.UnixMilli(int64(end*1000 + 0.5)), fields[1], status,
			seconds(fields[3]), seconds(fields[4]), seconds(fields[5]), seconds(fields[6])})
	}
	d.probeLines = len(lines)
	return entries
}

// traceStart begins an execution trace on r0 (LIVE_ACTIVITY_LOAD_TRACE only).
func (d *activityDeployment) traceStart() {
	d.t.Helper()
	d.docker("kill", "--signal", "USR1", d.containers[0])
	d.traces++
}

// traceStop ends the current trace and copies it to dest.
func (d *activityDeployment) traceStop(dest string) {
	d.t.Helper()
	d.docker("kill", "--signal", "USR2", d.containers[0])
	source := fmt.Sprintf("%s:/tmp/%d.trace", d.containers[0], d.traces)
	deadline := time.Now().Add(30 * time.Second)
	for {
		out, err := exec.CommandContext(d.t.Context(), "docker", "cp", source, dest).CombinedOutput()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			d.t.Fatalf("trace copy: %v %s", err, out)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// setAblation switches every replica's live-activity repository mode and waits
// until each has logged applying it (LIVE_ACTIVITY_ABLATION only).
func (d *activityDeployment) setAblation(mode string) {
	d.t.Helper()
	d.ablationSeq++
	since := time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
	path := filepath.Join(d.ablationDir, "mode")
	if err := os.WriteFile(path+".tmp", []byte(fmt.Sprintf("%d %s\n", d.ablationSeq, mode)), 0644); err != nil {
		d.t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		d.t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for _, container := range d.containers[:3] {
		for {
			applied := false
			for _, line := range strings.Split(d.docker("logs", "--since", since, container), "\n") {
				var record struct {
					Seq     int    `json:"seq"`
					Mode    string `json:"mode"`
					Applied bool   `json:"applied"`
				}
				if strings.Contains(line, "Activity ablation mode") && json.Unmarshal([]byte(line), &record) == nil && record.Seq == d.ablationSeq {
					if !record.Applied || record.Mode != mode {
						d.t.Fatalf("%s rejected ablation mode %q: %s", container, mode, line)
					}
					applied = true
				}
			}
			if applied {
				break
			}
			if time.Now().After(deadline) {
				d.t.Fatalf("%s did not apply ablation mode %q (seq %d)", container, mode, d.ablationSeq)
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
}

type loadProbeResult struct {
	Browse        time.Duration   `json:"browse_p95"`
	Write         time.Duration   `json:"write_p95"`
	Samples       int             `json:"samples"`
	BrowseSamples []time.Duration `json:"browse_samples"`
	WriteSamples  []time.Duration `json:"write_samples"`
	Errors        int             `json:"errors"`
	BrowseTimed   []probeSample   `json:"browse_timed"`
	WriteTimed    []probeSample   `json:"write_timed"`
}

// probeSample is one measured probe request; failures have Error set.
type probeSample struct {
	Started time.Time     `json:"started"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"`
}

// startActivityDeployment starts the replicas and proxy. extraServer is added to
// the proxy's listener block (for example a stub_status location).
func startActivityDeployment(t *testing.T, f mariaFixture, image, extraServer string) *activityDeployment {
	t.Helper()
	d := &activityDeployment{t: t}
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
	d.network = os.Getenv("LIVE_ACTIVITY_LOAD_NETWORK")
	if d.network == "" {
		d.network = "ticketopia-db_default"
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
	d.profile = newLoadProfile(t, f, "ticketopia-db-mariadb-1")
	t.Cleanup(func() {
		for i := len(d.containers) - 1; i >= 0; i-- {
			cmd := exec.Command("docker", "rm", "-f", d.containers[i])
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("container cleanup: %v %s", err, out)
			}
		}
	})
	// LIVE_ACTIVITY_LOAD_TRACE=true runs r0 (the probed replica) as the same
	// source built with -tags activitytrace, whose execution trace the caller
	// starts and stops per period (traceStart/traceStop). Not for gate results.
	// LIVE_ACTIVITY_ABLATION=true runs all three replicas as the same source
	// built with -tags activityablation, switched per period through a mounted
	// mode file (setAblation). Not for gate results.
	tags := []string{}
	if os.Getenv("LIVE_ACTIVITY_LOAD_TRACE") == "true" {
		tags = append(tags, "activitytrace")
		d.traceDir = dir
	}
	if os.Getenv("LIVE_ACTIVITY_ABLATION") == "true" {
		tags = append(tags, "activityablation")
		d.ablationDir = filepath.Join(dir, "ablation")
		if err := os.Mkdir(d.ablationDir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	var diagnostic []string
	if len(tags) > 0 {
		binary := filepath.Join(dir, "ticketopia-diagnostic")
		build := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-tags", strings.Join(tags, ","), "-o", binary, "../../cmd/ticketopia")
		build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+d.docker("version", "--format", "{{.Server.Arch}}"), "CGO_ENABLED=0")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("diagnostic build (%s): %v\n%s", strings.Join(tags, ","), err, out)
		}
		diagnostic = []string{"-v", binary + ":/diagnostic/ticketopia:ro", "--entrypoint", "/diagnostic/ticketopia"}
	}
	for i := range 3 {
		id := prefix + "-" + strconv.Itoa(i)
		d.containers = append(d.containers, id)
		args := []string{"run", "-d", "--name", id, "--network", d.network, "--memory", "2g", "--cpus", "1", "--env-file", filepath.Join(dir, "app.env"), "-v", filepath.Join(dir, "ca.crt") + ":/load/ca.crt:ro"}
		// The trace build traces only r0; ablation applies to every replica,
		// since they share the database whose contention is being measured.
		if d.ablationDir != "" || (i == 0 && d.traceDir != "") {
			args = append(args, diagnostic...)
		}
		if i == 0 && d.traceDir != "" {
			args = append(args, "-e", "TICKETOPIA_TRACE_DIR=/tmp")
		}
		if d.ablationDir != "" {
			args = append(args, "-e", "TICKETOPIA_ACTIVITY_ABLATION_FILE=/ablation/mode", "-v", d.ablationDir+":/ablation:ro")
		}
		d.docker(append(args, image)...)
	}
	// Only the in-network probe's requests are logged, with proxy and upstream
	// timings, so the log costs the same in loaded and no-live periods.
	config := "worker_rlimit_nofile 8192; events { worker_connections 4096; }\nhttp { access_log off; map $http_x_load_ip $probe_request { " + loadProbeIP + " 1; default 0; } " +
		"log_format probe '$msec $request_method $status $request_time $upstream_connect_time $upstream_header_time $upstream_response_time'; upstream apps {"
	for i := range 3 {
		config += fmt.Sprintf("server %s-%d:8080;", prefix, i)
	}
	config += "} server { listen 8080; access_log " + probeLogPath + " probe if=$probe_request; proxy_http_version 1.1; proxy_set_header Connection ''; proxy_set_header X-Forwarded-For $http_x_load_ip; proxy_buffering off; proxy_read_timeout 40s;"
	for i := range 3 {
		config += fmt.Sprintf("location /r%d/ { proxy_pass http://%s-%d:8080/; }", i, prefix, i)
	}
	config += extraServer + "location / { proxy_pass http://apps; } } }"
	if err := os.WriteFile(filepath.Join(dir, "nginx.conf"), []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	d.proxy = prefix + "-proxy"
	d.containers = append(d.containers, d.proxy)
	d.docker("run", "-d", "--name", d.proxy, "--network", d.network, "--memory", "128m", "--cpus", "0.5", "--ulimit", "nofile=8192:8192", "-p", "127.0.0.1::8080", "-v", filepath.Join(dir, "nginx.conf")+":/etc/nginx/nginx.conf:ro", "nginx:1.28-alpine")
	d.base = "http://" + d.docker("port", d.proxy, "8080/tcp")
	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		// Capture bounded, identity-free diagnostics before removing containers,
		// including failures which abort before the final telemetry assertions.
		for _, container := range d.containers[:3] {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			out, err := exec.CommandContext(ctx, "docker", "logs", container).CombinedOutput()
			cancel()
			if err != nil {
				t.Log("load diagnostics unavailable", err)
				continue
			}
			lines, warnings := []string{}, []string{}
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "Live activity metrics") || strings.Contains(line, "Live activity generation unavailable") {
					lines = append(lines, line)
				}
				// Operational logs carry no identities or filter values.
				if strings.Contains(line, `"level":"warn"`) || strings.Contains(line, `"level":"error"`) {
					warnings = append(warnings, line)
				}
			}
			for _, line := range lines[max(0, len(lines)-3):] {
				t.Logf("%s %s", container, line)
			}
			for _, line := range warnings[max(0, len(warnings)-10):] {
				t.Logf("%s warning %s", container, line)
			}
		}
	})
	d.client = &http.Client{Transport: &http.Transport{MaxIdleConns: 1024, MaxIdleConnsPerHost: 1024, ResponseHeaderTimeout: 10 * time.Second}}
	t.Cleanup(d.client.CloseIdleConnections)
	for i := range 3 {
		deadline := time.Now().Add(30 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			resp, err := d.request(ctx, "GET", fmt.Sprintf("/r%d/readyz", i), "198.51.100.1", "", "")
			cancel()
			ready := err == nil && resp.StatusCode == 200
			if resp != nil {
				resp.Body.Close()
			}
			if ready {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("replica not ready: %s", d.docker("logs", d.containers[i]))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	t.Logf("image=%s app_limits=3x2GiB/1CPU proxy=128MiB/0.5CPU docker=%s", image, d.docker("info", "--format", "{{.NCPU}} CPUs {{.MemTotal}} bytes"))
	t.Logf("image_id=%s", d.docker("image", "inspect", image, "--format", "{{.Id}}"))
	t.Logf("database=%s", d.docker("inspect", "ticketopia-db-mariadb-1", "--format", "{{.Config.Image}} memory={{.HostConfig.Memory}} nanocpus={{.HostConfig.NanoCpus}}"))
	d.profile.containers = []string{d.containers[0], d.proxy}
	// Probe ordinary traffic from its own container on the deployment network,
	// through the proxy. A host-side probe shares the test process and Docker's
	// host port forwarding with the load-generator streams; on 2026-10-20 that
	// path alone added ~2ms to write p95, even for an unmodified image.
	d.probe = filepath.Join(dir, "loadprobe")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", d.probe, "./testdata/loadprobe")
	build.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+d.docker("version", "--format", "{{.Server.Arch}}"), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("load probe build: %v\n%s", err, out)
	}
	return d
}

func (d *activityDeployment) docker(args ...string) string {
	d.t.Helper()
	cmd := exec.CommandContext(d.t.Context(), "docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		d.t.Fatalf("docker %s: %v\n%s", args[0], err, out)
	}
	return strings.TrimSpace(string(out))
}

func (d *activityDeployment) request(ctx context.Context, method, path, ip, credential, body string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, strings.NewReader(body))
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
	return d.client.Do(req)
}

// ordinary measures browse/write p95 from the in-network probe container.
func (d *activityDeployment) ordinary(label, token string) loadProbeResult {
	d.t.Helper()
	result := d.probeRun(label, token, 100)
	if result.Errors > 0 || len(result.BrowseSamples) != 100 || len(result.WriteSamples) != 100 {
		d.t.Fatalf("ordinary traffic failed: %d errors", result.Errors)
	}
	return result
}

// probeRun runs the in-network probe for iterations measured browse/write pairs
// and returns its samples, including failed requests, without judging them.
func (d *activityDeployment) probeRun(label, token string, iterations int) loadProbeResult {
	d.t.Helper()
	d.profile.begin()
	phase := time.Now()
	defer func() { d.profile.end(label, time.Since(phase)) }()
	var result loadProbeResult
	out := d.docker("run", "--rm", "--network", d.network, "--cpus", "1", "--memory", "128m", "-e", "LOAD_PROBE_TOKEN="+token, "-e", "LOAD_PROBE_ITERATIONS="+strconv.Itoa(iterations),
		"-v", d.probe+":/loadprobe:ro", "gcr.io/distroless/static-debian12:nonroot", "/loadprobe", "http://"+d.proxy+":8080/r0")
	// The result is the final line; a first run may precede it with pull output.
	if err := json.Unmarshal([]byte(out[strings.LastIndex(out, "\n")+1:]), &result); err != nil || result.Samples != iterations {
		d.t.Fatal("invalid load probe result", err)
	}
	return result
}
