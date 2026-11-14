package appmcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"golang.org/x/time/rate"
)

// Recommendations are the caller's public event recommendations.
type Recommendations interface {
	Publish(ctx context.Context, owner, id string, w recommendations.Write) (recommendations.Item, bool, error)
	WithdrawForAgent(ctx context.Context, owner, id string) error
	Own(ctx context.Context, owner string, values url.Values) (recommendations.List, error)
}

// Drafts are the caller's private drafts awaiting review on the web.
type Drafts interface {
	CreateRecommendation(ctx context.Context, account, eventID, text string, client drafts.Client) (drafts.Draft, error)
	List(ctx context.Context, account string) ([]drafts.Draft, error)
	Discard(ctx context.Context, account, id string) error
}

// Token info extras the HTTP boundary sets for OAuth connections: the app's
// self-declared name and verified metadata host, for attribution and logs,
// and the connection's ID, which owns its drafts. API tokens carry none.
const (
	ExtraClientName = "client_name"
	ExtraClientHost = "client_host"
	ExtraGrantID    = "grant_id"
)

func tokenExtra(req *mcp.CallToolRequest, key string) string {
	if req.Extra == nil || req.Extra.TokenInfo == nil {
		return ""
	}
	value, _ := req.Extra.TokenInfo.Extra[key].(string)
	return value
}

// PublishesPerHour bounds publishing tool calls per account per replica.
const PublishesPerHour = 20

// PublishLimiter is a per-replica token bucket per account: PublishesPerHour
// publishing calls an hour, refilling steadily. Accounts idle for an hour
// hold a full bucket and are swept.
type PublishLimiter struct {
	mu       sync.Mutex
	perHour  int
	now      func() time.Time
	accounts map[string]*publisher
	swept    time.Time
}

type publisher struct {
	limiter *rate.Limiter
	seen    time.Time
}

func NewPublishLimiter(perHour int, now func() time.Time) *PublishLimiter {
	if now == nil {
		now = time.Now
	}
	return &PublishLimiter{perHour: perHour, now: now, accounts: map[string]*publisher{}}
}

// Allow takes one publish from account's allowance, or reports how long to wait.
func (l *PublishLimiter) Allow(account string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.swept) > time.Hour {
		for key, p := range l.accounts {
			if now.Sub(p.seen) > time.Hour {
				delete(l.accounts, key)
			}
		}
		l.swept = now
	}
	p, ok := l.accounts[account]
	if !ok {
		p = &publisher{limiter: rate.NewLimiter(rate.Limit(float64(l.perHour)/3600), l.perHour)}
		l.accounts[account] = p
	}
	p.seen = now
	reservation := p.limiter.ReserveN(now, 1)
	if wait := reservation.DelayFrom(now); wait > 0 {
		reservation.CancelAt(now)
		return false, wait
	}
	return true, 0
}

const publicNote = "Only use this when the person asked for it in this conversation."

