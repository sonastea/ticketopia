package api

import (
	"context"
	"errors"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

func WithUserActivity(repository useractivity.Repository) Option {
	return func(a *api) { a.userActivity = useractivity.New(repository) }
}

func (a *api) communityActivityRoutes(e *echo.Echo) {
	e.GET("/api/v1/events/:event_id/community", a.eventCommunityHandler)
	e.GET("/api/v1/users/:user_id/activity", a.userActivityHandler)
}

func (a *api) eventCommunityEnabled() bool {
	return a.enabled() && a.saved != nil && a.interests != nil && a.recommendations != nil && a.eventActivity != nil
}

func (a *api) userActivityEnabled() bool { return a.enabled() && a.userActivity != nil }

// eventCommunity reads one event's public totals in two local queries and,
// when owner is set, that account's own save, interest and recommendation.
// Unknown events report zero totals: no provider lookup is made. Interest
// totals include private choices without identities, as on /interest. The
// HTTP route and the agents' event_community tool share it.
func (a *api) eventCommunity(ctx context.Context, owner, id string) (eventactivity.Community, error) {
	if !a.eventCommunityEnabled() {
		return eventactivity.Community{}, eventactivity.ErrUnavailable
	}
	if err := saved.ValidateID(id); err != nil {
		return eventactivity.Community{}, err
	}
	previews, err := a.eventActivity.Previews(ctx, []string{id})
	if err != nil {
		return eventactivity.Community{}, err
	}
	states, err := a.interests.States(ctx, owner, []string{id})
	if err != nil {
		return eventactivity.Community{}, err
	}
	interest := states[id]
	result := eventactivity.Community{EventID: id, Counts: eventactivity.CommunityCounts{
		Interested:      interest.Count,
		Recommendations: previews[id].Recommendations,
		Discussions:     previews[id].Questions,
	}}
	if owner == "" {
		return result, nil
	}
	saves, err := a.saved.States(ctx, owner, []string{id})
	if err != nil {
		return eventactivity.Community{}, err
	}
	_, err = a.recommendations.Get(ctx, owner, id)
	if err != nil && !errors.Is(err, recommendations.ErrNotFound) {
		return eventactivity.Community{}, err
	}
	result.Viewer = &eventactivity.CommunityViewer{
		Saved:              saves[id],
		Interested:         interest.Interested,
		InterestVisibility: interest.Visibility,
		Recommended:        err == nil,
	}
	return result, nil
}

// profileActivity lists one person's public contributions. The query is
// validated before the profile lookup, so malformed requests are rejected
// without a database read. Shared by the HTTP route and the agents'
// profile_activity tool.
func (a *api) profileActivity(ctx context.Context, user string, values url.Values) (useractivity.List, error) {
	if !a.userActivityEnabled() {
		return useractivity.List{}, useractivity.ErrUnavailable
	}
	if _, err := useractivity.ParseQuery(values, "user:"+user); err != nil {
		return useractivity.List{}, err
	}
	profile, err := a.accounts.Public(ctx, user)
	if err != nil {
		return useractivity.List{}, err
	}
	return a.userActivity.List(ctx, profile.ID, values)
}

// communityReads gives agent tools the same reads as the HTTP routes.
type communityReads struct{ a *api }

func (r communityReads) EventCommunity(ctx context.Context, viewer, id string) (eventactivity.Community, error) {
	return r.a.eventCommunity(ctx, viewer, id)
}

func (r communityReads) ProfileActivity(ctx context.Context, user string, values url.Values) (useractivity.List, error) {
	return r.a.profileActivity(ctx, user, values)
}

func (a *api) eventCommunityHandler(c echo.Context) error {
	privateAccountResponse(c) // Viewer state and visibility changes must not be cached.
	if !a.eventCommunityEnabled() {
		return accountProblem(c, eventactivity.ErrUnavailable)
	}
	if len(c.QueryParams()) != 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"})
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	// Anonymous reads are allowed, but a credential that was sent and failed is
	// reported rather than silently dropping the viewer's state.
	owner := ""
	p, err := a.principal(c, false)
	switch {
	case err == nil:
		owner = p.Account.ID
	case errors.Is(err, accounts.ErrUnauthenticated) && c.Request().Header.Get("Authorization") == "":
	default:
		return accountProblem(c, err)
	}
	result, err := a.eventCommunity(c.Request().Context(), owner, id)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, result)
}

// userActivityHandler's response is identical for every viewer, but stays
// private so a visibility change or removal is never served from a shared cache.
func (a *api) userActivityHandler(c echo.Context) error {
	privateAccountResponse(c)
	list, err := a.profileActivity(c.Request().Context(), c.Param("user_id"), c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
