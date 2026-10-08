package home

import (
	"net/url"

	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

type RadarPage struct {
	Enabled          bool
	ReturnURL, Error string
	List             radar.List
	Saves            SaveView
	Interests        InterestView
}

func (p RadarPage) nextURL() string {
	u, _ := url.Parse(p.ReturnURL)
	v := u.Query()
	if p.List.NextCursor != nil {
		v.Set("cursor", *p.List.NextCursor)
	}
	return "/radar?" + v.Encode()
}
func (p RadarPage) events() []models.Event {
	events := []models.Event{}
	for _, item := range p.List.Items {
		events = append(events, item.Event)
	}
	return events
}
func (p RadarPage) matches() map[string]radar.Item {
	items := map[string]radar.Item{}
	for _, item := range p.List.Items {
		items[item.Event.ID] = item
	}
	return items
}
func isRadarURL(raw string) bool { u, err := url.Parse(raw); return err == nil && u.Path == "/radar" }
