package appmcp

import (
	"context"
	"errors"
	"net/url"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/accounts"
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
	Set(ctx context.Context, who, event, thread, frequency string) (discussionfollows.Follow, error)
	Remove(ctx context.Context, who, event, thread string) error
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
			if errors.Is(err, discussionfollows.ErrNotificationNotFound) {
				// The route's "contribution" wording would point an agent at a post.
				return notificationState{}, errNotificationNotFound
			}
			return notificationState{NotificationID: in.NotificationID, Read: true}, err
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "follow_discussion", Title: "Follow discussion", OutputSchema: outputSchema[discussionFollowState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Follow a question or tip privately for the signed-in user, or change how often its replies notify them in my_notifications: " +
			"immediate (one notification per new reply from someone else), daily (one grouped notification at the next midnight UTC), weekly " +
			"(at the next Monday midnight UTC) or muted (keep the follow without notifications). Use its event_id and thread_id from event_discussions, " +
			"community_discussions, profile_activity or, to change a follow, my_followed_discussions. Following is never shown to other people. " +
			"Following again replaces the frequency."},
		privateTool(d, func(ctx context.Context, owner string, in followDiscussionInput) (discussionFollowState, error) {
			if err := threadTarget(in.ThreadID, nil); err != nil {
				return discussionFollowState{}, err
			}
			follow, err := d.FollowedDiscussions.Set(ctx, owner, in.EventID, in.ThreadID, in.Frequency)
			if err != nil {
				return discussionFollowState{}, threadTarget(in.ThreadID, err)
			}
			return discussionFollowState{EventID: in.EventID, ThreadID: in.ThreadID, Following: true, Follow: &follow}, nil
		}))
	mcp.AddTool(server, &mcp.Tool{Name: "unfollow_discussion", Title: "Unfollow discussion", OutputSchema: outputSchema[discussionFollowState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Stop following a question or tip, by its event_id and thread_id from my_followed_discussions. Its replies stop notifying the " +
			"signed-in user. Unfollowing one that is not followed changes nothing."},
		privateTool(d, func(ctx context.Context, owner string, in unfollowDiscussionInput) (discussionFollowState, error) {
			if err := threadTarget(in.ThreadID, nil); err != nil {
				return discussionFollowState{}, err
			}
			// Removing an absent follow succeeds, as on the route, so check the
			// thread first: a reply's id or the wrong event must not report an
			// unfollow while the real follow keeps notifying.
			if d.Discussions != nil {
				if _, err := d.Discussions.Thread(ctx, owner, in.EventID, in.ThreadID); err != nil {
					return discussionFollowState{}, threadTarget(in.ThreadID, err)
				}
			}
			if err := d.FollowedDiscussions.Remove(ctx, owner, in.EventID, in.ThreadID); err != nil {
				return discussionFollowState{}, threadTarget(in.ThreadID, err)
			}
			return discussionFollowState{EventID: in.EventID, ThreadID: in.ThreadID}, nil
		}))
}

// threadTarget validates thread_id under its own name and words a missing or
// foreign thread as discussion_thread does.
func threadTarget(thread string, err error) error {
	if discussions.PostID(thread) != nil {
		return &accounts.ValidationError{Field: "thread_id", Message: "use a question or tip id from event_discussions or community_discussions"}
	}
	if errors.Is(err, discussions.ErrNotFound) {
		return errThreadNotFound
	}
	return err
}

type followDiscussionInput struct {
	EventID   string `json:"event_id" jsonschema:"event ID the thread belongs to"`
	ThreadID  string `json:"thread_id" jsonschema:"thread_id from event_discussions, community_discussions, profile_activity or my_followed_discussions"`
	Frequency string `json:"frequency" jsonschema:"immediate (each reply), daily (midnight UTC), weekly (Monday midnight UTC) or muted"`
}

type unfollowDiscussionInput struct {
	EventID  string `json:"event_id" jsonschema:"event ID the thread belongs to"`
	ThreadID string `json:"thread_id" jsonschema:"thread_id of a followed discussion, from my_followed_discussions"`
}

type discussionFollowState struct {
	EventID   string                    `json:"event_id"`
	ThreadID  string                    `json:"thread_id"`
	Following bool                      `json:"following"`
	Follow    *discussionfollows.Follow `json:"follow,omitempty"`
}

type notificationInput struct {
	NotificationID string `json:"notification_id" jsonschema:"id from my_notifications"`
}

type notificationState struct {
	NotificationID string `json:"notification_id"`
	Read           bool   `json:"read"`
}
