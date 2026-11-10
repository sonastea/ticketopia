package appmcp

import (
	"context"
	"errors"
	"net/url"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/internal/saved"
)

// Saved is the caller's private saved-events collection.
type Saved interface {
	List(ctx context.Context, owner string, values url.Values) (saved.List, error)
	Save(ctx context.Context, owner, id string) (saved.Item, bool, error)
	Remove(ctx context.Context, owner, id string) error
}

// Reminders are the caller's public on-sale email reminders.
type Reminders interface {
	List(ctx context.Context, owner string, values url.Values) (reminders.Page, error)
	Set(ctx context.Context, owner, event string, lead int) (reminders.Item, error)
	Remove(ctx context.Context, owner, event string) error
}

// errSignIn is reported when a signed-in tool is called without a verified
// API token; the HTTP boundary rejects invalid tokens before tools run.
var errSignIn = errors.New("sign in required: create an API token under API access on your Ticketopia account page (/me), then connect with the header Authorization: Bearer <token>")

var (
	no  = false
	yes = true
)

// addPrivateTools registers the signed-in tools for the services available.
// Each acts only on the account behind the request's API token.
func addPrivateTools(server *mcp.Server, d Deps) {
	if d.Saved != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "my_saved_events", Title: "My saved events", OutputSchema: outputSchema[saved.List](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The signed-in user's private saved events, newest first. For the next page, pass next_cursor as cursor with the same limit."},
			privateTool(d, func(ctx context.Context, owner string, in pageInput) (saved.List, error) {
				return d.Saved.List(ctx, owner, in.values())
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "save_event", Title: "Save event", OutputSchema: outputSchema[savedState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Save an event to the signed-in user's private saved events. Saving again keeps the original save. Saving does not reserve tickets or turn on reminders."},
			privateTool(d, func(ctx context.Context, owner string, in eventInput) (savedState, error) {
				item, created, err := d.Saved.Save(ctx, owner, in.EventID)
				return savedState{Saved: true, Created: created, EventID: in.EventID, Item: &item}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "unsave_event", Title: "Remove saved event", OutputSchema: outputSchema[savedState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Remove an event from the signed-in user's saved events. This also stops its on-sale reminder. Removing an event that is not saved changes nothing."},
			privateTool(d, func(ctx context.Context, owner string, in eventInput) (savedState, error) {
				return savedState{EventID: in.EventID}, d.Saved.Remove(ctx, owner, in.EventID)
			}))
	}
	if d.Reminders != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "my_reminders", Title: "My reminders", OutputSchema: outputSchema[reminders.Page](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The signed-in user's public on-sale email reminders with their delivery state. For the next page, pass next_cursor as cursor with the same limit."},
			privateTool(d, func(ctx context.Context, owner string, in pageInput) (reminders.Page, error) {
				return d.Reminders.List(ctx, owner, in.values())
			}))
		// Destructive: changing an existing reminder replaces its timing.
		mcp.AddTool(server, &mcp.Tool{Name: "set_reminder", Title: "Set on-sale reminder", OutputSchema: outputSchema[reminders.Item](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Set or change the email reminder for an event's public on-sale time, lead_minutes before it (1 to 10080, up to seven days). " +
				"The event must be saved first (save_event). Delivery also depends on the user's email and notification preferences, shown in the result's state."},
			privateTool(d, func(ctx context.Context, owner string, in reminderInput) (reminders.Item, error) {
				return d.Reminders.Set(ctx, owner, in.EventID, in.LeadMinutes)
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "stop_reminder", Title: "Stop on-sale reminder", OutputSchema: outputSchema[reminderState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Stop the on-sale reminder for an event; the event stays saved. A message already being sent cannot be recalled. Stopping a reminder that is not set changes nothing."},
			privateTool(d, func(ctx context.Context, owner string, in eventInput) (reminderState, error) {
				return reminderState{EventID: in.EventID, Reminder: "stopped"}, d.Reminders.Remove(ctx, owner, in.EventID)
			}))
	}
}

// pageInput keeps whether each field was sent, so an explicit 0 or empty
// cursor fails validation as in the account API instead of meaning page one.
type pageInput struct {
	Limit  *int    `json:"limit,omitempty" jsonschema:"results per page, 1 to 100, default 20"`
	Cursor *string `json:"cursor,omitempty" jsonschema:"next_cursor from the previous page"`
}

func (in pageInput) values() url.Values {
	v := url.Values{}
	if in.Limit != nil {
		v.Set("limit", strconv.Itoa(*in.Limit))
	}
	if in.Cursor != nil {
		v.Set("cursor", *in.Cursor)
	}
	return v
}

type eventInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events, for example ticketmaster:G5v..."`
}

type reminderInput struct {
	EventID     string `json:"event_id" jsonschema:"ID of a saved event"`
	LeadMinutes int    `json:"lead_minutes" jsonschema:"minutes before the public on-sale time, 1 to 10080"`
}

// savedState is the resulting saved state, so agents need not re-read it.
type savedState struct {
	EventID string      `json:"event_id"`
	Saved   bool        `json:"saved"`
	Created bool        `json:"created"`
	Item    *saved.Item `json:"item,omitempty"`
}

type reminderState struct {
	EventID  string `json:"event_id"`
	Reminder string `json:"reminder"`
}

// privateTool resolves the account from the verified API token and reports
// errors in the account API's wording.
func privateTool[In, Out any](d Deps, call func(ctx context.Context, owner string, in In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		owner := ""
		if req.Extra != nil && req.Extra.TokenInfo != nil {
			owner = req.Extra.TokenInfo.UserID
		}
		if owner == "" {
			return toolError(errSignIn.Error()), zero, nil
		}
		out, err := call(ctx, owner, in)
		if err != nil {
			message := err.Error()
			if d.DescribePrivate != nil {
				message = d.DescribePrivate(err)
			}
			return toolError(message), zero, nil
		}
		return nil, out, nil
	}
}

func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}
