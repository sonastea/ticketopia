// Package appmcp serves Ticketopia's event tools, and with an API token the
// caller's saves and reminders, to users' agents over
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
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	// Metrics, when set, counts tool calls for the periodic MCP metrics log.
	Metrics *Metrics
	// Describe turns a service error into the user-facing message the JSON API
	// uses for it, including any retry delay, so agents see the same wording.
	Describe func(error) string
	// Saved, Reminders, Radar, Follows (with Catalog) and Interests enable
	// their signed-in tools; nil leaves them out.
	Saved     Saved
	Reminders Reminders
	Radar     Radar
	Follows   Follows
	Catalog   Catalog
	Interests Interests
	// Recommendations and Drafts enable the recommendation and draft tools;
	// Interests also enables the Interested visibility tools.
	Recommendations Recommendations
	Drafts          Drafts
	// DraftsURL is the absolute page where the person reviews drafts
	// (AUTH_BASE_URL/me/drafts). It only lists them: publishing is a POST.
	DraftsURL string
	// PublishLimit, when set, bounds publishing tool calls per account.
	PublishLimit *PublishLimiter
	// AgentPosts enables post_question and post_reply (with Discussions) and
	// publish_draft (with Drafts) for connections holding publish.
	AgentPosts AgentPosts
	// Helpful enables mark_helpful and unmark_helpful; with Drafts,
	// Discussions also enables draft_question and draft_reply.
	Helpful Helpful
	// SameQuestion enables mark_same_question and unmark_same_question.
	SameQuestion SameQuestion
	// OpenQuestions enables open_questions, the public "Can you help?" list.
	OpenQuestions OpenQuestions
	// EventCommunity and ProfileActivity enable the public community read
	// tools, which add the caller's own state when signed in.
	EventCommunity           EventCommunity
	ProfileActivity          ProfileActivity
	EventRecommendations     EventRecommendations
	InterestedPeople         InterestedPeople
	Discussions              Discussions
	CommunityRecommendations CommunityRecommendations
	Profiles                 Profiles
	LiveActivity             LiveActivity
	// FollowedDiscussions enables the signed-in followed-discussion tools.
	FollowedDiscussions FollowedDiscussions
	// Accounts reads the caller's remembered city, which find_events
	// defaults to.
	Accounts Accounts
	// DescribePrivate words signed-in tool errors as the account API does.
	DescribePrivate func(error) string
	// DescribeCatalog words artist and venue search errors as the catalog
	// API does, including any retry delay; Describe is used when nil.
	DescribeCatalog func(error) string

	// arguments collects input schemas as NewServer registers tools.
	arguments argumentSchemas
}

