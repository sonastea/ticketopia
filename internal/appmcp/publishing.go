package appmcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// AgentPosts publishes in the person's name for connections holding
// publish: questions, tips and replies posted directly, and the account's
// drafts. The HTTP API shares the same methods.
type AgentPosts interface {
	Post(ctx context.Context, owner, event, thread, parent, body, key string, client drafts.Client) (discussions.Post, bool, error)
	PublishDraft(ctx context.Context, owner, id string) (PublishedDraft, error)
}

// PublishedDraft is a draft an agent or API client published, and what it became.
type PublishedDraft struct {
	Draft          drafts.Draft          `json:"draft"`
	Published      bool                  `json:"published"`
	Recommendation *recommendations.Item `json:"recommendation,omitempty"`
	Post           *discussions.Post     `json:"post,omitempty"`
}

// agentClient is the app behind a call; for a personal API token, which
// identifies no app, publishing names it an API client.
func agentClient(req *mcp.CallToolRequest) drafts.Client { return draftClient(req) }

// agentVia is the attribution of an agent's public action.
func agentVia(req *mcp.CallToolRequest) (string, string) {
	if name := tokenExtra(req, ExtraClientName); name != "" {
		return name, tokenExtra(req, ExtraClientHost)
	}
	return drafts.APIClientName, ""
}

