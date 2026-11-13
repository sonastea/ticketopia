package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// documentLimit bounds a metadata document, as the CIMD draft recommends (5 KB).
	documentLimit = 5 << 10
	documentFetch = 5 * time.Second
	// Cache bounds: documents are cached per replica for their max-age, clamped.
	minDocumentTTL     = time.Minute
	maxDocumentTTL     = 24 * time.Hour
	defaultDocumentTTL = 10 * time.Minute
	maxCachedDocuments = 256
)

// Documents resolves clients from Client ID Metadata Documents fetched over
// HTTPS, refusing private and special-purpose addresses so a client_id cannot
// make the server reach internal services.
type Documents struct {
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]cachedClient
}

type cachedClient struct {
	client  Client
	expires time.Time
}

// NewDocuments fetches documents through a dialer that only connects to
// public unicast addresses on port 443, without proxies or redirects.
func NewDocuments() *Documents {
	dialer := &net.Dialer{Timeout: documentFetch, Control: publicOnly}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   documentFetch,
		ResponseHeaderTimeout: documentFetch,
		MaxIdleConns:          16,
		IdleConnTimeout:       time.Minute,
		ForceAttemptHTTP2:     true,
	}
	return NewDocumentsWithClient(&http.Client{Transport: transport, Timeout: documentFetch})
}

// NewDocumentsWithClient uses client for fetches; tests supply a stand-in.
// Redirects are never followed.
func NewDocumentsWithClient(client *http.Client) *Documents {
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Documents{client: &c, now: time.Now, cache: map[string]cachedClient{}}
}

// publicOnly refuses connections to anything but public unicast addresses
// on port 443. It runs after DNS resolution, for every address tried, so a
// hostname cannot resolve or rebind to an internal address.
func publicOnly(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" {
		return fmt.Errorf("refusing %s connection", network)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return fmt.Errorf("refusing connection to %s", address)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !PublicAddress(addr) {
		return fmt.Errorf("refusing connection to non-public address %s", host)
	}
	return nil
}

var specialPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can reach IPv4 private ranges
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"), // 6to4 embeds arbitrary IPv4
	netip.MustParsePrefix("fc00::/7"),
}

// PublicAddress reports whether addr is a globally routable unicast address.
func PublicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, p := range specialPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// ValidClientID checks the CIMD client_id shape: an https URL with a path,
// no credentials, query, fragment or dot segments.
func ValidClientID(raw string) bool {
	if len(raw) > 2048 || strings.ContainsAny(raw, " \t\r\n\\") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || strings.Contains(raw, "#") || u.Path == "" || u.Path == "/" || u.Opaque != "" {
		return false
	}
	if p := u.Port(); p != "" && p != "443" {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// Client returns the validated client for a client_id URL.
func (d *Documents) Client(ctx context.Context, id string) (Client, error) {
	if !ValidClientID(id) {
		return Client{}, &ClientError{"This app's identifier isn't a supported client metadata URL. Return to the app and try connecting again."}
	}
	now := d.now()
	d.mu.Lock()
	if cached, ok := d.cache[id]; ok && now.Before(cached.expires) {
		d.mu.Unlock()
		return cached.client, nil
	}
	d.mu.Unlock()
	client, ttl, err := d.fetch(ctx, id)
	if err != nil {
		return Client{}, err
	}
	if ttl > 0 {
		d.mu.Lock()
		if len(d.cache) >= maxCachedDocuments {
			for key, cached := range d.cache {
				if !now.Before(cached.expires) || len(d.cache) >= maxCachedDocuments {
					delete(d.cache, key)
				}
			}
		}
		d.cache[id] = cachedClient{client, now.Add(ttl)}
		d.mu.Unlock()
	}
	return client, nil
}

var errDocument = errors.New("client metadata document unavailable")

func (d *Documents) fetch(ctx context.Context, id string) (Client, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, documentFetch)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return Client{}, 0, errDocument
	}
	request.Header.Set("Accept", "application/json")
	response, err := d.client.Do(request)
	if err != nil {
		return Client{}, 0, errDocument
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Client{}, 0, &ClientError{"This app's client metadata couldn't be loaded. Return to the app and try connecting again."}
	}
	if media, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err != nil || (media != "application/json" && !strings.HasSuffix(media, "+json")) {
		return Client{}, 0, &ClientError{"This app's client metadata isn't JSON. Return to the app and try connecting again."}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, documentLimit+1))
	if err != nil {
		return Client{}, 0, errDocument
	}
	if len(body) > documentLimit {
		return Client{}, 0, &ClientError{"This app's client metadata is too large."}
	}
	client, err := ParseDocument(id, body)
	if err != nil {
		return Client{}, 0, err
	}
	return client, documentTTL(response.Header.Get("Cache-Control")), nil
}

