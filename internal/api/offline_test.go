package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// Browsers update a worker only when its bytes change, so the cached offline
// page must be versioned by its own content.
func TestServiceWorkerVersionFollowsTheOfflinePage(t *testing.T) {
	routes := (&api{}).Routes()
	page := publicRequest(routes, "/offline", "")
	worker := publicRequest(routes, "/sw.js", "")
	sum := sha256.Sum256(page.Body.Bytes())
	if page.Code != 200 || !strings.Contains(page.Body.String(), "You're offline") || strings.Contains(page.Body.String(), "/assets/") {
		t.Fatalf("offline page must be self-contained: %d\n%s", page.Code, page.Body)
	}
	// Rendered once and replayed from cache, it can't carry a valid per-response nonce.
	if strings.Contains(page.Body.String(), "nonce=") {
		t.Fatalf("cached offline page must not carry a nonce:\n%s", page.Body)
	}
	if worker.Code != 200 || worker.Header().Get("Content-Type") != "text/javascript; charset=utf-8" || worker.Header().Get("Cache-Control") != "no-cache" ||
		!strings.Contains(worker.Body.String(), "const VERSION = '"+hex.EncodeToString(sum[:8])+"';") {
		t.Fatalf("worker: %d %v\n%s", worker.Code, worker.Header(), worker.Body)
	}
}
