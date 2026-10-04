package location

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
)

func testResolver(t *testing.T, handler http.HandlerFunc) *Resolver {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	return New(t.Context(), cache, zerolog.Nop(), Config{BaseURL: server.URL})
}

func TestCityLookupCachesOnlyLocationAndExpires(t *testing.T) {
	var calls atomic.Int32
	r := testResolver(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		if req.URL.Path != "/8.8.8.8" || req.URL.Query().Get("fields") != "success,city,country_code" {
			t.Errorf("unexpected request: %s", req.URL)
		}
		_, _ = fmt.Fprint(w, `{"success":true,"city":"  Boston  ","country_code":"us","ip":"8.8.8.8","latitude":42.36,"longitude":-71.06}`)
	})
	for _, ip := range []string{"8.8.8.8", "::ffff:8.8.8.8", "8.8.8.8"} {
		city, ok := r.Lookup(t.Context(), ip)
		if !ok || city != (City{Name: "Boston", Country: "US"}) {
			t.Fatalf("unexpected city: %+v, %v", city, ok)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("equivalent IPs made %d calls", calls.Load())
	}
	sum := sha256.Sum256([]byte("8.8.8.8"))
	key := "location:v1:ipwhois:" + hex.EncodeToString(sum[:])
	data, err := r.cache.Get(t.Context(), key)
	if err != nil || strings.Contains(string(data), "8.8.8.8") || strings.Contains(string(data), "latitude") {
		t.Fatalf("cache contains excess location information or is missing: %s %v", data, err)
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil || time.Until(entry.ExpiresAt) < 23*time.Hour {
		t.Fatalf("successful lookup retention is wrong: %+v %v", entry, err)
	}
	entry.ExpiresAt = time.Now().Add(-time.Second)
	data, _ = json.Marshal(entry)
	_ = r.cache.Set(t.Context(), key, data, time.Hour)
	_, _ = r.Lookup(t.Context(), "8.8.8.8")
	if calls.Load() != 2 {
		t.Fatal("expired location was reused")
	}
	_ = r.cache.Set(t.Context(), key, []byte("broken"), time.Hour)
	_, _ = r.Lookup(t.Context(), "8.8.8.8")
	if calls.Load() != 3 {
		t.Fatal("corrupt location entry was not refreshed")
	}
}

func TestConcurrentLookupSurvivesCancelledCaller(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	r := testResolver(t, func(w http.ResponseWriter, req *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = fmt.Fprint(w, `{"success":true,"city":"Boston","country_code":"US"}`)
	})
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan bool, 1)
	go func() { _, ok := r.Lookup(ctx, "8.8.8.8"); first <- ok }()
	<-started
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if _, ok := r.Lookup(t.Context(), "8.8.8.8"); !ok {
				t.Error("shared lookup failed")
			}
		})
	}
	cancel()
	if <-first {
		t.Fatal("cancelled caller reported a result")
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent lookups made %d requests", calls.Load())
	}
}

func TestNonPublicAddressesAndDisabledLookupMakeNoRequests(t *testing.T) {
	var calls atomic.Int32
	r := testResolver(t, func(w http.ResponseWriter, req *http.Request) { calls.Add(1) })
	for _, ip := range []string{"", "not-an-ip", "127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.5", "192.168.1.1", "172.16.0.1", "169.254.1.1", "100.64.0.1", "0.0.0.0", "224.0.0.1", "198.18.0.1", "203.0.113.1", "fc00::1", "fe80::1%en0", "2001:db8::1"} {
		if _, ok := r.Lookup(t.Context(), ip); ok {
			t.Errorf("non-public address %s resolved", ip)
		}
	}
	r.disabled = true
	if _, ok := r.Lookup(t.Context(), "8.8.8.8"); ok || calls.Load() != 0 {
		t.Fatalf("non-public or disabled lookup called provider: %d", calls.Load())
	}
}

func TestFailedLookupsUseNegativeCacheAndProviderCooldown(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		pause  bool
	}{
		{"missing city", 200, `{"success":true,"country_code":"US"}`, false},
		{"missing country", 200, `{"success":true,"city":"Boston"}`, false},
		{"provider failure", 200, `{"success":false,"city":"Boston","country_code":"US"}`, false},
		{"bad JSON", 200, `broken`, true},
		{"outage", 503, `8.8.8.8`, true},
		{"rate limited", 429, `8.8.8.8`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			r := testResolver(t, func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			})
			var log bytes.Buffer
			r.logger = zerolog.New(&log)
			for range 2 {
				if city, ok := r.Lookup(t.Context(), "8.8.8.8"); ok || city != (City{}) {
					t.Fatalf("failed lookup returned a city: %+v", city)
				}
			}
			if calls.Load() != 1 || strings.Contains(log.String(), "8.8.8.8") {
				t.Fatal("failure cache missed or raw IP was logged")
			}
			if tc.pause {
				_, _ = r.Lookup(t.Context(), "1.1.1.1")
				if calls.Load() != 1 {
					t.Fatal("cooldown did not cover other IPs")
				}
			}
			if tc.status == 429 && time.Until(r.retryAt) < 119*time.Second {
				t.Fatal("Retry-After was not honored")
			}
		})
	}
}

func TestLookupBudgetAndTimeout(t *testing.T) {
	var calls atomic.Int32
	r := testResolver(t, func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		<-req.Context().Done()
	})
	r.window, r.used = time.Now(), 900
	if _, ok := r.Lookup(t.Context(), "8.8.8.8"); ok || calls.Load() != 0 {
		t.Fatal("lookup budget was exceeded")
	}
	r.window = time.Now().Add(-25 * time.Hour)
	r.client = &http.Client{Timeout: 20 * time.Millisecond}
	if _, ok := r.Lookup(t.Context(), "8.8.8.8"); ok || calls.Load() != 1 {
		t.Fatal("timeout did not fall back after budget reset")
	}
	if r.retryAt.IsZero() {
		t.Fatal("timeout did not pause lookups")
	}
}
