package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/models"
)

type CatalogQuery struct {
	Kind, Keyword, City, Country string
	Limit, Page                  int
}

func ParseCatalogQuery(kind string, values url.Values) (CatalogQuery, error) {
	q := CatalogQuery{Kind: kind, Limit: 20}
	if kind != "artist" && kind != "venue" {
		return q, &ValidationError{"kind", "choose artist or venue"}
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "keyword" && key != "limit" && key != "cursor" && (kind != "venue" || (key != "city" && key != "country"))) {
			return q, &ValidationError{key, "use one value for a supported search parameter"}
		}
		if !utf8.ValidString(entries[0]) || strings.ContainsFunc(entries[0], unicode.IsControl) {
			return q, &ValidationError{key, "use valid text without control characters"}
		}
	}
	q.Keyword = strings.Join(strings.Fields(values.Get("keyword")), " ")
	q.City = strings.Join(strings.Fields(values.Get("city")), " ")
	q.Country = strings.ToUpper(strings.TrimSpace(values.Get("country")))
	if q.Keyword == "" || len(q.Keyword) > 120 {
		return q, &ValidationError{"keyword", "enter a name, up to 120 bytes"}
	}
	if len(q.City) > 120 {
		return q, &ValidationError{"city", "use at most 120 bytes"}
	}
	if q.Country != "" && (len(q.Country) != 2 || q.Country[0] < 'A' || q.Country[0] > 'Z' || q.Country[1] < 'A' || q.Country[1] > 'Z') {
		return q, &ValidationError{"country", "use a two-letter country code"}
	}
	if raw, ok := values["limit"]; ok {
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > 100 {
			return q, &ValidationError{"limit", "use an integer from 1 to 100"}
		}
		q.Limit = n
	}
	if raw, ok := values["cursor"]; ok {
		var c pageCursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if len(raw[0]) > 512 || err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Filters != q.fingerprint() || c.Page < 1 || c.Page > 999/q.Limit {
			return q, &ValidationError{"cursor", "use the next cursor with the same search filters"}
		}
		q.Page = c.Page
	}
	return q, nil
}
func (q CatalogQuery) Values() url.Values {
	v := url.Values{"keyword": {q.Keyword}, "limit": {strconv.Itoa(q.Limit)}}
	if q.City != "" {
		v.Set("city", q.City)
	}
	if q.Country != "" {
		v.Set("country", q.Country)
	}
	return v
}
func (q CatalogQuery) fingerprint() string {
	sum := sha256.Sum256([]byte(q.Kind + "\x00" + q.Values().Encode()))
	return hex.EncodeToString(sum[:])
}
func catalogPath(kind string) string {
	if kind == "artist" {
		return "attractions"
	}
	return "venues"
}
func catalogKey(kind, id string) string { return "ticketmaster:v1:catalog:" + kind + ":" + id }

type providerArtist struct{ ID, Name, URL string }
type providerVenue struct {
	ID, URL, Timezone string
	providerPlace
}

func artistTarget(a providerArtist) models.FollowTarget {
	return models.ArtistTarget(models.Artist{ID: "ticketmaster:" + a.ID, Name: a.Name, Source: models.Source{Provider: "ticketmaster", ID: a.ID, URL: a.URL}})
}
func venueTarget(v providerVenue) models.FollowTarget {
	return models.VenueTarget(models.Venue{ID: "ticketmaster:" + v.ID, Source: models.Source{Provider: "ticketmaster", ID: v.ID, URL: v.URL}, Timezone: v.Timezone, Place: normalizePlace(v.providerPlace)})
}

type catalogPage struct {
	Items             []models.FollowTarget `json:"items"`
	Total, TotalPages int
}

