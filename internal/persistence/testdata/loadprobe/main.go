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
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 2 || os.Getenv("LOAD_PROBE_TOKEN") == "" {
		fmt.Fprintln(os.Stderr, "usage: LOAD_PROBE_TOKEN=... loadprobe BASE_URL")
		os.Exit(2)
	}
	base, token := os.Args[1], os.Getenv("LOAD_PROBE_TOKEN")
	client := &http.Client{Timeout: 10 * time.Second}
	var samples [2][]time.Duration
	for i := range 105 {
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
			if err != nil {
				fail(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				fail(fmt.Errorf("ordinary traffic %s: %d", method, resp.StatusCode))
			}
			if i >= 5 {
				samples[operation] = append(samples[operation], time.Since(started))
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	var p95 [2]time.Duration
	for i := range samples {
		slices.Sort(samples[i])
		p95[i] = samples[i][(len(samples[i])*95-1)/100]
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"browse_p95": p95[0], "write_p95": p95[1], "samples": len(samples[0])})
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
