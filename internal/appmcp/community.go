package appmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

// EventCommunity reads an event's public community totals, plus viewer's own
// state when viewer is set, as GET /api/v1/events/{id}/community does.
type EventCommunity interface {
	EventCommunity(ctx context.Context, viewer, id string) (eventactivity.Community, error)
}

// EventRecommendations reads an event's public recommendations, as
// GET /api/v1/events/{id}/recommendations does.
type EventRecommendations interface {
	Event(ctx context.Context, id string, values url.Values) (recommendations.List, error)
}

// InterestedPeople reads an event's public Interested identities, as
// GET /api/v1/events/{id}/interested-users does.
type InterestedPeople interface {
	Participants(ctx context.Context, id string, values url.Values) (interests.Participants, error)
}

// Discussions reads public event conversations, as the discussion routes
// under /api/v1/events/{id}/discussions and /api/v1/community/discussions
// do; viewer only adds that account's Helpful state.
type Discussions interface {
	List(ctx context.Context, viewer, event, thread string, values url.Values) (discussions.List, error)
	Thread(ctx context.Context, viewer, event, id string) (discussions.Post, error)
}

// CommunityRecommendations reads the grouped community feed, as
// GET /api/v1/community/recommendations does.
type CommunityRecommendations interface {
	Community(ctx context.Context, values url.Values) (recommendations.Feed, error)
}

// Profiles reads one public profile, as GET /api/v1/users/{id} does.
type Profiles interface {
	Public(ctx context.Context, id string) (accounts.PublicProfile, error)
}

// LiveActivity reads the public Happening Now snapshot, as
// GET /api/v1/community/activity does without credentials.
type LiveActivity interface {
	FilteredSnapshot(ctx context.Context, filter activity.Filters) (activity.Snapshot, error)
}

var errLiveUnavailable = errors.New("live activity is unavailable right now; try again shortly")

// ProfileActivity reads one person's public contributions, as
// GET /api/v1/users/{id}/activity does.
type ProfileActivity interface {
	ProfileActivity(ctx context.Context, user string, values url.Values) (useractivity.List, error)
}

// callMistake is an error in how the tool was called, reported verbatim
// rather than in the account API's wording.
type callMistake struct{ error }

var errThreadNotFound = callMistake{errors.New("no question or tip with this thread_id on this event: use an id from event_discussions or " +
	"community_discussions, or a reply's thread_id (not its id), with the event_id it belongs to")}

var errProfileNeeded = errors.New("user_id is required: pass a profile id from event_recommendations, event_interested_people or a discussion post, " +
	"or the ID from a Ticketopia profile URL (/users/<id>), or sign in to read your own public activity")

var errUserNeeded = callMistake{errors.New("user_id is required: pass a profile id from event_recommendations, event_interested_people or a discussion post, " +
	"or the ID from a Ticketopia profile URL (/users/<id>), or sign in to read your own public profile")}