// postKey makes posting retry-safe: the same text in the same place is the
// same post, as a browser's retry key would make it. It hashes the text as
// it is stored, so whitespace-only differences are retries too. The reserved
// prefix keeps it apart from keys callers choose.
func postKey(event, thread, parent, body string) string {
	if stored, err := discussions.Body(body); err == nil {
		body = stored
	}
	sum := sha256.Sum256([]byte(event + "\x00" + thread + "\x00" + parent + "\x00" + body))
	return discussions.ReservedKeyPrefix + hex.EncodeToString(sum[:20])
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

// PublishesPerHour bounds public actions per account per replica, by agents
// and API clients together.
const PublishesPerHour = 20

// PublishLimiter allows each account at most perHour public actions in any
// sliding hour, per replica: a hard cap, not a refilling bucket. Accounts
// with nothing in the last hour are swept.
type PublishLimiter struct {
	mu       sync.Mutex
	perHour  int
	now      func() time.Time
	accounts map[string][]time.Time
	swept    time.Time
}

func NewPublishLimiter(perHour int, now func() time.Time) *PublishLimiter {
	if now == nil {
		now = time.Now
	}
	return &PublishLimiter{perHour: perHour, now: now, accounts: map[string][]time.Time{}}
}

// Refund returns the most recent action Allow took for account, for an
// action that then failed: only public actions that happened count.
func (l *PublishLimiter) Refund(account string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if times := l.accounts[account]; len(times) > 0 {
		l.accounts[account] = times[:len(times)-1]
	}
}

// Allow takes one public action from account's allowance, or reports how
// long until the oldest action in the last hour ages out.
func (l *PublishLimiter) Allow(account string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	since := now.Add(-time.Hour)
	if now.Sub(l.swept) > time.Hour {
		for key, times := range l.accounts {
			if len(times) == 0 || !times[len(times)-1].After(since) {
				delete(l.accounts, key)
			}
		}
		l.swept = now
	}
	times := l.accounts[account]
	for len(times) > 0 && !times[0].After(since) {
		times = times[1:]
	}
	if len(times) >= l.perHour {
		l.accounts[account] = times
		return false, times[0].Sub(since)
	}
	l.accounts[account] = append(times, now)
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
			Description: "Publicly recommend an event as the signed-in user, optionally with a reason (plain text, up to 500 characters). Their display name and profile link appear on the event, in Community and on their profile, " +
				"labelled Agent and via this app. An existing recommendation is returned unchanged, reason included: this never replaces the person's words. " +
				"To let the person review a reason first, use draft_recommendation instead. Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in recommendInput) (recommendationState, error) {
				via, host := agentVia(req)
				item, created, err := d.Recommendations.Publish(ctx, owner, in.EventID, recommendations.Write{Reason: in.Reason, Via: via, ViaHost: host, KeepExisting: true})
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
	if d.AgentPosts != nil && d.Drafts != nil {
		addTool(server, d, &mcp.Tool{Name: "publish_draft", Title: "Publish draft", OutputSchema: outputSchema[PublishedDraft](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: false, OpenWorldHint: &no},
			Description: "Publish one of the signed-in user's pending drafts by its id from my_drafts, as the person would on review_url: a recommendation reason, question, tip or reply. " +
				"It appears under their name, labelled Agent and via the app that wrote the draft. A recommendation draft never replaces a reason the person wrote themselves. " +
				"Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, _ *mcp.CallToolRequest, in draftIDInput) (PublishedDraft, error) {
				out, err := d.AgentPosts.PublishDraft(ctx, owner, in.DraftID)
				if errors.Is(err, drafts.ErrNotFound) {
					return out, callMistake{errors.New("draft_id: no pending draft with this id; it may already be published, discarded or expired. Use an id from my_drafts")}
				}
				return out, err
			}))
	}
	if d.AgentPosts != nil && d.Discussions != nil {
		addTool(server, d, &mcp.Tool{Name: "post_question", Title: "Post question or tip", OutputSchema: outputSchema[postState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Post a new question or tip on an event as the signed-in user (plain text, up to 4000 characters). It appears in the event's questions and tips under their name, labelled Agent and via this app. " +
				"Posting the same text on the same event again returns the same post. To let the person review it first, use draft_question. " +
				"The person can edit or remove it on the web; agents can't. Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in questionPostInput) (postState, error) {
				post, created, err := d.AgentPosts.Post(ctx, owner, in.EventID, "", "", in.Body, postKey(in.EventID, "", "", in.Body), agentClient(req))
				return postState{Post: &post, Created: created}, err
			}))
		addTool(server, d, &mcp.Tool{Name: "post_reply", Title: "Post reply", OutputSchema: outputSchema[postState](),
			Annotations: &mcp.ToolAnnotations{DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
			Description: "Reply in a question or tip's conversation as the signed-in user (plain text, up to 4000 characters), labelled Agent and via this app. Use the event_id and thread_id from event_discussions, " +
				"community_discussions or discussion_thread; to answer a particular reply, pass its id as parent_id. It notifies the conversation's followers. " +
				"Posting the same text in the same place again returns the same reply. To let the person review it first, use draft_reply. Needs the connection's permission to post publicly. " + publicNote},
			publishTool(d, func(ctx context.Context, owner string, req *mcp.CallToolRequest, in replyPostInput) (postState, error) {
				if err := threadTarget(in.ThreadID, nil); err != nil {
					return postState{}, err
				}
				post, created, err := d.AgentPosts.Post(ctx, owner, in.EventID, in.ThreadID, in.ParentID, in.Body, postKey(in.EventID, in.ThreadID, in.ParentID, in.Body), agentClient(req))
				if errors.Is(err, discussions.ErrNotFound) && in.ParentID != "" {
					return postState{}, errParentNotFound
				}
				if err != nil {
					return postState{}, threadTarget(in.ThreadID, err)
				}
				return postState{Post: &post, Created: created}, nil
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
// Each is a private draft the person reads and publishes on the web, or an
// agent allowed to post publishes with publish_draft; either way it is
// attributed to the app that wrote it.
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
		} else if d.PublishLimit != nil {
			// A refused or failed action published nothing.
			d.PublishLimit.Refund(owner)
		}
		return out, err
	})
}

// errPublishLimit is worded for the agent, not the account API.
type errPublishLimit struct{ wait time.Duration }

func (e errPublishLimit) Error() string {
	return fmt.Sprintf("this account has reached its limit of %d public actions an hour through agents and API clients. Retry after %d seconds.",
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
		return "the person wrote or published this recommendation themselves, so only they can change or withdraw it, on the event's page"
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

type questionPostInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events, for example ticketmaster:G5v..."`
	Body    string `json:"body" jsonschema:"the question or tip to post now under the user's name, plain text up to 4000 characters"`
}

type replyPostInput struct {
	EventID  string `json:"event_id" jsonschema:"event ID the conversation belongs to"`
	ThreadID string `json:"thread_id" jsonschema:"thread_id of the question or tip, from event_discussions, community_discussions or discussion_thread"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"id of the reply this answers, from discussion_thread; omit to answer the question or tip"`
	Body     string `json:"body" jsonschema:"the reply to post now under the user's name, plain text up to 4000 characters"`
}

type recommendInput struct {
	EventID string `json:"event_id" jsonschema:"event ID from find_events, for example ticketmaster:G5v..."`
	Reason  string `json:"reason,omitempty" jsonschema:"optional public reason, plain text up to 500 characters"`
}

// postState is a post an agent published; created is false when the same
// post already existed.
type postState struct {
	Post    *discussions.Post `json:"post,omitempty"`
	Created bool              `json:"created"`
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
