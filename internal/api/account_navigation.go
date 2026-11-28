package api

import (
	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/views/layouts"
)

// Navigation is presentation only; destination handlers still enforce permissions.
// A failed role lookup must never advertise moderator tools or block other tasks.
func (a *api) accountNavigation(c echo.Context, viewerID string) layouts.AccountNavPermissions {
	p := layouts.AccountNavPermissions{ModerationEnabled: a.moderationEnabled()}
	if p.ModerationEnabled && viewerID != "" {
		moderator, err := a.moderation.IsModerator(c.Request().Context(), viewerID)
		p.Moderator = err == nil && moderator
		// Like the drafts count, a failed count only hides the badge.
		if p.Moderator {
			if n, ok, err := a.moderation.PendingCases(c.Request().Context(), viewerID); err == nil && ok {
				p.PendingCases = n
			}
		}
	}
	// Like the moderator lookup, a failed count only hides the badge.
	if p.Drafts = a.draftsEnabled(); p.Drafts && viewerID != "" {
		if count, err := a.drafts.Count(c.Request().Context(), viewerID); err == nil {
			p.PendingDrafts = count
		}
	}
	return p
}
