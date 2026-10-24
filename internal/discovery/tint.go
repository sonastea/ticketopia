package discovery

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// Artwork tints are computed once per image URL while normalized events are
// cached or stored, never while rendering. Every step is bounded: one small
// image per event, a shared batch deadline, limited parallelism and bytes, and
// a fixed-size memo. A failure leaves Tint empty and the page uses the brand.
const (
	tintBatchTimeout = 1500 * time.Millisecond
	tintParallel     = 4
	tintMaxBytes     = 512 << 10
	tintMaxPixels    = 2048 * 2048
	tintMemo         = 4096
	tintSamples      = 4096
	// A batch with a transient failure (timeout, 5xx, network error) pauses
	// artwork requests, so a slow or unavailable CDN delays at most one search
	// per pause rather than every uncached search by the full batch timeout.
	tintRetryAfter = time.Minute

	// Every tint has this OKLCH lightness and at most this chroma, so the
	// 18% surface mix keeps muted text above WCAG AA (see tint_test.go).
	tintLightness = 0.66
	tintMaxChroma = 0.12
)

type Tints struct {
	client *http.Client
	allow  func(*url.URL) bool
	now    func() time.Time
	mu     sync.Mutex
	known  map[string]string
	order  []string
	paused time.Time
}

// NewTints fetches Ticketmaster CDN artwork only. A nil client uses a short,
// redirect-free default.
func NewTints(client *http.Client) *Tints {
	if client == nil {
		client = &http.Client{
			Timeout:       tintBatchTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return &Tints{client: client, allow: ticketmasterArtwork, now: time.Now, known: map[string]string{}}
}

func ticketmasterArtwork(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	return u.Scheme == "https" && (host == "ticketm.net" || strings.HasSuffix(host, ".ticketm.net"))
}

// apply fills missing tints in place. It never fails the caller's read.
func (t *Tints) apply(parent context.Context, events []models.Event) {
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(parent, tintBatchTimeout)
	defer cancel()
	wanted := map[string][]int{}
	for i := range events {
		if events[i].Tint != "" {
			continue
		}
		source := tintImage(events[i].Images)
		if source == "" {
			continue
		}
		if tint, ok := t.recall(source); ok {
			events[i].Tint = tint
			continue
		}
		wanted[source] = append(wanted[source], i)
	}
	if len(wanted) == 0 || t.pausedNow() {
		return
	}
	var failed atomic.Bool
	var wg sync.WaitGroup
	var mu sync.Mutex
	slots := make(chan struct{}, tintParallel)
	for source, indexes := range wanted {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			tint, ok := t.fetch(ctx, source)
			if !ok {
				failed.Store(true)
				return
			}
			t.remember(source, tint)
			mu.Lock()
			for _, i := range indexes {
				events[i].Tint = tint
			}
			mu.Unlock()
		})
	}
	wg.Wait()
	// A request the caller abandoned says nothing about the CDN.
	if parent.Err() == nil && (failed.Load() || ctx.Err() != nil) {
		t.mu.Lock()
		t.paused = t.now().Add(tintRetryAfter)
		t.mu.Unlock()
	}
}

func (t *Tints) pausedNow() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.now().Before(t.paused)
}

func (t *Tints) recall(source string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tint, ok := t.known[source]
	return tint, ok
}

// remember records definitive answers, including "no usable colour", so a
// grey poster is not downloaded again. Timeouts are not remembered.
func (t *Tints) remember(source, tint string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, ok := t.known[source]; ok {
		return
	}
	if len(t.order) >= tintMemo {
		delete(t.known, t.order[0])
		t.order = t.order[1:]
	}
	t.known[source] = tint
	t.order = append(t.order, source)
}

func (t *Tints) fetch(ctx context.Context, source string) (string, bool) {
	u, err := url.Parse(source)
	if err != nil || !t.allow(u) {
		return "", true
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", true
	}
	res, err := t.client.Do(req)
	if err != nil {
		return "", false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		return "", res.StatusCode != http.StatusOK && res.StatusCode < 500
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, tintMaxBytes+1))
	if err != nil {
		return "", false
	}
	if len(data) > tintMaxBytes {
		return "", true
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width*config.Height > tintMaxPixels {
		return "", true
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", true
	}
	return dominantTint(img), true
}

