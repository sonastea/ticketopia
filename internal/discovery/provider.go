package discovery

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// Provider DTOs stay inside the adapter; neither templates nor API clients depend
// on Ticketmaster's embedded-resource layout.
type providerEvent struct {
	ID, Name, URL, Info string
	PleaseNote          string `json:"pleaseNote"`
	Images              []models.Image
	Dates               struct {
		Start struct {
			DateTime, LocalDate, LocalTime            string
			DateTBA, DateTBD, TimeTBA, NoSpecificTime bool
		}
		Timezone string
		Status   struct{ Code string }
	}
	Sales struct {
		Public struct {
			StartDateTime, EndDateTime string
			StartTBD                   bool
		}
		Presales []struct{ Name, Description, URL, StartDateTime, EndDateTime string }
	}
	PriceRanges []struct {
		Type, Currency string
		Min, Max       json.Number
	}
	Classifications []models.Classification
	Place           *providerPlace
	Embedded        struct {
		Venues []struct {
			ID, URL, Timezone string
			providerPlace
		}
		Attractions []struct{ ID, Name, URL string }
	} `json:"_embedded"`
}

type providerPlace struct {
	Name, PostalCode string
	Address          struct{ Line1, Line2, Line3 string }
	City             struct{ Name string }
	State            struct{ Name string }
	Country          struct{ Name, CountryCode string }
	Location         struct{ Latitude, Longitude string }
}

type providerPage struct {
	Size, TotalElements, TotalPages, Number int
}

type providerEvents struct {
	Embedded struct {
		Events []providerEvent
	} `json:"_embedded"`
	Page *providerPage
}

type providerSegment struct {
	ID, Name string
	Embedded struct {
		Genres []struct {
			models.NamedID
			Embedded struct {
				Subgenres []models.NamedID
			} `json:"_embedded"`
		}
	} `json:"_embedded"`
}

func normalizeEvent(raw providerEvent) models.Event {
	start := raw.Dates.Start
	status := raw.Dates.Status.Code
	if status == "" {
		status = "unknown"
	}
	event := models.Event{
		ID: "ticketmaster:" + raw.ID, Name: raw.Name,
		Source: models.Source{Provider: "ticketmaster", ID: raw.ID, URL: raw.URL},
		Start: models.EventStart{
			DateTime: instant(start.DateTime), LocalDate: optional(start.LocalDate),
			LocalTime: optional(start.LocalTime), Timezone: optional(raw.Dates.Timezone),
			DateTBA: start.DateTBA, DateTBD: start.DateTBD,
			TimeTBA: start.TimeTBA, NoSpecificTime: start.NoSpecificTime,
		},
		Status: status,
		PublicSale: models.PublicSale{
			Start: instant(raw.Sales.Public.StartDateTime), End: instant(raw.Sales.Public.EndDateTime),
			StartTBD: raw.Sales.Public.StartTBD,
		},
		Images: nonNil(raw.Images), Classifications: nonNil(raw.Classifications),
		Artists: []models.Artist{}, Venues: []models.Venue{}, Presales: []models.Presale{},
		Info: raw.Info, PleaseNote: raw.PleaseNote,
	}
	// Do not turn provider placeholder dates into known instants.
	if start.DateTBA || start.DateTBD || start.TimeTBA || start.NoSpecificTime {
		event.Start.DateTime = nil
	}
	if raw.Sales.Public.StartTBD {
		event.PublicSale.Start = nil
	}
	for _, price := range raw.PriceRanges {
		event.PriceRanges = append(event.PriceRanges, models.PriceRange{
			Type: price.Type, Currency: price.Currency,
			Min: optional(price.Min.String()), Max: optional(price.Max.String()),
		})
	}
	for _, sale := range raw.Sales.Presales {
		event.Presales = append(event.Presales, models.Presale{
			Name: sale.Name, Description: sale.Description, URL: sale.URL,
			Start: instant(sale.StartDateTime), End: instant(sale.EndDateTime),
		})
	}
	for _, artist := range raw.Embedded.Attractions {
		if sourceIDPattern.MatchString(artist.ID) {
			event.Artists = append(event.Artists, models.Artist{
				ID: "ticketmaster:" + artist.ID, Name: artist.Name,
				Source: models.Source{Provider: "ticketmaster", ID: artist.ID, URL: artist.URL},
			})
		}
	}
	for _, venue := range raw.Embedded.Venues {
		if event.Start.Timezone == nil {
			event.Start.Timezone = optional(venue.Timezone)
		}
		if !sourceIDPattern.MatchString(venue.ID) {
			// Keep location metadata without inventing a followable venue identity.
			if event.Place == nil {
				place := normalizePlace(venue.providerPlace)
				event.Place = &place
			}
			continue
		}
		event.Venues = append(event.Venues, models.Venue{
			ID: "ticketmaster:" + venue.ID, Timezone: venue.Timezone,
			Source: models.Source{Provider: "ticketmaster", ID: venue.ID, URL: venue.URL},
			Place:  normalizePlace(venue.providerPlace),
		})
	}
	if raw.Place != nil {
		place := normalizePlace(*raw.Place)
		event.Place = &place
	}
	return event
}

func normalizePlace(raw providerPlace) models.Place {
	var address []string
	for _, line := range []string{raw.Address.Line1, raw.Address.Line2, raw.Address.Line3} {
		if line != "" {
			address = append(address, line)
		}
	}
	return models.Place{
		Name: raw.Name, Address: strings.Join(address, ", "), City: raw.City.Name,
		State: raw.State.Name, Country: raw.Country.Name, CountryCode: raw.Country.CountryCode,
		PostalCode: raw.PostalCode, Latitude: raw.Location.Latitude, Longitude: raw.Location.Longitude,
	}
}

func instant(value string) *time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	return &t
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}
