package account

import (
	"net/url"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
)

type ReminderPage struct {
	Account         accounts.Account
	CSRF, ReturnURL string
	Enabled         bool
	List            reminders.Page
	Event           *models.Event
	Own             *reminders.Item
	Lead            int
}

func (p ReminderPage) Time(at *time.Time) string {
	if at == nil {
		return "Not scheduled"
	}
	zone, err := time.LoadLocation(p.Account.Preferences.Timezone)
	if err != nil {
		zone = time.UTC
	}
	return at.In(zone).Format("2 Jan 2006, 15:04 MST")
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
		return "Waiting for a known public on-sale time"
	case "scheduled":
		return "Scheduled"
	case "paused":
		return "Paused by your email or notification preferences"
	case "refresh_failed":
		return "Waiting for fresh event details; delivery is withheld"
	case "sending":
		return "Sending"
	case "pending":
		return "Waiting to retry after SES rejected the attempt"
	case "accepted":
		return "Accepted by SES (inbox delivery is not confirmed)"
	case "uncertain":
		return "Delivery uncertain; not automatically resent"
	case "failed":
		return "Email could not be sent; operator review needed"
	case "expired":
		return "Reminder expired; no late email will be sent"
	case "cancelled":
		return "Stopped or event cancelled/postponed"
	default:
		return "Waiting to schedule"
	}
}
