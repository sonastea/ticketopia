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
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	eventinterests "github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
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
		PleaseNote:  "Bring photo ID. Doors open one hour before the show.",
		Artists:     []models.Artist{{Name: "The Neighborhood Band"}},
	}
	longEvent := event
	longEvent.ID, longEvent.Name = "ticketmaster:CSS_1", strings.Repeat("LongEventName", 25)
	detail := models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: now}}
	saves := home.SaveView{Enabled: true, SignedIn: true, CSRF: "css-fixture", Reminders: true, States: map[string]bool{event.ID: true}}
	interests := home.InterestView{Enabled: true, SignedIn: true, CSRF: "css-fixture", DefaultVisibility: "private"}
	profile := accounts.PublicProfile{ID: "css-fixture", DisplayName: "Local event explorer"}
	post := discussions.Post{ID: "css-question", EventID: event.ID, Profile: &profile, Body: "Is there a coat check at the venue?", ReplyCount: 2, CreatedAt: now, UpdatedAt: now}
	search := home.SearchPage{
		Filters:   discovery.Query{City: "Berlin", StartDate: "2026-10-07", EndDate: "2027-01-07"},
		Events:    models.EventList{Items: []models.Event{event, longEvent}, Total: 2, Meta: detail.Meta},
		ReturnURL: "/", ActionReturnURL: "/", Section: "overview", Saves: saves, Interests: interests,
		Discussions:     home.DiscussionView{Enabled: true, ViewerID: profile.ID, CSRF: "css-fixture", List: discussions.List{Items: []discussions.Post{post}, Count: 1}},
		Recommendations: home.RecommendationView{Enabled: true, SignedIn: true, ViewerID: profile.ID, CSRF: "css-fixture", List: recommendations.List{Items: []recommendations.Item{{Profile: profile, Reason: "A welcoming venue with a great view from the balcony.", RecommendedAt: now, UpdatedAt: now}}}},
		Participants:    eventinterests.Participants{Items: []eventinterests.Participant{{Profile: profile}}},
		Activity: map[string]eventactivity.Preview{event.ID: {
			Questions: 1, Question: &eventactivity.Contribution{ID: post.ID, Body: post.Body, AuthorID: profile.ID, AuthorName: profile.DisplayName},
			Recommendations: 1, Recommendation: &eventactivity.Contribution{Body: "A welcoming venue with a great view from the balcony.", AuthorID: profile.ID, AuthorName: profile.DisplayName},
		}},
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
		if r.URL.Query().Get("selected") == "true" || r.URL.Query().Get("selected_event") != "" {
			page.SelectedID, page.Selected = event.ID, &detail
			if r.URL.Query().Get("selected_event") == longEvent.ID {
				selected := detail
				selected.Item = longEvent
				page.SelectedID, page.Selected = longEvent.ID, &selected
			}
		}
		if section := r.URL.Query().Get("section"); section != "" {
			page.Section = section
		}
		if r.URL.Query().Get("activity") == "none" {
			page.Activity = nil
			page.Discussions.List = discussions.List{}
			page.Recommendations.List = recommendations.List{}
			page.Participants = eventinterests.Participants{}
		}
		if r.URL.Query().Get("thread") == "true" {
			root := post
			root.Event, root.ReplyCount = event, 1
			reply := post
			reply.ID, reply.ThreadID, reply.ParentID, reply.Body = "css-reply", root.ID, root.ID, "Yes, the coat check is beside the entrance."
			page.Section = "discussion"
			page.Thread = &home.DiscussionPage{Event: event, Root: &root, ParentID: root.ID, ReturnURL: "/", URL: "/events/" + event.ID + "/discussions/" + root.ID,
				View: home.DiscussionView{Enabled: true, ViewerID: "css-other-viewer", CSRF: "css-fixture", List: discussions.List{Items: []discussions.Post{reply}, Count: 1}}}
		}
		render(w, r, home.Index(page))
	})
	mux.HandleFunc("GET /events/{id}", func(w http.ResponseWriter, r *http.Request) {
		section := r.URL.Query().Get("section")
		if section == "" {
			section = "overview"
		}
		page := home.EventPage{
			Detail: detail, ReturnURL: "/", ActionReturnURL: r.URL.RequestURI(), Section: section,
			Saves: saves, Interests: interests,
			Recommendations: search.Recommendations,
			Discussions:     search.Discussions,
			Participants:    search.Participants,
		}
		if r.PathValue("id") == longEvent.ID {
			page.Detail.Item = longEvent
		}
		if r.URL.Query().Get("ticket") == "missing" {
			page.Detail.Item.Source.URL = ""
		}
		if status := r.URL.Query().Get("status"); status != "" {
			page.Detail.Item.Status = status
		}
		page.Detail.Meta.Stale = r.URL.Query().Get("stale") == "true"
		switch r.URL.Query().Get("account") {
		case "guest":
			page.Saves.SignedIn = false
			page.Saves.CSRF = ""
			page.Interests.SignedIn = false
			page.Interests.CSRF = ""
			page.Recommendations.SignedIn = false
			page.Recommendations.ViewerID = ""
			page.Recommendations.CSRF = ""
		case "disabled":
			page.Saves = home.SaveView{}
			page.Interests = home.InterestView{}
			page.Recommendations = home.RecommendationView{}
		case "unavailable":
			page.Saves.Error = "Saved state could not be loaded. Reload to try again."
		}
		panel := r.Header.Get("X-Ticketopia-Panel") == "true"
		if panel {
			page.ActionReturnURL = home.SelectionURL(page.ReturnURL, page.Detail.Item.ID, page.Section)
		}
		if r.Header.Get("X-Ticketopia-Section") == "true" {
			render(w, r, home.EventSectionResponse(page, panel))
		} else if panel {
			render(w, r, home.EventContext(page))
		} else {
			render(w, r, home.Event(page))
		}
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
