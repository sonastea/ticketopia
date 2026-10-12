package account

import (
	"net/url"
	"strconv"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/views/layouts"
)

type ReminderPage struct {
	Navigation      layouts.AccountNavPermissions
	Account         accounts.Account
	CSRF, ReturnURL string
	Enabled         bool
	List            reminders.Page
	Event           *models.Event
	Own             *reminders.Item
	Lead            int
	Choice, Custom  string
	Submitted       bool
	Error, Notice   string
	Now             time.Time
}

func (p ReminderPage) Time(at *time.Time) string {
	if at == nil {
		return "Not scheduled"
	}
	zone, err := time.LoadLocation(p.Zone())
	if err != nil {
		zone = time.UTC
	}
	return at.In(zone).Format("2 Jan 2006, 15:04 MST") + " (" + zone.String() + ")"
}
func (p ReminderPage) Zone() string {
	if _, err := time.LoadLocation(p.Account.Preferences.Timezone); err != nil || p.Account.Preferences.Timezone == "" {
		return "UTC"
	}
	return p.Account.Preferences.Timezone
}
func (p ReminderPage) TimingChoice() string {
	if p.Submitted {
		return p.Choice
	}
	switch p.Lead {
	case 30, 60, 1440:
		return strconv.Itoa(p.Lead)
	default:
		return "custom"
	}
}
func (p ReminderPage) CustomValue() string {
	if p.Submitted {
		return p.Custom
	}
	return strconv.Itoa(p.Lead)
}
func (p ReminderPage) PreviewLead() int {
	value := p.TimingChoice()
	switch value {
	case "30", "60", "1440":
	case "custom":
		value = p.CustomValue()
	default:
		return 0
	}
	lead, err := strconv.Atoi(value)
	if err != nil || reminders.Lead(lead) != nil {
		return 0
	}
	return lead
}
func (p ReminderPage) SaleISO() string {
	if p.Event == nil || p.Event.PublicSale.StartTBD || p.Event.PublicSale.Start == nil {
		return ""
	}
	return p.Event.PublicSale.Start.Format(time.RFC3339)
}
func (p ReminderPage) SaleTime() string {
	if p.SaleISO() == "" {
		return "Time not announced"
	}
	return p.Time(p.Event.PublicSale.Start)
}
func (p ReminderPage) Preview() string {
	if p.PreviewLead() == 0 {
		return "Choose 1–10080 whole minutes to preview the target."
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	state, _, due := reminders.Schedule(*p.Event, p.PreviewLead(), now)
	switch state {
	case "cancelled":
		return "The event is cancelled or postponed; delivery is withheld."
	case "pending_date":
		return "Pending a known public-sale time; no target is scheduled yet."
	case "expired":
		return "Public tickets are already on sale; no late reminder will be sent."
	}
	if !due.After(now) {
		return "This lead time has already elapsed. A future sale can be eligible immediately after saving, subject to preferences and fresh evidence."
	}
	return "Target: " + p.Time(due) + ". Delivery depends on preferences, fresh event details and email service availability."
}
func reminderLead(lead int) string {
	switch lead {
	case 30:
		return "30 minutes"
	case 60:
		return "1 hour"
	case 1440:
		return "1 day"
	default:
		return strconv.Itoa(lead) + " minutes"
	}
}
func reminderURL(id string) string { return "/me/reminders/" + url.PathEscape(id) }
func (p ReminderPage) Next() string {
	if p.List.NextCursor == nil {
		return ""
	}
	return "/me/reminders?" + url.Values{"cursor": {*p.List.NextCursor}}.Encode()
}
func reminderState(state string) string {
	switch state {
	case "pending_date":
		return "Pending sale time"
	case "scheduled":
		return "Scheduled"
	case "paused":
		return "Paused"
	case "refresh_failed":
		return "Waiting for fresh evidence"
	case "sending":
		return "Sending"
	case "pending":
		return "Waiting to retry"
	case "accepted":
		return "SES accepted"
	case "uncertain":
		return "Uncertain"
	case "failed":
		return "Failed"
	case "expired":
		return "Expired"
	case "cancelled":
		return "Stopped"
	default:
		return "Awaiting scheduling"
	}
}

func reminderStateDetail(state string) string {
	switch state {
	case "pending_date":
		return "Waiting for a known public-sale time; no target is scheduled yet."
	case "scheduled":
		return "Target configured, not guaranteed delivery. Preferences and fresh event details are checked before sending."
	case "paused":
		return "Withheld by email permission or Pause all notifications. Your timing choice is retained."
	case "refresh_failed":
		return "Fresh event details are missing; delivery is withheld until they can be checked."
	case "sending":
		return "An email attempt is in progress. Stopping cannot recall a message already being sent."
	case "pending":
		return "SES rejected the attempt. A retry is eligible only before the original deadline."
	case "accepted":
		return "Accepted by SES; inbox delivery is not confirmed. This sale will not be automatically resent."
	case "uncertain":
		return "The outcome of the send is unknown. It is not automatically resent, to avoid duplicates."
	case "failed":
		return "The email could not be sent. Operator review is needed; it is not automatically resent."
	case "expired":
		return "The reminder window has passed. No late email will be sent for this sale."
	case "cancelled":
		return "The reminder is stopped or the event is cancelled/postponed. No new email is eligible."
	default:
		return "Your choice is saved; delivery readiness has not been established yet."
	}
}
