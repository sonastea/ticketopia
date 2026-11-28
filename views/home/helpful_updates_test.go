package home

import (
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discussionfollows"
)

func TestHelpfulSummaryNamesOnlyOptedInPeople(t *testing.T) {
	n := func(count int, reply bool, names ...string) discussionfollows.Notification {
		return discussionfollows.Notification{Kind: discussionfollows.KindHelpful, HelpfulCount: count, HelpfulNames: names, Reply: reply}
	}
	for _, tc := range []struct {
		n    discussionfollows.Notification
		want string
	}{
		{n(1, true), "Someone found your reply helpful"},
		{n(3, false), "3 people found your question or tip helpful"},
		{n(1, true, "Sam"), "Sam found your reply helpful"},
		{n(2, true, "Sam"), "Sam and 1 other found your reply helpful"},
		{n(4, true, "Sam"), "Sam and 3 others found your reply helpful"},
		{n(2, true, "Sam", "Ada"), "Sam and Ada found your reply helpful"},
		{n(5, true, "Sam", "Ada"), "Sam, Ada and 3 others found your reply helpful"},
		// Names never outnumber the count, even from an inconsistent read.
		{n(1, true, "Sam", "Ada"), "Sam found your reply helpful"},
	} {
		if got := helpfulSummary(tc.n); got != tc.want {
			t.Errorf("got %q want %q", got, tc.want)
		}
	}
}

func TestNotificationsInboxShowsBothKinds(t *testing.T) {
	at := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	body := renderNeighbor(t, FollowedDiscussions(DiscussionFollowPage{Notifications: true, CSRF: "c", URL: "/me/notifications",
		Inbox: discussionfollows.Page[discussionfollows.Notification]{Items: []discussionfollows.Notification{
			{ID: "h1", Kind: discussionfollows.KindHelpful, EventName: "Night show", HelpfulCount: 2, HelpfulNames: []string{"Sam"}, Reply: true, AvailableAt: at, URL: "/events/e/discussions/t?post_id=p#post-p"},
			{ID: "r1", Kind: discussionfollows.KindReplies, EventName: "Day show", ReplyCount: 3, AvailableAt: at, URL: "/events/e/discussions/t2?post_id=q#post-q"},
		}}}))
	for _, want := range []string{"<h1", "Notifications", "Sam and 1 other found your reply helpful", "Open your reply", "new replies", "Open conversation at reply", "Unread"} {
		if !strings.Contains(body, want) {
			t.Errorf("inbox lacks %q", want)
		}
	}
	if strings.Contains(body, "Reply notifications") {
		t.Error("inbox kept the replies-only title")
	}
}
