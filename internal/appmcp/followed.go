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
}
