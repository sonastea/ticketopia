package appmcp

import (
	"context"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

// Radar is the caller's personalized radar: upcoming events near their
// remembered city matching their follows and categories.
type Radar interface {
	List(ctx context.Context, owner string, values url.Values) (radar.List, error)
}

// Follows are the caller's private artist and venue follows.
type Follows interface {
	List(ctx context.Context, owner string, values url.Values) (follows.List, error)
	Follow(ctx context.Context, owner, kind, id string) (follows.Item, bool, error)
	Remove(ctx context.Context, owner, kind, id string) error
}

// Catalog searches the provider's artists and venues to follow.
type Catalog interface {
	Catalog(ctx context.Context, kind string, values url.Values) (models.CatalogList, error)
}

// Interests are the caller's Interested choices, each private or public.
type Interests interface {
	List(ctx context.Context, owner string, public bool, values url.Values) (interests.List, error)
	Set(ctx context.Context, owner, id string, visibility *string, defaultVisibility string) (interests.Item, bool, error)
	Remove(ctx context.Context, owner, id string) error
}

// Accounts reads the signed-in caller's own account.
type Accounts interface {
	Account(ctx context.Context, id string) (accounts.Account, error)
}

// addPersonalTools registers radar, follow and Interested tools for the
// services available, each acting only on the token's account.
func addPersonalTools(server *mcp.Server, d Deps) {
	if d.Radar != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "my_radar", Title: "My radar", OutputSchema: outputSchema[radar.List](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "Upcoming events picked for the signed-in user over the next 90 days, near the city saved in their account, " +
				"from the artists and venues they follow and their chosen categories, each with the reasons it matched (mode personalized). " +
				"With no follows or categories it lists nearby events instead (mode nearby); without a saved city it is empty (mode needs_location), " +
				"and the person can set one in their account preferences. " +
				"For the next page, pass next_cursor as cursor with the same limit; a cursor lasts 24 hours."},
			privateTool(d, func(ctx context.Context, owner string, in pageInput) (radar.List, error) {
				return d.Radar.List(ctx, owner, in.values())
			}))
	}
	if d.Follows != nil && d.Catalog != nil {
		// Searches reach the provider, as on the web, so they need sign-in too.
		mcp.AddTool(server, &mcp.Tool{Name: "find_artists", Title: "Find artists", OutputSchema: outputSchema[models.CatalogList](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			Description: "Search performers (artists, bands, teams, comedians) by name, for their IDs to follow. " +
				"For the next page, pass next_cursor as cursor with the same other arguments."},
			catalogTool(d, "artist", func(in artistSearch) url.Values { return in.values() }))
		mcp.AddTool(server, &mcp.Tool{Name: "find_venues", Title: "Find venues", OutputSchema: outputSchema[models.CatalogList](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
			Description: "Search venues by name, optionally within a city and country, for their IDs to follow. " +
				"For the next page, pass next_cursor as cursor with the same other arguments."},
			catalogTool(d, "venue", func(in venueSearch) url.Values { return in.values() }))
		mcp.AddTool(server, &mcp.Tool{Name: "my_follows", Title: "My follows", OutputSchema: outputSchema[follows.List](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The artists and venues the signed-in user follows privately, newest first; kind narrows to artist or venue. " +
				"For the next page, pass next_cursor as cursor with the same kind and limit."},
			privateTool(d, func(ctx context.Context, owner string, in followsInput) (follows.List, error) {
				return d.Follows.List(ctx, owner, in.values())
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "follow_artist_or_venue", Title: "Follow artist or venue", OutputSchema: outputSchema[followState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Follow an artist or venue privately, by its ID from find_artists or find_venues, so its events appear in my_radar. " +
				"Following again keeps the original follow. Follows are never shown to other people."},
			privateTool(d, func(ctx context.Context, owner string, in followInput) (followState, error) {
				item, created, err := d.Follows.Follow(ctx, owner, in.Kind, in.TargetID)
				return followState{Kind: in.Kind, TargetID: in.TargetID, Following: true, Created: created, Item: &item}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "unfollow_artist_or_venue", Title: "Unfollow artist or venue", OutputSchema: outputSchema[followState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Stop following an artist or venue. Unfollowing one that is not followed changes nothing."},
			privateTool(d, func(ctx context.Context, owner string, in followInput) (followState, error) {
				return followState{Kind: in.Kind, TargetID: in.TargetID}, d.Follows.Remove(ctx, owner, in.Kind, in.TargetID)
			}))
	}
	if d.Interests != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "my_interested_events", Title: "My Interested events", OutputSchema: outputSchema[interests.List](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "Events the signed-in user marked Interested, newest first, each with its visibility (private, or public on their profile). " +
				"For the next page, pass next_cursor as cursor with the same limit."},
			privateTool(d, func(ctx context.Context, owner string, in pageInput) (interests.List, error) {
				return d.Interests.List(ctx, owner, false, in.values())
			}))
		// Agents never publish: connecting promises the app can't post anything
		// publicly, so new choices are private and existing ones keep the
		// visibility the person chose. Making a choice public stays on the web.
		mcp.AddTool(server, &mcp.Tool{Name: "mark_interested", Title: "Mark Interested", OutputSchema: outputSchema[interestState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Mark an event Interested for the signed-in user. A new choice is private: it counts only toward the event's anonymous total. " +
				"An existing choice keeps its visibility; only the person can make a choice public, on the event's page. " +
				"Interested is separate from saving and does not save the event."},
			privateTool(d, func(ctx context.Context, owner string, in eventInput) (interestState, error) {
				item, created, err := d.Interests.Set(ctx, owner, in.EventID, nil, "private")
				return interestState{EventID: in.EventID, Interested: true, Created: created, Item: &item}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "unmark_interested", Title: "Remove Interested", OutputSchema: outputSchema[interestState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Remove the user's Interested choice for an event, public or private. Removing one that is not set changes nothing."},
			privateTool(d, func(ctx context.Context, owner string, in eventInput) (interestState, error) {
				return interestState{EventID: in.EventID}, d.Interests.Remove(ctx, owner, in.EventID)
			}))
	}
}

// catalogTool is a signed-in search worded as the artist and venue API, with
// any retry delay in the message since a tool error carries no header.
func catalogTool[In any](d Deps, kind string, values func(In) url.Values) mcp.ToolHandlerFor[In, models.CatalogList] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, models.CatalogList, error) {
		if callerID(req) == "" {
			return toolError(errSignIn.Error()), models.CatalogList{}, nil
		}
		list, err := d.Catalog.Catalog(ctx, kind, values(in))
		if err != nil {
			if d.DescribeCatalog != nil {
				return toolError(d.DescribeCatalog(err)), models.CatalogList{}, nil
			}
			return d.describe(err), models.CatalogList{}, nil
		}
		return nil, list, nil
	}
}

func callerID(req *mcp.CallToolRequest) string {
	if req.Extra != nil && req.Extra.TokenInfo != nil {
		return req.Extra.TokenInfo.UserID
	}
	return ""
}

type artistSearch struct {
	Keyword string  `json:"keyword" jsonschema:"artist, band or team name"`
	Limit   *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor  *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (in artistSearch) values() url.Values {
	return catalogValues(in.Keyword, "", "", in.Limit, in.Cursor)
}

type venueSearch struct {
	Keyword string  `json:"keyword" jsonschema:"venue name"`
	City    string  `json:"city,omitempty" jsonschema:"city name, for example Chicago"`
	Country string  `json:"country,omitempty" jsonschema:"two-letter country code, for example US"`
	Limit   *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor  *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (in venueSearch) values() url.Values {
	return catalogValues(in.Keyword, in.City, in.Country, in.Limit, in.Cursor)
}

// catalogValues renders a search as catalog query parameters, so the
// catalog's own validation applies.
func catalogValues(keyword, city, country string, limit *int, cursor *string) url.Values {
	v := url.Values{"keyword": {keyword}}
	for key, value := range map[string]string{"city": city, "country": country} {
		if value != "" {
			v.Set(key, value)
		}
	}
	if limit != nil {
		v.Set("limit", strconv.Itoa(*limit))
	}
	if cursor != nil {
		v.Set("cursor", *cursor)
	}
	return v
}

type followsInput struct {
	Kind   string  `json:"kind,omitempty" jsonschema:"artist or venue; omit for both"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (in followsInput) values() url.Values {
	v := pageInput{Limit: in.Limit, Cursor: in.Cursor}.values()
	if in.Kind != "" {
		v.Set("kind", in.Kind)
	}
	return v
}

type followInput struct {
	Kind     string `json:"kind" jsonschema:"artist or venue"`
	TargetID string `json:"target_id" jsonschema:"ID from find_artists or find_venues, for example ticketmaster:K8v..."`
}

type followState struct {
	Kind      string        `json:"kind"`
	TargetID  string        `json:"target_id"`
	Following bool          `json:"following"`
	Created   bool          `json:"created"`
	Item      *follows.Item `json:"item,omitempty"`
}

type interestState struct {
	EventID    string          `json:"event_id"`
	Interested bool            `json:"interested"`
	Created    bool            `json:"created"`
	Item       *interests.Item `json:"item,omitempty"`
}
