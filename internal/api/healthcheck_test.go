package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthProbes(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// No discovery, location, or cache services: probes must not depend on them.
	a := &api{shutdown: ctx.Done()}
	routes := a.Routes()
	probe := func(path string, status int, body string) {
		t.Helper()
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != status || rec.Body.String() != body {
			t.Fatalf("%s: got %d %q, want %d %q", path, rec.Code, rec.Body.String(), status, body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
			t.Fatalf("%s: unexpected probe headers: %v", path, rec.Header())
		}
	}
	probe("/healthz", http.StatusOK, "ok\n")
	probe("/readyz", http.StatusOK, "ok\n")
	cancel()
	probe("/healthz", http.StatusOK, "ok\n")
	probe("/readyz", http.StatusServiceUnavailable, "not ready\n")
}

func TestReadinessDependencyRecoveryAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	available, calls := false, 0
	a := &api{shutdown: ctx.Done(), ready: func(context.Context) error {
		calls++
		if !available {
			return errors.New("secret database error")
		}
		return nil
	}}
	routes := a.Routes()
	probe := func(path string, expected int) {
		t.Helper()
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != expected || strings.Contains(rec.Body.String(), "secret") {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	probe("/healthz", 200)
	if calls != 0 {
		t.Fatal("liveness checked a dependency")
	}
	probe("/readyz", 503)
	available = true
	probe("/readyz", 200)
	before := calls
	cancel()
	probe("/readyz", 503)
	probe("/healthz", 200)
	if calls != before {
		t.Fatal("shutdown still checked the database")
	}
}