// addCommunityTools registers the public community reads. They work without
// sign-in, like the routes they mirror; signed in with read access, the
// event read adds the caller's own state and profile_activity defaults to
// the caller.
func addCommunityTools(server *mcp.Server, d Deps) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no}
	if d.EventCommunity != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "event_community", Title: "Event community", OutputSchema: outputSchema[eventactivity.Community](), Annotations: readOnly,
			Description: "An event's community totals by its ID from find_events: people Interested (including private choices, never who), " +
				"active public recommendations, and visible questions and tips. Signed in, viewer adds the user's own saved, Interested " +
				"(with its visibility) and recommended state; viewer is null otherwise. Unknown events report zeros."},
			publicRead(d, func(ctx context.Context, viewer string, in eventInput) (eventactivity.Community, error) {
				return d.EventCommunity.EventCommunity(ctx, viewer, in.EventID)
			}))
	}
	if d.ProfileActivity != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "profile_activity", Title: "Profile activity", OutputSchema: outputSchema[useractivity.List](), Annotations: readOnly,
			Description: "One person's public contributions on Ticketopia, newest first: discussion posts and replies, recommendations, " +
				"and events they made their Interested choice public for, each with the event. Never includes private interest, saves or follows. " +
				"user_id is a profile id from event_recommendations, event_interested_people or a discussion post, or the ID in a profile URL (/users/<id>); " +
				"omit it for the signed-in user's own public activity. To open a post's conversation, pass its thread_id (not its id) and its event's id to discussion_thread. " +
				"For the next page, pass next_cursor as cursor with the same user_id."},
			publicRead(d, func(ctx context.Context, viewer string, in profileActivityInput) (useractivity.List, error) {
				user := in.UserID
				if user == "" {
					user = viewer
				}
				if user == "" {
					return useractivity.List{}, errProfileNeeded
				}
				return d.ProfileActivity.ProfileActivity(ctx, user, pageInput{Limit: in.Limit, Cursor: in.Cursor}.values())
			}))
	}
	if d.EventRecommendations != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "event_recommendations", Title: "Event recommendations", OutputSchema: outputSchema[recommendations.List](), Annotations: readOnly,
			Description: "Public recommendations of one event by its ID from find_events, newest first: each person's public profile (its id works as " +
				"profile_activity's user_id), their optional reason, and via when an app published it. count is the event's total. " +
				"For the next page, pass next_cursor as cursor with the same event_id."},
			publicRead(d, func(ctx context.Context, _ string, in eventPageInput) (recommendations.List, error) {
				return d.EventRecommendations.Event(ctx, in.EventID, in.values())
			}))
	}
	if d.InterestedPeople != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "event_interested_people", Title: "Event's interested people", OutputSchema: outputSchema[interests.Participants](), Annotations: readOnly,
			Description: "People who made their Interested choice for one event public, newest first, with their public profile (its id works as " +
				"profile_activity's user_id). Private choices are never listed, so this is usually fewer than event_community's interested count. " +
				"For the next page, pass next_cursor as cursor with the same event_id."},
			publicRead(d, func(ctx context.Context, _ string, in eventPageInput) (interests.Participants, error) {
				return d.InterestedPeople.Participants(ctx, in.EventID, in.values())
			}))
	}
	if d.Discussions != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "event_discussions", Title: "Event questions and tips", OutputSchema: outputSchema[discussions.List](), Annotations: readOnly,
			Description: "Public questions and tips about one event, newest first, each with its author's public profile, reply_count and helpful_count; " +
				"signed in, helpful is the user's own Helpful choice. Removed or hidden roots that still have visible replies appear without text. " +
				"count is the event's visible questions and tips. Open one with discussion_thread using its thread_id; signed in, follow_discussion follows it. " +
				"For the next page, pass next_cursor as cursor with the same event_id and the same sign-in (cursors belong to the caller)."},
			publicRead(d, func(ctx context.Context, viewer string, in eventPageInput) (discussions.List, error) {
				// An empty event means every event to the service; this tool is one event's.
				if err := saved.ValidateID(in.EventID); err != nil {
					return discussions.List{}, err
				}
				return d.Discussions.List(ctx, viewer, in.EventID, "", in.values())
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "discussion_thread", Title: "Discussion thread", OutputSchema: outputSchema[discussionThread](), Annotations: readOnly,
			Description: "One question or tip with its replies oldest first, by thread_id: the thread_id of any post from event_discussions, community_discussions, " +
				"profile_activity, my_followed_discussions or my_notifications (for a reply, its thread_id, not its id), with that post's event_id. parent_id and parent_name show which post a reply answers. " +
				"count is the thread's visible replies. For more replies, pass next_cursor as cursor with the same event_id, thread_id and sign-in; " +
				"thread is returned on every page. Signed in, follow_discussion follows the thread for reply notifications."},
			publicRead(d, func(ctx context.Context, viewer string, in threadInput) (discussionThread, error) {
				if discussions.PostID(in.ThreadID) != nil {
					return discussionThread{}, &accounts.ValidationError{Field: "thread_id", Message: "use a question or tip id from event_discussions or community_discussions"}
				}
				root, err := d.Discussions.Thread(ctx, viewer, in.EventID, in.ThreadID)
				if errors.Is(err, discussions.ErrNotFound) {
					return discussionThread{}, errThreadNotFound
				}
				if err != nil {
					return discussionThread{}, err
				}
				replies, err := d.Discussions.List(ctx, viewer, in.EventID, in.ThreadID, pageInput{Limit: in.Limit, Cursor: in.Cursor}.values())
				return discussionThread{Thread: root, Replies: replies}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "community_discussions", Title: "Community questions and tips", OutputSchema: outputSchema[discussions.List](), Annotations: readOnly,
			Description: "Recent public questions and tips across events, newest first, each with its event. city, country (ISO code such as US) and category_id " +
				"from list_categories each narrow the list on their own. Open one with discussion_thread using its event_id and thread_id. " +
				"For the next page, pass next_cursor as cursor with the same filters and sign-in (cursors belong to the caller)."},
			publicRead(d, func(ctx context.Context, viewer string, in communityInput) (discussions.List, error) {
				return d.Discussions.List(ctx, viewer, "", "", in.values())
			}))
	}
	if d.CommunityRecommendations != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "community_recommendations", Title: "Community recommendations", OutputSchema: outputSchema[recommendations.Feed](), Annotations: readOnly,
			Description: "Events people recommend, one entry per event with its total count and up to three recommendations, newest first by when " +
				"the event was first recommended. city, country (ISO code such as US) and category_id from list_categories each narrow the feed on their own. " +
				"For the next page, pass next_cursor as cursor with the same filters."},
			publicRead(d, func(ctx context.Context, _ string, in communityInput) (recommendations.Feed, error) {
				return d.CommunityRecommendations.Community(ctx, in.values())
			}))
	}
	if d.Profiles != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "user_profile", Title: "User profile", OutputSchema: outputSchema[accounts.PublicProfile](), Annotations: readOnly,
			Description: "One person's public profile: id, display name and bio, never email or private settings. user_id is a profile id from " +
				"event_recommendations, event_interested_people or a discussion post, or the ID in a profile URL (/users/<id>); " +
				"omit it for the signed-in user's own public profile. profile_activity lists what they contributed."},
			publicRead(d, func(ctx context.Context, viewer string, in userInput) (accounts.PublicProfile, error) {
				user := in.UserID
				if user == "" {
					user = viewer
				}
				if user == "" {
					return accounts.PublicProfile{}, errUserNeeded
				}
				return d.Profiles.Public(ctx, user)
			}))
	}
	if d.LiveActivity != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "happening_now", Title: "Happening now", OutputSchema: outputSchema[activity.Snapshot](), Annotations: readOnly,
			Description: "Events people recommended in the last ten minutes, as on Ticketopia's Happening Now page: each with its title, city, " +
				"recent recommendation count and at most a recommender's first name, and expires_at, when it leaves the list. Optionally narrow by city with " +
				"country (ISO code such as US) and category_ids from list_categories. A current snapshot, not a history; call again for updates."},
			func(ctx context.Context, _ *mcp.CallToolRequest, in liveInput) (*mcp.CallToolResult, activity.Snapshot, error) {
				filter, err := in.filters()
				if err != nil {
					return nil, activity.Snapshot{}, toolError(d.describePrivate(err))
				}
				out, err := d.LiveActivity.FilteredSnapshot(ctx, filter)
				if err != nil {
					return nil, activity.Snapshot{}, toolError(errLiveUnavailable.Error())
				}
				return nil, out, nil
			})
	}
}

