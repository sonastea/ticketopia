# Community list revamp: making participation feel alive

**Status: Phases 0–2 shipped (2026-11-22); Phases 3–6 proposed.** Each phase
can ship on its own and lists what it needs, what success looks like and what
it leaves out. Shipped phases end with a **Shipped** note recording departures
from the plan. Check a phase only when its user-facing behavior ships and is
verified.

- [x] Phase 0 — Baseline (fixture numbers; production numbers still to record)
- [x] Phase 1 — Show what is already there
- [x] Phase 2 — "Can you help?"
- [ ] Phase 3 — Threads that update while you read (needs a product decision)
- [ ] Phase 4 — "Your answer helped"
- [ ] Phase 5 — Conversations that fit the moment
- [ ] Phase 6, optional — Anonymous signs of others

## Why

Community already covers the core loop in the
[event communities goal](../goals/event-communities.md): Interested, Recommend,
**Questions & tips** with threaded replies, Helpful, followed discussions, live
recommendation toasts and **Happening Now**. What it lacks is the feeling that
other people are around and that a post will get a reply:

- A question with no reply looks the same as an answered one. No surface routes
  people who can answer to the questions that need them.
- Open threads never change while you read them: [discussions](../event-discussions.md)
  include no live polling, and live activity covers recommendations only.
- Helpful is silent for the person who helped. [Followed discussions](../followed-discussions.md)
  deliberately generate no update for Helpful.
- A new event's **Questions & tips** start empty, even when earlier shows at the same
  venue gathered useful tips.
- Recommendation reasons are the app's warmest content, but outside `/community`
  and the Discover Up next lead they are reduced to counts.

