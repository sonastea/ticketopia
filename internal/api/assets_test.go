package api

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/views/assets"
	"github.com/sonastea/ticketopia/views/layouts"
)

// Pages link content-versioned assets so a deploy is never hidden behind the CDN
// or a browser cache: the current version is immutable, anything else revalidates.
func TestAssetsAreVersionedByContent(t *testing.T) {
	var page strings.Builder
	if err := layouts.Base("Ticketopia").Render(context.Background(), &page); err != nil {
		t.Fatal(err)
	}
	v, ok := assets.Version("app.css")
	if !ok || !strings.Contains(page.String(), `href="/assets/app.css?v=`+v+`"`) || !strings.Contains(page.String(), `src="/assets/app.js?v=`) {
		t.Fatalf("page must link versioned assets (app.css %q):\n%s", v, page.String())
	}

	// Fonts are linked from the compiled stylesheet, which can't call
	// assets.Path: its hand-written versions must match the embedded files, and
	// the preloads must request the same URLs.
	css, err := assets.Files.ReadFile("app.css")
	if err != nil {
		t.Fatal(err)
	}
	urls := regexp.MustCompile(`url\(['"]?/assets/([^'")?]+)(\?v=([^'")]*))?['"]?\)`).FindAllStringSubmatch(string(css), -1)
	if len(urls) == 0 {
		t.Fatal("app.css links no /assets/ URLs")
	}
	for _, m := range urls {
		if want, ok := assets.Version(m[1]); !ok || m[3] != want {
			t.Errorf("app.css links /assets/%s?v=%s; want ?v=%s (first 12 hex digits of its SHA-256)", m[1], m[3], want)
		}
	}
	for _, font := range []string{"fonts/manrope-latin.woff2", "fonts/bricolage-grotesque-latin.woff2"} {
		if href := assets.Path(font); !strings.Contains(page.String(), `rel="preload" href="`+href+`"`) || !strings.Contains(string(css), href) {
			t.Errorf("preload and app.css must share %s", href)
		}
	}

	routes := (&api{}).Routes()
	tag := `"` + v + `"`
	for _, tc := range []struct {
		path, tag, policy string
		status            int
	}{
		{"/assets/app.css?v=" + v, "", "public, max-age=31536000, immutable", 200},
		{"/assets/app.css?v=stale", "", "no-cache", 200},
		{"/assets/app.css", "", "no-cache", 200},
		{"/assets/app.css", tag, "no-cache", 304},
	} {
		w := publicRequest(routes, tc.path, tc.tag)
		if w.Code != tc.status || w.Header().Get("Cache-Control") != tc.policy || w.Header().Get("ETag") != tag {
			t.Errorf("%s (If-None-Match %q): %d %v", tc.path, tc.tag, w.Code, w.Header())
		}
	}
}
