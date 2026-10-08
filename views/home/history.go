package home

import (
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

func historyLabel(at time.Time) string { return at.UTC().Format("Jan 2, 2006, 15:04 UTC") }

func sortLabel(sort string) string {
	switch sort {
	case "date_desc":
		return "Date, latest first"
	case "name_asc":
		return "Name, A–Z"
	default:
		return "Date, soonest first"
	}
}
func emptySearchTitle(meta models.Freshness) string {
	if meta.Coverage != nil {
		switch meta.Coverage.Status {
		case "not_collected":
			return "Events haven't been collected yet"
		case "partial":
			return "No matches in the collected portion"
		case "stale":
			return "No matches in older results"
		}
	}
	return "No events found for these filters"
}
func emptySearchMessage(meta models.Freshness) string {
	if meta.Coverage != nil && meta.Coverage.Status != "complete" {
		return "Coverage is incomplete or older. Try a shorter date range or return after the provider recovers."
	}
	return "Try a nearby city, a wider date range, or all categories."
}