// Catalog shares the provider gate, cache and singleflight with event discovery.
func (s *Service) Catalog(ctx context.Context, kind string, values url.Values) (models.CatalogList, error) {
	q, err := ParseCatalogQuery(kind, values)
	if err != nil {
		return models.CatalogList{}, err
	}
	page, meta, err := cached(ctx, s, fmt.Sprintf("ticketmaster:v1:catalog-search:%s:%d", q.fingerprint(), q.Page), genrePolicy, func(ctx context.Context) (catalogPage, error) {
		var raw struct {
			Embedded struct {
				Attractions []providerArtist
				Venues      []providerVenue
			} `json:"_embedded"`
			Page *providerPage
		}
		v := url.Values{"keyword": {q.Keyword}, "size": {strconv.Itoa(q.Limit)}, "page": {strconv.Itoa(q.Page)}, "locale": {"en"}, "sort": {"name,asc"}}
		if q.City != "" {
			v.Set("city", q.City)
		}
		if q.Country != "" {
			v.Set("countryCode", q.Country)
		}
		if err := s.get(ctx, "/"+catalogPath(kind)+".json", v, &raw); err != nil {
			return catalogPage{}, err
		}
		if raw.Page == nil || raw.Page.Number != q.Page || raw.Page.TotalElements < 0 || raw.Page.TotalPages < 0 {
			return catalogPage{}, s.unavailable(0)
		}
		p := catalogPage{Items: []models.FollowTarget{}, Total: raw.Page.TotalElements, TotalPages: raw.Page.TotalPages}
		if kind == "artist" {
			for _, a := range raw.Embedded.Attractions {
				p.Items = append(p.Items, artistTarget(a))
			}
		} else {
			for _, v := range raw.Embedded.Venues {
				p.Items = append(p.Items, venueTarget(v))
			}
		}
		if len(p.Items) > q.Limit || (len(p.Items) == 0 && q.Page < p.TotalPages && p.Total > 0) {
			return catalogPage{}, s.unavailable(0)
		}
		seen := map[string]bool{}
		for _, target := range p.Items {
			if !sourceIDPattern.MatchString(target.Source.ID) || strings.TrimSpace(target.Name) == "" || seen[target.ID] {
				return catalogPage{}, s.unavailable(0)
			}
			seen[target.ID] = true
		}
		now := s.now()
		for _, target := range p.Items {
			writeRecord(ctx, s, catalogKey(kind, target.ID), cacheRecord[models.FollowTarget]{Data: target, FetchedAt: now, FreshUntil: now.Add(genrePolicy.fresh), StaleUntil: now.Add(genrePolicy.retain)})
		}
		return p, nil
	})
	if err != nil {
		return models.CatalogList{}, err
	}
	list := models.CatalogList{Items: page.Items, Total: page.Total, Meta: meta}
	if len(page.Items) > 0 && q.Page+1 < page.TotalPages {
		if (q.Page+1)*q.Limit < 1000 {
			data, _ := json.Marshal(pageCursor{1, q.Page + 1, q.fingerprint()})
			next := base64.RawURLEncoding.EncodeToString(data)
			list.NextCursor = &next
		} else {
			list.Limited = true
		}
	}
	return list, nil
}

func (s *Service) CatalogDetail(ctx context.Context, kind, id string) (models.CatalogDetail, error) {
	if kind != "artist" && kind != "venue" {
		return models.CatalogDetail{}, &ValidationError{"kind", "choose artist or venue"}
	}
	rawID, err := sourceID(id)
	if err != nil {
		return models.CatalogDetail{}, &ValidationError{"target_id", "use a ticketmaster: prefixed ID"}
	}
	target, meta, err := cached(ctx, s, catalogKey(kind, id), genrePolicy, func(ctx context.Context) (models.FollowTarget, error) {
		var target models.FollowTarget
		if kind == "artist" {
			var raw providerArtist
			if err := s.get(ctx, "/attractions/"+rawID+".json", url.Values{}, &raw); err != nil {
				return target, err
			}
			target = artistTarget(raw)
		} else {
			var raw providerVenue
			if err := s.get(ctx, "/venues/"+rawID+".json", url.Values{}, &raw); err != nil {
				return target, err
			}
			target = venueTarget(raw)
		}
		if target.ID != id || strings.TrimSpace(target.Name) == "" {
			return target, s.unavailable(0)
		}
		return target, nil
	})
	return models.CatalogDetail{Item: target, Meta: meta}, err
}
