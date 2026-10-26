package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
	"golang.org/x/net/html"
)

const historyQuery = "city=Boston&country=US&start_date=2026-10-02&end_date=2026-10-31"

// Exercise the HTML handler through the real discovery service, with a narrow
// catalog fake for keyset pages. No collection, account, or provider writes run.
type historyCatalog struct {
	discovery.Catalog
	items   []models.Event
	queries []discovery.Query
	// asOf dates the page starting at an offset; nil reads as now.
	asOf func(start int) time.Time
}

func (r *historyCatalog) Search(_ context.Context, q discovery.Query, _ time.Duration) (discovery.CatalogRead, error) {
	r.queries = append(r.queries, q)
	start := 0
	if q.Cursor != nil {
		start = slices.IndexFunc(r.items, func(e models.Event) bool { return e.ID == q.Cursor.ID }) + 1
	}
	end := min(start+q.Limit, len(r.items))
	at := time.Now().UTC()
	if r.asOf != nil {
		at = r.asOf(start)
	}
	list := models.EventList{Items: slices.Clone(r.items[start:end]), Total: len(r.items), Meta: models.Freshness{DataAsOf: at}}
	if end < len(r.items) {
		last := r.items[end-1]
		cursor := q.NextCursor(discovery.Cursor{Date: "2026-10-12", Name: last.Name, ID: last.ID, Total: len(r.items)})
		list.NextCursor = &cursor
	}
	return discovery.CatalogRead{List: list}, nil
}

func historyAPI(t *testing.T, count int) (*api, *historyCatalog) {
	t.Helper()
	r := &historyCatalog{}
	for i := range count {
		r.items = append(r.items, models.Event{ID: fmt.Sprintf("ticketmaster:history-%03d", i), Name: fmt.Sprintf("History event %03d", i)})
	}
	// Taxonomy is optional but has a valid response, avoiding outage/cooldown
	// behavior unrelated to the cursor walk being tested.
	fixture := categoryFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.Contains(req.URL.Path, "classifications") {
			t.Errorf("stored restore unexpectedly called provider: %s", req.URL)
		}
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(upstream.Close)
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL, Catalog: r})}
	return a, r
}

func historyRequest(a *api, path, browser string, partial bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if browser != "" {
		req.AddCookie(&http.Cookie{Name: a.authConfig.CookieName("session"), Value: browser})
	}
	if partial {
		req.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	a.Routes().ServeHTTP(w, req)
	return w
}

func historyCursors(t *testing.T, a *api, query string, count int) []string {
	t.Helper()
	path := "/api/v1/events?" + query
	var cursors []string
	for range count {
		w := historyRequest(a, path, "", false)
		var list models.EventList
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil || list.NextCursor == nil {
			t.Fatalf("cursor walk: %d %s", w.Code, w.Body.String())
		}
		cursors = append(cursors, *list.NextCursor)
		path = "/api/v1/events?" + query + "&cursor=" + url.QueryEscape(*list.NextCursor)
	}
	return cursors
}

func historyNodes(t *testing.T, body string, match func(*html.Node) bool) []*html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var found []*html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && match(n) {
			found = append(found, n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	return found
}

func historyRows(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	var ids []string
	for _, n := range historyNodes(t, w.Body.String(), func(n *html.Node) bool { return n.Data == "li" && attribute(n, "data-event-id") != "" }) {
		ids = append(ids, attribute(n, "data-event-id"))
	}
	return ids
}

func historyIDs(items []models.Event) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

func historyPagination(t *testing.T, w *httptest.ResponseRecorder, last, next string) {
	t.Helper()
	pages := historyNodes(t, w.Body.String(), func(n *html.Node) bool { return attribute(n, "id") == "pagination" })
	if len(pages) != 1 || attribute(pages[0], "data-page-cursor") != last {
		t.Fatalf("pagination must name the last rendered cursor %q (found %d regions)", last, len(pages))
	}
	links := historyNodes(t, w.Body.String(), func(n *html.Node) bool { return n.Data == "a" && attribute(n, "hx-target") == "#pagination" })
	if next == "" {
		if len(links) != 0 {
			t.Fatal("exhausted results retained Load more")
		}
		return
	}
	if len(links) != 1 || attribute(links[0], "href") != attribute(links[0], "hx-get") {
		t.Fatal("Load more must have matching native and fragment URLs")
	}
	u, err := url.Parse(attribute(links[0], "href"))
	if err != nil || u.Path != "/" || u.Query().Get("cursor") != next || u.Query().Has("through") {
		t.Fatalf("Load more does not resume at the first unrendered page: %s", attribute(links[0], "href"))
	}
	if _, err := discovery.ParseQuery(u.Query(), time.Now()); err != nil {
		t.Fatalf("Load more lost the filters bound to its cursor: %s: %v", u, err)
	}
}

func TestDiscoverHistoryStoredPagesAndLimits(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		size, throughPage, events int
	}{
		{"page boundary", 1, 9, 10},
		{"beyond page boundary", 1, 10, 10},
		{"event boundary", 50, 3, 200},
		{"beyond event boundary", 50, 4, 200},
		// A full page must fit: trimming 25 rows from page three would make
		// its remaining events unreachable from the emitted Load more cursor.
		{"non-divisible event boundary", 75, 3, 150},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, catalog := historyAPI(t, 1000)
			query := historyQuery + "&limit=" + strconv.Itoa(tc.size)
			cursors := historyCursors(t, a, query, tc.throughPage+1)
			catalog.queries = nil
			w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[tc.throughPage-1]), "", false)
			ids := historyRows(t, w)
			if len(ids) != tc.events {
				t.Fatalf("restore rendered %d events, want %d without skipping events on Load more (maximum 200)", len(ids), tc.events)
			}
			if !slices.Equal(ids, historyIDs(catalog.items[:tc.events])) {
				t.Fatal("restored pages were reordered, skipped, or duplicated")
			}
			pages := tc.events / tc.size
			if len(catalog.queries) > min(tc.throughPage+1, 10) {
				t.Fatalf("restore read beyond its page cap: %d reads", len(catalog.queries))
			}
			historyPagination(t, w, cursors[pages-2], cursors[pages-1])
		})
	}
}

