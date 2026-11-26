package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

// Agents and API tokens holding publish act publicly in the person's name
// through these, from /api/v1 and /mcp alike. Everything they publish carries
// via (the app's name, or "an API client"), which pages show beside an Agent
// label. They never change or remove the person's own words: a recommendation
// the person wrote stays as it is, and posts are edited or removed only on
// the web. Each action takes one of the account's PublishesPerHour.

// errPublishLimited is an account out of public actions for the hour.
type errPublishLimited struct{ wait time.Duration }

func (e errPublishLimited) Error() string {
	return fmt.Sprintf("this account has reached its limit of %d public actions an hour through agents and API clients; retry after %d seconds",
		appmcp.PublishesPerHour, e.retryAfter())
}

func (e errPublishLimited) retryAfter() int { return max(1, int(math.Ceil(e.wait.Seconds()))) }

// allowPublish takes one public action from the account's hourly allowance,
// shared by /api/v1 and /mcp.
func (a *api) allowPublish(owner string) error {
	if a.publishLimit == nil {
		return nil
	}
	if ok, wait := a.publishLimit.Allow(owner); !ok {
		return errPublishLimited{wait}
	}
	return nil
}

// agentVia names who published: the connected app, or an API client for a
// personal token, which identifies no app.
func agentVia(client drafts.Client) (string, string) {
	if client.Name == "" {
		return drafts.APIClientName, ""
	}
	return client.Name, client.Host
}

// agentPost posts a question, tip or reply directly, with the discussion
// routes' rate limit and the retry key the caller chose.
func (a *api) agentPost(ctx context.Context, owner, event, thread, parent, body, key string, client drafts.Client) (discussions.Post, bool, error) {
	if !a.discussionsEnabled() {
		return discussions.Post{}, false, discussions.ErrUnavailable
	}
	// Invalid text never counts toward the posting limit.
	if _, err := discussions.Body(body); err != nil {
		return discussions.Post{}, false, err
	}
	if err := a.accounts.Allow(ctx, "discussion-create", owner); err != nil {
		return discussions.Post{}, false, err
	}
	via, host := agentVia(client)
	return a.discussions.Publish(ctx, owner, event, thread, parent, body, key, discussions.Attribution{Via: via, ViaHost: host})
}

// agentRecommend recommends an event, with an optional reason. An existing
// recommendation is returned unchanged, reason included.
func (a *api) agentRecommend(ctx context.Context, owner, event, reason string, client drafts.Client) (recommendations.Item, bool, error) {
	if a.recommendations == nil {
		return recommendations.Item{}, false, recommendations.ErrUnavailable
	}
	via, host := agentVia(client)
	return a.recommendations.Publish(ctx, owner, event, recommendations.Write{Reason: reason, Via: via, ViaHost: host, KeepExisting: true})
}

// agentPublishDraft publishes one of the account's drafts as the person would
// on /me/drafts, attributed to the app that wrote it and consuming it in the
// same transaction. A recommendation draft never replaces a reason the person
// wrote themselves (ErrPersonal).
func (a *api) agentPublishDraft(ctx context.Context, owner, id string) (appmcp.PublishedDraft, error) {
	if !a.draftsEnabled() {
		return appmcp.PublishedDraft{}, drafts.ErrUnavailable
	}
	draft, err := a.drafts.Get(ctx, owner, id)
	if err != nil {
		return appmcp.PublishedDraft{}, err
	}
	if draft.Discussion() {
		if !a.discussionsEnabled() {
			return appmcp.PublishedDraft{}, discussions.ErrUnavailable
		}
		if err := a.accounts.Allow(ctx, "discussion-create", owner); err != nil {
			return appmcp.PublishedDraft{}, err
		}
		post, _, err := a.discussions.Publish(ctx, owner, draft.EventID, draft.ThreadID, draft.ParentID, draft.Text, draft.PublishKey(),
			discussions.Attribution{Via: draft.ClientName, ViaHost: draft.ClientHost, Draft: draft.ID})
		if err != nil {
			return appmcp.PublishedDraft{}, err
		}
		return appmcp.PublishedDraft{Draft: draft, Published: true, Post: &post}, nil
	}
	current, err := a.recommendations.Get(ctx, owner, draft.EventID)
	active := err == nil
	if err != nil && !errors.Is(err, recommendations.ErrNotFound) {
		return appmcp.PublishedDraft{}, err
	}
	if active && current.Via == "" && current.Reason != "" {
		return appmcp.PublishedDraft{}, recommendations.ErrPersonal
	}
	item, _, err := a.recommendations.Publish(ctx, owner, draft.EventID, recommendations.Write{Reason: draft.Text, Via: draft.ClientName, ViaHost: draft.ClientHost,
		Expect: &recommendations.Current{Active: active, ReasonHash: recommendations.ReasonHash(current.Reason)}, Draft: draft.ID, KeepPersonal: true})
	if err != nil {
		return appmcp.PublishedDraft{}, err
	}
	return appmcp.PublishedDraft{Draft: draft, Published: true, Recommendation: &item}, nil
}

// agentPublishing serves the MCP publishing tools from the same code.
type agentPublishing struct{ a *api }

func (p agentPublishing) Post(ctx context.Context, owner, event, thread, parent, body, key string, client drafts.Client) (discussions.Post, bool, error) {
	return p.a.agentPost(ctx, owner, event, thread, parent, body, key, client)
}

func (p agentPublishing) PublishDraft(ctx context.Context, owner, id string) (appmcp.PublishedDraft, error) {
	return p.a.agentPublishDraft(ctx, owner, id)
}
