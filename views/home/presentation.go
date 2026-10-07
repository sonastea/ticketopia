package home

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

type SearchPage struct {
	Filters               discovery.Query
	Events                models.EventList
	Categories            models.CategoryList
	CategoriesUnavailable bool
	LocationSource        string
	NeedsLocation         bool
	Error                 string
	ReturnURL             string
	SelectedID            string
	Selected              *models.EventDetail
	SelectionError        string
	Section               string
	Saves                 SaveView
	Interests             InterestView
	Participants          interests.Participants
	ParticipantsError     string
	Recommendations       RecommendationView
	ActionReturnURL       string
}

type EventPage struct {
	Detail            models.EventDetail
	Section           string
	ReturnURL         string
	Error             string
	Saves             SaveView
	Interests         InterestView
	Participants      interests.Participants
	ParticipantsError string
	Recommendations   RecommendationView
	ActionReturnURL   string
}

type SaveView struct {
	Enabled  bool
	SignedIn bool
	CSRF     string
	States   map[string]bool
	Error    string
}

type RecommendationView struct {
	Enabled, SignedIn, Open          bool
	CSRF, ViewerID, Error, ListError string
	Own                              *recommendations.Item
	List                             recommendations.List
}

func (v RecommendationView) reason() string {
	if v.Own != nil {
		return v.Own.Reason
	}
	return ""
}

type CommunityPage struct {
	Enabled               bool
	Filters               recommendations.Query
	List                  recommendations.List
	Categories            []models.Category
	CategoriesUnavailable bool
	ReturnURL, Error      string
}

func recommendationAction(id string) string { return "/recommendations/" + url.PathEscape(id) }
func recommendationListURL(id, origin string) string {
	path := "/events/" + url.PathEscape(id) + "/recommendations"
	if origin != "" && origin != "/" {
		path += "?" + url.Values{"return_to": {origin}}.Encode()
	}
	return path
}
func profileRecommendationNextURL(raw string, cursor *string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	values := u.Query()
	if cursor != nil {
		values.Set("recommendation_cursor", *cursor)
	}
	u.RawQuery = values.Encode()
	return u.String()
}
func isCommunityURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Path == "/community"
}
func isProfileURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.HasPrefix(u.Path, "/users/")
}
func recommendationCountLabel(count int) string {
	if count == 1 {
		return "recommendation"
	}
	return "recommendations"
}
func (p CommunityPage) hasCategory() bool {
	for _, category := range p.Categories {
		if category.ID == p.Filters.CategoryID {
			return true
		}
	}
	return false
}
func returnOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	if strings.HasPrefix(u.Path, "/events/") {
		if origin := u.Query().Get("return_to"); origin != "" {
			return origin
		}
		return "/"
	}
	values := u.Query()
	values.Del("selected_event")
	values.Del("section")
	u.RawQuery = values.Encode()
	return u.String()
}

type SavedPage struct {
	List      saved.List
	Saves     SaveView
	Interests InterestView
	ReturnURL string
	Error     string
}

func (page SavedPage) events() []models.Event {
	items := make([]models.Event, 0, len(page.List.Items))
	for _, item := range page.List.Items {
		items = append(items, item.Event)
	}
	return items
}
func (page SavedPage) nextURL() string {
	u, _ := url.Parse(page.ReturnURL)
	values := u.Query()
	if page.List.NextCursor != nil {
		values.Set("cursor", *page.List.NextCursor)
	}
	return "/saved?" + values.Encode()
}
func saveAction(id string) string { return "/saved/" + url.PathEscape(id) }
func signInURL(returnURL string) string {
	return "/auth/sign-in?return_to=" + url.QueryEscape(returnURL)
}
func saveVerb(isSaved bool) string {
	if isSaved {
		return "remove"
	}
	return "save"
}
func saveLabel(isSaved bool) string {
	if isSaved {
		return "Remove from Saved"
	}
	return "Save"
}
func isSavedURL(raw string) bool { u, err := url.Parse(raw); return err == nil && u.Path == "/saved" }

type InterestView struct {
	Enabled, SignedIn              bool
	CSRF, Error, DefaultVisibility string
	States                         map[string]interests.State
}