func TestDiscoverHistoryStoredExhaustionAndRequestedCursor(t *testing.T) {
	a, catalog := historyAPI(t, 7)
	query := historyQuery + "&limit=2"
	cursors := historyCursors(t, a, query, 3)
	for _, partial := range []bool{false, true} {
		w := historyRequest(a, "/?"+query+"&cursor="+url.QueryEscape(cursors[0])+"&through="+url.QueryEscape(cursors[2]), "", partial)
		if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[2:])) {
			t.Fatalf("requested keyset page was not restored through exhaustion: %v", ids)
		}
		historyPagination(t, w, cursors[2], "")
		if partial && (!strings.Contains(w.Body.String(), `hx-swap-oob="beforeend:#event-list"`) || strings.Contains(w.Body.String(), "<html")) {
			t.Fatal("restored fragment is not appendable rows plus pagination")
		}
	}
	// A stale through must retain the independently valid requested cursor.
	w := historyRequest(a, "/?"+query+"&cursor="+url.QueryEscape(cursors[1])+"&through=not-a-cursor", "", false)
	if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[4:6])) {
		t.Fatalf("stale through discarded the requested page: %v", ids)
	}
	historyPagination(t, w, cursors[1], cursors[2])
	// through equal to the requested cursor must not append the next page.
	w = historyRequest(a, "/?"+query+"&cursor="+url.QueryEscape(cursors[1])+"&through="+url.QueryEscape(cursors[1]), "", false)
	if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[4:6])) {
		t.Fatalf("through at the requested cursor fetched extra pages: %v", ids)
	}
	historyPagination(t, w, cursors[1], cursors[2])
}

func TestDiscoverHistoryUpdatedLabelUsesOldestPage(t *testing.T) {
	a, catalog := historyAPI(t, 6)
	query := historyQuery + "&limit=2"
	cursors := historyCursors(t, a, query, 2)
	now := time.Now().UTC()
	older := now.Add(-2 * time.Minute)
	catalog.asOf = func(start int) time.Time {
		if start == 2 {
			return older
		}
		return now
	}
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[1]), "", false)
	if ids := historyRows(t, w); len(ids) != 6 {
		t.Fatalf("restore rendered %v", ids)
	}
	label := func(at time.Time) string { return "Updated " + at.Format("Jan 2, 15:04 UTC") }
	if body := w.Body.String(); !strings.Contains(body, label(older)) || strings.Contains(body, label(now)) {
		t.Fatal("restored results claim the newest page's update time")
	}
}