The aim is to show existing activity at the moment it is useful, before adding new
kinds of posts. This does not replace finding a pilot city. If nobody is there,
none of this helps; see the
[delivery checklist](../goals/delivery.md#current-focus-use-refine-and-verify).

## Guardrails

These carry over from the current product and apply to every phase:

1. **Asynchronous, not chat.** No direct messages, typing indicators, presence
   lists or real-time chat ([out of scope](../goals/feature-backlog.md#out-of-scope)).
2. **Private by default.** Saves, private Interested choices and connection
   filters are never revealed. Public attribution stays first-name-only where the
   live feed already uses it, and named where posts already are.
3. **Positive only.** No thumbs-down, scores, leaderboards or public counts of
   "people helped". Acknowledgments go to the person, not into a ranking.
4. **No bumping and no personalized ranking.** Lists keep their documented order
   (newest started, first-recommendation time). New marks filter or annotate
   items; they never reorder them.
5. **Moderation wins.** Hidden, removed and withheld contributions never feed a
   new surface, count, prompt or notification.
6. **Native first.** Every surface works without JavaScript and meets the
   [app rework](app-rework.md#the-design-philosophy) acceptance criteria: notes
   budget, 44px targets, visible focus, forced colours, reduced motion and 200%
   text.
7. **API and agents.** Every new `/api/v1` endpoint ships as an agent tool sharing
   its service method ([App MCP](../app-mcp.md)); OpenAPI stays aligned.

## Phase 0 — Baseline

Measure before changing anything, using the review scenarios and production logs.
Add no new tracking.

- Share of visible root posts with no visible reply, and median time to first reply.
- Share of replies that receive Helpful.
- Community and Conversations page heights at 1440px and 390px, as the app rework
  recorded.

**Done when** the numbers are recorded in this doc. They are the comparison point
for Phases 2 and 4.

**Recorded (2026-11-22).** Page heights on the `populated` review scenario,
signed in, before Phase 2 added the open-questions line:

| Page | 1440px desktop | 390px phone |
| --- | --- | --- |
| `/community` | 1,359px | 2,584px |
| `/community/discussions` | 1,069px | 2,001px |

The fixture's discussion numbers describe seeded data, not people, so the
reply and Helpful baseline must come from production. Run these read-only
queries there and record the results here before judging Phase 2:

```sql
-- Visible roots with no visible reply, and the share they make up.
SELECT COUNT(*) AS roots,
       SUM(NOT EXISTS (SELECT 1 FROM discussion_posts r WHERE r.root_id = p.post_id
           AND r.removed_at IS NULL AND r.hidden_at IS NULL)) AS unanswered
FROM discussion_posts p
WHERE p.root_id IS NULL AND p.removed_at IS NULL AND p.hidden_at IS NULL;

-- Hours to the first visible reply, per answered root (take the median).
SELECT TIMESTAMPDIFF(MINUTE, p.created_at, MIN(r.created_at)) / 60 AS hours
FROM discussion_posts p JOIN discussion_posts r ON r.root_id = p.post_id
WHERE p.root_id IS NULL AND p.removed_at IS NULL AND p.hidden_at IS NULL
  AND r.removed_at IS NULL AND r.hidden_at IS NULL
GROUP BY p.post_id ORDER BY hours;

-- Share of visible replies with at least one Helpful.
SELECT COUNT(*) AS replies, SUM(EXISTS (SELECT 1 FROM post_helpful h WHERE h.post_id = r.post_id)) AS helped
FROM discussion_posts r
WHERE r.root_id IS NOT NULL AND r.removed_at IS NULL AND r.hidden_at IS NULL;
```

## Phase 1 — Show what is already there

Low-risk presentation work with no schema changes.

- **Reasons on more cards.** Shared event cards on Discover, Radar and collections
  quote the newest public recommendation reason, one line, with the author's
  first name, as the Up next lead already does. Events without a reason show
  nothing extra; counts stay where they are.
- **Composer starters.** The empty **Questions & tips** composer offers two or
  three starters as native buttons that fill the field ("Has anyone seen this
  artist live?", "Tips for getting to this venue?"). They insert editable text and
  never publish. Without JavaScript they render as example text in the composer
  guidance.
- **First-post welcome.** A person's first visible root post shows a quiet "First
  post" label, so others can reply kindly. It is derived from post history, needs
  no stored flag, and goes away once they have three visible posts.

**Done when** cards stay inside their measured heights, starters never publish by
themselves, and the welcome label is never shown on hidden or removed posts.

**Shipped (2026-11-22).** This reverses the earlier rule that only the Up next
lead quotes a reason ([DESIGN.md](../../DESIGN.md#indexed-event-rows)).
Departures from the plan:

- Discover and Radar cards quote reasons; Saved, Interests and other compact
  collection rows don't, because rows summarize and cards browse. Quotes show
  the author's display name, as the lead already did, not a first name. Cards
  allow two lines and the lead three, because one line cut most reasons
  mid-phrase.
- The starters are "First time at this venue. Any tips for getting in?", "What
  time should I arrive?" and "Has anyone been to one of these before?". They ask
  about going, not about a performer, so they fit sports and arts too. They show
  only while the field is empty.
- First post shows on the conversation, the event's notes and Community cards.

## Phase 2 — "Can you help?"

A third Community collection beside **Recommendations** and **Conversations**.

- **Open questions:** visible root posts with no visible reply, on upcoming
  events. It uses the same city, country and category filters and keeps
  newest-started order. Once a root gets a visible reply, it leaves the list.
  If that reply is later removed or hidden, the root returns at its original
  position.
- **Your events (signed in):** an extra filter limited to events the person has
  saved, marked Interested, or whose artist or venue they follow. It reuses the
  private connection matching from
  [live activity Part 4](live-activity-filters.md). Matching runs server-side
  for the signed-in person only, and the reason a question matched is never
  shown to anyone else.
- **Same question:** a signed-in person can add or remove a "Same question" mark
  on someone else's open root. It shows "*N* people want to know" and works like
  Helpful (one per account per post, no negative form), but it never reorders the
  list. Marks are removed with the post and hidden with it.
- **Entry points:** a short "*N* open questions near you" line on Community, and
  an **Answer** link on each card that opens the thread with the reply composer
  ready.
- **API and agents:** `GET /api/v1/community/questions` (public, filters as
  `/community/discussions`) and an `open_questions` read tool. The Same question
  mark is a write tool under the existing `publish` scope, as Helpful is.
- **Persistence:** a `discussion_same_question` table keyed by post and account.
  The unanswered state is computed from existing reply visibility, not stored.

**Done when** the time to first reply in the pilot improves on the Phase 0
baseline, and no hidden, removed or past-event root ever appears in the list.

**Shipped (2026-11-22)**, at `/community/questions` under an **Open questions**
tab, headed "Can you help?". Departures from the plan:

- Same question can be marked on any visible question or tip that isn't the
  person's own, answered or not, so people can still find a question's answer.
  It also appears beside Helpful on the conversation's opening post.
- The Community line follows the page's city, country and category filters
  ("3 open questions in Chicago nobody has answered yet. Can you help?") rather
  than a detected location, which Community doesn't use.
- The signed-out "Your events" link sends people to sign in. The agent tools
  are `open_questions` (public) and `mark_same_question` /
  `unmark_same_question` (`publish`), next to Helpful.
- Schema 25 adds `discussion_same_question`, with runtime INSERT and DELETE.
  "Unanswered" and "upcoming" are computed from replies and `events.start_utc`
  (or the local date when no time is known); events with no date stay listed.

## Phase 3 — Threads that update while you read

- An open thread (contextual panel or full page) shows a polite live region and a
  "*N* new replies, show" button when visible replies arrive. Nothing moves until
  the person asks. Showing them refreshes only the conversation, keeping drafts,
  focus and reading position, as confirmed writes already do.
- Reply events reuse the live activity stream and its cross-replica delivery.
  The stream subscribes per open thread and is limited by the existing stream
  caps and refusals (429/503). Edits, removals and hides refresh the thread on
  the next fetch and are never pushed as content.
- Without JavaScript, or when the stream is refused, nothing changes: reloading
  still works, and the page shows no error.
- This reverses the documented "no live polling" choice for discussions, so it
  needs an explicit product decision. Record it in `PRODUCT.md`.

**Done when** two browsers on different replicas see each other's replies
without reloading, a draft survives a refresh, and stream metrics stay within
the [live activity rollout](../live-activity-rollout.md) gates.

## Phase 4 — "Your answer helped"

- When someone marks a person's post Helpful, that person gets a private in-app
  update: "*First name* found your reply helpful". Updates are batched and follow
  the account's existing followed-discussion frequency and pause settings. It
  does not depend on following the thread.
- **Optional thanks:** when marking Helpful, a person may add a one-line thank-you
  (up to 140 plain-text characters). Only the post's author sees it, it can be
  reported privately, and it is removed if the Helpful mark is removed. It is a
  note, not a conversation, and it cannot be replied to.
- Removing Helpful before delivery cancels the update. Hidden posts generate
  nothing.
- Agents with `publish` may set Helpful but never write thank-you notes.

**Done when** a pilot participant sees their answer acknowledged, and pausing
notifications stops these updates along with reply updates.

## Phase 5 — Conversations that fit the moment

- **Event-phase prompts.** Composer starters and an empty-state line change with
  the event date in the venue's time zone: before the event, "First time at this
  venue?"; on the day, "Heading there tonight? Share a tip"; after it, "How was
  it? Share something that helps the next visitor". These are prompts only and
  record no attendance. This is the lightweight first step toward the backlog's
  Went reflections.
- **Venue tips.** **Questions & tips** on an event shows up to three "From past
  shows at this venue" posts: visible root posts or replies on past events at the
  same retained venue, chosen by Helpful count with ties going to the newest. Each
  links to its original thread with that event named. They appear in a clearly
  separate section and never count toward the current event's numbers.
- **Persistence:** venue identity already exists in event snapshots; add an index
  for venue lookups if measurement shows it's needed.

**Done when** new events at well-covered venues stop opening to an empty section,
and no withheld post appears as a venue tip.

## Phase 6, optional — Anonymous signs of others

Only after Phases 1–5 have pilot evidence, and only if the room still feels empty.

- **Taste neighbours:** on an event, "People who recommended this also
  recommended…" from public recommendations only, shown only when at least five
  distinct accounts are behind it. No names.
- **Activity counts:** "*N* people looked at this today", shown only above a
  minimum of ten. This needs new aggregate-only counting (no account, IP or
  per-request history), so it needs its own privacy review before design.

## Open decisions

1. Phase 3 reverses "no live polling" for discussions. Approve it in `PRODUCT.md`
   or drop the phase.
2. Whether "Same question" also notifies people who follow the thread when the
   question is answered. Recommended: yes, through the existing reply updates.
3. Whether the "First post" label needs an opt-out in profile privacy settings.
4. Pilot city and event set, since Phases 2 and 4 are measured there.

## Verification per phase

Isolated Chromium fixtures on the review server, with a scenario per phase
(unanswered roots, hidden replies, past-venue tips, two-replica replies). Check
desktop and phone, keyboard, JavaScript off, reduced motion, forced colours and
200% text, plus an axe pass. Have an agent run the new tools as described in
[App MCP](../app-mcp.md#verifying-new-tools-with-an-agent). Update
[event discussions](../event-discussions.md), [followed discussions](../followed-discussions.md)
and `docs/changes/` as each phase ships.
