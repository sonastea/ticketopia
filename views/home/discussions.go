package home

import (
	"net/url"
	"strings"

	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
)

type DiscussionView struct {
	Enabled                    bool
	ViewerID, CSRF, Key, Error string
	List                       discussions.List
}
type DiscussionPage struct {
	Event                    models.Event
	Root                     *discussions.Post
	ReplyTo                  *discussions.Post
	View                     DiscussionView
	ReturnURL, URL, ParentID string
}
type DiscussionCommunityPage struct {
	Enabled               bool
	Filters               discussions.Query
	List                  discussions.List
	Categories            []models.Category
	CategoriesUnavailable bool
	URL, Error            string
}
type DiscussionForm struct{ EventID, ThreadID, ParentID, PostID, Body, Key, Action, ReturnURL, CSRF, ViewerID string }

func (p DiscussionCommunityPage) hasCategory() bool {
	for _, c := range p.Categories {
		if c.ID == p.Filters.CategoryID {
			return true
		}
	}
	return false
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
func (f DiscussionForm) draftScope() string {
	return strings.Join([]string{f.ViewerID, f.EventID, f.ThreadID, f.PostID, f.ParentID}, ":")
}
func helpfulVerb(set bool) string {
	if set {
		return "unhelpful"
	}
	return "helpful"
}
