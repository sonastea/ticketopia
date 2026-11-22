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
	"github.com/sonastea/ticketopia/internal/discussions"
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
	CreateQuestion(ctx context.Context, account, eventID, body string, client drafts.Client) (drafts.Draft, error)
	CreateReply(ctx context.Context, account, eventID, threadID, parentID, body string, client drafts.Client) (drafts.Draft, error)
	List(ctx context.Context, account string) ([]drafts.Draft, error)
	Discard(ctx context.Context, account, id string) (bool, error)
}

// Helpful sets or clears the caller's Helpful mark on a post, as
// PUT/DELETE /api/v1/posts/{post_id}/reactions/helpful do. Agents get no
// edit or removal of posts: those change the person's words.
type Helpful interface {
	React(ctx context.Context, who, id string, set bool) (discussions.Post, error)
}

// SameQuestion sets or clears the caller's Same question mark on someone
// else's question or tip, as PUT/DELETE
// /api/v1/posts/{post_id}/reactions/same-question do.
type SameQuestion interface {
	SameQuestion(ctx context.Context, who, id string, set bool) (discussions.Post, error)
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

// addPublishingTools registers the Interested visibility, recommendation,
// draft and Helpful tools for the services available. Publishing tools need
// the publish scope; free text never becomes public through an agent, only as
// a draft the person reviews and publishes on the web. No tool edits or
// removes the person's posts.
func addPublishingTools(server *mcp.Server, d Deps) {
	if d.Interests != nil {
		addTool(server, d, &mcp.Tool{Name: "make_interest_public", Title: "Make Interested public", OutputSchema: outputSchema[interestState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Make the signed-in user's Interested choice for an event public, marking it Interested first if needed. " +
				"Public means their display name and profile link appear among the event's interested people and on their public profile. " +
				"Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in eventInput) (interestState, error) {
				visibility := "public"
				item, created, err := d.Interests.Set(ctx, owner, in.EventID, &visibility, "private")
				return interestState{EventID: in.EventID, Interested: true, Created: created, Item: &item}, err
			}))
		addTool(server, d, &mcp.Tool{Name: "make_interest_private", Title: "Make Interested private", OutputSchema: outputSchema[interestState](),
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
		addTool(server, d, &mcp.Tool{Name: "my_recommendations", Title: "My recommendations", OutputSchema: outputSchema[recommendations.List](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The signed-in user's public event recommendations, newest first, each with its reason and, when an app published it or wrote its reason, via (the app's name). " +
				"For the next page, pass next_cursor as cursor with the same limit."},
			privateTool(d, func(ctx context.Context, owner string, in pageInput) (recommendations.List, error) {
				return d.Recommendations.Own(ctx, owner, in.values())
			}))
		addTool(server, d, &mcp.Tool{Name: "recommend_event", Title: "Recommend event", OutputSchema: outputSchema[recommendationState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Publicly recommend an event as the signed-in user, without a reason. Their display name and profile link appear on the event, in Community and on their profile, " +
				"marked as via this app with its host. An existing recommendation is returned unchanged, reason included. " +
				"This tool never takes text: to recommend with a reason, use draft_recommendation, and the person reviews and publishes it on Ticketopia. " +
				"Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in eventInput) (recommendationState, error) {
				item, created, err := d.Recommendations.Publish(ctx, owner, in.EventID, recommendations.Write{Via: tokenExtra(req, ExtraClientName), ViaHost: tokenExtra(req, ExtraClientHost), KeepExisting: true})
				return recommendationState{EventID: in.EventID, Recommended: true, Created: created, Item: &item}, err
			}))
		addTool(server, d, &mcp.Tool{Name: "withdraw_recommendation", Title: "Withdraw recommendation", OutputSchema: outputSchema[recommendationState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Withdraw the signed-in user's public recommendation of an event, only when an app published it without a reason. " +
				"A recommendation the person published or gave a reason for is left alone: they can withdraw it on the event's page. " +
				"Withdrawing one that does not exist changes nothing. Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in eventInput) (recommendationState, error) {
				return recommendationState{EventID: in.EventID}, d.Recommendations.WithdrawForAgent(ctx, owner, in.EventID)
			}))
	}
	if d.Drafts != nil {
		addTool(server, d, &mcp.Tool{Name: "draft_recommendation", Title: "Draft recommendation", OutputSchema: outputSchema[draftResult](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Save a private draft of a recommendation reason (plain text, up to 500 characters) for the signed-in user to review. Nothing is published: " +
				"tell the person to open review_url, where they can read, publish or discard it. Publishing replaces any reason they already have for that event. " + draftNote},
			privateToolWith(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in draftInput) (draftResult, error) {
				draft, err := d.Drafts.CreateRecommendation(ctx, owner, in.EventID, in.Reason, draftClient(req))
				return d.drafted(draft), err
			}))
		if d.Discussions != nil {
			addDiscussionDraftTools(server, d)
		}
		addTool(server, d, &mcp.Tool{Name: "my_drafts", Title: "My drafts", OutputSchema: outputSchema[draftList](),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
			Description: "The signed-in user's private drafts awaiting review, newest first, from any connected app: kind is recommendation (a reason), " +
				"question (a question or tip) or reply (with its thread_id and, when it answers a reply, parent_id). Each is unpublished until the person publishes it at review_url."},
			privateTool(d, func(ctx context.Context, owner string, _ struct{}) (draftList, error) {
				items, err := d.Drafts.List(ctx, owner)
				return draftList{Items: items, ReviewURL: d.DraftsURL}, err
			}))
		addTool(server, d, &mcp.Tool{Name: "discard_draft", Title: "Discard draft", OutputSchema: outputSchema[draftState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Delete one of the signed-in user's unpublished drafts by its id from my_drafts. discarded is true when a pending draft was deleted, " +
				"and false when none had this id (already published, discarded or expired, or never a draft of this user): nothing changes then. Published recommendations and posts are not affected."},
			privateTool(d, func(ctx context.Context, owner string, in draftIDInput) (draftState, error) {
				discarded, err := d.Drafts.Discard(ctx, owner, in.DraftID)
				return draftState{DraftID: in.DraftID, Discarded: discarded}, err
			}))
	}
	if d.Helpful != nil {
		addHelpfulTools(server, d)
	}
	if d.SameQuestion != nil {
		addSameQuestionTools(server, d)
	}
}

var errQuestionNotFound = callMistake{errors.New("post_id: no visible question or tip with this id; use a question or tip id (not a reply's) from open_questions, " +
	"event_discussions or community_discussions (removed or hidden posts can't be marked)")}

var errQuestionGone = callMistake{errors.New("post_id: no question or tip with this id; use a question or tip id (not a reply's) from open_questions, " +
	"event_discussions or community_discussions")}

// addSameQuestionTools registers Same question: like Helpful, a public signal
// without words, so it is a publish action rather than a draft.
func addSameQuestionTools(server *mcp.Server, d Deps) {
	addTool(server, d, &mcp.Tool{Name: "mark_same_question", Title: "Mark Same question", OutputSchema: outputSchema[sameQuestionState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Mark someone else's question or tip as the signed-in user's question too, by its id (post_id) from open_questions, event_discussions or community_discussions. " +
			"It is public: it adds to the post's same_question_count that everyone sees (\"N people want to know\"), though nobody sees who marked it. " +
			"It never changes any list's order. Replies and the user's own posts can't be marked. Marking again changes nothing. " +
			"Needs the connection's permission to post publicly. " + publicNote},
		publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in questionInput) (sameQuestionState, error) {
			return d.sameQuestion(ctx, owner, in.PostID, true)
		}))
	addTool(server, d, &mcp.Tool{Name: "unmark_same_question", Title: "Remove Same question", OutputSchema: outputSchema[sameQuestionState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Remove the signed-in user's Same question mark from a question or tip, by its id (post_id). Its public same_question_count goes down by one. " +
			"Removing a mark that isn't there changes nothing. Needs the connection's permission to post publicly. " + publicNote},
		publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in questionInput) (sameQuestionState, error) {
			return d.sameQuestion(ctx, owner, in.PostID, false)
		}))
}

func (d Deps) sameQuestion(ctx context.Context, owner, id string, set bool) (sameQuestionState, error) {
	post, err := d.SameQuestion.SameQuestion(ctx, owner, id, set)
	if errors.Is(err, discussions.ErrNotFound) {
		if !set {
			return sameQuestionState{}, errQuestionGone
		}
		return sameQuestionState{}, errQuestionNotFound
	}
	if errors.Is(err, discussions.ErrOwnQuestion) {
		return sameQuestionState{}, callMistake{errors.New("post_id: mark Same question on someone else's question or tip; this one is the user's own")}
	}
	if err != nil {
		return sameQuestionState{}, err
	}
	return sameQuestionState{PostID: id, SameQuestion: post.SameQuestion, Post: &post}, nil
}

// draftNote ends every drafting tool's description.
var draftNote = fmt.Sprintf("Drafts expire after seven days, or when the user disconnects this app; an account holds at most %d.", drafts.MaxPending)

// addDiscussionDraftTools registers drafting questions, tips and replies.
// Agents never post words: each is a private draft the person reads and
// publishes on the web, attributed to the app that wrote it.
func addDiscussionDraftTools(server *mcp.Server, d Deps) {
	addTool(server, d, &mcp.Tool{Name: "draft_question", Title: "Draft question or tip", OutputSchema: outputSchema[draftResult](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Save a private draft of a new question or tip on an event (plain text, up to 4000 characters) for the signed-in user to review. Nothing is published: " +
			"tell the person to open review_url, where they can read, publish or discard it. Published, it appears in the event's questions and tips under their name, " +
			"marked as via this app. To answer an existing question, use draft_reply. " + draftNote},
		privateToolWith(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in questionDraftInput) (draftResult, error) {
			draft, err := d.Drafts.CreateQuestion(ctx, owner, in.EventID, in.Body, draftClient(req))
			return d.drafted(draft), err
		}))
	addTool(server, d, &mcp.Tool{Name: "draft_reply", Title: "Draft reply", OutputSchema: outputSchema[draftResult](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Save a private draft of a reply in a question or tip's conversation (plain text, up to 4000 characters) for the signed-in user to review. Nothing is published: " +
			"tell the person to open review_url, where they see what it answers and can publish or discard it. Use the event_id and thread_id from event_discussions, " +
			"community_discussions or discussion_thread; to answer a particular reply, pass its id as parent_id, otherwise the reply answers the question or tip. " +
			"Published, it appears in the conversation under their name, marked as via this app, and notifies the conversation's followers. " + draftNote},
		privateToolWith(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in replyDraftInput) (draftResult, error) {
			if err := threadTarget(in.ThreadID, nil); err != nil {
				return draftResult{}, err
			}
			draft, err := d.Drafts.CreateReply(ctx, owner, in.EventID, in.ThreadID, in.ParentID, in.Body, draftClient(req))
			if errors.Is(err, drafts.ErrParentNotFound) {
				return draftResult{}, errParentNotFound
			}
			if err != nil {
				return draftResult{}, threadTarget(in.ThreadID, err)
			}
			return d.drafted(draft), nil
		}))
}

var errParentNotFound = callMistake{errors.New("parent_id: no post with this id in that conversation; use a reply's id from discussion_thread, " +
	"or omit parent_id to answer the question or tip itself")}

var errPostNotFound = callMistake{errors.New("post_id: no visible question, tip or reply with this id; use an id from event_discussions, " +
	"discussion_thread or community_discussions (removed or hidden posts can't be marked Helpful)")}

// addHelpfulTools registers Helpful, a public signal without words, so it
// is a publish action rather than a draft.
func addHelpfulTools(server *mcp.Server, d Deps) {
	addTool(server, d, &mcp.Tool{Name: "mark_helpful", Title: "Mark Helpful", OutputSchema: outputSchema[helpfulState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Mark a question, tip or reply Helpful as the signed-in user, by its id (post_id) from event_discussions, discussion_thread or community_discussions. " +
			"Helpful is public: it adds to the post's helpful_count that everyone sees, though nobody sees who marked it. Marking again changes nothing. " +
			"Needs the connection's permission to post publicly. " + publicNote},
		publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in postInput) (helpfulState, error) {
			return d.react(ctx, owner, in.PostID, true)
		}))
	addTool(server, d, &mcp.Tool{Name: "unmark_helpful", Title: "Remove Helpful", OutputSchema: outputSchema[helpfulState](),
		Annotations: &mcp.ToolAnnotations{DestructiveHint: &yes, IdempotentHint: true, OpenWorldHint: &no},
		Description: "Remove the signed-in user's Helpful mark from a question, tip or reply, by its id (post_id). Its public helpful_count goes down by one. " +
			"Removing a mark that isn't there changes nothing. Needs the connection's permission to post publicly. " + publicNote},
		publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in postInput) (helpfulState, error) {
			return d.react(ctx, owner, in.PostID, false)
		}))
}

