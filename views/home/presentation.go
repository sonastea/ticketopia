package home

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/eventactivity"
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
	Discussions           DiscussionView
	Thread                *DiscussionPage
	ActionReturnURL       string
	Activity              map[string]eventactivity.Preview
	ActivityError         string
	// PageCursor read the last rendered page; empty for the first page.
	PageCursor string
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
	Discussions       DiscussionView
	Thread            *DiscussionPage
	ActionReturnURL   string
}

func eventPresentation(panel bool) string {
	if panel {
		return "context"
	}
	return "standalone"
}

type SaveView struct {
	Reminders bool
	Enabled   bool
	SignedIn  bool
	CSRF      string
	States    map[string]bool
	Error     string
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
	List                  recommendations.Feed
	Categories            []models.Category
	CategoriesUnavailable bool
	ReturnURL, Error      string
}

// listContext tells a shared list where it renders, so its empty state can name
// the action available there instead of sending people somewhere else.
type listContext struct {
	Place   listPlace
	EventID string // the single event, when Place is inEvent
	// FirstPage links back to the newest items from a later cursor page; it is
	// empty on the first page, which reserves "on this page" copy for later ones.
	FirstPage string
	Filtered  bool // collection filters narrowed the results
}

type listPlace int

const (
	inEvent listPlace = iota
	inCommunity
	onProfile
)

func (c listContext) showsEvent() bool { return c.Place != inEvent }

func eventContext(id string) listContext { return listContext{Place: inEvent, EventID: id} }

func communityContext(pageURL, city, country, category string) listContext {
	return listContext{Place: inCommunity, FirstPage: laterPage(pageURL, "cursor"), Filtered: city != "" || country != "" || category != ""}
}

// laterPage returns the first-page URL when raw is a later cursor page,
// keeping filters and return context, and "" otherwise.
func laterPage(raw, key string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	values := u.Query()
	if values.Get(key) == "" {
		return ""
	}
	values.Del(key)
	u.RawQuery = values.Encode()
	return u.String()
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
	return err == nil && (u.Path == "/community" || u.Path == "/community/discussions")
}
func isHappeningNowURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Path == "/happening-now"
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

// CommunityFilterBar is the shared compact filter form for Community lists.
type CommunityFilterBar struct {
	Action, Prefix, Label     string
	City, Country, CategoryID string
	Categories                []models.Category
}

func (f CommunityFilterBar) hasCategory() bool {
	for _, category := range f.Categories {
		if category.ID == f.CategoryID {
			return true
		}
	}
	return false
}

// initials draws a profile's avatar letters from its first and last words.
func initials(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return ""
	}
	letter := func(word string) string { r, _ := utf8.DecodeRuneInString(word); return string(unicode.ToUpper(r)) }
	if len(words) == 1 {
		return letter(words[0])
	}
	return letter(words[0]) + letter(words[len(words)-1])
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
	values.Del("selected_thread")
	values.Del("reply_to")
	values.Del("discussion_cursor")
	values.Del("recommend")
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

// saveSignInURL asks sign-in to complete the Save for the event it returns to.
func saveSignInURL(returnURL string) string { return signInURL(returnURL) + "&intent=save" }

// Signing in resumes the event, not just the collection containing its control.
// Preserve an already selected section/thread; a different row starts at Overview.
func EventSignInReturn(id, returnURL string) string {
	u, err := url.Parse(returnURL)
	if err != nil {
		return eventURL(id, "overview", "/")
	}
	if u.Path == "/" {
		if u.Query().Get("selected_event") == id {
			return returnURL
		}
		return SelectionURL(returnOrigin(returnURL), id, "overview")
	}
	if strings.HasPrefix(u.Path, "/events/") {
		return returnURL
	}
	return eventURL(id, "overview", returnURL)
}

