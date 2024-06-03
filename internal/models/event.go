package models

type Event struct {
	Name       string         `json:"name"`
	Info       string         `json:"info"`
	PleaseNote string         `json:"please_note"`
	Images     []EventImage   `json:"images"`
	Embedded   *EventEmbedded `json:"_embedded,omitempty"`
	Place      *EventPlace    `json:"place,omitempty"`
	Promoter   *EventPromoter `json:"promoter,omitempty"`
	Sales      *EventSales    `json:"sales"`
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

type EventPromoter struct {
	Id          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type EventSales struct {
	Public   EventSalesPublic     `json:"public"`
	Presales []EventSalesPresales `json:"presales"`
}

type EventSalesPublic struct {
	StartDateTime string `json:"startDateTime"`
	EndDateTime   string `json:"endDateTime"`
}

type EventSalesPresales struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Url           string `json:"url"`
	StartDateTime string `json:"startDateTime"`
	EndDateTime   string `json:"endDateTime"`
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
	Id         string        `json:"id"`
	Name       string        `json:"name"`
	Url        string        `json:"url"`
	PostalCode string        `json:"postalCode"`
	Images     []VenueImage  `json:"images"`
	Address    Address       `json:"address"`
	City       City          `json:"city"`
	State      State         `json:"state"`
	Country    Country       `json:"country"`
	Location   EventLocation `json:"location"`
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
