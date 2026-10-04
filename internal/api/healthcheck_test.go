package api

import (
	"context"
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
