package appmcp

import (
	"context"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
)

// FollowedDiscussions are the caller's private discussion follows and their
// reply notifications, as /api/v1/me/discussions and /me/notifications.
type FollowedDiscussions interface {
	List(ctx context.Context, who string, values url.Values) (discussionfollows.Page[discussionfollows.Follow], error)
	Notifications(ctx context.Context, who string, values url.Values) (discussionfollows.Page[discussionfollows.Notification], error)
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
		Description: "New replies in discussions the signed-in user follows, newest first: each with the event name, reply_count, whether it is read, " +
			"its post_id, and url, a path on the Ticketopia site to the reply. Open the conversation with discussion_thread using event_id and thread_id. " +
			"Reading this list does not mark anything read. For the next page, pass next_cursor as cursor."},
		privateTool(d, func(ctx context.Context, owner string, in pageInput) (discussionfollows.Page[discussionfollows.Notification], error) {
			return d.FollowedDiscussions.Notifications(ctx, owner, in.values())
		}))
}
