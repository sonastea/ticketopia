package api

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/moderation"
)

func TestNavigationModeratorLookupFailsClosed(t *testing.T) {
	a, _, _, _, _, _ := discussionAPI(t)
	repo := &moderationStub{allowed: true, err: errors.New("role storage unavailable")}
	a.moderation = moderation.New(repo)
	c := echo.New().NewContext(httptest.NewRequest("GET", "/follows", nil), httptest.NewRecorder())
	viewer := accounts.ID()
	if p := a.accountNavigation(c, viewer); !p.ModerationEnabled || p.Moderator {
		t.Fatal("failed role lookup advertised tools")
	}
	repo.err = nil
	if p := a.accountNavigation(c, ""); p.Moderator {
		t.Fatal("guest advertised tools")
	}
	if p := a.accountNavigation(c, viewer); !p.Moderator {
		t.Fatal("authorized viewer lost tools")
	}
}
