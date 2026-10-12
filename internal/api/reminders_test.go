package api

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/internal/saved"
)

type reminderStub struct {
	reminders.Repository
	owner string
	item  reminders.Item
	stops int
	sets  int
	list  []reminders.Item
}

func (r *reminderStub) Unsubscribe(context.Context, string) error { r.stops++; return nil }

func (r *reminderStub) Get(_ context.Context, owner, event string) (reminders.Item, error) {
	if owner != r.owner || event != r.item.EventID {
		return reminders.Item{}, reminders.ErrNotFound
	}
	return r.item, nil
}

func (r *reminderStub) List(_ context.Context, owner string, _ saved.Query) ([]reminders.Item, error) {
	if owner != r.owner {
		return nil, nil
	}
	return r.list, nil
}

func TestReminderUnsubscribeRequiresConfirmation(t *testing.T) {
	a, _, _, _, _ := accountAPI(t)
	r := &reminderStub{}
	a.reminders = reminders.New(r)
	path := "/reminders/unsubscribe/00000000000000000000000000000001"
	get := accountRequest(a, "GET", path, "", "", "", "", "", "")
	if get.Code != 200 || r.stops != 0 {
		t.Fatal("mail scanner changed subscription", get.Code, r.stops)
	}
	post := accountRequest(a, "POST", path, "", "", "", "", "", "application/x-www-form-urlencoded")
	if post.Code != 200 || r.stops != 1 {
		t.Fatal("confirmation failed", post.Code, r.stops, post.Body.String())
	}
	if post.Header().Get("Referrer-Policy") != "no-referrer" || post.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("unsubscribe token could leak")
	}
}
func (r *reminderStub) Set(_ context.Context, owner, event string, lead int) (reminders.Item, error) {
	r.sets++
	r.owner = owner
	r.item = reminders.Item{EventID: event, LeadMinutes: lead, State: "pending_date"}
	return r.item, nil
}

func TestReminderHTMLTimingChoicesValidationAndPrivacy(t *testing.T) {
	a, auth, browser, _, csrf := accountAPI(t)
	id := "ticketmaster:Test"
	r := &reminderStub{owner: auth.account.ID, item: reminders.Item{EventID: id, LeadMinutes: 45, State: "pending_date"}}
	a.reminders = reminders.New(r)
	s := &savedStub{items: map[string]map[string]saved.Item{auth.account.ID: {id: {Event: models.Event{ID: id, Name: "Private reminder event"}}}}}
	a.saved = saved.New(s, nil)
	path := "/me/reminders/" + id
	for _, body := range []string{"action=set&timing=30", "action=set&timing=30&csrf_token=" + csrf + "&csrf_token=" + csrf} {
		w := accountRequest(a, "POST", path, body, browser, "", "", "", "application/x-www-form-urlencoded")
		if w.Code != 403 || r.sets != 0 {
			t.Fatal("HTML CSRF bypass", w.Code)
		}
	}
	get := accountRequest(a, "GET", path, "", browser, "", "", "", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), `value="custom" selected`) || !strings.Contains(get.Body.String(), `value="45"`) {
		t.Fatal(get.Code, get.Body.String())
	}
	for _, tc := range []struct {
		timing, custom string
		lead, status   int
	}{
		{"30", "", 30, 303}, {"60", "invalid-but-unused", 60, 303}, {"1440", "", 1440, 303}, {"custom", "45", 45, 303}, {"custom", "1", 1, 303}, {"custom", "10080", 10080, 303},
		{"custom", "0", 0, 400}, {"custom", "10081", 0, 400}, {"custom", "1.5", 0, 400}, {"custom", "", 0, 400}, {"week", "45", 0, 400}, {"45", "45", 0, 400}, {"", "45", 0, 400},
	} {
		body := url.Values{"action": {"set"}, "timing": {tc.timing}, "lead_minutes": {tc.custom}, "csrf_token": {csrf}}
		before := r.sets
		w := accountRequest(a, "POST", path, body.Encode(), browser, "", "", "", "application/x-www-form-urlencoded")
		if w.Code != tc.status {
			t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
		}
		if tc.status == 303 {
			if r.item.LeadMinutes != tc.lead || !strings.HasSuffix(w.Header().Get("Location"), "?saved=reminder") {
				t.Fatal("choice not normalized", tc, r.item)
			}
		} else {
			if r.sets != before || !strings.Contains(w.Body.String(), `id="reminder-error"`) || (tc.custom != "" && !strings.Contains(w.Body.String(), `value="`+tc.custom+`"`)) {
				t.Fatal("invalid timing mutated or lost input", w.Body.String())
			}
		}
		if !auth.account.Preferences.NotificationsPaused || auth.account.Preferences.EmailReminders {
			t.Fatal("save changed consent")
		}
		if w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private response cached")
		}
	}
	for _, fields := range []string{"timing=30&timing=60", "timing=custom&lead_minutes=45&lead_minutes=60", "timing=30&owner=other"} {
		before := r.sets
		w := accountRequest(a, "POST", path, "action=set&csrf_token="+csrf+"&"+fields, browser, "", "", "", "application/x-www-form-urlencoded")
		if w.Code != 400 || r.sets != before || !strings.Contains(w.Body.String(), "unsupported or repeated") {
			t.Fatal("strict fields lost", w.Code, w.Body.String())
		}
	}
	for _, query := range []string{"?saved=reminder", "?saved=stopped", "?saved=reminder&saved=stopped", "?other=value"} {
		w := accountRequest(a, "GET", path+query, "", browser, "", "", "", "")
		want := 200
		if strings.Contains(query, "&") || strings.Contains(query, "other") {
			want = 400
		}
		if w.Code != want {
			t.Fatal(query, w.Code)
		}
	}
	guest := accountRequest(a, "GET", path, "", "", "", "", "", "")
	if guest.Code != 303 || strings.Contains(guest.Body.String(), "Private reminder event") {
		t.Fatal("guest privacy")
	}
	// A newly authenticated owner cannot use the former owner's saved-event form.
	auth.account.ID = "00000000000000000000000000000001"
	w := accountRequest(a, "GET", path, "", browser, "", "", "", "")
	if w.Code != 404 || strings.Contains(w.Body.String(), "Private reminder event") {
		t.Fatal("changed session leaked detail", w.Code, w.Body.String())
	}
	w = accountRequest(a, "POST", path, "action=set&timing=30&csrf_token="+csrf, browser, "", "", "", "application/x-www-form-urlencoded")
	if w.Code != 404 {
		t.Fatal("foreign owner write", w.Code)
	}
	// Stop remains an idempotent operation even after the save was removed.
	w = accountRequest(a, "POST", path, "action=stop&csrf_token="+csrf, browser, "", "", "", "application/x-www-form-urlencoded")
	if w.Code != 303 || r.stops != 1 {
		t.Fatal("idempotent stop changed", w.Code)
	}
}

