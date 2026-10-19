package home

import (
	"fmt"
	"strings"

	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/layouts"
)

type HappeningNowPage struct {
	Enabled, Paused, SignedIn, LiveEnabled bool
	Snapshot                               activity.Snapshot
	Filters                                activity.Filters
	DraftFilters                           *activity.Filters
	Editor                                 layouts.LiveFilterEditor
	Categories                             []models.Category
	Revision                               uint64
	CSRF, ReturnURL, Error                 string
}

func (p HappeningNowPage) summary() string {
	place := "Everywhere"
	if p.Filters.Location.Mode == "city" {
		place = p.Filters.Location.City + ", " + p.Filters.Location.Country
	}
	names := []string{}
	for _, id := range p.Filters.CategoryIDs {
		name := id
		for _, c := range p.Categories {
			if c.ID == id {
				name = c.Name
			}
		}
		names = append(names, name)
	}
	types := "All event types"
	if len(names) > 0 {
		types = strings.Join(names, " or ")
	}
	parts := []string{place, types}
	for _, d := range []struct {
		kind string
		ids  []string
	}{{"artist", p.Filters.ArtistIDs}, {"venue", p.Filters.VenueIDs}} {
		labels := []string{}
		for _, id := range d.ids {
			labels = append(labels, p.Editor.Label(d.kind, id))
		}
		if len(labels) > 0 {
			parts = append(parts, strings.Join(labels, " or "))
		}
	}
	connections := []string{}
	for _, id := range p.Filters.Connections.Sources {
		for _, choice := range layouts.LiveConnectionChoices {
			if choice.ID == id {
				connections = append(connections, choice.Name)
			}
		}
	}
	if len(connections) == 0 {
		connections = append(connections, "All events")
	}
	return strings.Join(append(parts, strings.Join(connections, " or ")), " · ")
}

func (p HappeningNowPage) editingFilters() activity.Filters {
	if p.DraftFilters != nil {
		return *p.DraftFilters
	}
	return p.Filters
}

func activityAttribution(item activity.Item) string {
	if item.RecentRecommendations > 1 {
		return fmt.Sprintf("%d people recommended ", item.RecentRecommendations)
	}
	if item.ActorFirstName != nil && *item.ActorFirstName != "" {
		return *item.ActorFirstName + " recommended "
	}
	return "Someone recommended "
}

func (p HappeningNowPage) relativeTime(item activity.Item) string {
	minutes := int(p.Snapshot.AsOf.Sub(item.LatestRecommendedAt).Minutes())
	if minutes < 1 {
		return "just now"
	}
	if minutes == 1 {
		return "1 minute ago"
	}
	return fmt.Sprintf("%d minutes ago", minutes)
}
