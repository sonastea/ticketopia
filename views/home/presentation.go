package home

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type SearchPage struct {
	Filters           discovery.Query
	Events            models.EventList
	Genres            models.GenreList
	GenresUnavailable bool
	LocationSource    string
	NeedsLocation     bool
	Error             string
	ReturnURL         string
	SelectedID        string
	Selected          *models.EventDetail
	SelectionError    string
	Section           string
}

type EventPage struct {
	Detail    models.EventDetail
	Section   string
	ReturnURL string
	Error     string
}

func eventPageTitle(page EventPage) string {
	if page.Error != "" {
		return "Event unavailable"
	}
	return page.Detail.Item.Name
}

func eventURL(id, section, returnURL string) string {
	values := url.Values{}
	if returnURL != "" && returnURL != "/" {
		values.Set("return_to", returnURL)
	}
	if section != "" && section != "overview" {
		values.Set("section", section)
	}
	path := "/events/" + url.PathEscape(id)
	if len(values) > 0 {
		path += "?" + values.Encode()
	}
	return path
}

func sectionURL(page EventPage, section string) string {
	return eventURL(page.Detail.Item.ID, section, page.ReturnURL)
}

func datePart(start models.EventStart, format string) string {
	if start.LocalDate != nil && !start.DateTBA && !start.DateTBD {
		if date, err := time.Parse(time.DateOnly, *start.LocalDate); err == nil {
			return date.Format(format)
		}
	}
	if format == "Jan" {
		return "Date"
	}
	return "TBA"
}

func genreLabel(event models.Event) string {
	for _, classification := range event.Classifications {
		if classification.Genre != nil && classification.Genre.Name != "Undefined" {
			return classification.Genre.Name
		}
	}
	return "Music"
}

func dateRangeLabel(query discovery.Query) string {
	start, firstErr := time.Parse(time.DateOnly, query.StartDate)
	end, lastErr := time.Parse(time.DateOnly, query.EndDate)
	if firstErr != nil || lastErr != nil {
		return "Choose dates"
	}
	return start.Format("Jan 2") + " – " + end.Format("Jan 2, 2006")
}

func genreFilterLabel(page SearchPage) string {
	for _, genre := range page.Genres.Items {
		if genre.ID == page.Filters.GenreID {
			return genre.Name
		}
	}
	return "All music"
}

func cityLabel(page SearchPage) string {
	if page.Filters.Country != "" {
		return page.Filters.City + ", " + page.Filters.Country
	}
	return page.Filters.City
}

func nextURL(page SearchPage) string {
	values := page.Filters.Values()
	if page.Events.NextCursor != nil {
		values.Set("cursor", *page.Events.NextCursor)
	}
	return "/?" + values.Encode()
}

func resultsTitle(total int) string {
	if total == 1 {
		return "1 show"
	}
	return fmt.Sprintf("%d shows", total)
}

func startLabel(start models.EventStart) string {
	if start.DateTBA || start.DateTBD || start.LocalDate == nil {
		return "Show date to be announced"
	}
	date, err := time.Parse(time.DateOnly, *start.LocalDate)
	if err != nil {
		return "Show date to be announced"
	}
	label := date.Format("Mon, Jan 2, 2006")
	if start.NoSpecificTime {
		return label
	}
	if start.TimeTBA || start.LocalTime == nil {
		return label + " · Time to be announced"
	}
	if clock, err := time.Parse(time.TimeOnly, *start.LocalTime); err == nil {
		return label + " · " + clock.Format("3:04 PM")
	}
	return label + " · Time to be announced"
}

func venueLabel(event models.Event) string {
	var place models.Place
	if len(event.Venues) > 0 {
		place = event.Venues[0].Place
	} else if event.Place != nil {
		place = *event.Place
	}
	var parts []string
	for _, value := range []string{place.Name, place.City, place.State, place.CountryCode} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	if len(parts) == 0 {
		return "Venue to be announced"
	}
	return strings.Join(parts, " · ")
}

func artistsLabel(artists []models.Artist) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if artist.Name != "" {
			names = append(names, artist.Name)
		}
	}
	return strings.Join(names, ", ")
}

func statusLabel(status string) string {
	switch status {
	case "onsale":
		return "On sale"
	case "offsale":
		return "Off sale"
	case "canceled", "cancelled":
		return "Canceled"
	case "postponed":
		return "Postponed"
	case "rescheduled":
		return "Rescheduled"
	default:
		return "Sale status unknown"
	}
}

func priceLabel(prices []models.PriceRange) string {
	for _, price := range prices {
		if price.Min != nil && price.Currency != "" {
			label := price.Currency + " " + *price.Min
			if price.Max != nil && *price.Max != *price.Min {
				label += " to " + *price.Max
			}
			return label
		}
	}
	return "Price not listed"
}

func saleLabel(event models.Event) string {
	if event.PublicSale.Start == nil {
		return ""
	}
	date := event.PublicSale.Start.UTC()
	if event.Start.Timezone != nil {
		if zone, err := time.LoadLocation(*event.Start.Timezone); err == nil {
			date = date.In(zone)
		}
	}
	return "Public sale: " + date.Format("Jan 2, 2006, 3:04 PM MST")
}

func freshnessLabel(at time.Time) string { return at.UTC().Format("Jan 2, 15:04 UTC") }

func imageURL(images []models.Image) string { return sizedImageURL(images, 480) }

func sizedImageURL(images []models.Image, width int) string {
	best, distance := "", int(^uint(0)>>1)
	for _, image := range images {
		diff := image.Width - width
		if diff < 0 {
			diff = -diff
		}
		if image.URL != "" && diff < distance {
			best, distance = image.URL, diff
		}
	}
	return best
}

func eventPlace(event models.Event) models.Place {
	if len(event.Venues) > 0 {
		return event.Venues[0].Place
	}
	if event.Place != nil {
		return *event.Place
	}
	return models.Place{}
}

func hasGenre(genres []models.Genre, id string) bool {
	for _, genre := range genres {
		if genre.ID == id {
			return true
		}
	}
	return false
}
