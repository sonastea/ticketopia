package home

import "github.com/sonastea/ticketopia/internal/discussionfollows"

type DiscussionFollowPage struct {
	CSRF, URL, Error      string
	Notifications, Paused bool
	Follows               discussionfollows.Page[discussionfollows.Follow]
	Inbox                 discussionfollows.Page[discussionfollows.Notification]
}