// publicRead is a public tool that, signed in with read access, knows the
// caller. Errors use the account API's wording, as the routes they mirror do;
// errProfileNeeded and similar call mistakes are passed through.
func publicRead[In, Out any](d Deps, call func(ctx context.Context, viewer string, in In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := call(ctx, readingCaller(req), in)
		if err != nil {
			var zero Out
			var mistake callMistake
			if errors.Is(err, errProfileNeeded) || errors.As(err, &mistake) {
				return nil, zero, toolError(err.Error())
			}
			return nil, zero, toolError(d.describePrivate(err))
		}
		return nil, out, nil
	}
}

// readingCaller is the signed-in account when the connection may read it,
// otherwise "" so the tool answers as it would anonymously.
func readingCaller(req *mcp.CallToolRequest) string {
	if req.Extra == nil || req.Extra.TokenInfo == nil || !slices.Contains(req.Extra.TokenInfo.Scopes, ScopeRead) {
		return ""
	}
	return req.Extra.TokenInfo.UserID
}

type profileActivityInput struct {
	UserID string  `json:"user_id,omitempty" jsonschema:"profile id from event_recommendations, event_interested_people or a discussion post, or the ID in a profile URL (/users/<id>); omit for your own"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

// eventPageInput is one event's paginated collection.
type eventPageInput struct {
	EventID string  `json:"event_id" jsonschema:"event ID from find_events, for example ticketmaster:G5v..."`
	Limit   *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor  *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (in eventPageInput) values() url.Values {
	return pageInput{Limit: in.Limit, Cursor: in.Cursor}.values()
}

type threadInput struct {
	EventID  string  `json:"event_id" jsonschema:"event ID the thread belongs to"`
	ThreadID string  `json:"thread_id" jsonschema:"thread_id from event_discussions, community_discussions, profile_activity, my_followed_discussions or my_notifications (for a reply, its thread_id, not its id)"`
	Limit    *int    `json:"limit,omitempty" jsonschema:"replies per page, 1 to 100, default 20"`
	Cursor   *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page of replies"`
}

