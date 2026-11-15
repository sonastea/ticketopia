package persistence

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/useractivity"
)

// Question and reply drafts check their conversation, stay private, and
// publish at most once as the person's post attributed to the app, even when
// replicas race. The person's edit or removal clears the attribution.
func TestMariaDBDiscussionDraftPublication(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	p := pools[0]
	ctx := t.Context()
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "talk-draft-owner")
	other, _ := loginAccount(t, auth, "talk-draft-other")
	detail := savedDetail("TalkDraftCase")
	event := detail.Item.ID
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	service := drafts.New(p.Drafts(), discussionDetailFixture{detail}, posts)
	root, _, err := posts.Create(ctx, other.ID, event, "", "", "Is there a cloakroom?", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	answer, _, err := posts.Create(ctx, other.ID, event, root.ID, root.ID, "Yes, by the bar.", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	grant := oauth.Grant{ID: accounts.ID(), AccountID: owner.ID, ClientID: "https://agent.example/client.json", ClientName: "Agent",
		Scopes: []string{"read", "write"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour), AccessHash: oauth.Hash(accounts.ID()), AccessUntil: now.Add(time.Hour)}
	if err := p.OAuth().CreateGrant(ctx, grant, now); err != nil {
		t.Fatal(err)
	}
	client := drafts.Client{Name: "Agent", Host: "agent.example", GrantID: grant.ID}

	reply, err := service.CreateReply(ctx, owner.ID, event, root.ID, answer.ID, "Does it take card?", client)
	if err != nil || reply.ThreadID != root.ID || reply.ParentID != answer.ID || reply.Event.ID != event {
		t.Fatalf("reply draft: %+v %v", reply, err)
	}
	// A retry is the same draft; the same words answering the question are another.
	if again, err := service.CreateReply(ctx, owner.ID, event, root.ID, answer.ID, "Does it take card?", client); err != nil || again.ID != reply.ID {
		t.Fatalf("retried reply draft: %+v %v", again, err)
	}
	if toRoot, err := service.CreateReply(ctx, owner.ID, event, root.ID, "", "Does it take card?", client); err != nil || toRoot.ID == reply.ID {
		t.Fatalf("reply to the question: %+v %v", toRoot, err)
	}
	// A reply's id is not a thread, and a thread is bound to its event.
	if _, err := service.CreateReply(ctx, owner.ID, event, answer.ID, "", "x", client); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatalf("reply as thread: %v", err)
	}
	if _, err := service.CreateReply(ctx, owner.ID, "ticketmaster:Elsewhere", root.ID, "", "x", client); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatalf("thread on another event: %v", err)
	}
	stored, err := service.Get(ctx, owner.ID, reply.ID)
	if err != nil || stored.Kind != drafts.KindReply || stored.ThreadID != root.ID || stored.ParentID != answer.ID || stored.ClientHost != "agent.example" {
		t.Fatalf("stored reply draft: %+v %v", stored, err)
	}
	question, err := service.CreateQuestion(ctx, owner.ID, event, "Where do rideshares drop off?", client)
	if err != nil || question.Kind != drafts.KindQuestion || question.ThreadID != "" {
		t.Fatalf("question draft: %+v %v", question, err)
	}
	visible := func(text string) bool {
		t.Helper()
		for _, thread := range []string{"", root.ID} {
			list, err := posts.List(ctx, "", event, thread, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, post := range list.Items {
				if post.Body == text {
					return true
				}
			}
		}
		return false
	}
	if visible(reply.Text) || visible(question.Text) {
		t.Fatal("draft text is public before publication")
	}

	// Another account cannot consume the draft.
	attribution := discussions.Attribution{Via: stored.ClientName, ViaHost: stored.ClientHost, Draft: question.ID}
	if _, _, err := posts.Publish(ctx, other.ID, event, "", "", question.Text, question.PublishKey(), attribution); !errors.Is(err, discussions.ErrDraftGone) {
		t.Fatalf("other published the draft: %v", err)
	}
	published, created, err := posts.Publish(ctx, owner.ID, event, "", "", question.Text, question.PublishKey(), attribution)
	if err != nil || !created || published.Via != "Agent" || published.ViaHost != "agent.example" || published.ThreadID != published.ID {
		t.Fatalf("published question: %+v %v", published, err)
	}

	// Racing publications on three replicas make one post; repeats find it.
	var wg sync.WaitGroup
	var mu sync.Mutex
	made, ids := 0, map[string]bool{}
	for i := range 6 {
		wg.Go(func() {
			s := discussions.New(pools[i%3].Discussions(), discussionDetailFixture{detail})
			post, fresh, err := s.Publish(ctx, owner.ID, event, root.ID, answer.ID, reply.Text, reply.PublishKey(),
				discussions.Attribution{Via: "Agent", ViaHost: "agent.example", Draft: reply.ID})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				ids[post.ID] = true
				if fresh {
					made++
				}
			case !errors.Is(err, discussions.ErrDraftGone):
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if made != 1 || len(ids) != 1 {
		t.Fatalf("created %d posts %v", made, ids)
	}
	if _, err := service.Get(ctx, owner.ID, reply.ID); !errors.Is(err, drafts.ErrNotFound) {
		t.Fatalf("published draft kept: %v", err)
	}
	// A consumed draft publishes nothing under another key.
	if _, _, err := posts.Publish(ctx, owner.ID, event, root.ID, answer.ID, reply.Text, accounts.ID(),
		discussions.Attribution{Via: "Agent", Draft: reply.ID}); !errors.Is(err, discussions.ErrDraftGone) {
		t.Fatalf("consumed draft: %v", err)
	}
	var replyID string
	for id := range ids {
		replyID = id
	}
	post, err := posts.Post(ctx, "", replyID)
	if err != nil || post.Via != "Agent" || post.ViaHost != "agent.example" || post.ParentID != answer.ID || post.Profile == nil || post.Profile.ID != owner.ID {
		t.Fatalf("published reply: %+v %v", post, err)
	}
	activity, err := p.UserActivity().List(ctx, owner.ID, useractivity.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	attributed := 0
	for _, item := range activity {
		if item.Post != nil && item.Post.Via == "Agent" && item.Post.ViaHost == "agent.example" {
			attributed++
		}
	}
	if attributed != 2 {
		t.Fatalf("profile activity attributed %d posts: %+v", attributed, activity)
	}

	// Saving the same words keeps the attribution; the person's own words clear it.
	if post, err = posts.Edit(ctx, owner.ID, replyID, reply.Text); err != nil || post.Via != "Agent" {
		t.Fatalf("unchanged edit: %+v %v", post, err)
	}
	if post, err = posts.Edit(ctx, owner.ID, replyID, "My own words."); err != nil || post.Via != "" || post.ViaHost != "" {
		t.Fatalf("person's edit: %+v %v", post, err)
	}
	if err := posts.Remove(ctx, owner.ID, published.ID); err != nil {
		t.Fatal(err)
	}
	var via, host *string
	if err := f.admin.QueryRowContext(ctx, "SELECT via_client_name,via_client_host FROM "+f.runtime.Database+".discussion_posts WHERE post_id=?", published.ID).Scan(&via, &host); err != nil || via != nil || host != nil {
		t.Fatalf("removal kept attribution: %v %v %v", via, host, err)
	}
}
