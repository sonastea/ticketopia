package home

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
)

type DiscussionView struct {
	Enabled                    bool
	ModerationEnabled          bool
	ViewerID, CSRF, Key, Error string
	List                       discussions.List
}
type DiscussionPage struct {
	Event                              models.Event
	Meta                               models.Freshness
	Root                               *discussions.Post
	ReplyTo                            *discussions.Post
	Selected                           *discussions.Post
	Follow                             *discussionfollows.Follow
	FollowEnabled, NotificationsPaused bool
	FollowError                        string
	View                               DiscussionView
	ReturnURL, URL, ParentID           string
	// LiveReplies offers this signed-in viewer live reply checks; Engaged
	// starts them at once (they follow the thread, wrote it or replied on
	// this page), otherwise they start when the viewer writes a reply.
	LiveReplies, Engaged bool
}

// liveRepliesURL is the conversation's live check, or "" when it has none.
func (p DiscussionPage) liveRepliesURL() string {
	if !p.LiveReplies || p.Root == nil || p.View.List.AsOf.IsZero() {
		return ""
	}
	return "/events/" + url.PathEscape(p.Event.ID) + "/discussions/" + url.PathEscape(p.Root.ID) + "/new-replies"
}

func (p DiscussionPage) liveSince() string {
	return p.View.List.AsOf.UTC().Format(time.RFC3339Nano)
}

type DiscussionCommunityPage struct {
	Enabled               bool
	Filters               discussions.Query
	List                  discussions.List
	Categories            []models.Category
	CategoriesUnavailable bool
	URL, Error            string
}

// OpenQuestionsPage is the "Can you help?" collection: questions and tips on
// upcoming events that nobody has answered yet.
type OpenQuestionsPage struct {
	Enabled               bool
	Filters               discussions.Query
	Mine                  bool // limited to the viewer's own events
	List                  discussions.List
	Categories            []models.Category
	CategoriesUnavailable bool
	ViewerID, CSRF        string
	URL, Error            string
}

// earlierReplies is the first reply page of a conversation opened from a
// reply (from), without that selection, or "" for any other page.
func earlierReplies(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Query().Get("from") == "" {
		return ""
	}
	values := u.Query()
	values.Del("from")
	values.Del("post_id")
	u.RawQuery, u.Fragment = values.Encode(), ""
	return u.String()
}

// answerURL opens a question's conversation at its reply composer.
func answerURL(post discussions.Post, returnURL string) string {
	return ThreadURL(post.EventID, post.ID, DiscussionOrigin(returnURL)) + "#discussion-composer"
}

// canMarkSame is whether the viewer may mark this question as theirs too.
func canMarkSame(post discussions.Post, viewer string) bool {
	return viewer != "" && post.ID == post.ThreadID && !post.Removed && !post.Hidden && post.Profile != nil && post.Profile.ID != viewer
}

func sameQuestionVerb(set bool) string {
	if set {
		return "not_same_question"
	}
	return "same_question"
}

// wantToKnow is a question's Same question count in words, "" for none.
func wantToKnow(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return "1 person wants to know"
	}
	return fmt.Sprintf("%d people want to know", n)
}

type DiscussionForm struct {
	EventID, ThreadID, ParentID, PostID, Body, Key, Action, ReturnURL, CSRF, ViewerID string
	Conflict                                                                          bool
}

func contextThreadID(p EventPage) string {
	if p.Thread != nil {
		return p.Thread.threadID()
	}
	return ""
}

// discussionLaterPage recognizes only an event's own conversation list: a
// Discover return URL can carry an unrelated results cursor.
func discussionLaterPage(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(u.Path, "/events/") || !strings.HasSuffix(u.Path, "/discussions") {
		return ""
	}
	return laterPage(raw, "cursor")
}

// first reports an event without conversations yet, so its actions invite
// the first question rather than joining an existing discussion.
func (v DiscussionView) first(returnURL string) bool {
	return v.Error == "" && len(v.List.Items) == 0 && discussionLaterPage(returnURL) == ""
}

func ThreadURL(event, thread, origin string) string {
	path := "/events/" + url.PathEscape(event) + "/discussions"
	if thread != "" {
		path += "/" + url.PathEscape(thread)
	}
	if origin != "" && origin != "/" {
		path += "?" + url.Values{"return_to": {origin}}.Encode()
	}
	return path
}

// ContributionURL selects a reply independently of the paginated reply list,
// so a private receipt links to its exact public contribution or placeholder.
func ContributionURL(event, thread, post string) string {
	u, _ := url.Parse(ThreadURL(event, thread, "/"))
	if post != thread {
		q := u.Query()
		q.Set("post_id", post)
		u.RawQuery = q.Encode()
	}
	u.Fragment = "post-" + post
	return u.String()
}
func (p DiscussionPage) selectedOffPage() bool {
	if p.Selected == nil || (p.Root != nil && p.Root.ID == p.Selected.ID) {
		return false
	}
	for _, post := range p.View.List.Items {
		if post.ID == p.Selected.ID {
			return false
		}
	}
	return true
}
func DiscussionOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "/"
	}
	if strings.HasPrefix(u.Path, "/events/") {
		if v := u.Query().Get("return_to"); v != "" {
			return v
		}
		return "/"
	}
	if u.Path != "/" {
		return u.String()
	}
	return returnOrigin(raw)
}
func replyURL(raw, id string) string {
	u, _ := url.Parse(raw)
	q := u.Query()
	q.Set("reply_to", id)
	u.RawQuery = q.Encode()
	u.Fragment = "discussion-composer"
	return u.String()
}
func (p DiscussionPage) threadID() string {
	if p.Root != nil {
		return p.Root.ID
	}
	return ""
}
func discussionForm(post discussions.Post, v DiscussionView, action, returnURL string) DiscussionForm {
	return DiscussionForm{EventID: post.EventID, ThreadID: post.ThreadID, PostID: post.ID, Body: post.Body, Action: action, ReturnURL: returnURL, CSRF: v.CSRF, ViewerID: v.ViewerID}
}

// startsThread is a new question or tip, not a reply or an edit.
func (f DiscussionForm) startsThread() bool {
	return f.Action == "create" && f.ThreadID == "" && f.PostID == ""
}

// composerStarters work for any event category: they ask about going, not
// about a particular kind of performer.
var composerStarters = []string{
	"First time at this venue. Any tips for getting in?",
	"What time should I arrive?",
	"Has anyone been to one of these before?",
}

func composerStarterExamples() string {
	quoted := make([]string, len(composerStarters))
	for i, s := range composerStarters {
		quoted[i] = "“" + s + "”"
	}
	return strings.Join(quoted, ", ")
}

func (f DiscussionForm) draftScope() string {
	return strings.Join([]string{f.ViewerID, f.EventID, f.ThreadID, f.PostID, f.ParentID}, ":")
}
func helpfulVerb(set bool) string {
	if set {
		return "unhelpful"
	}
	return "helpful"
}