// discussionThread is a root post with one page of its replies, as
// GET .../discussions/{id} and .../discussions/{id}/replies return them.
type discussionThread struct {
	Thread  discussions.Post `json:"thread"`
	Replies discussions.List `json:"replies"`
}

// communityInput is a city/country/category-scoped community collection.
type communityInput struct {
	City       string  `json:"city,omitempty" jsonschema:"city name, for example Chicago"`
	Country    string  `json:"country,omitempty" jsonschema:"two-letter country code, for example US"`
	CategoryID string  `json:"category_id,omitempty" jsonschema:"category ID from list_categories"`
	Limit      *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor     *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page; repeat the same filters"`
}

func (in communityInput) values() url.Values {
	v := pageInput{Limit: in.Limit, Cursor: in.Cursor}.values()
	for key, value := range map[string]string{"city": in.City, "country": in.Country, "category_id": in.CategoryID} {
		if value != "" {
			v.Set(key, value)
		}
	}
	return v
}

type userInput struct {
	UserID string `json:"user_id,omitempty" jsonschema:"account ID from a profile URL (/users/<id>) or another tool's profile.id; omit for your own"`
}

type liveInput struct {
	City        string   `json:"city,omitempty" jsonschema:"city name, for example Chicago; needs country"`
	Country     string   `json:"country,omitempty" jsonschema:"two-letter country code, for example US; needs city"`
	CategoryIDs []string `json:"category_ids,omitempty" jsonschema:"up to 16 category IDs from list_categories"`
}

var (
	liveCountry  = regexp.MustCompile(`^[A-Za-z]{2}$`)
	liveCategory = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// filters builds the route's public filter JSON and decodes it with the same
// validation, so both accept and reject the same filters. The checks first
// only name the argument an agent got wrong; the decoder stays authoritative.
func (in liveInput) filters() (activity.Filters, error) {
	invalid := func(field, message string) (activity.Filters, error) {
		return activity.Filters{}, &accounts.ValidationError{Field: field, Message: message}
	}
	switch {
	case in.City != "" && in.Country == "":
		return invalid("country", "give the city's two-letter country code too, for example US")
	case in.City == "" && in.Country != "":
		return invalid("city", "give a city with the country; to see everywhere, omit both")
	case in.Country != "" && !liveCountry.MatchString(in.Country):
		return invalid("country", "use a two-letter country code, for example US")
	case len(in.CategoryIDs) > 16:
		return invalid("category_ids", "use at most 16 category IDs")
	}
	for _, id := range in.CategoryIDs {
		if !liveCategory.MatchString(id) {
			return invalid("category_ids", "use category IDs from list_categories")
		}
	}
	raw := map[string]any{}
	if in.City != "" || in.Country != "" {
		raw["location"] = map[string]string{"mode": "city", "city": in.City, "country": in.Country}
	}
	if in.CategoryIDs != nil {
		raw["category_ids"] = in.CategoryIDs
	}
	data, _ := json.Marshal(raw)
	f, err := activity.DecodePublicFilters(data)
	if err != nil {
		return f, &accounts.ValidationError{Field: "filter", Message: "use a city with its two-letter country, and at most 16 valid category IDs"}
	}
	return f, nil
}
