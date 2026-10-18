package home

import (
	"fmt"
	"strings"

	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/models"
)

type HappeningNowPage struct {
	Enabled, Paused, SignedIn, LiveEnabled bool
	Snapshot                               activity.Snapshot
	Filters                                activity.Filters
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
	return place + " · " + types + " · All events"
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
