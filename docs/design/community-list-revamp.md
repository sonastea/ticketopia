# Community list revamp: making participation feel alive

**Status: Phases 0–3 shipped (2026-11-22); Phases 4–6 proposed.** Each phase
can ship on its own and lists what it needs, what success looks like and what
it leaves out. Shipped phases end with a **Shipped** note recording departures
from the plan. Check a phase only when its user-facing behavior ships and is
verified.

- [x] Phase 0 — Baseline (fixture numbers; production numbers still to record)
- [x] Phase 1 — Show what is already there
- [x] Phase 2 — "Can you help?"
- [x] Phase 3 — Threads that update while you read
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

**Shipped (2026-11-22)**, approved in [PRODUCT.md](../../PRODUCT.md) for people
actively taking part. See [live replies](../event-discussions.md#live-replies).
Departures from the plan:

- **Every reply is found.** Each check lists replies created after the page's
  `as_of`, plus a short overlap before it. The overlap is the query timeout
  plus a second, which bounds every reply's write, so a reply committing after
  the page read is still found. The browser drops replies it already shows by
  ID. "Show" opens the replies starting at the first unseen one (`from`), so
  every new reply is on that page on long threads too.
- **Polling, not the stream.** The browser checks a small web route instead of
  subscribing to the live activity stream. The stream fans out recommendation
  candidates per city and filter, while a thread's question is one indexed
  count. Polling holds no connection per reader and works on any replica
  because it reads MariaDB, and its cost is measurable on its own. So the
  live activity rollout gates don't apply; this phase has its own benchmark.
- **Only for people taking part**: signed in, the tab visible, and following
  the thread, its author, a replier on the page, or typing in its composer.
  Checks run every 10s (±20%) while active, every 30s after two idle minutes,
  and stop after ten. Others reload as before.
- The control reads "Show *N* new replies", sticks above the phone navigation,
  and opens the conversation at the first new reply, focused, keeping drafts.
  Edits, removals and hides appear on that refresh, never pushed.
- `DISCUSSION_LIVE_CHECKS_PER_MINUTE` (default 30 per account per replica, 0
  off) bounds runaway or shared sessions. The route is browser-only, so it has
  no `/api/v1` twin or agent tool: agents read threads with `discussion_thread`
  and have no open view to keep current.

**Benchmark (2026-11-22, Apple M1, 8 cores; k6, the app and MariaDB on one
machine, nothing else running).** [`make live-replies-benchmark`](../load-testing.md#live-replies)
on the final implementation, holding 10 threads with a reply every 10
seconds. "People" is the held checks per second × 10: people checking at the
browser's ten-second pace.

| People | Checks/s | p50 | p95 | p99 | Noticed p95 | App CPU avg | MariaDB CPU avg / peak | Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 400 | 40 | 3 ms | 5 ms | 9 ms | 9.8 s | 2% | 3% / 5% | pass |
| 1,600 | 160 | 3 ms | 9 ms | 28 ms | 10.1 s | 9% | 11% / 18% | pass |
| 3,200 | 320 | 4 ms | 15 ms | 29 ms | 9.9 s | 21% | 28% / 67% | pass |
| 7,950 | 795 | 9 ms | 29 ms | 106 ms | 1.9 s¹ | 60% | 85% / 116% | pass |
| 11,860 | 1,186 | 11 ms | 115 ms | 242 ms | 1.9 s¹ | 70% | 102% / 122% | pass |
| 13,940 of 16,000 | 1,394 | 207 ms | **932 ms** | 1.35 s | 2.6 s¹ | 68% | 97% / 116% | fail (p95 over 300 ms; held 87% of pace) |

¹ From 7,950 people the steps used 2-second checks, each VU standing for five
people, so replies were noticed within that shorter interval. No check or
write failed at any step.

Locally the limit is **about 12,000–14,000 people taking part at once**
(roughly 1,200–1,400 checks/s). MariaDB runs out first, at about one core; the
app was at 70% of one. An earlier version without the commit-safe overlap
(one statement per check) held 12,000 at a 29 ms p95. The second statement
and overlap read cost about a sixth of that headroom, in exchange for never
missing a reply. The production profile (app 1 CPU, MariaDB 2 CPUs) is
unmeasured; a production run should precede raising the default limit or
relying on these figures. Taking part is a small subset of visitors, so this
leaves wide headroom over the 768-tab ceiling the live activity stream is
configured for.

In a real browser (`scripts/live-replies-browser.cjs`, replies at random
points in the interval), **Show N new replies** appeared 5.8 s after a reply was
created at p50 (11.3 s p95, about one interval plus a check). After the click
the reply was shown and focused in 62 ms (263 ms p95). Nothing moved before the
click, the draft was kept, each round offered only its own reply, and it was
announced. On a thread with a full first page, three new replies all opened on
one page starting at the first. Alongside 8,000 simulated people an earlier
build took 3.8 s (8.3 s p95) and 134 ms (354 ms p95).

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
