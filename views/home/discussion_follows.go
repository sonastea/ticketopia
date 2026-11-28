package home

import (
	"fmt"
	"strings"

	"github.com/sonastea/ticketopia/internal/discussionfollows"
)

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

// helpfulSummary says who found the recipient's post helpful: opted-in first
// names first, everyone else only counted.
func helpfulSummary(n discussionfollows.Notification) string {
	what := ternary(n.Reply, "your reply", "your question or tip")
	names := n.HelpfulNames
	if len(names) > n.HelpfulCount {
		names = names[:n.HelpfulCount]
	}
	others := n.HelpfulCount - len(names)
	var who string
	switch {
	case len(names) == 0 && others == 1:
		who = "Someone"
	case len(names) == 0:
		who = fmt.Sprintf("%d people", others)
	case others == 0:
		who = joinNames(names)
	default:
		who = strings.Join(names, ", ") + fmt.Sprintf(" and %d %s", others, ternary(others == 1, "other", "others"))
	}
	return who + " found " + what + " helpful"
}

func joinNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