func TestDiscoverHistoryShiftedStoredTargetStopsAtCap(t *testing.T) {
	a, catalog := historyAPI(t, 40)
	query := historyQuery + "&limit=1"
	cursors := historyCursors(t, a, query, 10)
	values, _ := url.ParseQuery(query)
	q, err := discovery.ParseQuery(values, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// The cursor was valid for the same filters, but its last row disappeared
	// from the catalog. It never recurs in the current keyset chain.
	missing := q.NextCursor(discovery.Cursor{Date: "2026-10-12", ID: "ticketmaster:removed", Name: "Removed event", Total: 40})
	catalog.queries = nil
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(missing), "", false)
	if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[:10])) || len(catalog.queries) != 10 {
		t.Fatalf("shifted stored target caused an unbounded or truncated walk: rows=%v reads=%d", ids, len(catalog.queries))
	}
	historyPagination(t, w, cursors[8], cursors[9])
}

func TestDiscoverHistoryThroughUsesSameNormalizedFilters(t *testing.T) {
	a, catalog := historyAPI(t, 12)
	base, _ := url.ParseQuery(historyQuery + "&limit=2&keyword=jazz+%26+blues&sort=name_asc&category_id=Music&genre_id=rock")
	cursors := historyCursors(t, a, base.Encode(), 3)
	// Equivalent spelling must bind to the same cursor.
	equivalent := withHistoryParam(base, "city", " Boston ")
	equivalent.Set("country", "us")
	equivalent.Set("keyword", " jazz   & blues ")
	equivalent.Set("through", cursors[1])
	w := historyRequest(a, "/?"+equivalent.Encode(), "", false)
	if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[:6])) {
		t.Fatalf("normalized filters failed to restore: %v", ids)
	}
	historyPagination(t, w, cursors[1], cursors[2])
	for key, value := range map[string]string{"city": "Berlin", "keyword": "other", "sort": "date_desc", "country": "DE", "category_id": "Sports", "genre_id": "pop", "start_date": "2026-10-03", "end_date": "2026-10-30", "limit": "3", "artist_id": "ticketmaster:Artist", "venue_id": "ticketmaster:Venue"} {
		t.Run(key, func(t *testing.T) {
			changed := withHistoryParam(base, key, value)
			next := historyCursors(t, a, changed.Encode(), 1)[0]
			changed.Set("through", cursors[1])
			catalog.queries = nil
			w := historyRequest(a, "/?"+changed.Encode(), "", false)
			limit, _ := strconv.Atoi(changed.Get("limit"))
			if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[:limit])) || len(catalog.queries) != 1 {
				t.Fatalf("foreign through used with changed %s: rows=%v reads=%d", key, ids, len(catalog.queries))
			}
			historyPagination(t, w, "", next)
			if fragment := historyRequest(a, "/?"+changed.Encode(), "", true); fragment.Code != http.StatusBadRequest {
				t.Fatalf("foreign through fragment: %d %s", fragment.Code, fragment.Body.String())
			}
		})
	}
}

func withHistoryParam(values url.Values, key, value string) url.Values {
	copy := make(url.Values, len(values)+1)
	for k, v := range values {
		copy[k] = slices.Clone(v)
	}
	copy.Set(key, value)
	return copy
}

