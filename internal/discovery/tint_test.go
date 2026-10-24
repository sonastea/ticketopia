package discovery

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

func solid(c color.Color, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}
	return img
}

func hexOKLCH(t *testing.T, hex string) (float64, float64, float64) {
	t.Helper()
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if len(hex) != 7 || err != nil {
		t.Fatalf("tint %q is not #rrggbb", hex)
	}
	l, a, b := oklab(float64(v>>16&0xff)/255, float64(v>>8&0xff)/255, float64(v&0xff)/255)
	return l, math.Hypot(a, b), math.Atan2(b, a)
}

func luminance(l, c, h float64) float64 {
	r, g, b := oklchLinear(l, c, h)
	clip := func(v float64) float64 { return min(max(v, 0), 1) }
	return .2126*clip(r) + .7152*clip(g) + .0722*clip(b)
}

func TestDominantTint(t *testing.T) {
	if got := dominantTint(solid(color.Gray{Y: 128}, 40, 40)); got != "" {
		t.Fatalf("grey artwork tint = %q, want brand fallback", got)
	}
	// A mostly grey poster with a red block still takes the red hue.
	img := solid(color.Gray{Y: 200}, 100, 100)
	for y := range 30 {
		for x := range 100 {
			img.Set(x, y, color.RGBA{200, 30, 40, 255})
		}
	}
	got := dominantTint(img)
	l, c, h := hexOKLCH(t, got)
	_, _, red := hexOKLCH(t, "#c81e28")
	if math.Abs(l-tintLightness) > .01 || c > tintMaxChroma+.005 || math.Abs(h-red) > .2 {
		t.Fatalf("tint %s = L%.3f C%.3f H%.3f, want normalized red", got, l, c, h)
	}
}

// The event header mixes the tint 18% into white in OKLCH. Every hue, and the
// --brand-tint fallback (brand hue/chroma at tint lightness), must keep header
// text at WCAG AA. The raw brand colour would not: muted text falls to 4.35:1.
// muted-warm is --muted mixed 12% toward --brand in OKLCH.
func TestTintMixKeepsHeaderTextAA(t *testing.T) {
	deg := math.Pi / 180
	texts := map[string][3]float64{
		"ink":        {0.272162, 0.015629, 307.825 * deg},
		"muted":      {0.504217, 0.021128, 314.624 * deg},
		"muted-warm": {.88*0.504217 + .12*0.427958, .88*0.021128 + .12*0.080387, (.88*314.624 + .12*337.297) * deg},
		"brand-dark": {0.82 * 0.427958, 0.82 * 0.080387, 337.297 * deg},
		"success":    {0.449361, 0.075890, 160.799 * deg},
		"warning":    {0.472777, 0.088443, 73.757 * deg},
		"error":      {0.478708, 0.154331, 18.922 * deg},
	}
	check := func(name string, l, c, h float64) {
		bg := luminance(.18*l+.82, .18*c, h)
		for text, v := range texts {
			fg := luminance(v[0], v[1], v[2])
			if ratio := (bg + .05) / (fg + .05); ratio < 4.5 {
				t.Errorf("%s: %s on mixed surface = %.2f:1", name, text, ratio)
			}
		}
	}
	for d := 0; d < 360; d += 5 {
		l, c, h := hexOKLCH(t, normalizedTint(float64(d)*deg, 1))
		check("hue "+strconv.Itoa(d), l, c, h)
	}
	check("brand fallback", tintLightness, 0.080387, 337.297*deg)
}

func TestTintsApplyIsBoundedAndMemoized(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, solid(color.RGBA{20, 90, 200, 255}, 64, 36)); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/large.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(make([]byte, tintMaxBytes+1))
		case "/text":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write(buf.Bytes())
		default:
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(buf.Bytes())
		}
	}))
	defer server.Close()
	tints := NewTints(server.Client())
	tints.allow = func(u *url.URL) bool { return u.Host == server.Listener.Addr().String() }
	events := []models.Event{
		{ID: "a", Images: []models.Image{{URL: server.URL + "/big.png", Width: 1024, Height: 576}, {URL: server.URL + "/small.png", Width: 100, Height: 56}}},
		{ID: "b", Images: []models.Image{{URL: server.URL + "/small.png", Width: 100, Height: 56}}},
		{ID: "c", Images: []models.Image{{URL: server.URL + "/large.png", Width: 100, Height: 56}}},
		{ID: "d", Images: []models.Image{{URL: server.URL + "/text", Width: 100, Height: 56}}},
		{ID: "e", Images: []models.Image{{URL: "https://example.com/elsewhere.png", Width: 100, Height: 56}}},
		{ID: "f", Tint: "#123456", Images: []models.Image{{URL: server.URL + "/kept.png", Width: 100, Height: 56}}},
		{ID: "g"},
	}
	tints.apply(context.Background(), events)
	if events[0].Tint == "" || events[0].Tint != events[1].Tint {
		t.Fatalf("shared smallest artwork tints = %q, %q", events[0].Tint, events[1].Tint)
	}
	for _, i := range []int{2, 3, 4, 6} {
		if events[i].Tint != "" {
			t.Fatalf("event %s tint = %q, want brand fallback", events[i].ID, events[i].Tint)
		}
	}
	if events[5].Tint != "#123456" {
		t.Fatal("existing tint was replaced")
	}
	// small.png once, large.png and text once; the other host is never requested.
	if got := requests.Load(); got != 3 {
		t.Fatalf("artwork requests = %d, want 3", got)
	}
	again := []models.Event{{ID: "h", Images: []models.Image{{URL: server.URL + "/small.png", Width: 100, Height: 56}}}, {ID: "i", Images: []models.Image{{URL: server.URL + "/large.png", Width: 100, Height: 56}}}}
	tints.apply(context.Background(), again)
	if again[0].Tint != events[0].Tint || requests.Load() != 3 {
		t.Fatalf("memoized tint = %q after %d requests", again[0].Tint, requests.Load())
	}
	var nilTints *Tints
	nilTints.apply(context.Background(), again)
}

// An unavailable CDN must not cost every uncached search the batch timeout.
func TestTintsPauseAfterTransientFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	tints := NewTints(server.Client())
	tints.allow = func(*url.URL) bool { return true }
	tints.now = func() time.Time { return now }
	events := func(path string) []models.Event {
		return []models.Event{{Images: []models.Image{{URL: server.URL + path, Width: 100, Height: 56}}}}
	}
	tints.apply(context.Background(), events("/a.png"))
	tints.apply(context.Background(), events("/b.png"))
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests while paused = %d, want 1", got)
	}
	now = now.Add(tintRetryAfter)
	tints.apply(context.Background(), events("/b.png"))
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests after pause = %d, want 2", got)
	}
	// A caller that went away does not pause later searches.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	now = now.Add(tintRetryAfter)
	tints.apply(cancelled, events("/c.png"))
	if tints.pausedNow() {
		t.Fatal("cancelled request paused artwork tints")
	}
}
