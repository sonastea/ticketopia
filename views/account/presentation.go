package account

import (
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
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
	Account                                accounts.Account
	CSRF, Section, Error, Notice, NewToken string
	Tokens                                 []accounts.Credential
	Categories                             []models.Category
	CategoriesUnavailable                  bool
	EventInterests                         home.InterestCollectionPage
	ModerationEnabled, Moderator           bool
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