// NewServer returns the MCP server with the public tools registered, plus the
// signed-in tools whose services Deps provides.
func NewServer(d Deps) *mcp.Server {
	if d.Now == nil {
		d.Now = time.Now
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "ticketopia", Title: "Ticketopia", Version: "0.1.0"}, &mcp.ServerOptions{
		Instructions: "Ticketopia finds live events (music, sports, arts and more) from Ticketmaster data. " +
			"Results carry meta.data_as_of and meta.stale; say when data is not live, and send people to the event's ticket link to buy. " +
			"Community reads are public: event_community totals an event's interest, recommendations and discussions; event_recommendations, " +
			"event_interested_people, event_discussions and discussion_thread show them; community_recommendations, community_discussions, open_questions and " +
			"happening_now browse across events (open_questions lists questions nobody has answered yet, for people who can help); user_profile and profile_activity describe a person, by a profile id those tools return. " +
			"Signed in, my_radar suggests events from the user's follows and categories, and tools manage their saves, reminders, follows and Interested choices privately. " +
			"Only make_interest_public, recommend_event, withdraw_recommendation, post_question, post_reply, publish_draft, mark_helpful, unmark_helpful, mark_same_question and unmark_same_question act publicly in the user's name; " +
			"they need the user's permission to post publicly, and you should use them only when the user asks. Everything you post or publish is labelled as an agent's. " +
			"To let the user review words first, draft_recommendation, draft_question and draft_reply save a private draft they read and publish at the review_url it returns. " +
			"You cannot edit or remove the user's posts; they do that on Ticketopia.",
		SchemaCache: schemaCache,
	})
	d.arguments = argumentSchemas{}
	server.AddReceivingMiddleware(observe(d.Logger, d.Metrics), checkArguments(d.arguments))
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	addTool(server, d, &mcp.Tool{Name: "find_events", Title: "Find events", OutputSchema: outputSchema[eventSearch](), Annotations: readOnly,
		Description: "Search events by city and country (ISO code such as US), dates, category, or keyword. " +
			"Dates are YYYY-MM-DD and match each event's local date; start_date defaults to today in UTC and end_date to 90 days after it, at most 366 days. " +
			"When signed in, a search without city, country or keyword uses the city saved in the user's account, reported as default_location. " +
			"area=metro widens a city in a known metro area (currently Los Angeles, with Inglewood, Hollywood, Anaheim and other nearby cities) and reports it as area; other cities are searched exactly. " +
			"fold=run lists each run (the same event name at the same venue, such as a theatre run or timed entry slots) once, as its earliest listing with a run summary whose count includes that listing; total then counts runs. " +
			"Ticket add-ons such as seat upgrades are not listed. " +
			"For the next page, pass next_cursor as cursor with exactly the same other arguments."},
		func(ctx context.Context, req *mcp.CallToolRequest, in findInput) (*mcp.CallToolResult, eventSearch, error) {
			var out eventSearch
			if in.City == "" && in.Country == "" && in.Keyword == "" {
				out.DefaultLocation = d.rememberedCity(ctx, callerID(req))
				if out.DefaultLocation != nil {
					in.City, in.Country = out.DefaultLocation.City, out.DefaultLocation.Country
				}
			}
			q, err := discovery.ParseQuery(in.values(), d.Now())
			if err == nil {
				out.EventList, err = d.Events.Events(ctx, q)
			}
			if err != nil {
				return nil, eventSearch{}, d.describe(err)
			}
			return nil, out, nil
		})
	addTool(server, d, &mcp.Tool{Name: "list_categories", Title: "List categories", OutputSchema: outputSchema[models.CategoryList](), Annotations: readOnly,
		Description: "Event categories (such as Music, Sports, Arts & Theatre) and their genres, with the IDs find_events accepts as category_id."},
		toolErrors(d, func(ctx context.Context, _ struct{}) (models.CategoryList, error) {
			return d.Events.Categories(ctx)
		}))
	addTool(server, d, &mcp.Tool{Name: "event_details", Title: "Event details", OutputSchema: outputSchema[models.EventDetail](), Annotations: readOnly,
		Description: "Details for one event by its ID from find_events (for example ticketmaster:G5v...), including prices, sale dates, venue, freshness and recent changes. " +
			"For a repeated event, dates lists its upcoming showings at the same venue (up to 100, including this one unless it has started); dates_more means more follow, so open the last date's event_details for the next ones."},
		toolErrors(d, func(ctx context.Context, in detailsInput) (models.EventDetail, error) {
			return d.Details.Detail(ctx, in.EventID)
		}))
	addPrivateTools(server, d)
	addPersonalTools(server, d)
	addPublishingTools(server, d)
	addCommunityTools(server, d)
	addFollowedTools(server, d)
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
	Area       string `json:"area,omitempty" jsonschema:"metro to include the city's nearby cities, or city (default) for the city only"`
	Fold       string `json:"fold,omitempty" jsonschema:"run to list each run of same-named listings at one venue once; omit for every listing"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page; repeat the same other arguments"`
}

