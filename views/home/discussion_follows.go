package home

import "github.com/sonastea/ticketopia/internal/discussionfollows"
import "github.com/sonastea/ticketopia/views/layouts"

type DiscussionFollowPage struct {
	Navigation            layouts.AccountNavPermissions
	CSRF, URL, Error      string
	Notifications, Paused bool
	Follows               discussionfollows.Page[discussionfollows.Follow]
	Inbox                 discussionfollows.Page[discussionfollows.Notification]
}

func conversationFrequency(frequency string) string {
	switch frequency {
	case "immediate":
		return "Every new reply"
	case "daily":
		return "Daily summary · UTC"
	case "weekly":
		return "Weekly summary · UTC"
	case "muted":
		return "Muted"
	default:
		return "Frequency unavailable"
	}
}