func (v InterestView) visibility(id string) string {
	if state := v.States[id]; state.Interested {
		return state.Visibility
	}
	if v.DefaultVisibility == "public" {
		return "public"
	}
	return "private"
}
func interestLabel(visibility string) string {
	if visibility == "public" {
		return "Public"
	}
	return "Private"
}
func interestVerb(selected bool) string {
	if selected {
		return "remove"
	}
	return "set"
}
func interestActionLabel(selected bool) string {
	if selected {
		return "Remove"
	}
	return "Mark"
}
func interestCountLabel(count int) string {
	if count == 1 {
		return "person interested"
	}
	return "people interested"
}
func interestAction(id string) string { return "/interests/" + url.PathEscape(id) }
func participantsURL(id, origin string) string {
	path := "/events/" + url.PathEscape(id) + "/interested-users"
	if origin != "" && origin != "/" {
		path += "?" + url.Values{"return_to": {origin}}.Encode()
	}
	return path
}
func isInterestsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Path == "/me/interests"
}
func CollectionNextURL(raw string, cursor *string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	values := u.Query()
	if cursor != nil {
		values.Set("cursor", *cursor)
	}
	values.Del("saved")
	u.RawQuery = values.Encode()
	return u.String()
}

type InterestCollectionPage struct {
	List             interests.List
	Saves            SaveView
	Interests        InterestView
	ReturnURL, Error string
}

func (p InterestCollectionPage) events() []models.Event {
	items := make([]models.Event, 0, len(p.List.Items))
	for _, item := range p.List.Items {
		items = append(items, item.Event)
	}
	return items
}
func SelectionURL(returnURL, id, section string) string {
	u, err := url.Parse(returnURL)
	if err != nil || u.Path != "/" {
		return returnURL
	}
	values := u.Query()
	values.Set("selected_event", id)
	if section != "overview" {
		values.Set("section", section)
	} else {
		values.Del("section")
	}
	u.RawQuery = values.Encode()
	return u.String()
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
func recommendURL(page EventPage) string {
	u, _ := url.Parse(sectionURL(page, "community"))
	values := u.Query()
	values.Set("recommend", "true")
	u.RawQuery = values.Encode()
	return u.String()
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

func classificationLabel(event models.Event) string {
	label := func(classification models.Classification) string {
		var names []string
		for _, item := range []*models.NamedID{classification.Segment, classification.Genre} {
			if item != nil && strings.TrimSpace(item.Name) != "" && !strings.EqualFold(item.Name, "Undefined") {
				names = append(names, item.Name)
			}
		}
		return strings.Join(names, " · ")
	}
	for _, classification := range event.Classifications {
		if classification.Primary && label(classification) != "" {
			return label(classification)
		}
	}
	for _, classification := range event.Classifications {
		if value := label(classification); value != "" {
			return value
		}
	}
	return "Category not listed"
}

func eventIcon(event models.Event) string {
	for _, classification := range event.Classifications {
		if classification.Segment != nil && classification.Segment.Name == "Music" {
			return "music"
		}
	}
	return "ticket"
}

func dateRangeLabel(query discovery.Query) string {
	start, firstErr := time.Parse(time.DateOnly, query.StartDate)
	end, lastErr := time.Parse(time.DateOnly, query.EndDate)
	if firstErr != nil || lastErr != nil {
		return "Choose dates"
	}
	return start.Format("Jan 2") + " – " + end.Format("Jan 2, 2006")
}

func (page SearchPage) category() models.Category {
	for _, category := range page.Categories.Items {
		if category.ID == page.Filters.CategoryID {
			return category
		}
	}
	return models.Category{}
}

func categoryFilterLabel(page SearchPage) string {
	if page.Filters.CategoryID == "" {
		return "All categories"
	}
	category := page.category()
	name := category.Name
	if name == "" {
		name = page.Filters.CategoryID
	}
	for _, genre := range category.Genres {
		if genre.ID == page.Filters.GenreID {
			return name + " · " + genre.Name
		}
	}
	if page.Filters.GenreID != "" {
		return name + " · " + page.Filters.GenreID
	}
	return name
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
		return "1 event"
	}
	return fmt.Sprintf("%d events", total)
}

func startLabel(start models.EventStart) string {
	if start.DateTBA || start.DateTBD || start.LocalDate == nil {
		return "Event date to be announced"
	}
	date, err := time.Parse(time.DateOnly, *start.LocalDate)
	if err != nil {
		return "Event date to be announced"
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
				label += " - " + *price.Max
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