func TestDiscoverHistoryInvalidThroughAndStrictJSON(t *testing.T) {
	a, catalog := historyAPI(t, 8)
	query := historyQuery + "&limit=2"
	cursor := historyCursors(t, a, query, 1)[0]
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatal(err)
	}
	var unknown map[string]any
	if err := json.Unmarshal(decoded, &unknown); err != nil {
		t.Fatal(err)
	}
	unknown["v"] = 999
	encoded, _ := json.Marshal(unknown)
	for _, suffix := range []string{
		"through=", "through=not-a-cursor", "through=" + strings.Repeat("a", 513),
		"through=" + base64.RawURLEncoding.EncodeToString(encoded),
		"through=" + url.QueryEscape(cursor) + "&through=" + url.QueryEscape(cursor),
	} {
		t.Run(suffix[:min(len(suffix), 40)], func(t *testing.T) {
			catalog.queries = nil
			w := historyRequest(a, "/?"+query+"&"+suffix, "", false)
			if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[:2])) || len(catalog.queries) != 1 {
				t.Fatalf("invalid through did not fall back: rows=%v reads=%d", ids, len(catalog.queries))
			}
			if w := historyRequest(a, "/?"+query+"&"+suffix, "", true); w.Code != http.StatusBadRequest {
				t.Fatalf("invalid through fragment: %d %s", w.Code, w.Body.String())
			}
		})
	}
	// HTML-only parameters are rejected, not silently added to the JSON API.
	for _, suffix := range []string{"through=" + url.QueryEscape(cursor), "through=bad", "through="} {
		catalog.queries = nil
		w := historyRequest(a, "/api/v1/events?"+query+"&"+suffix, "", false)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"code":"invalid_query"`) || !strings.Contains(w.Body.String(), `"through"`) || len(catalog.queries) != 0 {
			t.Fatalf("JSON accepted HTML through: %d %s; reads=%d", w.Code, w.Body.String(), len(catalog.queries))
		}
	}
	w := historyRequest(a, "/api/v1/events?"+query, "", false)
	var list models.EventList
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 2 || strings.Contains(w.Body.String(), "through") || strings.Contains(w.Body.String(), "page_cursor") {
		t.Fatalf("restore metadata changed JSON: %d %s", w.Code, w.Body.String())
	}
}

func TestDiscoverHistoryCanonicalAndSafeReturnURLs(t *testing.T) {
	a, _ := historyAPI(t, 8)
	query := historyQuery + "&limit=2"
	cursor := historyCursors(t, a, query, 1)[0]
	back := "/?" + query + "&through=" + url.QueryEscape(cursor)
	w := historyRequest(a, back+"&utm_source=shared", "", false)
	if w.Code != http.StatusFound || w.Header().Get("Location") != back {
		t.Fatalf("canonical redirect dropped loaded history: %d %s", w.Code, w.Header().Get("Location"))
	}
	for _, target := range []string{back, back + "&through=edited", "/?" + query + "&through=", "/?" + query + "&through=edited"} {
		if got := safeReturnURL(target); got != target {
			t.Fatalf("return to Discover cannot use whole-page fallback: got %q, want %q", got, target)
		}
	}
	for _, target := range []string{"//evil.example/" + back, "https://evil.example" + back, back + "#outside", back + "&limit=-1"} {
		if got := safeReturnURL(target); got != "/" {
			t.Fatalf("through bypassed safe-return validation: %q => %q", target, got)
		}
	}
	for _, partial := range []bool{false, true} {
		w := historyRequest(a, "/?"+query+"&limit=-1&through=edited", "", partial)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("through fallback hid an invalid search: %d %s", w.Code, w.Body.String())
		}
	}
}

type historySaved struct {
	*savedStub
	batches [][]string
	failAt  int
}

func (r *historySaved) States(ctx context.Context, owner string, ids []string) (map[string]bool, error) {
	r.batches = append(r.batches, slices.Clone(ids))
	if len(r.batches) == r.failAt {
		return nil, saved.ErrUnavailable
	}
	return r.savedStub.States(ctx, owner, ids)
}

type historyInterests struct {
	*interestStub
	batches [][]string
	owners  []string
	failAt  int
}

func (r *historyInterests) States(ctx context.Context, owner string, ids []string) (map[string]interests.State, error) {
	r.batches = append(r.batches, slices.Clone(ids))
	r.owners = append(r.owners, owner)
	if len(r.batches) == r.failAt {
		return nil, interests.ErrUnavailable
	}
	return r.interestStub.States(ctx, owner, ids)
}

func historyBatches(t *testing.T, batches [][]string, want []string, maxCalls int) {
	t.Helper()
	if len(batches) == 0 || len(batches) > maxCalls {
		t.Fatalf("state reads are not bounded batches: %d calls, want at most %d", len(batches), maxCalls)
	}
	var ids []string
	for _, batch := range batches {
		if len(batch) > 100 {
			t.Fatalf("state batch exceeds 100: %d", len(batch))
		}
		ids = append(ids, batch...)
	}
	for _, id := range want {
		if !slices.Contains(ids, id) {
			t.Fatalf("state batch omitted restored event %s", id)
		}
	}
}

func TestDiscoverHistoryCurrentPrivateStateCSRFAndBatches(t *testing.T) {
	a, catalog := historyAPI(t, 220)
	auth, account, browser, _, csrf := accountAPI(t)
	a.accounts, a.authConfig = auth.accounts, auth.authConfig
	owner := account.account.ID
	saves := &historySaved{savedStub: &savedStub{items: map[string]map[string]saved.Item{owner: {}}}}
	choices := &historyInterests{interestStub: &interestStub{items: map[string]map[string]interests.Item{owner: {}}}}
	a.saved = saved.New(saves, nil)
	a.interests = interests.New(choices, a.saved)
	activity := &eventActivityStub{}
	WithEventActivity(activity)(a)
	query := historyQuery + "&limit=25"
	cursors := historyCursors(t, a, query, 7)
	// All 175 events must get fresh state, including the second private batch.
	for i, e := range catalog.items[:175] {
		if i%2 == 0 {
			saves.items[owner][e.ID] = saved.Item{Event: e}
			choices.items[owner][e.ID] = interests.Item{Event: e, Visibility: "private"}
		}
	}
	selected := catalog.items[150]
	saves.detail = models.EventDetail{Item: selected, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}
	path := "/?" + query + "&through=" + url.QueryEscape(cursors[5]) + "&selected_event=" + url.QueryEscape(selected.ID)
	for _, signedIn := range []bool{true, true, false, true} {
		saves.batches, choices.batches, choices.owners, activity.batches = nil, nil, nil, nil
		cookie := ""
		if signedIn {
			cookie = browser
		}
		w := historyRequest(a, path, cookie, false)
		if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[:175])) {
			t.Fatal("private enrichment changed restored rows")
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("restored private controls may be shared by an intermediary")
		}
		historyBatches(t, choices.batches, historyIDs(catalog.items[:175]), 2)
		historyBatches(t, activity.batches, historyIDs(catalog.items[:175]), 2)
		if strings.Count(w.Body.String(), "2 questions &amp; tips") != 2 {
			t.Fatal("activity counts from the first or second batch were lost")
		}
		wantOwner := ""
		if signedIn {
			wantOwner = owner
			historyBatches(t, saves.batches, historyIDs(catalog.items[:175]), 2)
			forms := historyNodes(t, w.Body.String(), func(n *html.Node) bool { return n.Data == "form" && attribute(n, "data-save-id") != "" })
			if len(forms) < 175 {
				t.Fatalf("restored rows lack native save forms: %d", len(forms))
			}
			for _, form := range forms {
				id := attribute(form, "data-save-id")
				var token, action string
				for child := range form.Descendants() {
					if child.Data == "input" {
						switch attribute(child, "name") {
						case "csrf_token":
							token = attribute(child, "value")
						case "action":
							action = attribute(child, "value")
						}
					}
				}
				_, savedNow := saves.items[owner][id]
				want := "save"
				if savedNow {
					want = "remove"
				}
				if token != csrf || action != want {
					t.Fatalf("%s has stale save/CSRF: action=%s want=%s token matches=%t", id, action, want, token == csrf)
				}
			}
			forms = historyNodes(t, w.Body.String(), func(n *html.Node) bool { return n.Data == "form" && attribute(n, "data-interest-id") == selected.ID })
			if len(forms) == 0 {
				t.Fatal("selected restored event lost interest controls")
			}
			for _, form := range forms {
				_, interested := choices.items[owner][selected.ID]
				var token string
				for child := range form.Descendants() {
					if attribute(child, "name") == "csrf_token" {
						token = attribute(child, "value")
					}
					if pressed := attribute(child, "aria-pressed"); child.Data == "button" && pressed != "" && pressed != strconv.FormatBool(interested) {
						t.Fatal("selected restored event lost current interest from the second batch")
					}
				}
				if token != csrf {
					t.Fatal("selected restored event has stale or missing interest CSRF")
				}
			}
		} else if len(saves.batches) != 0 || strings.Contains(w.Body.String(), csrf) || strings.Contains(w.Body.String(), `data-saved="true"`) || !strings.Contains(w.Body.String(), "Sign in to save") || !strings.Contains(w.Body.String(), "Sign in for Interested") {
			t.Fatal("anonymous restore reused private controls or read private saves")
		}
		for _, got := range choices.owners {
			if got != wantOwner {
				t.Fatal("interest state used another viewer", got, wantOwner)
			}
		}
		// Change state between responses without changing public event caches.
		delete(saves.items[owner], selected.ID)
		delete(choices.items[owner], selected.ID)
		if !signedIn {
			saves.items[owner][selected.ID] = saved.Item{Event: selected}
			choices.items[owner][selected.ID] = interests.Item{Event: selected, Visibility: "private"}
		}
	}
}

func TestDiscoverHistorySelectedOutsideResultsDoesNotExpandActivityReads(t *testing.T) {
	a, catalog := historyAPI(t, 220)
	auth, account, browser, _, _ := accountAPI(t)
	a.accounts, a.authConfig = auth.accounts, auth.authConfig
	owner := account.account.ID
	selected := catalog.items[219]
	saves := &historySaved{savedStub: &savedStub{
		items:  map[string]map[string]saved.Item{owner: {selected.ID: {Event: selected}}},
		detail: models.EventDetail{Item: selected, Meta: models.Freshness{DataAsOf: time.Now().UTC()}},
	}}
	choices := &historyInterests{interestStub: &interestStub{items: map[string]map[string]interests.Item{owner: {selected.ID: {Event: selected, Visibility: "private"}}}}}
	a.saved = saved.New(saves, nil)
	a.interests = interests.New(choices, a.saved)
	activity := &eventActivityStub{}
	WithEventActivity(activity)(a)
	query := historyQuery + "&limit=25"
	cursors := historyCursors(t, a, query, 8)
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[6])+"&selected_event="+url.QueryEscape(selected.ID), browser, false)
	if ids := historyRows(t, w); !slices.Equal(ids, historyIDs(catalog.items[:200])) {
		t.Fatal("selected event changed the bounded restored results")
	}
	privateIDs := append(historyIDs(catalog.items[:200]), selected.ID)
	historyBatches(t, saves.batches, privateIDs, 3)
	historyBatches(t, choices.batches, privateIDs, 3)
	historyBatches(t, activity.batches, historyIDs(catalog.items[:200]), 2)
	for _, batch := range activity.batches {
		if slices.Contains(batch, selected.ID) {
			t.Fatal("selected event outside the results expanded public activity reads")
		}
	}
	if !strings.Contains(w.Body.String(), `data-saved="true"`) || !strings.Contains(w.Body.String(), `aria-pressed="true"`) {
		t.Fatal("selected event lost private state from the third bounded batch")
	}
}

func TestDiscoverHistoryLaterPrivateBatchFailureIsVisible(t *testing.T) {
	a, _ := historyAPI(t, 160)
	auth, account, browser, _, _ := accountAPI(t)
	a.accounts, a.authConfig = auth.accounts, auth.authConfig
	saves := &historySaved{savedStub: &savedStub{items: map[string]map[string]saved.Item{}}, failAt: 2}
	choices := &historyInterests{interestStub: &interestStub{items: map[string]map[string]interests.Item{}}, failAt: 2}
	a.saved = saved.New(saves, nil)
	a.interests = interests.New(choices, a.saved)
	saves.detail = models.EventDetail{Item: models.Event{ID: "ticketmaster:history-125", Name: "Selected show"}}
	query := historyQuery + "&limit=25"
	cursors := historyCursors(t, a, query, 5)
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[4])+"&selected_event=ticketmaster:history-125", browser, false)
	if len(historyRows(t, w)) != 150 || !strings.Contains(w.Body.String(), "Saved unavailable") || !strings.Contains(w.Body.String(), "Interest unavailable") || strings.Contains(w.Body.String(), `data-saved="false"`) {
		t.Fatal("later state failure blocked results or fabricated unsaved/uninterested state")
	}
	if len(saves.batches) != 2 || len(choices.batches) != 2 || choices.owners[1] != account.account.ID {
		t.Fatal("later private read did not remain a viewer-scoped batch")
	}
}

type historyActivity struct {
	eventActivityStub
	failAt int
}

func (r *historyActivity) Previews(ctx context.Context, ids []string) (map[string]eventactivity.Preview, error) {
	r.fail = len(r.batches)+1 == r.failAt
	return r.eventActivityStub.Previews(ctx, ids)
}

func TestDiscoverHistoryLaterActivityFailureDoesNotInventCounts(t *testing.T) {
	a, _ := historyAPI(t, 160)
	activity := &historyActivity{failAt: 2}
	WithEventActivity(activity)(a)
	query := historyQuery + "&limit=25"
	cursors := historyCursors(t, a, query, 5)
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[4]), "", false)
	if len(historyRows(t, w)) != 150 || len(activity.batches) != 2 {
		t.Fatal("later activity failure blocked results or caused per-event reads")
	}
	body := w.Body.String()
	if !strings.Contains(body, "Community previews couldn&#39;t load") || strings.Contains(body, "2 questions") || strings.Contains(body, "0 questions") {
		t.Fatal("later activity failure left misleading partial counts or no visible warning")
	}
}

// Provider pages can overlap when results shift between reads. The server must
// not render duplicate event rows/DOM IDs when rebuilding one continuous list.
func TestDiscoverHistoryProviderOverlapDoesNotDuplicateRows(t *testing.T) {
	fixture := categoryFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write(fixture)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		_, _ = fmt.Fprintf(w, `{"page":{"number":%d,"totalElements":6,"totalPages":3},"_embedded":{"events":[{"id":"overlap-%d","name":"Overlap show"},{"id":"overlap-%d","name":"Overlap show"}]}}`, page, page, page+1)
	}))
	t.Cleanup(upstream.Close)
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	query := historyQuery + "&limit=2"
	cursors := historyCursors(t, a, query, 2)
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[1]), "", false)
	want := []string{"ticketmaster:overlap-0", "ticketmaster:overlap-1", "ticketmaster:overlap-2", "ticketmaster:overlap-3"}
	if ids := historyRows(t, w); !slices.Equal(ids, want) {
		t.Fatalf("overlapping provider pages rendered duplicate events: got %v, want %v", ids, want)
	}
	historyPagination(t, w, cursors[1], "")
}

// A provider cursor far past the page cap renders 10 pages and resumes Load
// more at the 11th, rather than walking to the cursor.
func TestDiscoverHistoryProviderTargetBeyondPageCap(t *testing.T) {
	fixture := categoryFixture(t)
	var mu sync.Mutex
	var reads int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write(fixture)
			return
		}
		mu.Lock()
		reads++
		mu.Unlock()
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		_, _ = fmt.Fprintf(w, `{"page":{"number":%d,"totalElements":100,"totalPages":100},"_embedded":{"events":[{"id":"cap-%d","name":"Cap show"}]}}`, page, page)
	}))
	t.Cleanup(upstream.Close)
	newService := func() *discovery.Service {
		cache := kv.NewMemory()
		t.Cleanup(func() { _ = cache.Close() })
		return discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})
	}
	a := &api{events: newService()}
	query := historyQuery + "&limit=1"
	cursors := historyCursors(t, a, query, 15)
	a.events = newService()
	mu.Lock()
	reads = 0
	mu.Unlock()
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[14]), "", false)
	want := make([]string, 10)
	for i := range want {
		want[i] = fmt.Sprintf("ticketmaster:cap-%d", i)
	}
	if ids := historyRows(t, w); !slices.Equal(ids, want) {
		t.Fatalf("provider restore ignored its page cap: %v", ids)
	}
	historyPagination(t, w, cursors[8], cursors[9])
	mu.Lock()
	defer mu.Unlock()
	if reads != 10 {
		t.Fatalf("provider restore read %d pages, want 10", reads)
	}
}

func TestDiscoverHistoryProviderFailureRetainsSuccessfulPagesAndRetryCursor(t *testing.T) {
	fixture := categoryFixture(t)
	var mu sync.Mutex
	var fail bool
	var calls []int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write(fixture)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		calls = append(calls, page)
		failed := fail && page == 2
		mu.Unlock()
		if failed {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"private-provider-detail"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"page":{"number":%d,"totalElements":5,"totalPages":5},"_embedded":{"events":[{"id":"retry-%d","name":"Retry show"}]}}`, page, page)
	}))
	t.Cleanup(upstream.Close)
	newService := func() *discovery.Service {
		cache := kv.NewMemory()
		t.Cleanup(func() { _ = cache.Close() })
		return discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})
	}
	a := &api{events: newService()}
	query := historyQuery + "&limit=1"
	cursors := historyCursors(t, a, query, 4)
	// Clear public cache, as for a reload on another replica, then fail only
	// the third upstream page. Earlier successes remain useful and retryable.
	a.events = newService()
	mu.Lock()
	fail, calls = true, nil
	mu.Unlock()
	w := historyRequest(a, "/?"+query+"&through="+url.QueryEscape(cursors[2]), "", false)
	if ids := historyRows(t, w); !slices.Equal(ids, []string{"ticketmaster:retry-0", "ticketmaster:retry-1"}) {
		t.Fatalf("mid-chain failure discarded successful results: %v", ids)
	}
	historyPagination(t, w, cursors[0], cursors[1])
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(calls, []int{0, 1, 2}) || strings.Contains(w.Body.String(), "private-provider-detail") {
		t.Fatalf("restore continued past a failed page or leaked upstream data: calls=%v", calls)
	}
}
