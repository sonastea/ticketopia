package api

import (
	"context"
	"errors"
	"testing"

	"github.com/sonastea/ticketopia/internal/reminders"
)

type reminderStub struct {
	reminders.Repository
	owner string
	item  reminders.Item
	stops int
}

func (r *reminderStub) Unsubscribe(context.Context, string) error { r.stops++; return nil }

func (r *reminderStub) Get(_ context.Context, owner, event string) (reminders.Item, error) {
	if owner != r.owner {
		return reminders.Item{}, reminders.ErrNotFound
	}
	return r.item, nil
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
	r.owner = owner
	r.item = reminders.Item{EventID: event, LeadMinutes: lead, State: "pending_date"}
	return r.item, nil
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