// addPublishingTools registers the Interested visibility, recommendation and
// draft tools for the services available. Publishing tools need the publish
// scope; free text never becomes public through an agent, only as a draft the
// person reviews and publishes on the web.
func addPublishingTools(server *mcp.Server, d Deps) {
	if d.Interests != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "make_interest_public", Title: "Make Interested public", OutputSchema: outputSchema[interestState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Make the signed-in user's Interested choice for an event public, marking it Interested first if needed. " +
				"Public means their display name and profile link appear among the event's interested people and on their public profile. " +
				"Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in eventInput) (interestState, error) {
				visibility := "public"
				item, created, err := d.Interests.Set(ctx, owner, in.EventID, &visibility, "private")
				return interestState{EventID: in.EventID, Interested: true, Created: created, Item: &item}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "make_interest_private", Title: "Make Interested private", OutputSchema: outputSchema[interestState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Make the signed-in user's Interested choice for an event private, marking it Interested first if needed. " +
				"A private choice counts only toward the event's anonymous total; their name leaves the event and their profile."},
			privateTool(d, func(ctx context.Context, owner string, in eventInput) (interestState, error) {
				visibility := "private"
				item, created, err := d.Interests.Set(ctx, owner, in.EventID, &visibility, "private")
				return interestState{EventID: in.EventID, Interested: true, Created: created, Item: &item}, err
			}))
	}
	if d.Recommendations != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "my_recommendations", Title: "My recommendations", OutputSchema: outputSchema[recommendations.List](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The signed-in user's public event recommendations, newest first, each with its reason and, when an app published it or wrote its reason, via (the app's name). " +
				"For the next page, pass next_cursor as cursor with the same limit."},
			privateTool(d, func(ctx context.Context, owner string, in pageInput) (recommendations.List, error) {
				return d.Recommendations.Own(ctx, owner, in.values())
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "recommend_event", Title: "Recommend event", OutputSchema: outputSchema[recommendationState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Publicly recommend an event as the signed-in user, without a reason. Their display name and profile link appear on the event, in Community and on their profile, " +
				"marked as via this app with its host. An existing recommendation is returned unchanged, reason included. " +
				"This tool never takes text: to recommend with a reason, use draft_recommendation, and the person reviews and publishes it on Ticketopia. " +
				"Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in eventInput) (recommendationState, error) {
				item, created, err := d.Recommendations.Publish(ctx, owner, in.EventID, recommendations.Write{Via: tokenExtra(req, ExtraClientName), ViaHost: tokenExtra(req, ExtraClientHost), KeepExisting: true})
				return recommendationState{EventID: in.EventID, Recommended: true, Created: created, Item: &item}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "withdraw_recommendation", Title: "Withdraw recommendation", OutputSchema: outputSchema[recommendationState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Withdraw the signed-in user's public recommendation of an event, only when an app published it without a reason. " +
				"A recommendation the person published or gave a reason for is left alone: they can withdraw it on the event's page. " +
				"Withdrawing one that does not exist changes nothing. Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in eventInput) (recommendationState, error) {
				return recommendationState{EventID: in.EventID}, d.Recommendations.WithdrawForAgent(ctx, owner, in.EventID)
			}))
	}
	if d.Drafts != nil {
		mcp.AddTool(server, &mcp.Tool{Name: "draft_recommendation", Title: "Draft recommendation", OutputSchema: outputSchema[draftResult](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Save a private draft of a recommendation reason (plain text, up to 500 characters) for the signed-in user to review. Nothing is published: " +
				"tell the person to open review_url, where they can read, publish or discard it. Publishing replaces any reason they already have for that event. " +
				fmt.Sprintf("Drafts expire after seven days, or when the user disconnects this app; an account holds at most %d.", drafts.MaxPending)},
			privateToolWith(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in draftInput) (draftResult, error) {
				draft, err := d.Drafts.CreateRecommendation(ctx, owner, in.EventID, in.Reason,
					drafts.Client{Name: tokenExtra(req, ExtraClientName), Host: tokenExtra(req, ExtraClientHost), GrantID: tokenExtra(req, ExtraGrantID)})
				return draftResult{Draft: draft, Published: false, ReviewURL: d.DraftsURL,
					Next: "Not published. Ask the person to open review_url to read and publish it."}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "my_drafts", Title: "My drafts", OutputSchema: outputSchema[draftList](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The signed-in user's private drafts awaiting review, newest first, from any connected app. Each is unpublished until the person publishes it at review_url."},
			privateTool(d, func(ctx context.Context, owner string, _ struct{}) (draftList, error) {
				items, err := d.Drafts.List(ctx, owner)
				return draftList{Items: items, ReviewURL: d.DraftsURL}, err
			}))
		mcp.AddTool(server, &mcp.Tool{Name: "discard_draft", Title: "Discard draft", OutputSchema: outputSchema[draftState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Delete one of the signed-in user's unpublished drafts by its id from my_drafts. Discarding one that does not exist changes nothing; published recommendations are not affected."},
			privateTool(d, func(ctx context.Context, owner string, in draftIDInput) (draftState, error) {
				return draftState{DraftID: in.DraftID, Discarded: true}, d.Drafts.Discard(ctx, owner, in.DraftID)
			}))
	}
}

// publishTool is a signed-in tool that acts publicly: after privateTool's
// sign-in and publish scope checks, it applies the per-account publish limit
// and logs each success with only the tool and the app's host.
func publishTool[In, Out any](d Deps, call func(ctx context.Context, owner string, req *mcp.CallToolRequest, in In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return privateToolWith(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in In) (Out, error) {
		var zero Out
		if d.PublishLimit != nil {
			if ok, wait := d.PublishLimit.Allow(owner); !ok {
				return zero, errPublishLimit{wait}
			}
		}
		out, err := call(ctx, owner, req, in)
		if err == nil {
			d.Logger.Info().Str("tool", req.Params.Name).Str("client_host", tokenExtra(req, ExtraClientHost)).Msg("Agent published")
		}
		return out, err
	})
}

// errPublishLimit is worded for the agent, not the account API.
type errPublishLimit struct{ wait time.Duration }

func (e errPublishLimit) Error() string {
	return fmt.Sprintf("this account has reached its limit of %d public actions an hour through agents. Retry after %d seconds.",
		PublishesPerHour, max(1, int(math.Ceil(e.wait.Seconds()))))
}

func (d Deps) describePrivate(err error) string {
	var limit errPublishLimit
	if errors.As(err, &limit) {
		return limit.Error()
	}
	if errors.Is(err, recommendations.ErrPersonal) {
		return "the person wrote or published this recommendation themselves, so only they can withdraw it, on the event's page"
	}
	if errors.Is(err, drafts.ErrLimit) {
		return fmt.Sprintf("this account already has %d drafts awaiting review. Ask the person to publish or discard some at %s, or discard one with discard_draft.", drafts.MaxPending, d.DraftsURL)
	}
	if d.DescribePrivate != nil {
		return d.DescribePrivate(err)
	}
	return err.Error()
}

type draftInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events, for example ticketmaster:G5v..."`
	Reason  string `json:"reason" jsonschema:"the recommendation reason for the person to review, plain text up to 500 characters"`
}

type draftIDInput struct {
	DraftID string `json:"draft_id" jsonschema:"id from my_drafts or draft_recommendation"`
}

type recommendationState struct {
	EventID     string                `json:"event_id"`
	Recommended bool                  `json:"recommended"`
	Created     bool                  `json:"created"`
	Item        *recommendations.Item `json:"item,omitempty"`
}

type draftResult struct {
	Draft     drafts.Draft `json:"draft"`
	Published bool         `json:"published" jsonschema:"always false: the person publishes drafts on the web"`
	ReviewURL string       `json:"review_url" jsonschema:"where the person reviews, publishes or discards their drafts"`
	Next      string       `json:"next"`
}

type draftList struct {
	Items     []drafts.Draft `json:"items"`
	ReviewURL string         `json:"review_url"`
}

type draftState struct {
	DraftID   string `json:"draft_id"`
	Discarded bool   `json:"discarded"`
}
