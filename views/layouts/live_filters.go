package layouts

import "github.com/sonastea/ticketopia/internal/models"

var LiveConnectionChoices = []models.NamedID{
	{ID: "saved", Name: "Saved"},
	{ID: "interested", Name: "Interested"},
	{ID: "followed_artists", Name: "Followed artists"},
	{ID: "followed_venues", Name: "Followed venues"},
}

// LiveFilterEditor holds transient lookup results, never persisted preferences.
type LiveFilterEditor struct {
	SignedIn, Draft      bool
	Kind, Keyword, Error string
	Results              []models.FollowTarget
	Labels               map[string]string
}

func (e LiveFilterEditor) Label(kind, id string) string {
	if label := e.Labels[kind+":"+id]; label != "" {
		return label
	}
	return "Selected " + kind + ": " + id
}
