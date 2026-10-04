package models

import "time"

// Event is a client-independent view of one occurrence, never a title-based group.
type Event struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	Source          Source           `json:"source"`
	Start           EventStart       `json:"start"`
	Status          string           `json:"status"`
	PublicSale      PublicSale       `json:"public_sale"`
	Presales        []Presale        `json:"presales"`
	PriceRanges     []PriceRange     `json:"price_ranges"`
	Images          []Image          `json:"images"`
	Artists         []Artist         `json:"artists"`
	Venues          []Venue          `json:"venues"`
	Place           *Place           `json:"place"`
	Classifications []Classification `json:"classifications"`
	Info            string           `json:"info"`
	PleaseNote      string           `json:"please_note"`
}

type Source struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url"`
}

type EventStart struct {
	DateTime       *time.Time `json:"date_time"`
	LocalDate      *string    `json:"local_date"`
	LocalTime      *string    `json:"local_time"`
	Timezone       *string    `json:"timezone"`
	DateTBA        bool       `json:"date_tba"`
	DateTBD        bool       `json:"date_tbd"`
	TimeTBA        bool       `json:"time_tba"`
	NoSpecificTime bool       `json:"no_specific_time"`
}

type PublicSale struct {
	Start    *time.Time `json:"start"`
	End      *time.Time `json:"end"`
	StartTBD bool       `json:"start_tbd"`
}

type Presale struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	URL         string     `json:"url"`
	Start       *time.Time `json:"start"`
	End         *time.Time `json:"end"`
}

type PriceRange struct {
	Type         string  `json:"type"`
	Currency     string  `json:"currency"`
	Min          *string `json:"min"`
	Max          *string `json:"max"`
	FeesIncluded *bool   `json:"fees_included"`
}

type Image struct {
	URL      string `json:"url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Ratio    string `json:"ratio"`
	Fallback bool   `json:"fallback"`
}

type Artist struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source Source `json:"source"`
}

type Place struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	City        string `json:"city"`
	State       string `json:"state"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	PostalCode  string `json:"postal_code"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

type Venue struct {
	ID       string `json:"id"`
	Source   Source `json:"source"`
	Timezone string `json:"timezone"`
	Place
}

type NamedID struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Classification struct {
	Primary  bool     `json:"primary"`
	Segment  *NamedID `json:"segment"`
	Genre    *NamedID `json:"genre"`
	Subgenre *NamedID `json:"subgenre"`
}

type Genre struct {
	NamedID
	Subgenres []NamedID `json:"subgenres"`
}

type Category struct {
	NamedID
	Genres []Genre `json:"genres"`
}

type CategoryList struct {
	Items []Category `json:"items"`
	Meta  Freshness  `json:"meta"`
}

type Freshness struct {
	DataAsOf time.Time `json:"data_as_of"`
	Stale    bool      `json:"stale"`
}

type EventList struct {
	Items      []Event   `json:"items"`
	NextCursor *string   `json:"next_cursor"`
	Total      int       `json:"total"`
	Limited    bool      `json:"limited"`
	Meta       Freshness `json:"meta"`
}

type EventDetail struct {
	Item Event     `json:"item"`
	Meta Freshness `json:"meta"`
}

type GenreList struct {
	Items []Genre   `json:"items"`
	Meta  Freshness `json:"meta"`
}