func TestReminderHTMLCollectionPaginationIsOwnerBound(t *testing.T) {
	a, auth, browser, _, _ := accountAPI(t)
	r := &reminderStub{owner: auth.account.ID, list: []reminders.Item{{EventID: "ticketmaster:a", Name: "Owner event", State: "pending_date", CreatedAt: time.Now()}, {EventID: "ticketmaster:b", Name: "Next event", State: "scheduled", CreatedAt: time.Now().Add(-time.Minute)}}}
	a.reminders = reminders.New(r)
	w := accountRequest(a, "GET", "/me/reminders?limit=1", "", browser, "", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "More reminders") || strings.Contains(w.Body.String(), "Next event") || !strings.Contains(w.Body.String(), "Not scheduled") {
		t.Fatal(w.Code, w.Body.String())
	}
	page, err := a.reminders.List(t.Context(), auth.account.ID, url.Values{"limit": {"1"}})
	if err != nil || page.NextCursor == nil {
		t.Fatal(err)
	}
	w = accountRequest(a, "GET", "/me/reminders?cursor="+url.QueryEscape(*page.NextCursor), "", browser, "", "", "", "")
	if w.Code != 200 {
		t.Fatal("owner cursor rejected", w.Code, w.Body.String())
	}
	auth.account.ID = "00000000000000000000000000000001"
	w = accountRequest(a, "GET", "/me/reminders?cursor="+url.QueryEscape(*page.NextCursor), "", browser, "", "", "", "")
	if w.Code != 400 || strings.Contains(w.Body.String(), "Owner event") {
		t.Fatal("foreign reminder cursor accepted", w.Code)
	}
}
func (r *reminderStub) Remove(context.Context, string, string) error { r.stops++; return nil }
func TestReminderAPIStrictInputsAndCSRF(t *testing.T) {
	a, _, browser, bearer, csrf := accountAPI(t)
	r := &reminderStub{}
	a.reminders = reminders.New(r)
	path := "/api/v1/me/reminders/ticketmaster:Test/public-on-sale"
	for _, tc := range []struct {
		method, body, cookie, token, csrf, origin string
		status                                    int
	}{
		{"GET", "", "", "", "", "", 401},
		{"PUT", `{"lead_minutes":30}`, browser, "", "", "", 403},
		{"PUT", `{"lead_minutes":30}`, browser, "", csrf, "https://evil.example", 403},
		{"PUT", `{}`, "", bearer, "", "", 400},
		{"PUT", `{"lead_minutes":null}`, "", bearer, "", "", 400},
		{"PUT", `{"lead_minutes":30,"lead_minutes":60}`, "", bearer, "", "", 400},
		{"PUT", `{"lead_minutes":30,"email":"other@example.com"}`, "", bearer, "", "", 400},
		{"PUT", `{"lead_minutes":0}`, "", bearer, "", "", 400},
		{"PUT", `{"lead_minutes":30}`, "", bearer, "", "", 200},
		{"GET", "", "", bearer, "", "", 200},
		{"DELETE", `{}`, "", bearer, "", "", 400},
		{"DELETE", "", "", bearer, "", "", 204},
	} {
		rec := accountRequest(a, tc.method, path, tc.body, tc.cookie, tc.token, tc.csrf, tc.origin, "application/json")
		if rec.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.body, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal("private cache header missing")
		}
	}
	if r.stops != 1 {
		t.Fatal("invalid delete changed state")
	}
	_, err := a.reminders.Get(t.Context(), "00000000000000000000000000000001", "ticketmaster:Test")
	if !errors.Is(err, reminders.ErrNotFound) {
		t.Fatal("foreign owner", err)
	}
}
