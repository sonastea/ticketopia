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
	for i := range 3 {
		id := prefix + "-" + strconv.Itoa(i)
		d.containers = append(d.containers, id)
		d.docker("run", "-d", "--name", id, "--network", d.network, "--memory", "2g", "--cpus", "1", "--env-file", filepath.Join(dir, "app.env"), "-v", filepath.Join(dir, "ca.crt")+":/load/ca.crt:ro", image)
	}
	config := "worker_rlimit_nofile 8192; events { worker_connections 4096; }\nhttp { access_log off; upstream apps {"
	for i := range 3 {
		config += fmt.Sprintf("server %s-%d:8080;", prefix, i)
	}
	config += "} server { listen 8080; proxy_http_version 1.1; proxy_set_header Connection ''; proxy_set_header X-Forwarded-For $http_x_load_ip; proxy_buffering off; proxy_read_timeout 40s;"
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
