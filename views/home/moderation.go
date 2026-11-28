package home

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/moderation"
	"github.com/sonastea/ticketopia/views/layouts"
)

type ReportPage struct {
	Post                                 discussions.Post
	PostID, CSRF, Reason, Context, Error string
}
type OwnModerationPage struct {
	Reports      moderation.List[moderation.Report]
	Outcomes     moderation.List[moderation.Outcome]
	CSRF, URL    string
	OutcomesView bool
	Navigation   layouts.AccountNavPermissions
}
type AppealForm struct{ DecisionID, CSRF, Context, Error string }
type ModerationQueuePage struct {
	List moderation.List[moderation.QueueItem]
	URL  string
	All  bool
	// Later is a page after the first (a cursor), so empty means "no more".
	Later bool
	// Pending counts cases awaiting review, shown when PendingKnown.
	Pending      int
	PendingKnown bool
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

// reasonCount reads a pending reason with its count: "Spam or scam × 2".
func reasonCount(r moderation.ReasonCount) string {
	if r.Count > 1 {
		return fmt.Sprintf("%s × %d", reportReason(r.Reason), r.Count)
	}
	return reportReason(r.Reason)
}

// pendingSummary names what a decision would close, leaving out zero counts.
func pendingSummary(reports, appeals int) string {
	parts := []string{}
	if reports > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", reports, ternary(reports == 1, "report", "reports")))
	}
	if appeals > 0 {
		parts = append(parts, fmt.Sprintf("%d author review %s", appeals, ternary(appeals == 1, "request", "requests")))
	}
	return strings.Join(parts, " · ")
}

// caseKind names the contribution under review.
func caseKind(p discussions.Post) string {
	return ternary(p.ID == p.ThreadID, "Question or tip", "Reply")
}

// caseTitle names a case distinctly: its kind, its author when shown, and the
// event, so several replies on one event don't share a link name.
func caseTitle(p discussions.Post) string {
	if p.Profile != nil && p.Profile.DisplayName != "" && !p.Hidden && !p.Removed {
		return caseKind(p) + " by " + p.Profile.DisplayName + " on " + p.Event.Name
	}
	return caseKind(p) + " on " + p.Event.Name
}

// decisionChoice is one action a moderator can record, with its effect.
type decisionChoice struct{ Value, Label, Effect string }

// decisionChoices lists the actions this case allows, each with what it does.
func decisionChoices(c moderation.Case) []decisionChoice {
	choices := []decisionChoice{}
	if !c.Post.Hidden {
		choices = append(choices, decisionChoice{"keep", "Keep it", "The contribution stays as it is. People with pending reports see your reason; the author is not notified."})
	}
	if !c.Post.Removed {
		label := "Hide it"
		if c.Post.Hidden {
			label = "Keep it hidden"
		}
		choices = append(choices, decisionChoice{"hide", label, "Its text, author and Helpful count are withheld; replies stay. The author and people with pending reports see your reason, and the author can ask for another review."})
	}
	if c.Post.Hidden && !c.Post.Removed {
		choices = append(choices, decisionChoice{"restore", "Restore it", "The contribution is visible again. The author and people with pending reports see your reason."})
	}
	return choices
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
