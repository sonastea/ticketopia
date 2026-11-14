package appmcp

import (
	"context"
	"errors"
	"net/url"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

// EventCommunity reads an event's public community totals, plus viewer's own
// state when viewer is set, as GET /api/v1/events/{id}/community does.
type EventCommunity interface {
	EventCommunity(ctx context.Context, viewer, id string) (eventactivity.Community, error)
}

// ProfileActivity reads one person's public contributions, as
// GET /api/v1/users/{id}/activity does.
type ProfileActivity interface {
	ProfileActivity(ctx context.Context, user string, values url.Values) (useractivity.List, error)
}

var errProfileNeeded = errors.New("user_id is required: pass the ID from a Ticketopia profile URL (/users/<id>), or sign in to read your own public activity")

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
			func(ctx context.Context, req *mcp.CallToolRequest, in eventInput) (*mcp.CallToolResult, eventactivity.Community, error) {
				out, err := d.EventCommunity.EventCommunity(ctx, readingCaller(req), in.EventID)
				if err != nil {
					return toolError(d.describePrivate(err)), eventactivity.Community{}, nil
				}
				return nil, out, nil
			})
	}
	if d.ProfileActivity != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "profile_activity", Title: "Profile activity", OutputSchema: outputSchema[useractivity.List](), Annotations: readOnly,
			Description: "One person's public contributions on Ticketopia, newest first: discussion posts and replies, recommendations, " +
				"and events they made their Interested choice public for, each with the event. Never includes private interest, saves or follows. " +
				"user_id is the ID in a profile URL (/users/<id>); omit it for the signed-in user's own public activity. " +
				"For the next page, pass next_cursor as cursor with the same user_id and limit."},
			func(ctx context.Context, req *mcp.CallToolRequest, in profileActivityInput) (*mcp.CallToolResult, useractivity.List, error) {
				user := in.UserID
				if user == "" {
					user = readingCaller(req)
				}
				if user == "" {
					return toolError(errProfileNeeded.Error()), useractivity.List{}, nil
				}
				out, err := d.ProfileActivity.ProfileActivity(ctx, user, pageInput{Limit: in.Limit, Cursor: in.Cursor}.values())
				if err != nil {
					return toolError(d.describePrivate(err)), useractivity.List{}, nil
				}
				return nil, out, nil
			})
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
	UserID string  `json:"user_id,omitempty" jsonschema:"account ID from a profile URL (/users/<id>); omit for your own"`
	Limit  *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}
