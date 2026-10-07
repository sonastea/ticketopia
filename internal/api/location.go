package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/location"
)

const cityCookie = "ticketopia_city"

func clientIPExtractor(cidrs string) (echo.IPExtractor, error) {
	if strings.TrimSpace(cidrs) == "" {
		return echo.ExtractIPDirect(), nil
	}
	options := []echo.TrustOption{echo.TrustLoopback(false), echo.TrustLinkLocal(false), echo.TrustPrivateNet(false)}
	for _, cidr := range strings.Split(cidrs, ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain comma-separated proxy CIDRs")
		}
		options = append(options, echo.TrustIPRange(network))
	}
	return echo.ExtractIPFromXFFHeader(options...), nil
}

// Web defaults never broaden an unresolved location into a worldwide search.
// Pagination already carries explicit location filters, so it never re-detects.
func (a *api) locateSearch(c echo.Context, query *discovery.Query) (string, error) {
	if query.City != "" {
		if query.Page == 0 && c.Request().Header.Get("HX-Request") != "true" {
			rememberCity(c, location.City{Name: query.City, Country: query.Country})
		}
		return "search", nil
	}
	if query.VenueID != "" {
		return "venue", nil
	}
	if query.Page > 0 {
		return "", &discovery.ValidationError{Field: "city", Message: "keep the city filter when loading more shows"}
	}
	// An explicitly empty city or a country-only form is a request to choose a
	// location, not permission to silently restore a previous city.
	if c.QueryParams().Has("city") || c.QueryParams().Has("country") {
		return "", nil
	}
	if city, ok := rememberedCity(c); ok {
		query.City, query.Country = city.Name, city.Country
		return "saved", nil
	}
	if a.enabled() {
		if principal, err := a.browserPrincipal(c); err == nil && principal.Account.Preferences.City != "" {
			query.City, query.Country = principal.Account.Preferences.City, principal.Account.Preferences.Country
			return "account", nil
		}
	}
	if city, ok := a.locations.Lookup(c.Request().Context(), c.RealIP()); ok {
		query.City, query.Country = city.Name, city.Country
		return "ip", nil
	}
	return "", nil
}

func rememberCity(c echo.Context, city location.City) {
	data, _ := json.Marshal(city)
	c.SetCookie(&http.Cookie{
		Name: cityCookie, Value: base64.RawURLEncoding.EncodeToString(data),
		Path: "/", MaxAge: 30 * 24 * 60 * 60, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: c.Scheme() == "https",
	})
}

func rememberedCity(c echo.Context) (location.City, bool) {
	cookie, err := c.Cookie(cityCookie)
	if err != nil || len(cookie.Value) > 512 {
		return location.City{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	var city location.City
	if err != nil || json.Unmarshal(data, &city) != nil {
		return location.City{}, false
	}
	return city.Normalize()
}
