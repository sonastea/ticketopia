// Package appmcp serves Ticketopia's public event tools to users' agents over
// MCP (Streamable HTTP, stateless). Tools reuse the discovery services behind
// the HTML pages and /api/v1, so validation, caching, freshness and errors
// match. See docs/app-mcp.md and docs/design/app-mcp.md.
package appmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
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
	// uses for it, including any retry delay, so agents see the same wording.
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
	server.AddReceivingMiddleware(observe(d.Logger))
	mcp.AddTool(server, &mcp.Tool{Name: "find_events", Title: "Find events", OutputSchema: outputSchema[models.EventList](),
		Description: "Search events by city and country (ISO code such as US), dates, category, or keyword. " +
			"Dates are YYYY-MM-DD and match each event's local date; start_date defaults to today in UTC and end_date to 90 days after it, at most 366 days. " +
			"For the next page, pass next_cursor as cursor with exactly the same other arguments."},
		toolErrors(d, func(ctx context.Context, in findInput) (models.EventList, error) {
			q, err := discovery.ParseQuery(in.values(), d.Now())
			if err != nil {
				return models.EventList{}, err
			}
			return d.Events.Events(ctx, q)
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "list_categories", Title: "List categories", OutputSchema: outputSchema[models.CategoryList](),
		Description: "Event categories (such as Music, Sports, Arts & Theatre) and their genres, with the IDs find_events accepts as category_id."},
		toolErrors(d, func(ctx context.Context, _ struct{}) (models.CategoryList, error) {
			return d.Events.Categories(ctx)
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "event_details", Title: "Event details", OutputSchema: outputSchema[models.EventDetail](),
		Description: "Details for one event by its ID from find_events (for example ticketmaster:G5v...), including prices, sale dates, venue, freshness and recent changes."},
		toolErrors(d, func(ctx context.Context, in detailsInput) (models.EventDetail, error) {
			return d.Details.Detail(ctx, in.EventID)
		}))
	return server
}

// Handler serves the tools statelessly: every request stands alone, so any
// replica can answer it and nothing is retained between calls. The SDK's
// loopback DNS-rebinding check is off because the HTTP boundary validates
// Origin against configured origins instead; the check would otherwise
// reject public-host requests from a reverse proxy connecting over loopback.
func Handler(d Deps) http.Handler {
	server := NewServer(d)
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: true})
}

type findInput struct {
	City       string `json:"city,omitempty" jsonschema:"city name, for example Chicago"`
	Country    string `json:"country,omitempty" jsonschema:"two-letter country code, for example US"`
	Keyword    string `json:"keyword,omitempty" jsonschema:"artist, team, venue or event words"`
	CategoryID string `json:"category_id,omitempty" jsonschema:"category ID from list_categories"`
	StartDate  string `json:"start_date,omitempty" jsonschema:"first event-local date, YYYY-MM-DD, default today in UTC"`
	EndDate    string `json:"end_date,omitempty" jsonschema:"last event-local date, YYYY-MM-DD, default 90 days after start_date, at most 366 days after it"`
	Sort       string `json:"sort,omitempty" jsonschema:"date_asc (default), date_desc, or name_asc"`
	Limit      *int   `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page; repeat the same other arguments"`
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
	if in.Limit != nil {
		v.Set("limit", strconv.Itoa(*in.Limit))
	}
	return v
}

type detailsInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events"`
}

// outputSchema describes a tool's structured result, overriding inference for
// custom JSON: Freshness serializes an unknown data_as_of as null, and history
// changes carry raw JSON values, not byte arrays.
func outputSchema[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](&jsonschema.ForOptions{TypeSchemas: map[reflect.Type]*jsonschema.Schema{
		reflect.TypeFor[json.RawMessage](): {},
		reflect.TypeFor[models.Freshness](): {Type: "object", Properties: map[string]*jsonschema.Schema{
			"data_as_of": {Types: []string{"string", "null"}, Format: "date-time"},
			"stale":      {Type: "boolean"},
			"coverage": {Type: "object", Properties: map[string]*jsonschema.Schema{
				"status": {Type: "string"}, "collected_days": {Type: "integer"}, "total_days": {Type: "integer"},
			}},
		}, Required: []string{"data_as_of", "stale"}},
	}})
	if err != nil {
		panic(err)
	}
	return s
}

// toolErrors reports service errors as tool errors the agent can act on.
func toolErrors[In, Out any](d Deps, call func(context.Context, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := call(ctx, in)
		if err != nil {
			message := err.Error()
			if d.Describe != nil {
				message = d.Describe(err)
			}
			var zero Out
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}, zero, nil
		}
		return nil, out, nil
	}
}

// observe logs every tools/call with its tool, outcome and duration, never
// its arguments, including calls the SDK rejects before a tool runs.
func observe(logger zerolog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok {
				return next(ctx, method, req)
			}
			start := time.Now()
			result, err := next(ctx, method, req)
			outcome := "ok"
			if err != nil {
				outcome = "error"
			} else if r, ok := result.(*mcp.CallToolResult); ok && r.IsError {
				outcome = "tool_error"
			}
			logger.Info().Str("tool", call.Params.Name).Str("outcome", outcome).Dur("duration", time.Since(start)).Msg("MCP tool call")
			return result, err
		}
	}
}
