package models

// FollowTarget keeps provider identity independent of a user's private follows.
// Location is supplied for venues, not invented for artists.
type FollowTarget struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Source   Source `json:"source"`
	Location *Place `json:"location,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

func ArtistTarget(a Artist) FollowTarget {
	return FollowTarget{Kind: "artist", ID: a.ID, Name: a.Name, Source: a.Source}
}
func VenueTarget(v Venue) FollowTarget {
	return FollowTarget{Kind: "venue", ID: v.ID, Name: v.Name, Source: v.Source, Location: &v.Place, Timezone: v.Timezone}
}

type CatalogDetail struct {
	Item FollowTarget `json:"item"`
	Meta Freshness    `json:"meta"`
}
type CatalogList struct {
	Items      []FollowTarget `json:"items"`
	NextCursor *string        `json:"next_cursor"`
	Total      int            `json:"total"`
	Limited    bool           `json:"limited"`
	Meta       Freshness      `json:"meta"`
}
