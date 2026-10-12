package account

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
)

func TestReminderTimingAndDSTPreview(t *testing.T) {
	// Subtract elapsed minutes across the spring DST change, not local wall hours.
	sale := time.Date(2026, 3, 8, 8, 30, 0, 0, time.UTC)
	event := models.Event{ID: "ticketmaster:test", Name: "An event"}
	event.PublicSale.Start = &sale
	p := ReminderPage{Enabled: true, Event: &event, Lead: 60, Now: sale.Add(-48 * time.Hour), Account: accounts.Account{Preferences: accounts.Preferences{Timezone: "America/Chicago"}}}
	if got := p.Preview(); !strings.Contains(got, "8 Mar 2026, 01:30 CST (America/Chicago)") {
		t.Fatal("incorrect DST target", got)
	}
	if got := p.SaleTime(); !strings.Contains(got, "03:30 CDT") {
		t.Fatal("incorrect sale time", got)
	}
	fall := time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC)
	event.PublicSale.Start = &fall
	p.Now = fall.Add(-24 * time.Hour)
	if got := p.Preview(); !strings.Contains(got, "1 Nov 2026, 01:30 CDT") {
		t.Fatal("incorrect fall DST target", got)
	}
	if got := p.SaleTime(); !strings.Contains(got, "01:30 CST") {
		t.Fatal("incorrect fall sale time", got)
	}
	event.PublicSale.Start = &sale
	for _, tc := range []struct {
		lead   int
		choice string
	}{{30, "30"}, {60, "60"}, {1440, "1440"}, {45, "custom"}, {1, "custom"}, {10080, "custom"}} {
		p.Lead = tc.lead
		if p.TimingChoice() != tc.choice {
			t.Fatalf("lead %d: %s", tc.lead, p.TimingChoice())
		}
	}
	p.Lead = 60
	p.Now = sale.Add(-10 * time.Minute)
	if !strings.Contains(p.Preview(), "eligible immediately") {
		t.Fatal(p.Preview())
	}
	p.Now = sale
	if !strings.Contains(p.Preview(), "no late reminder") {
		t.Fatal(p.Preview())
	}
	event.PublicSale.StartTBD = true
	if p.SaleTime() != "Time not announced" || !strings.Contains(p.Preview(), "no target is scheduled") {
		t.Fatal(p.SaleTime(), p.Preview())
	}
	event.Status = "postponed"
	if !strings.Contains(p.Preview(), "delivery is withheld") {
		t.Fatal(p.Preview())
	}
	p.Account.Preferences.Timezone = "invalid"
	if p.Zone() != "UTC" {
		t.Fatal("zone fallback")
	}
	p.Submitted, p.Choice = true, "45"
	if p.PreviewLead() != 0 {
		t.Fatal("unsupported HTML choice previewed as valid")
	}
}

func TestReminderDetailPreservesCustomAndTruthfulOutcomes(t *testing.T) {
	event := models.Event{ID: "ticketmaster:test", Name: "<long event>"}
	p := ReminderPage{Enabled: true, Event: &event, Lead: 45, Own: &reminders.Item{LeadMinutes: 45, State: "accepted"}}
	var out bytes.Buffer
	if err := Reminders(p).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`value="custom" selected`, `value="45"`, `Custom lead time in minutes`, `Event Overview`, `section=overview`, `&lt;long event&gt;`, `SES accepted`, `inbox delivery is not confirmed`, `Public ticket sale:`, `Time not announced`, `Reminder target:`, `Not scheduled`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	p.Submitted, p.Choice, p.Custom, p.Error = true, "custom", "10081", "Changes weren't saved. Choose up to seven days."
	out.Reset()
	if err := Reminders(p).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`value="10081"`, `role="alert"`, `Changes weren`, `Choose 1–10080 whole minutes`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing invalid state %q", want)
		}
	}
}

func TestReminderReadinessStatesAndCollection(t *testing.T) {
	for _, state := range []string{"pending_date", "scheduled", "paused", "refresh_failed", "sending", "pending", "accepted", "uncertain", "failed", "expired", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			p := ReminderPage{Enabled: true, List: reminders.Page{Items: []reminders.Item{{EventID: "ticketmaster:test", Name: "Event name", State: state}}}}
			var out bytes.Buffer
			if err := Reminders(p).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			html := out.String()
			for _, want := range []string{"Event name", reminderState(state), reminderStateDetail(state), "Not scheduled"} {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q", want)
				}
			}
		})
	}
}

func TestPreferencesDigestIsNotAFormControl(t *testing.T) {
	for _, digest := range []bool{false, true} {
		var out bytes.Buffer
		p := Page{Section: "preferences", Account: accounts.Account{Preferences: accounts.Preferences{WeeklyDigest: digest}}, Error: "Invalid time zone"}
		if err := Settings(p).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		html := out.String()
		if strings.Contains(html, `name="weekly_digest"`) {
			t.Fatal("HTML must not submit digest")
		}
		if strings.Contains(html, "Weekly discovery digest") || strings.Contains(html, "Not available yet") {
			t.Fatal("preferences must not advertise out-of-scope notifications")
		}
		for _, want := range []string{"Location &amp; time zone", "Notifications", "email-reminders-help", "notification-pause-help", "/me/reminders", "/me/discussions", "/me/notifications"} {
			if !strings.Contains(html, want) {
				t.Errorf("missing %q", want)
			}
		}
	}
}
