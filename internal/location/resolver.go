// Package location provides a best-effort city hint, not a precise user location.
package location

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"golang.org/x/sync/singleflight"
)

type City struct {
	Name    string `json:"city"`
	Country string `json:"country"`
}

func (city City) Normalize() (City, bool) {
	city.Name = strings.Join(strings.Fields(city.Name), " ")
	city.Country = strings.ToUpper(strings.TrimSpace(city.Country))
	validCountry := city.Country == "" || (len(city.Country) == 2 && city.Country[0] >= 'A' && city.Country[0] <= 'Z' && city.Country[1] >= 'A' && city.Country[1] <= 'Z')
	return city, city.Name != "" && len(city.Name) <= 120 && validCountry
}

type Config struct {
	Disabled   bool
	BaseURL    string
	HTTPClient *http.Client
}

type Resolver struct {
	ctx      context.Context
	cache    kv.Store
	logger   zerolog.Logger
	disabled bool
	baseURL  string
	client   *http.Client
	flights  singleflight.Group
	mu       sync.Mutex
	retryAt  time.Time
	window   time.Time
	used     int
}

func New(ctx context.Context, cache kv.Store, logger zerolog.Logger, config Config) *Resolver {
	if config.BaseURL == "" {
		config.BaseURL = "https://ipwho.is"
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{
			Timeout:       2 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return &Resolver{
		ctx: ctx, cache: cache, logger: logger, disabled: config.Disabled,
		baseURL: strings.TrimRight(config.BaseURL, "/"), client: config.HTTPClient,
	}
}

type cacheEntry struct {
	City      City      `json:"location"`
	Found     bool      `json:"found"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Lookup shares lookups across callers. Only the city/country hint is retained;
// no coordinates, full provider payloads, or raw IP addresses go into cache values.
func (r *Resolver) Lookup(ctx context.Context, rawIP string) (City, bool) {
	if r == nil || r.disabled || ctx.Err() != nil {
		return City{}, false
	}
	ip, err := netip.ParseAddr(rawIP)
	if err != nil || !publicIP(ip) {
		return City{}, false
	}
	ip = ip.Unmap()
	sum := sha256.Sum256([]byte(ip.String()))
	key := "location:v1:ipwhois:" + hex.EncodeToString(sum[:])
	if entry, ok := r.read(ctx, key); ok {
		return entry.City, entry.Found
	}
	result := r.flights.DoChan(key, func() (any, error) {
		lookupCtx, cancel := context.WithTimeout(r.ctx, 3*time.Second)
		defer cancel()
		if entry, ok := r.read(lookupCtx, key); ok {
			return entry, nil
		}
		if !r.allow() {
			return cacheEntry{}, nil
		}
		city, found := r.fetch(lookupCtx, ip.String())
		ttl := 15 * time.Minute
		if found {
			ttl = 24 * time.Hour
		}
		entry := cacheEntry{City: city, Found: found, ExpiresAt: time.Now().Add(ttl)}
		data, _ := json.Marshal(entry)
		if err := r.cache.Set(lookupCtx, key, data, ttl); err != nil && lookupCtx.Err() == nil {
			r.logger.Warn().Msg("Could not cache city lookup")
		}
		return entry, nil
	})
	select {
	case <-ctx.Done():
		return City{}, false
	case result := <-result:
		if result.Err != nil {
			return City{}, false
		}
		entry := result.Val.(cacheEntry)
		return entry.City, entry.Found
	}
}

func (r *Resolver) read(ctx context.Context, key string) (cacheEntry, bool) {
	data, err := r.cache.Get(ctx, key)
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	if json.Unmarshal(data, &entry) != nil || !time.Now().Before(entry.ExpiresAt) {
		return cacheEntry{}, false
	}
	if entry.Found {
		city, valid := entry.City.Normalize()
		if !valid || city.Country == "" {
			return cacheEntry{}, false
		}
		entry.City = city
	}
	return entry, true
}

func (r *Resolver) fetch(ctx context.Context, ip string) (City, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/"+ip+"?fields=success,city,country_code", nil)
	if err != nil {
		return City{}, false
	}
	req.Header.Set("Accept", "application/json")
	res, err := r.client.Do(req)
	if err != nil {
		// Transport errors can contain the request URL and the visitor's IP.
		r.pause(time.Minute)
		return City{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		delay := time.Minute
		if res.StatusCode == http.StatusTooManyRequests {
			delay = 24 * time.Hour
			if seconds, err := strconv.ParseInt(res.Header.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
				delay = min(time.Duration(seconds)*time.Second, 24*time.Hour)
			} else if until, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil && until.After(time.Now()) {
				delay = min(time.Until(until), 24*time.Hour)
			}
		}
		r.pause(delay)
		return City{}, false
	}
	const maxBody = 64 << 10
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	var response struct {
		Success bool   `json:"success"`
		City    string `json:"city"`
		Country string `json:"country_code"`
	}
	if err != nil || len(body) > maxBody || json.Unmarshal(body, &response) != nil {
		r.pause(time.Minute)
		return City{}, false
	}
	city, valid := (City{Name: response.City, Country: response.Country}).Normalize()
	if !response.Success || !valid || city.Country == "" {
		return City{}, false
	}
	return city, true
}

func (r *Resolver) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if r.window.IsZero() || !now.Before(r.window.Add(24*time.Hour)) {
		r.window, r.used = now, 0
	}
	// The free endpoint allows 1,000 requests/day; reserve headroom for other uses.
	if now.Before(r.retryAt) || r.used >= 900 {
		return false
	}
	r.used++
	return true
}

func (r *Resolver) pause(delay time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if until := time.Now().Add(delay); until.After(r.retryAt) {
		r.retryAt = until
		r.logger.Warn().Dur("retry_after", delay).Msg("City lookup unavailable; using manual location selection")
	}
}

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, prefix := range reserved {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