// tintImage picks the smallest supplied artwork of a useful size.
func tintImage(images []models.Image) string {
	best, area := "", math.MaxInt
	for _, image := range images {
		if image.URL == "" {
			continue
		}
		a := image.Width * image.Height
		if image.Width < 64 {
			a = math.MaxInt - 1
		}
		if a < area {
			best, area = image.URL, a
		}
	}
	return best
}

// dominantTint returns the most heavily weighted hue family as a normalized
// "#rrggbb", or "" for near-grey artwork. Pixels are sampled on a grid, so cost
// is bounded regardless of image size.
func dominantTint(img image.Image) string {
	b := img.Bounds()
	step := max(1, int(math.Sqrt(float64(b.Dx()*b.Dy())/tintSamples)))
	const buckets = 24
	var weight [buckets]float64
	var count, sumA, sumB [buckets]float64
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			l, oa, ob := oklab(float64(r)/0xffff, float64(g)/0xffff, float64(bl)/0xffff)
			c := math.Hypot(oa, ob)
			// Ignore greys, near-black shadows and blown highlights.
			if c < .04 || l < .2 || l > .95 {
				continue
			}
			h := math.Atan2(ob, oa)
			i := int((h+math.Pi)/(2*math.Pi)*buckets) % buckets
			weight[i] += c
			count[i]++
			sumA[i] += oa
			sumB[i] += ob
		}
	}
	best := -1
	for i := range weight {
		if weight[i] > 0 && (best < 0 || weight[i] > weight[best]) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return normalizedTint(math.Atan2(sumB[best], sumA[best]), math.Hypot(sumA[best], sumB[best])/count[best])
}

// normalizedTint fixes lightness, caps chroma and reduces it into sRGB gamut.
func normalizedTint(hue, chroma float64) string {
	c := min(max(chroma, .04), tintMaxChroma)
	for ; c > 0; c -= .005 {
		if r, g, b, ok := oklchRGB(tintLightness, c, hue); ok {
			return fmt.Sprintf("#%02x%02x%02x", r, g, b)
		}
	}
	r, g, b, _ := oklchRGB(tintLightness, 0, hue)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

func oklab(r, g, b float64) (float64, float64, float64) {
	r, g, b = linear(r), linear(g), linear(b)
	l := math.Cbrt(.4122214708*r + .5363325363*g + .0514459929*b)
	m := math.Cbrt(.2119034982*r + .6806995451*g + .1073969566*b)
	s := math.Cbrt(.0883024619*r + .2817188376*g + .6299787005*b)
	return .2104542553*l + .7936177850*m - .0040720468*s,
		1.9779984951*l - 2.4285922050*m + .4505937099*s,
		.0259040371*l + .7827717662*m - .8086757660*s
}

// oklchLinear converts OKLCH to linear sRGB without clipping.
func oklchLinear(l, c, h float64) (float64, float64, float64) {
	a, b := c*math.Cos(h), c*math.Sin(h)
	l1 := math.Pow(l+.3963377774*a+.2158037573*b, 3)
	m1 := math.Pow(l-.1055613458*a-.0638541728*b, 3)
	s1 := math.Pow(l-.0894841775*a-1.2914855480*b, 3)
	return 4.0767416621*l1 - 3.3077115913*m1 + .2309699292*s1,
		-1.2684380046*l1 + 2.6097574011*m1 - .3413193965*s1,
		-.0041960863*l1 - .7034186147*m1 + 1.7076147010*s1
}

func oklchRGB(l, c, h float64) (uint8, uint8, uint8, bool) {
	r, g, b := oklchLinear(l, c, h)
	for _, v := range []float64{r, g, b} {
		if v < -1e-4 || v > 1+1e-4 {
			return 0, 0, 0, false
		}
	}
	return encode(r), encode(g), encode(b), true
}

func linear(v float64) float64 {
	if v <= .04045 {
		return v / 12.92
	}
	return math.Pow((v+.055)/1.055, 2.4)
}

func encode(v float64) uint8 {
	v = min(max(v, 0), 1)
	if v <= .0031308 {
		v *= 12.92
	} else {
		v = 1.055*math.Pow(v, 1/2.4) - .055
	}
	return uint8(math.Round(v * 255))
}
