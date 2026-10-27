package home

import (
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

func historyLabel(at time.Time) string { return at.UTC().Format("Jan 2, 2006, 15:04 UTC") }

func sortLabel(sort string) string {
	switch sort {
	// Short enough to share the results heading line on compact screens.
	case "date_desc":
		return "Latest first"
	case "name_asc":
		return "Name A–Z"
	default:
		return "Soonest first"
	}
}
// coverageNote is the coverage clause of the merged results line. Complete
// coverage needs none, and stale coverage would repeat the stale notice.
func coverageNote(meta models.Freshness) string {
	if meta.Coverage == nil {
		return ""
	}
	switch meta.Coverage.Status {
	case "not_collected":
		return "Ticketmaster couldn't be refreshed for every requested date, so some events may be missing."
	case "partial":
		return "Partial coverage: more events may exist. Try a shorter date range."
	case "stale":
		if !meta.Stale {
			return "Older results while collection catches up."
		}
	}
	return ""
}

// resultsNoticeSummary is the one-line form of a results warning; the full
// explanation sits behind a disclosure. Ordinary freshness needs no summary.
func resultsNoticeSummary(meta models.Freshness) string {
	partial := meta.Coverage != nil && meta.Coverage.Status == "partial"
	switch {
	case meta.Stale && partial:
		return "Last-known, partial results"
	case meta.Stale:
		return "Showing last-known results"
	case meta.Coverage == nil:
		return ""
	}
	switch meta.Coverage.Status {
	case "not_collected":
		// Shown above results too, so it must not read as "nothing here".
		return "Results may be incomplete"
	case "partial":
		return "Partial results: more events may exist"
	case "stale":
		return "Older results"
	}
	return ""
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
