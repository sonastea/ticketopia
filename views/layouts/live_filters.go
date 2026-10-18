package layouts

import "github.com/sonastea/ticketopia/internal/models"

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
