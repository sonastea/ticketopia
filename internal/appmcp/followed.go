package appmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
)

// errNotificationNotFound covers unknown, not-yet-due and other accounts'
// notifications alike, so it reveals nothing about another account.
var errNotificationNotFound = callMistake{errors.New("notification_id: no notification with this id for you; use an id from my_notifications")}

// FollowedDiscussions are the caller's private discussion follows and their
// reply notifications, as /api/v1/me/discussions and /me/notifications.
type FollowedDiscussions interface {
	List(ctx context.Context, who string, values url.Values) (discussionfollows.Page[discussionfollows.Follow], error)
	Notifications(ctx context.Context, who string, values url.Values) (discussionfollows.Page[discussionfollows.Notification], error)
	Read(ctx context.Context, who, id string) error
}

// addFollowedTools registers the signed-in followed-discussion tools, each
// acting only on the token's account.
func addFollowedTools(server *mcp.Server, d Deps) {
	if d.FollowedDiscussions == nil {
		return
	}
	mcp.AddTool(server, &mcp.Tool{Name: "my_followed_discussions", Title: "My followed discussions", OutputSchema: outputSchema[discussionfollows.Page[discussionfollows.Follow]](),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
		Description: "Questions and tips the signed-in user follows privately, newest follow first, each with its event_id, thread_id, " +
			"notification frequency (immediate, daily, weekly or muted) and the question itself. Open one with discussion_thread using its " +
			"event_id and thread_id. For the next page, pass next_cursor as cursor."},
		privateTool(d, func(ctx context.Context, owner string, in pageInput) (discussionfollows.Page[discussionfollows.Follow], error) {
			return d.FollowedDiscussions.List(ctx, owner, in.values())
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "my_notifications", Title: "My reply notifications", OutputSchema: outputSchema[discussionfollows.Page[discussionfollows.Notification]](),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
		Description: "Reply notifications for discussions the signed-in user follows, read and unread, newest first: each with the event name, " +
			"reply_count (the replies it covers), read, the reply's post_id, and url, a path on the Ticketopia site (prefix the site's address to link it). " +
			"Open the conversation with discussion_thread using event_id and thread_id. Listing marks nothing read; use mark_notification_read with its id. " +
			"For the next page, pass next_cursor as cursor."},
		privateTool(d, func(ctx context.Context, owner string, in pageInput) (discussionfollows.Page[discussionfollows.Notification], error) {
			return d.FollowedDiscussions.Notifications(ctx, owner, in.values())
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "mark_notification_read", Title: "Mark notification read", OutputSchema: outputSchema[notificationState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Mark one of the signed-in user's reply notifications read, by its id from my_notifications. Marking it again changes nothing. " +
			"Read state is private."},
		privateTool(d, func(ctx context.Context, owner string, in notificationInput) (notificationState, error) {
			err := d.FollowedDiscussions.Read(ctx, owner, in.NotificationID)
			if errors.Is(err, discussions.ErrNotFound) {
				// The route's "contribution" wording would point an agent at a post.
				return notificationState{}, errNotificationNotFound
			}
			return notificationState{NotificationID: in.NotificationID, Read: true}, err
		}))
}

type notificationInput struct {
	NotificationID string `json:"notification_id" jsonschema:"id from my_notifications"`
}

type notificationState struct {
	NotificationID string `json:"notification_id"`
	Read           bool   `json:"read"`
}