func recommendationSignInReturn(id, returnURL string) string {
	u, err := url.Parse(EventSignInReturn(id, returnURL))
	if err != nil {
		return returnURL
	}
	values := u.Query()
	values.Set("section", "community")
	values.Del("selected_thread")
	values.Del("reply_to")
	values.Del("discussion_cursor")
	if index := strings.Index(u.Path, "/discussions/"); index >= 0 {
		u.Path = u.Path[:index]
		values.Del("cursor")
		values.Del("limit")
		values.Del("post_id")
	}
	values.Set("recommend", "true")
	u.RawQuery = values.Encode()
	return u.String()
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

func interestButtonText(view InterestView, id, returnURL string) string {
	if isInterestsURL(returnURL) && view.States[id].Interested {
		return "Remove interest · " + interestLabel(view.visibility(id))
	}
	return "Interested · " + interestLabel(view.visibility(id))
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

func hasCollectionCursor(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Query().Get("cursor") != ""
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

func activityCount(count int, singular, plural string) string {
	if count == 1 {
		return "1 " + singular
	}
	return fmt.Sprintf("%d %s", count, plural)
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

// EventActionURL uses only the validated return context and selected section.
func EventActionURL(page EventPage) string {
	return sectionURL(page, page.Section)
}
func recommendURL(page EventPage) string {
	u, _ := url.Parse(sectionURL(page, "community"))
	values := u.Query()
	values.Set("recommend", "true")
	u.RawQuery = values.Encode()
	return u.String()
}

// transitionStyle names an event's artwork or title so cross-document view
// transitions can carry it between rows, the context panel and the event page.
// Provider IDs are escaped into a valid CSS identifier.
func transitionStyle(part, id string) templ.SafeCSS {
	if id == "" {
		return ""
	}
	var name strings.Builder
	name.WriteString("event-" + part + "-")
	for _, r := range id {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			name.WriteRune(r)
		} else {
			fmt.Fprintf(&name, "_%x", r)
		}
	}
	return templ.SafeCSS("view-transition-name:" + name.String())
}

// tintStyle carries the stored artwork colour into the event header. Only a
// strict #rrggbb value is emitted; anything else keeps the brand fallback.
func tintStyle(event models.Event) templ.SafeCSS {
	t := event.Tint
	if len(t) != 7 || t[0] != '#' {
		return ""
	}
	for _, r := range t[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return ""
		}
	}
	return templ.SafeCSS("--event-tint:" + t)
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

type dateChip struct {
	Label, Href string
	Current     bool
}

func dateChips(page SearchPage) []dateChip { return dateChipsAt(page, time.Now()) }

// Chips are plain links to start_date/end_date URLs that keep the other
// filters. Only the first matching range is marked current.
func dateChipsAt(page SearchPage, now time.Time) []dateChip {
	chips := []dateChip{}
	matched := false
	for _, p := range discovery.DatePresets(searchToday(page.Events.Items, now)) {
		current := !matched && page.Filters.StartDate == p.StartDate && page.Filters.EndDate == p.EndDate
		matched = matched || current
		chips = append(chips, dateChip{Label: p.Label, Href: "/?" + page.Filters.WithDates(p.StartDate, p.EndDate).Values().Encode(), Current: current})
	}
	return chips
}

func chipCurrent(current bool) string {
	if current {
		return "true"
	}
	return "false"
}

// searchToday takes the calendar day in the browsed city's time zone, read
// from its events, so "Tonight" in Los Angeles is not the server's UTC date.
func searchToday(events []models.Event, now time.Time) time.Time {
	today, _ := discovery.EventsToday(events, now)
	return today
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

// featuringLabel omits performers already named by the event title, so rows and
// Overview don't repeat "Featuring X" beneath "X: The Tour".
func featuringLabel(event models.Event) string {
	title := strings.ToLower(event.Name)
	var rest []models.Artist
	for _, artist := range event.Artists {
		if name := strings.TrimSpace(artist.Name); name != "" && !strings.Contains(title, strings.ToLower(name)) {
			rest = append(rest, artist)
		}
	}
	return artistsLabel(rest)
}

func statusLabel(status string) string {
	switch status {
	case "onsale":
		return "On sale"
	case "offsale":
		return "Off sale"
	case "soldout", "sold_out":
		return "Sold out"
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

// artSlot names where event artwork is drawn, so its sizes hint describes the
// widths that frame actually covers.
type artSlot int

const (
	artResult     artSlot = iota // result rows and cards
	artThumb                     // fixed 5.5rem squares (Happening Now)
	artCollection                // stacked art, 8rem squares or community cards
	artDetail                    // event page and preview banner
)

// artBand is one viewport range of a slot: the frame's inline size, in rem or
// vw, and its aspect ratio. maxWidth is empty for the widest range.
type artBand struct {
	maxWidth string
	size     float64
	vw       bool
	aspect   float64
}

// Bands follow the app.css layouts at the default text size: results stack
// below an 18rem container, keep a 5.5rem thumbnail to 26rem, a thumbnail of at
// most 11rem to 48rem and become cards beyond; viewport bounds add the page
// gutters. Hints may round up but never down, so artwork stays sharp.
var artBands = map[artSlot][]artBand{
	artResult: {
		{"20rem", 100, true, 16.0 / 9},
		{"28.5rem", 5.5, false, 1},
		{"51rem", 11, false, 1},
		{"", 26, false, 16.0 / 9},
	},
	artThumb:      {{"", 5.5, false, 1}},
	artCollection: {{"42rem", 100, true, 16.0 / 9}, {"", 30, false, 16.0 / 9}},
	artDetail:     {{"62rem", 100, true, 2}, {"", 50, false, 2}},
}

type eventArt struct{ Src, SrcSet, Sizes string }

// artFor picks one crop of the artwork and lists its renditions, so the
// browser can download the smallest that covers the frame and switching
// between them never reframes the picture. Small frames prefer the tallest
// crop and the detail banner the widest, since those waste the fewest pixels;
// a crop needs two renditions to offer a choice.
func artFor(images []models.Image, slot artSlot) eventArt {
	target := 480
	if slot == artDetail {
		target = 1000
	}
	crops := map[string][]models.Image{}
	var order []string
	for _, image := range images {
		if image.URL == "" {
			continue
		}
		if _, ok := crops[image.Ratio]; !ok {
			order = append(order, image.Ratio)
		}
		crops[image.Ratio] = append(crops[image.Ratio], image)
	}
	var chosen []models.Image
	aspect := 0.0
	for _, ratio := range order {
		crop := renditions(crops[ratio])
		if len(crop) < 2 {
			continue
		}
		a := cropAspect(crop)
		if chosen == nil || (slot == artDetail && a > aspect) || (slot != artDetail && a < aspect) {
			chosen, aspect = crop, a
		}
	}
	if chosen == nil {
		if src := closestImage(images, target); src.URL != "" {
			return eventArt{Src: src.URL}
		}
		return eventArt{}
	}
	sources := make([]string, len(chosen))
	for i, image := range chosen {
		sources[i] = fmt.Sprintf("%s %dw", image.URL, image.Width)
	}
	return eventArt{
		Src:    closestImage(chosen, target).URL,
		SrcSet: strings.Join(sources, ", "),
		Sizes:  artSizes(artBands[slot], aspect),
	}
}

// renditions orders a crop by width, one image per width.
func renditions(images []models.Image) []models.Image {
	var out []models.Image
	for _, image := range images {
		if image.Width <= 0 || slices.ContainsFunc(out, func(seen models.Image) bool { return seen.Width == image.Width }) {
			continue
		}
		out = append(out, image)
	}
	slices.SortFunc(out, func(a, b models.Image) int { return a.Width - b.Width })
	return out
}

func cropAspect(crop []models.Image) float64 {
	if a, b, ok := strings.Cut(crop[0].Ratio, "_"); ok {
		w, errW := strconv.ParseFloat(a, 64)
		h, errH := strconv.ParseFloat(b, 64)
		if errW == nil && errH == nil && w > 0 && h > 0 {
			return w / h
		}
	}
	for _, image := range crop {
		if image.Height > 0 {
			return float64(image.Width) / float64(image.Height)
		}
	}
	return 16.0 / 9
}

// artSizes gives each band the width a covering (object-fit: cover) image
// needs: a crop wider than its frame is scaled to the frame's height.
func artSizes(bands []artBand, aspect float64) string {
	parts := make([]string, len(bands))
	for i, band := range bands {
		// Round up to a tenth, ignoring floating-point noise.
		width := math.Ceil(band.size*max(1, aspect/band.aspect)*10-1e-9) / 10
		unit := "rem"
		if band.vw {
			unit = "vw"
		}
		parts[i] = strconv.FormatFloat(width, 'f', -1, 64) + unit
		if band.maxWidth != "" {
			parts[i] = "(max-width: " + band.maxWidth + ") " + parts[i]
		}
	}
	return strings.Join(parts, ", ")
}

func closestImage(images []models.Image, width int) models.Image {
	var best models.Image
	distance := int(^uint(0) >> 1)
	for _, image := range images {
		diff := image.Width - width
		if diff < 0 {
			diff = -diff
		}
		if image.URL != "" && diff < distance {
			best, distance = image, diff
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

// mapsURL opens the venue in Google Maps' universal search link, which Android
// hands to its Maps app and every other browser serves on the web. app.js sends
// iPhone and iPad taps to Apple Maps instead, from the same query.
func mapsURL(event models.Event) string {
	return "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(mapsQuery(event))
}

func mapsQuery(event models.Event) string {
	place := eventPlace(event)
	var parts []string
	for _, value := range []string{place.Name, place.Address, place.City, place.State, place.PostalCode, place.CountryCode} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

// shareURL is the event's canonical path, without the reader's return context.
func shareURL(id string) string { return eventURL(id, "overview", "/") }

// noticeBullet matches the provider's inline " - " bullets, including one that
// opens the notice or a line.
var noticeBullet = regexp.MustCompile(`(?:^|\s)-\s+`)

// noticeList splits a provider "Before you go" notice written as inline
// " - " bullets into an optional lead-in and its items. It only changes the
// display: prose with a stray dash (for example "7pm - 11pm") stays a paragraph
// unless it has two or more items after an empty or sentence-ending lead-in.
func noticeList(text string) (string, []string) {
	parts := noticeBullet.Split(strings.TrimSpace(text), -1)
	if len(parts) < 3 {
		return "", nil
	}
	intro := strings.TrimSpace(parts[0])
	if intro != "" && !strings.ContainsAny(intro[len(intro)-1:], ":.!?") {
		return "", nil
	}
	var items []string
	for _, part := range parts[1:] {
		if part = strings.TrimSpace(part); part != "" {
			items = append(items, part)
		}
	}
	if len(items) < 2 {
		return "", nil
	}
	return intro, items
}

func hasGenre(genres []models.Genre, id string) bool {
	for _, genre := range genres {
		if genre.ID == id {
			return true
		}
	}
	return false
}
