package home

import (
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/moderation"
	"net/url"
)

type ReportPage struct {
	Post                                 discussions.Post
	PostID, CSRF, Reason, Context, Error string
}
type OwnModerationPage struct {
	Reports                 moderation.List[moderation.Report]
	Outcomes                moderation.List[moderation.Outcome]
	CSRF, URL               string
	OutcomesView, Moderator bool
}
type AppealForm struct{ DecisionID, CSRF, Context, Error string }
type ModerationQueuePage struct {
	List moderation.List[moderation.QueueItem]
	URL  string
	All  bool
}
type ModerationCasePage struct {
	Case                       moderation.Case
	Review                     moderation.Review
	CSRF, ViewerID, Error, URL string
}

func caseNextURL(raw, kind string, cursor *string) string {
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set(kind+"_cursor", *cursor)
	u.RawQuery = q.Encode()
	return u.String()
}

func ternary(condition bool, yes, no string) string {
	if condition {
		return yes
	}
	return no
}
func reportReason(value string) string {
	switch value {
	case "spam":
		return "Spam or scam"
	case "abuse":
		return "Abusive behaviour"
	case "private_information":
		return "Private information"
	default:
		return "Another concern"
	}
}
func moderationAction(value string) string {
	switch value {
	case "hide":
		return "Contribution hidden"
	case "restore":
		return "Contribution restored"
	default:
		return "Contribution kept"
	}
}
