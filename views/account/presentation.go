package account

import (
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
	"slices"
)

type Page struct {
	Account                                accounts.Account
	CSRF, Section, Error, Notice, NewToken string
	Tokens                                 []accounts.Credential
	Categories                             []models.Category
	CategoriesUnavailable                  bool
	EventInterests                         home.InterestCollectionPage
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
