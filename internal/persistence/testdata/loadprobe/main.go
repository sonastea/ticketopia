// Command loadprobe measures ordinary browse/write p95 for the opt-in live
// activity deployment fixture. It runs as its own container on the deployment
// network, so the fixture's 768 load-generator streams and the host's Docker
// port forwarding are not part of the measured request path.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// sample is one measured request. Failed requests carry Error and no latency
// is counted toward the percentiles.
type sample struct {
	Started time.Time     `json:"started"`
	Latency time.Duration `json:"latency"`
	Error   string        `json:"error,omitempty"`
}

func main() {
	if len(os.Args) != 2 || os.Getenv("LOAD_PROBE_TOKEN") == "" {
		fmt.Fprintln(os.Stderr, "usage: LOAD_PROBE_TOKEN=... [LOAD_PROBE_ITERATIONS=100] loadprobe BASE_URL")
		os.Exit(2)
	}
	base, token := os.Args[1], os.Getenv("LOAD_PROBE_TOKEN")
	iterations := 100
	if value := os.Getenv("LOAD_PROBE_ITERATIONS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 20 {
			fail(fmt.Errorf("LOAD_PROBE_ITERATIONS must be at least 20"))
		}
		iterations = n
	}
	client := &http.Client{Timeout: 10 * time.Second}
	var samples [2][]time.Duration
	var timed [2][]sample
	errors := 0
	for i := range iterations + 5 {
		for operation := range 2 {
			method, path, body, credential := "GET", "/community", "", ""
			if operation == 1 {
				method, path, credential = "PUT", "/api/v1/me/event-interests/ticketmaster:LiveFilter_0", token
				body = `{"visibility":"private"}`
				if i%2 == 0 {
					method, body = "DELETE", ""
				}
			}
			req, err := http.NewRequest(method, base+path, strings.NewReader(body))
			if err != nil {
				fail(err)
			}
			req.Header.Set("X-Load-IP", "203.0.113.90")
			if credential != "" {
				req.Header.Set("Authorization", "Bearer "+credential)
			}
			if body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			started := time.Now()
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode >= 300 {
					err = fmt.Errorf("ordinary traffic %s: %d", method, resp.StatusCode)
				}
			}
			elapsed := time.Since(started)
			// The first five iterations warm connections and are not measured.
			if i < 5 {
				if err != nil {
					fail(err)
				}
				continue
			}
			if err != nil {
				// Record and continue so callers can report error counts.
				errors++
				timed[operation] = append(timed[operation], sample{Started: started, Error: err.Error()})
				continue
			}
			samples[operation] = append(samples[operation], elapsed)
			timed[operation] = append(timed[operation], sample{Started: started, Latency: elapsed})
		}
		time.Sleep(50 * time.Millisecond)
	}
	var p95 [2]time.Duration
	raw := [2][]time.Duration{slices.Clone(samples[0]), slices.Clone(samples[1])}
	for i := range samples {
		if len(samples[i]) == 0 {
			continue
		}
		slices.Sort(samples[i])
		p95[i] = samples[i][(len(samples[i])*95-1)/100]
	}
	// Raw samples let long soaks pool several runs instead of averaging p95s;
	// timed samples keep start times and errors for temporal analysis.
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"browse_p95": p95[0], "write_p95": p95[1], "samples": iterations, "errors": errors,
		"browse_samples": raw[0], "write_samples": raw[1], "browse_timed": timed[0], "write_timed": timed[1]})
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
