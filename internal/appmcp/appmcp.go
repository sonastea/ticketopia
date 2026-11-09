// Package appmcp serves Ticketopia's public event tools to users' agents over
// MCP (Streamable HTTP, stateless). Tools reuse the discovery services behind
// the HTML pages and /api/v1, so validation, caching, freshness and errors
// match. See docs/app-mcp.md and docs/design/app-mcp.md.
package appmcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

// Events searches events with the same query rules as Discover and lists the
// provider's categories (segments) with their genres.
type Events interface {
	Events(context.Context, discovery.Query) (models.EventList, error)
	Categories(context.Context) (models.CategoryList, error)
}

// Details reads one event, including any durable last-known fallback.
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}

type Deps struct {
	Events  Events
	Details Details
	Now     func() time.Time
	Logger  zerolog.Logger
	// Describe turns a service error into the user-facing message the JSON API
	// uses for it, so agents see the same wording.
	Describe func(error) string
}

// NewServer returns the MCP server with the public tools registered.
func NewServer(d Deps) *mcp.Server {
	if d.Now == nil {
		d.Now = time.Now
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "ticketopia", Title: "Ticketopia", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Ticketopia finds live events (music, sports, arts and more) from Ticketmaster data. " +
			"Results carry meta.data_as_of and meta.stale; say when data is not live, and send people to the event's ticket link to buy.",
	})
	mcp.AddTool(server, &mcp.Tool{Name: "find_events", Title: "Find events",
		Description: "Search upcoming events by city and country (ISO code such as US), dates, category, or keyword. " +
			"Dates default to the next 90 days. Use next_cursor from a result to fetch the next page."},
		logged(d, "find_events", func(ctx context.Context, in findInput) (any, error) {
			q, err := discovery.ParseQuery(in.values(), d.Now())
			if err != nil {
				return nil, err
			}
			return d.Events.Events(ctx, q)
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "list_categories", Title: "List categories",
		Description: "Event categories (such as Music, Sports, Arts & Theatre) with their IDs for find_events category_id."},
		logged(d, "list_categories", func(ctx context.Context, _ struct{}) (any, error) {
			return d.Events.Categories(ctx)
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "event_details", Title: "Event details",
		Description: "Details for one event by its ID from find_events (for example ticketmaster:G5v...), including prices, sale dates, venue, freshness and recent changes."},
		logged(d, "event_details", func(ctx context.Context, in detailsInput) (any, error) {
			return d.Details.Detail(ctx, in.EventID)
		}))
	return server
}

// Handler serves the tools statelessly: every request stands alone, so any
// replica can answer it and nothing is retained between calls.
func Handler(d Deps) http.Handler {
	server := NewServer(d)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

type findInput struct {
	City       string `json:"city,omitempty" jsonschema:"city name, for example Chicago"`
	Country    string `json:"country,omitempty" jsonschema:"two-letter country code, for example US"`
	Keyword    string `json:"keyword,omitempty" jsonschema:"artist, team, venue or event words"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"category ID from list_categories"`
	StartDate  string `json:"start_date,omitempty" jsonschema:"first date, YYYY-MM-DD"`
	EndDate    string `json:"end_date,omitempty" jsonschema:"last date, YYYY-MM-DD, at most 366 days after start_date"`
	Sort       string `json:"sort,omitempty" jsonschema:"date_asc (default), date_desc, or name_asc"`
	Limit      int    `json:"limit,omitempty" jsonschema:"results per page, default 20"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

// values renders the input as Discover query parameters, so ParseQuery
// applies exactly the HTML and /api/v1 validation and defaults.
func (in findInput) values() url.Values {
	v := url.Values{}
	for key, value := range map[string]string{"city": in.City, "country": in.Country, "keyword": in.Keyword,
		"category_id": in.CategoryID, "start_date": in.StartDate, "end_date": in.EndDate, "sort": in.Sort, "cursor": in.Cursor} {
		if value != "" {
			v.Set(key, value)
		}
	}
	if in.Limit != 0 {
		v.Set("limit", strconv.Itoa(in.Limit))
	}
	return v
}

type detailsInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events"`
}

// logged records each call's tool, outcome and duration, never its arguments,
// and reports service errors as tool errors the agent can act on.
func logged[In any](d Deps, name string, call func(context.Context, In) (any, error)) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		out, err := call(ctx, in)
		outcome := "ok"
		if err != nil {
			outcome = errorKind(err)
		}
		d.Logger.Info().Str("tool", name).Str("outcome", outcome).Dur("duration", time.Since(start)).Msg("MCP tool call")
		if err != nil {
			message := err.Error()
			if d.Describe != nil {
				message = d.Describe(err)
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, nil, nil
		}
		return nil, out, nil
	}
}

func errorKind(err error) string {
	var invalid *discovery.ValidationError
	switch {
	case errors.As(err, &invalid):
		return "invalid"
	case errors.Is(err, discovery.ErrNotFound):
		return "not_found"
	default:
		return "unavailable"
	}
}