func (d Deps) react(ctx context.Context, owner, id string, set bool) (helpfulState, error) {
	post, err := d.Helpful.React(ctx, owner, id, set)
	if errors.Is(err, discussions.ErrNotFound) {
		return helpfulState{}, errPostNotFound
	}
	if err != nil {
		return helpfulState{}, err
	}
	return helpfulState{PostID: id, Helpful: post.Helpful, Post: &post}, nil
}

// draftClient identifies the app writing a draft from the verified
// connection, never from tool input.
func draftClient(req *mcp.CallToolRequest) drafts.Client {
	return drafts.Client{Name: tokenExtra(req, ExtraClientName), Host: tokenExtra(req, ExtraClientHost), GrantID: tokenExtra(req, ExtraGrantID)}
}

// drafted is a drafting tool's result: never published.
func (d Deps) drafted(draft drafts.Draft) draftResult {
	return draftResult{Draft: draft, Published: false, ReviewURL: d.DraftsURL,
		Next: "Not published. Ask the person to open review_url to read and publish it."}
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
	var mistake callMistake
	if errors.As(err, &mistake) {
		return mistake.Error()
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

type questionDraftInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events, for example ticketmaster:G5v..."`
	Body    string `json:"body" jsonschema:"the question or tip for the person to review, plain text up to 4000 characters"`
}

type replyDraftInput struct {
	EventID  string `json:"event_id" jsonschema:"event ID the conversation belongs to"`
	ThreadID string `json:"thread_id" jsonschema:"thread_id of the question or tip, from event_discussions, community_discussions or discussion_thread"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"id of the reply this answers, from discussion_thread; omit to answer the question or tip"`
	Body     string `json:"body" jsonschema:"the reply for the person to review, plain text up to 4000 characters"`
}

type postInput struct {
	PostID string `json:"post_id" jsonschema:"id of a question, tip or reply from event_discussions, discussion_thread or community_discussions"`
}

// helpfulState is the caller's resulting Helpful choice and the post with
// its public helpful_count.
type helpfulState struct {
	PostID  string            `json:"post_id"`
	Helpful bool              `json:"helpful"`
	Post    *discussions.Post `json:"post,omitempty"`
}

// questionInput names a question or tip; replies can't take Same question.
type questionInput struct {
	PostID string `json:"post_id" jsonschema:"id of a question or tip (not a reply) from open_questions, event_discussions or community_discussions"`
}

// sameQuestionState is the caller's resulting Same question mark and the
// post with its public count.
type sameQuestionState struct {
	PostID       string            `json:"post_id"`
	SameQuestion bool              `json:"same_question"`
	Post         *discussions.Post `json:"post,omitempty"`
}

type draftIDInput struct {
	DraftID string `json:"draft_id" jsonschema:"a draft's id from my_drafts, draft_recommendation, draft_question or draft_reply (not a post or event id)"`
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
	Discarded bool   `json:"discarded" jsonschema:"true when a pending draft was deleted; false when there was none with this id"`
}