// documentTTL honors max-age within bounds; no-store or no-cache disables caching.
func documentTTL(header string) time.Duration {
	ttl := defaultDocumentTTL
	for _, directive := range strings.Split(strings.ToLower(header), ",") {
		directive = strings.TrimSpace(directive)
		switch {
		case directive == "no-store" || directive == "no-cache":
			return 0
		case strings.HasPrefix(directive, "max-age="):
			seconds, err := strconv.Atoi(strings.TrimPrefix(directive, "max-age="))
			if err != nil || seconds < 0 {
				continue
			}
			ttl = min(max(time.Duration(seconds)*time.Second, minDocumentTTL), maxDocumentTTL)
		}
	}
	return ttl
}

type document struct {
	ClientID                string   `json:"client_id"`
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	// TokenEndpointAuthMethods lists every method the client can use (MCP
	// SEP-3149); the singular field is then only a legacy preference.
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
	ClientSecret             *string  `json:"client_secret"`
}

// public reports whether the client can authenticate at the token endpoint
// with "none", the only method Ticketopia accepts. A listed set of methods
// wins over the singular preference, so a client such as ChatGPT that prefers
// private_key_jwt but also supports none connects as a public client.
func (d document) public() bool {
	if d.ClientSecret != nil {
		return false
	}
	if len(d.TokenEndpointAuthMethods) > 0 {
		return slices.Contains(d.TokenEndpointAuthMethods, "none")
	}
	return d.TokenEndpointAuthMethod == "" || d.TokenEndpointAuthMethod == "none"
}

// ParseDocument validates a metadata document fetched from id.
func ParseDocument(id string, body []byte) (Client, error) {
	invalid := func(reason string) error {
		return &ClientError{"This app's client metadata is invalid (" + reason + "). Return to the app and try connecting again."}
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return Client{}, invalid("not a JSON object")
	}
	if doc.ClientID != id {
		return Client{}, invalid("client_id does not match its URL")
	}
	// Ticketopia supports only public clients; a document cannot carry secrets.
	if !doc.public() {
		return Client{}, invalid("only public clients without a secret are supported")
	}
	if len(doc.GrantTypes) > 0 && !slices.Contains(doc.GrantTypes, "authorization_code") {
		return Client{}, invalid("the authorization_code grant is required")
	}
	if len(doc.ResponseTypes) > 0 && !slices.Contains(doc.ResponseTypes, "code") {
		return Client{}, invalid("the code response type is required")
	}
	name := cleanName(doc.ClientName)
	if name == "" {
		return Client{}, invalid("client_name is required")
	}
	if len(doc.RedirectURIs) == 0 || len(doc.RedirectURIs) > 20 {
		return Client{}, invalid("list between 1 and 20 redirect_uris")
	}
	for _, raw := range doc.RedirectURIs {
		if !validRedirect(raw) {
			return Client{}, invalid("redirect_uris must be https, loopback http or an app scheme")
		}
	}
	return Client{ID: id, Name: name, RedirectURIs: doc.RedirectURIs, Refresh: slices.Contains(doc.GrantTypes, "refresh_token")}, nil
}

// cleanName keeps a displayable client name: no control or format
// characters (which could reorder text on the consent page), at most 80 runes.
func cleanName(raw string) string {
	if !utf8.ValidString(raw) {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	name := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(name) > 80 {
		name = string([]rune(name)[:80])
	}
	return name
}

var blockedSchemes = []string{"javascript", "data", "file", "vbscript", "about", "blob", "ftp", "ws", "wss"}

// validRedirect accepts https URLs, http on loopback (RFC 8252 section 7.3)
// and private-use app schemes such as cursor:// (section 7.1).
func validRedirect(raw string) bool {
	if len(raw) > 2048 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Fragment != "" || strings.Contains(raw, "#") || u.User != nil {
		return false
	}
	switch scheme := strings.ToLower(u.Scheme); {
	case scheme == "https":
		return u.Hostname() != ""
	case scheme == "http":
		return loopback(u)
	default:
		return !slices.Contains(blockedSchemes, scheme)
	}
}

func loopback(u *url.URL) bool {
	if u.Scheme != "http" {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// RedirectAllowed matches a requested redirect URI exactly against the
// registered ones, except that loopback http URIs may use any port, since
// native apps listen on an ephemeral one (RFC 8252 section 7.3).
func RedirectAllowed(registered []string, requested string) bool {
	if !validRedirect(requested) {
		return false
	}
	if slices.Contains(registered, requested) {
		return true
	}
	want, err := url.Parse(requested)
	if err != nil || !loopback(want) {
		return false
	}
	for _, raw := range registered {
		u, err := url.Parse(raw)
		if err == nil && loopback(u) && u.Hostname() == want.Hostname() && u.EscapedPath() == want.EscapedPath() && u.RawQuery == want.RawQuery {
			return true
		}
	}
	return false
}
