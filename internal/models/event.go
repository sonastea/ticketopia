package models

type Event struct {
	Name     string         `json:"name"`
	Images   []EventImage   `json:"images"`
	Embedded *EventEmbedded `json:"_embedded,omitempty"`
	Place    *EventPlace    `json:"place,omitempty"`
}

type EventImage struct {
	Ratio    string `json:"ratio"`
	Url      string `json:"url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Fallback bool   `json:"fallback"`
}

type EventPlace struct {
	Name     string        `json:"name"`
	Area     Area          `json:"area"`
	Address  Address       `json:"address"`
	City     City          `json:"city"`
	State    State         `json:"state"`
	Country  Country       `json:"country"`
	Location EventLocation `json:"location"`
}

type Area struct {
	Name string `json:"name"`
}

type Address struct {
	Line1 string `json:"line1"`
	Line2 string `json:"line2"`
	Line3 string `json:"line3"`
}

type City struct {
	Name string `json:"name"`
}

type State struct {
	StateCode string `json:"stateCode"`
	Name      string `json:"name"`
}

type Country struct {
	CountryCode string `json:"countryCode"`
	Name        string `json:"name"`
}

type EventLocation struct {
	Longitude string `json:"longitude"`
	Latitude  string `json:"latitude"`
}

type EventVenue struct {
	Id         string       `json:"id"`
	Name       string       `json:"name"`
	Url        string       `json:"url"`
	PostalCode string       `json:"postalCode"`
	Images     []VenueImage `json:"images"`
}

type VenueImage struct {
	Ratio    string `json:"ratio"`
	Url      string `json:"url"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Fallback bool   `json:"fallback"`
}

type EventEmbedded struct {
	Venues []EventVenue `json:"venues"`
}

type EventsResponse struct {
	Embedded struct {
		Events []Event `json:"events"`
	} `json:"_embedded,omitempty"`
}
