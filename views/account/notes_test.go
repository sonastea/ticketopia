package account

import (
	"bytes"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/htmlcheck"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

// Every account page keeps to one explanatory note per section, the budget
// that keeps them from sliding back into clutter.
func TestAccountPagesKeepTheNotesBudget(t *testing.T) {
	expires := time.Now().Add(24 * time.Hour)
	name := "Riley"
	page := Page{
		Account:            accounts.Account{ID: "a1", DisplayName: "Riley Review", Email: "r@example.test", InterestVisibility: "private", ActivityFirstName: &name},
		CSRF:               "csrf",
		Tokens:             []accounts.Credential{{ID: "t1", Name: "Script", ExpiresAt: expires}},
		ConnectionsEnabled: true,
		Connections:        []oauth.Grant{{ID: "g1", ClientName: "Agent", CreatedAt: time.Now()}},
		Categories:         []models.Category{{NamedID: models.NamedID{ID: "music", Name: "Music"}}},
		Activity:           []PublicActivity{{Event: models.Event{ID: "ticketmaster:e", Name: "Show"}, Recommended: true}},
	}
	failed := page
	failed.Error, failed.Account.Bio = "Display name is required.", ""
	for name, component := range map[string]templ.Component{
		"profile": Profile(page), "profile after a failed save": Profile(failed),
		"privacy": Privacy(page), "preferences": Preferences(page), "security": Security(page),
	} {
		var out bytes.Buffer
		if err := component.Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		over, err := htmlcheck.NotesOverBudget(&out, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(over) > 0 {
			t.Errorf("%s exceeds the notes budget: %v", name, over)
		}
	}
}

// The public profile and sign-in keep the budget too.
func TestPublicProfileAndSignInKeepTheNotesBudget(t *testing.T) {
	for name, component := range map[string]templ.Component{
		"public profile, no bio": Public(accounts.PublicProfile{ID: "a1", DisplayName: "Riley"}, interests.List{}, "/users/a1", "", recommendations.List{}, ""),
		// An unavailable list's message sits under main beside the card's
		// own note, which the card's section keeps apart.
		"public profile, interest unavailable": Public(accounts.PublicProfile{ID: "a1", DisplayName: "Riley"}, interests.List{}, "/users/a1", "Public event interest is unavailable on this server.", recommendations.List{}, ""),
		"sign in to save":        SignInContext(true, "/events/ticketmaster:E?section=community", "", "Show", "save"),
	} {
		var out bytes.Buffer
		if err := component.Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		over, err := htmlcheck.NotesOverBudget(&out, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(over) > 0 {
			t.Errorf("%s exceeds the notes budget: %v", name, over)
		}
	}
}
