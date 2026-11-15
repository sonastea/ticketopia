package account

import (
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/views/home"
	"github.com/sonastea/ticketopia/views/layouts"
	"net/url"
	"slices"
	"strings"
)

// returnTo has already passed the HTTP allowlist. Private destinations cannot
// be continued as a guest; public journeys retain their exact native URL.
func signInBackURL(returnTo string) string {
	u, err := url.Parse(returnTo)
	if err == nil && (u.Path == "/" || strings.HasPrefix(u.Path, "/events/") || u.Path == "/community" || strings.HasPrefix(u.Path, "/community/") || strings.HasPrefix(u.Path, "/users/")) {
		return returnTo
	}
	return "/"
}

func signInBackLabel(returnTo string) string {
	u, _ := url.Parse(returnTo)
	if u != nil {
		if strings.HasPrefix(u.Path, "/events/") || u.Query().Get("selected_event") != "" {
			return "Return to event without signing in"
		}
		if u.Path == "/" && u.RawQuery != "" {
			return "Return to results without signing in"
		}
		if signInBackURL(returnTo) != "/" {
			return "Return without signing in"
		}
	}
	return "Discover without signing in"
}

func googleStartURL(returnTo, intent string) string {
	values := url.Values{"return_to": {returnTo}}
	if intent != "" {
		values.Set("intent", intent)
	}
	return "/auth/google/start?" + values.Encode()
}

func signInSection(returnTo string) string {
	u, _ := url.Parse(returnTo)
	if u == nil || (u.Query().Get("selected_event") == "" && !strings.HasPrefix(u.Path, "/events/")) {
		return ""
	}
	if strings.Contains(strings.TrimPrefix(u.Path, "/events/"), "/discussions/") {
		return "Conversation"
	}
	switch u.Query().Get("section") {
	case "discussion":
		return "Questions & tips"
	case "community":
		return "Recommendations"
	default:
		return "Overview"
	}
}

type Page struct {
	LivePreferences                        *accounts.LivePreferences
	LiveEditor                             layouts.LiveFilterEditor
	Account                                accounts.Account
	CSRF, Section, Error, Notice, NewToken string
	Tokens                                 []accounts.Credential
	Categories                             []models.Category
	CategoriesUnavailable                  bool
	EventInterests                         home.InterestCollectionPage
	ModerationEnabled, Moderator           bool
	// Connections are agents signed in through OAuth, when that is enabled.
	Connections        []oauth.Grant
	ConnectionsEnabled bool
	// ConnectionsOpen expands the list after a disconnection.
	ConnectionsOpen bool
}

// ConsentPage asks whether to connect an agent to the account.
type ConsentPage struct {
	ClientName, ClientHost, RedirectHost string
	// LocalRedirect is set when the app receives the result on this device.
	LocalRedirect bool
	// Write is set when the app asks to change saves and reminders.
	Write bool
	// Publish is set when the app asks to act publicly in the person's
	// name; the checkbox for it starts unchecked.
	Publish bool
	// DisplayName is the public name posts would appear under.
	DisplayName         string
	Email, CSRF, Action string
	Fields              []HiddenField
}

// DraftsPage lists the account's pending agent drafts.
type DraftsPage struct {
	Navigation layouts.AccountNavPermissions
	Enabled    bool
	CSRF       string
	Drafts     []drafts.Draft
	// Current is the account's recommendation per drafted event, nil when
	// there is none; publishing a draft replaces its reason.
	Current map[string]*recommendations.Item
	// Posts are reply drafts' thread roots and parents by ID, nil when one
	// no longer exists on its event; such a draft can only be discarded.
	Posts map[string]*discussions.Post
	// Notice confirms the last decision; NoticeURL, labelled NoticeLink,
	// opens what was published.
	Notice, NoticeURL, NoticeLink string
}

// draftEventName falls back to the ID when a stored snapshot has no name.
func draftEventName(d drafts.Draft) string {
	if d.Event.Name != "" {
		return d.Event.Name
	}
	return d.EventID
}

// draftEventURL opens the part of the event a draft would publish to.
func draftEventURL(d drafts.Draft) string {
	switch d.Kind {
	case drafts.KindQuestion:
		return "/events/" + url.PathEscape(d.EventID) + "?section=discussion"
	case drafts.KindReply:
		return home.ThreadURL(d.EventID, d.ThreadID, "")
	}
	return "/events/" + url.PathEscape(d.EventID) + "?section=community"
}

// draftKindLabel names what a draft would publish, for its source line.
func draftKindLabel(d drafts.Draft) string {
	switch d.Kind {
	case drafts.KindQuestion:
		return "Question or tip"
	case drafts.KindReply:
		return "Reply"
	}
	return "Recommendation reason"
}

// draftPublishLabel is the draft's Publish button.
func draftPublishLabel(d drafts.Draft) string {
	switch d.Kind {
	case drafts.KindQuestion:
		return "Publish question or tip"
	case drafts.KindReply:
		return "Publish reply"
	}
	return "Publish recommendation"
}

// replyContext is what a reply draft answers: the reply it targets, or the
// thread's question or tip. ok is false when either no longer exists, so
// the draft cannot be published.
func (p DraftsPage) replyContext(d drafts.Draft) (target *discussions.Post, ok bool) {
	root := p.Posts[d.ThreadID]
	if root == nil {
		return nil, false
	}
	if d.ParentID == "" {
		return root, true
	}
	parent := p.Posts[d.ParentID]
	return parent, parent != nil
}

// publishable reports whether the draft can be published as it stands: a
// reply needs its conversation and target still on the event.
func (p DraftsPage) publishable(d drafts.Draft) bool {
	if d.Kind != drafts.KindReply {
		return true
	}
	_, ok := p.replyContext(d)
	return ok
}

// replyLead names a reply target's author before its quoted text, or says
// the text is no longer visible.
func replyLead(post *discussions.Post) string {
	switch {
	case post.Removed:
		return "replying to a contribution its author removed."
	case post.Hidden:
		return "replying to a contribution a moderator hid."
	case post.Profile != nil:
		return "replying to " + post.Profile.DisplayName + ":"
	}
	return "replying to this contribution:"
}

// HiddenField carries an authorization request parameter through the form.
type HiddenField struct{ Name, Value string }

// signInConnects reports a sign-in that resumes an agent connection.
func signInConnects(returnTo string) bool {
	return strings.HasPrefix(returnTo, "/oauth/authorize?")
}

func (p Page) Active() string {
	if p.Section == "interests" {
		return "interests"
	}
	return "profile"
}
func (p Page) Title() string {
	switch p.Section {
	case "preferences":
		return "Your preferences"
	case "interests":
		return "Your interests"
	default:
		return "Your profile"
	}
}
func (p Page) Selected(id string) bool { return slices.Contains(p.Account.Preferences.CategoryIDs, id) }
func (p Page) MissingCategories() []string {
	result := []string{}
	for _, id := range p.Account.Preferences.CategoryIDs {
		found := false
		for _, c := range p.Categories {
			if c.ID == id {
				found = true
			}
		}
		if !found {
			result = append(result, id)
		}
	}
	return result
}
