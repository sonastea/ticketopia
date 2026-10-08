package home

import (
	"net/url"
	"strings"

	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/models"
)

type FollowsPage struct {
	Enabled, Searched                                     bool
	CSRF, ReturnURL, Kind, Keyword, City, Country, Filter string
	Error, SearchError                                    string
	List                                                  follows.List
	Search                                                models.CatalogList
	States                                                map[string]bool
}

func (p FollowsPage) nextFollowURL(search bool) string {
	u, _ := url.Parse(p.ReturnURL)
	v := u.Query()
	key, cursor := "cursor", p.List.NextCursor
	if search {
		key, cursor = "search_cursor", p.Search.NextCursor
	}
	if cursor != nil {
		v.Set(key, *cursor)
	}
	return "/follows?" + v.Encode()
}
func followAction(t models.FollowTarget) string {
	return "/follows/" + t.Kind + "/" + url.PathEscape(t.ID)
}
func followVerb(followed bool) string {
	if followed {
		return "unfollow"
	}
	return "follow"
}
func followLabel(followed bool) string {
	if followed {
		return "Unfollow"
	}
	return "Follow"
}
func followKind(kind string) string {
	if kind == "venue" {
		return "Venue"
	}
	return "Artist"
}
func followLocation(t models.FollowTarget) string {
	if t.Location == nil {
		return "Location not supplied"
	}
	parts := []string{}
	for _, value := range []string{t.Location.Address, t.Location.City, t.Location.State, t.Location.Country} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "Location not supplied"
	}
	return strings.Join(parts, ", ")
}