// values renders the input as Discover query parameters, so ParseQuery
// applies exactly the HTML and /api/v1 validation and defaults.
func (in findInput) values() url.Values {
	v := url.Values{}
	for key, value := range map[string]string{"city": in.City, "country": in.Country, "keyword": in.Keyword,
		"category_id": in.CategoryID, "start_date": in.StartDate, "end_date": in.EndDate, "sort": in.Sort, "cursor": in.Cursor,
		"area": in.Area, "fold": in.Fold} {
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

// schemaCache shares inferred and resolved tool schemas between servers, which
// tests and fixtures build many times; resolution dominated their build cost.
// Cached schemas are shared, so nothing may modify a registered tool's schema.
var (
	schemaCache   = mcp.NewSchemaCache()
	outputSchemas sync.Map // reflect.Type -> *jsonschema.Schema
)

// outputSchema describes a tool's structured result, overriding inference for
// custom JSON: Freshness serializes an unknown data_as_of as null, and history
// changes carry raw JSON values, not byte arrays. Each type's schema is built
// once, so schemaCache recognizes it by pointer and resolves it once.
func outputSchema[T any]() *jsonschema.Schema {
	if s, ok := outputSchemas.Load(reflect.TypeFor[T]()); ok {
		return s.(*jsonschema.Schema)
	}
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
	cached, _ := outputSchemas.LoadOrStore(reflect.TypeFor[T](), s)
	return cached.(*jsonschema.Schema)
}

// eventSearch is find_events' result: the event list, plus the account's
// city when the search defaulted to it.
type eventSearch struct {
	models.EventList
	DefaultLocation *searchLocation `json:"default_location,omitempty"`
}

type searchLocation struct {
	City    string `json:"city"`
	Country string `json:"country"`
	Source  string `json:"source" jsonschema:"account: the city saved in the user's account preferences"`
}

// rememberedCity is the caller's saved city, or nil when anonymous, without
// a saved city, or when the account cannot be read: the search then runs as
// it would anonymously rather than failing.
func (d Deps) rememberedCity(ctx context.Context, owner string) *searchLocation {
	if owner == "" || d.Accounts == nil {
		return nil
	}
	account, err := d.Accounts.Account(ctx, owner)
	if err != nil || account.Preferences.City == "" {
		return nil
	}
	return &searchLocation{City: account.Preferences.City, Country: account.Preferences.Country, Source: "account"}
}

func (d Deps) describe(err error) error {
	message := err.Error()
	if d.Describe != nil {
		message = d.Describe(err)
	}
	return toolError(message)
}

// toolErrors reports service errors as tool errors the agent can act on.
func toolErrors[In, Out any](d Deps, call func(context.Context, In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		out, err := call(ctx, in)
		if err != nil {
			var zero Out
			return nil, zero, d.describe(err)
		}
		return nil, out, nil
	}
}

// observe records tool calls, including SDK rejections, without arguments or results.
func observe(logger zerolog.Logger, metrics *Metrics) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			call, ok := req.(*mcp.CallToolRequest)
			if !ok {
				return next(ctx, method, req)
			}
			start := time.Now()
			// Client-sent names that match no tool are reported as "unknown".
			tool := toolName(call.Params.Name)
			ctx, span := telemetry.Tracer().Start(ctx, "execute_tool "+tool, trace.WithAttributes(
				attribute.String("gen_ai.operation.name", "execute_tool"),
				attribute.String("gen_ai.tool.name", tool)))
			outcome := "panic"
			var elapsed time.Duration
			defer func() {
				span.SetAttributes(attribute.String("mcp.tool.outcome", outcome))
				if outcome != "ok" {
					span.SetStatus(codes.Error, "")
				}
				span.End()
				if outcome != "panic" {
					telemetry.LogTrace(logger.Info(), ctx).Str("tool", tool).Str("outcome", outcome).Dur("duration", elapsed).Msg("MCP tool call")
				}
			}()
			result, err := next(ctx, method, req)
			outcome = "ok"
			if err != nil {
				outcome = "error"
			} else if r, ok := result.(*mcp.CallToolResult); ok && r.IsError {
				outcome = "tool_error"
			}
			elapsed = time.Since(start)
			metrics.observe(ctx, tool, outcome, call.Extra != nil && call.Extra.TokenInfo != nil, elapsed)
			return result, err
		}
	}
}
