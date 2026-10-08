// css-preview renders production templates with isolated, read-only CSS fixtures.
// Run from the repository root; no database, provider, or credentials are used.
package main

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/account"
	"github.com/sonastea/ticketopia/views/home"
)

func main() {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	date, price := "2026-11-14", "35.00"
	event := models.Event{
		ID: "ticketmaster:CSS_0", Name: "A good night in the city",
		Start: models.EventStart{LocalDate: &date}, Status: "onsale",
		Source:      models.Source{Provider: "ticketmaster", URL: "https://www.ticketmaster.com/"},
		PriceRanges: []models.PriceRange{{Currency: "EUR", Min: &price, Max: &price}},
		Venues:      []models.Venue{{Place: models.Place{Name: "Neighborhood Hall", City: "Berlin"}}},
		Info:        "Compare dates, venues, and practical details before planning a night out.\n" + strings.Repeat("LongProviderWord", 20),
	}
	longEvent := event
	longEvent.ID, longEvent.Name = "ticketmaster:CSS_1", strings.Repeat("LongEventName", 25)
	detail := models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: now}}
	saves := home.SaveView{Enabled: true, SignedIn: true, CSRF: "css-fixture"}
	interests := home.InterestView{Enabled: true, SignedIn: true, CSRF: "css-fixture", DefaultVisibility: "private"}
	search := home.SearchPage{
		Filters:   discovery.Query{City: "Berlin", StartDate: "2026-10-07", EndDate: "2027-01-07"},
		Events:    models.EventList{Items: []models.Event{event, longEvent}, Total: 2, Meta: detail.Meta},
		ReturnURL: "/", ActionReturnURL: "/", Section: "overview", Saves: saves, Interests: interests,
	}
	mux := http.NewServeMux()
	// Read the built stylesheet directly so a rebuilt CSS file needs no restart.
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("views/assets"))))
	render := func(w http.ResponseWriter, r *http.Request, component templ.Component) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := component.Render(r.Context(), w); err != nil {
			log.Print(err)
		}
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		page := search
		if r.URL.Query().Get("selected") == "true" {
			page.SelectedID, page.Selected = event.ID, &detail
		}
		render(w, r, home.Index(page))
	})
	mux.HandleFunc("GET /events/{id}", func(w http.ResponseWriter, r *http.Request) {
		section := r.URL.Query().Get("section")
		if section == "" {
			section = "overview"
		}
		render(w, r, home.Event(home.EventPage{
			Detail: detail, ReturnURL: "/", ActionReturnURL: r.URL.RequestURI(), Section: section,
			Saves: saves, Interests: interests,
			Recommendations: home.RecommendationView{Enabled: true, SignedIn: true, Open: true, CSRF: "css-fixture"},
			Discussions:     home.DiscussionView{Enabled: true, ViewerID: "css-fixture", CSRF: "css-fixture"},
		}))
	})
	mux.HandleFunc("GET /me", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, account.Settings(account.Page{
			Section: "profile", CSRF: "css-fixture",
			Account: accounts.Account{ID: "css-fixture", DisplayName: "Local event explorer", Email: "fixture@example.test", Bio: strings.Repeat("LongProfileWord", 20)},
		}))
	})
	mux.HandleFunc("GET /auth/sign-in", func(w http.ResponseWriter, r *http.Request) {
		render(w, r, account.SignIn(true, "/", ""))
	})
	log.Print("Read-only CSS fixtures: http://127.0.0.1:18088")
	log.Fatal(http.ListenAndServe("127.0.0.1:18088", mux))
}
