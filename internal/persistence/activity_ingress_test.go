package persistence

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Opt-in verification of the actual staging/deployment ingress (rollout P1.5).
// It sends real traffic to LIVE_ACTIVITY_INGRESS_URL: run it only against an
// environment you operate, with live activity enabled there. It needs no
// database or credentials; drain checks need an operator to restart replicas.
func TestLiveActivityIngress(t *testing.T) {
	base := strings.TrimSuffix(os.Getenv("LIVE_ACTIVITY_INGRESS_URL"), "/")
	if base == "" {
		t.Skip("set LIVE_ACTIVITY_INGRESS_URL to an operated staging ingress")
	}
	replicas := 3
	if v := os.Getenv("LIVE_ACTIVITY_INGRESS_REPLICAS"); v != "" {
		var err error
		if replicas, err = strconv.Atoi(v); err != nil || replicas < 1 {
			t.Fatal("LIVE_ACTIVITY_INGRESS_REPLICAS must be a positive integer")
		}
	}
	hold := 2 * time.Minute
	if v := os.Getenv("LIVE_ACTIVITY_INGRESS_HOLD"); v != "" {
		var err error
		if hold, err = time.ParseDuration(v); err != nil {
			t.Fatal("LIVE_ACTIVITY_INGRESS_HOLD must be a duration")
		}
	}
	type frame struct {
		event string
		at    time.Time
	}
	// stream opens one SSE connection and reports frames until it ends.
	stream := func(ctx context.Context, client *http.Client) (*http.Response, <-chan frame, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/community/activity/stream", nil)
		if err != nil {
			return nil, nil, err
		}
		// Invite compression and caching transforms; the ingress must refuse both.
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Accept-Encoding", "gzip, br")
		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			return resp, nil, fmt.Errorf("stream status %d", resp.StatusCode)
		}
		frames := make(chan frame, 64)
		go func() {
			defer close(frames)
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			scanner.Buffer(make([]byte, 4096), 2<<20)
			event := ""
			for scanner.Scan() {
				line := scanner.Text()
				if v, ok := strings.CutPrefix(line, "event: "); ok {
					event = v
				}
				if strings.HasPrefix(line, "data: ") {
					select {
					case frames <- frame{event, time.Now()}:
					default:
					}
				}
			}
		}()
		return resp, frames, nil
	}
	next := func(frames <-chan frame, timeout time.Duration) (frame, bool) {
		select {
		case f, ok := <-frames:
			return f, ok
		case <-time.After(timeout):
			return frame{}, false
		}
	}
	protocols := map[string]*http.Client{
		"default": {Transport: &http.Transport{ForceAttemptHTTP2: true, DisableCompression: true}},
		// An empty TLSNextProto map disables HTTP/2 negotiation.
		"http1.1": {Transport: &http.Transport{TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}, DisableCompression: true}},
	}
	for name, client := range protocols {
		t.Run("flush/"+name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), hold+time.Minute)
			defer cancel()
			started := time.Now()
			resp, frames, err := stream(ctx, client)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("protocol=%s status=%d content-type=%q content-encoding=%q cache-control=%q", resp.Proto, resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Encoding"), resp.Header.Get("Cache-Control"))
			if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") || resp.Header.Get("Content-Encoding") != "" {
				t.Error("ingress transformed the event stream (content type or compression)")
			}
			first, ok := next(frames, 10*time.Second)
			if !ok || first.event != "activity.snapshot" {
				t.Fatal("initial snapshot was not flushed within 10s")
			}
			t.Logf("initial snapshot after %s", first.at.Sub(started))
			if first.at.Sub(started) > 2*time.Second {
				t.Error("initial snapshot took longer than 2s through the ingress")
			}
			// Heartbeats arrive every fifteen seconds; buffering shows as late or
			// batched frames, and a short idle timeout as a premature end.
			last, beats := first.at, 0
			for time.Since(started) < hold {
				f, ok := next(frames, 20*time.Second)
				if !ok {
					t.Fatalf("stream stalled or closed after %s (idle timeout or buffering)", time.Since(started).Round(time.Second))
				}
				if f.event != "heartbeat" {
					continue
				}
				if gap := f.at.Sub(last); beats > 0 && (gap < 13*time.Second || gap > 17*time.Second) {
					t.Errorf("heartbeat interval %s (expected 15s): proxy buffering", gap)
				}
				last = f.at
				beats++
			}
			t.Logf("held %s with %d heartbeats", hold, beats)
		})
	}
	t.Run("shared-ip-limit", func(t *testing.T) {
		// Spoofed forwarding headers must not create new limiter identities. Each
		// replica limits separately, so up to replicas×60 starts may succeed.
		client := &http.Client{Timeout: 10 * time.Second}
		limitedAt := 0
		for i := 1; i <= replicas*60+1; i++ {
			req, _ := http.NewRequestWithContext(t.Context(), "GET", base+"/api/v1/community/activity", nil)
			spoof := fmt.Sprintf("203.0.113.%d", i%250+1)
			req.Header.Set("X-Forwarded-For", spoof)
			req.Header.Set("X-Real-IP", spoof)
			req.Header.Set("Forwarded", "for="+spoof)
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 429 {
				if resp.Header.Get("Retry-After") != "60" {
					t.Error("429 without Retry-After: 60")
				}
				limitedAt = i
				break
			}
			if resp.StatusCode != 200 {
				t.Fatalf("snapshot %d returned %d", i, resp.StatusCode)
			}
		}
		t.Logf("first 429 at request %d (replicas=%d)", limitedAt, replicas)
		if limitedAt == 0 || limitedAt < 61 {
			t.Error("trusted client-IP extraction failed: spoofed headers bypassed the limit, or the limit fired early")
		}
		t.Log("also verify from a second network that a fresh client is not limited (shared proxy IP misconfiguration)")
	})
	wait := os.Getenv("LIVE_ACTIVITY_INGRESS_DRAIN_WAIT")
	if wait == "" {
		t.Log("drain/replacement routing skipped: set LIVE_ACTIVITY_INGRESS_DRAIN_WAIT and restart replicas during the window")
		return
	}
	window, err := time.ParseDuration(wait)
	if err != nil {
		t.Fatal("LIVE_ACTIVITY_INGRESS_DRAIN_WAIT must be a duration")
	}
	t.Run("drain", func(t *testing.T) {
		t.Logf("restart or replace replicas now (for example kubectl rollout restart); observing for %s", window)
		ctx, cancel := context.WithTimeout(t.Context(), window)
		defer cancel()
		client := protocols["default"]
		var mu sync.Mutex
		ends, reconnects, stalls, failed := 0, 0, 0, 0
		var wg sync.WaitGroup
		for range 6 {
			wg.Go(func() {
				for ctx.Err() == nil {
					connection, cancelConnection := context.WithCancel(ctx)
					_, frames, err := stream(connection, client)
					if err != nil || func() bool { f, ok := next(frames, 10*time.Second); return !ok || f.event != "activity.snapshot" }() {
						cancelConnection()
						if ctx.Err() == nil {
							mu.Lock()
							failed++
							mu.Unlock()
							time.Sleep(time.Second)
						}
						continue
					}
					mu.Lock()
					reconnects++
					mu.Unlock()
				reading:
					for {
						select {
						case <-ctx.Done():
							cancelConnection()
							return
						case _, ok := <-frames:
							if !ok {
								// A drained replica ends the stream with EOF.
								mu.Lock()
								ends++
								mu.Unlock()
								break reading
							}
						case <-time.After(20 * time.Second):
							// No frame or heartbeat: the proxy is holding a dead upstream.
							mu.Lock()
							stalls++
							mu.Unlock()
							break reading
						}
					}
					cancelConnection()
				}
			})
		}
		wg.Wait()
		t.Logf("drain window: stream ends=%d successful (re)connects=%d failed attempts=%d stalls=%d", ends, reconnects, failed, stalls)
		if ends == 0 {
			t.Error("no stream ended during the window: replicas were not restarted, or drain left connections hanging")
		}
		if stalls > 0 {
			t.Error("streams stalled silently instead of ending during drain")
		}
		if failed > 0 || reconnects < 6+ends {
			t.Error("not every ended stream reconnected through the ingress")
		}
	})
}
