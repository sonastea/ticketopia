package history

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
)

type Config struct {
	Enabled  bool
	Cities   []Scope
	Days     int
	Interval time.Duration
}

func ConfigFromEnv() (Config, error) {
	c := Config{Days: 90, Interval: 6 * time.Hour}
	if v := os.Getenv("EVENT_HISTORY_ENABLED"); v != "" {
		var err error
		c.Enabled, err = strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("EVENT_HISTORY_ENABLED must be true or false")
		}
	}
	if !c.Enabled {
		return c, nil
	}
	if json.Unmarshal([]byte(os.Getenv("EVENT_HISTORY_CITIES")), &c.Cities) != nil || len(c.Cities) < 1 || len(c.Cities) > 20 {
		return c, fmt.Errorf("EVENT_HISTORY_CITIES must be a JSON array of 1–20 city/country objects")
	}
	seen := map[string]bool{}
	unique := []Scope{}
	for _, city := range c.Cities {
		q, err := discovery.ParseQuery(discovery.Query{City: city.City, Country: city.Country, Limit: 100}.Values(), time.Now())
		if err != nil || q.City == "" || q.Country == "" {
			return c, fmt.Errorf("event history requires a valid city and two-letter country for every scope")
		}
		city = Scope{q.City, q.Country}
		if !seen[city.ID()] {
			unique = append(unique, city)
			seen[city.ID()] = true
		}
	}
	c.Cities = unique
	if v := os.Getenv("EVENT_HISTORY_DAYS"); v != "" {
		var err error
		c.Days, err = strconv.Atoi(v)
		if err != nil || c.Days < 1 || c.Days > 366 {
			return c, fmt.Errorf("EVENT_HISTORY_DAYS must be from 1 to 366")
		}
	}
	if v := os.Getenv("EVENT_HISTORY_INTERVAL"); v != "" {
		var err error
		c.Interval, err = time.ParseDuration(v)
		if err != nil || c.Interval < time.Hour || c.Interval > 7*24*time.Hour {
			return c, fmt.Errorf("EVENT_HISTORY_INTERVAL must be from 1h to 168h")
		}
	}
	if strings.TrimSpace(os.Getenv("TICKETMASTER_KEY")) == "" {
		return c, fmt.Errorf("event history requires TICKETMASTER_KEY")
	}
	return c, nil
}
