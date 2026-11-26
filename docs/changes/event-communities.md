# Event community changes

[Older history](archive/event-communities/).

## Unreleased

### 2026-11-26 — Replies expand in place

- On an event's **Questions & tips**, each question or tip has a **N replies**
  toggle that expands its replies beneath it, like a comment thread: oldest
  first, **Show more replies**, Helpful and Reply on each, and a reply box at
  the end. Reply on a reply loads its own reply box, and a write refreshes only
  that conversation. Without script the link still opens the conversation page,
  which stays for sharing and notifications. See
  [web experience](../event-discussions.md#web-experience).

### 2026-11-22 — Live replies for people taking part

- While a signed-in person takes part in an open conversation (following it,
  its author, a replier on the page, or writing a reply) and its tab is
  visible, it checks for other people's new replies. It checks every 10s while
  active, every 30s when idle, and stops after ten idle minutes or when hidden.
  A polite announcement and a sticky **Show N new replies** open the
  conversation at the first new reply, keeping drafts; nothing moves first.
  Approved in PRODUCT.md. `DISCUSSION_LIVE_CHECKS_PER_MINUTE` (default 30, 0
  off) bounds each account.
- Checks find every reply, including one committing just after the page's
  read (an overlap bounded by the query timeout). "Show" opens the replies from
  the first unseen one, so long threads show them all.
- New `make live-replies` and `make live-replies-benchmark`, and a browser
  measurement. Locally the limit is about 12,000–14,000 people taking part at
  once, with MariaDB saturating first. Replies are offered within one interval
  and shown in about 60 ms. See [live replies](../event-discussions.md#live-replies)
  and the [benchmark](../design/community-list-revamp.md#phase-3--threads-that-update-while-you-read).

### 2026-11-22 — Open questions, Same question, reasons on cards and First post

- **Can you help?** (`/community/questions`, a new **Open questions** tab) lists
  questions and tips on upcoming events that nobody has answered yet, newest
  started first, with Community's filters. Signed in, **Your events** keeps
  saved, Interested and followed events privately. **Answer** opens the reply
  composer. `/community` shows how many are waiting for its filters.
- **Same question** marks someone else's question as yours too ("*N* people want
  to know") on cards and the conversation's opening post. It never reorders a
  list, and it is withheld or deleted with the post. API and agent tools:
  `GET /api/v1/community/questions`, `PUT`/`DELETE …/reactions/same-question`,
  `open_questions`, `mark_same_question` and `unmark_same_question`.
- Discover and Radar cards quote their newest public recommendation reason (two
  lines; three on Up next), reversing the lead-only rule. A person's first
  question or tip carries a quiet **First post** label while they have fewer
  than three visible contributions. Empty question composers offer starters
  that fill the field and never publish.
- Verified with MariaDB integration, HTTP, agent-tool and Chromium checks
  (desktop/phone, JavaScript off, forced colours, 200% text at 320px, axe). See
  [open questions](../event-discussions.md#open-questions-same-question-and-first-post)
  and the [revamp plan](../design/community-list-revamp.md).

### 2026-11-21 — Community card actions

- Community recommendation and conversation cards end in an action bar instead
  of two underlined links: a soft-plum primary button (Recommendations, or Open
  conversation with its reply count) and a ghost companion (See all, or Questions
  & tips). Destinations and accessible names are unchanged.
- Community card bylines follow the event's quote cards: the ink name link, then
  a muted "recommends this event" line with any app. Community, conversation and
  profile event titles are ink like Discover's (plum and underlined on hover).

### 2026-11-20 — Community depth: quote cards and raised panels

- The event's community sections and threads gain the depth layer (raised by
  hierarchy alongside Discover and the event page): hairline-ringed, plum-tinted
  elevation, the brand gradient and a corner glow, as documented tokens in [DESIGN.md](../../DESIGN.md#public-recommendations).
- Recommendations become quote cards in an auto-fit grid, with the reason as the
  headline and a gradient frame on your own. Questions, the thread's opening post
  and its replies sit on raised panels; composers get a gradient icon tile, a
  glowing field and a gradient publish action. Titles use the display face.
- One motion moment: cards rise into place as they scroll in (position only,
  motion-allowed browsers with scroll timelines). No behaviour or copy change.
- Chromium journeys pass with zero axe violations: recommendations (126 checks/22
  views), responsive (150/5), discussions (69/14), conversation ergonomics (now on
  the shared `axe-scan.cjs`, which discounts fixed-navigation overlap) and moderation.
  WebKit iPhone emulation shows no overflow; real phones/screen readers remain open.

### 2026-11-20 — Readable event community notes and composer cards

- On the event, recommendations, questions and replies share one note shape:
  initials avatar, linked name and date on one 44px line, then the person's words
  at body size in ink. Your own recommendation is marked **You** on a pale-plum wash;
  public interest shows as name chips.
- Composers are cards whose summary invites the action; publishing is the primary
  action and spans phone-width cards. Each post has one compact action row (Reply,
  Helpful, Edit, Remove, Report) with full accessible names. Publishing, privacy,
  public-audience guidance and the API are unchanged. See [DESIGN.md](../../DESIGN.md#public-recommendations).
- Chromium journeys pass with zero axe violations: recommendations (126 checks/22
  views), responsive (150/5), discussions (69/14), plus moderation. Browser scripts
  now use the stable `[data-recommendation-editor]` hook and the note's body path.
  Real phones, WebKit and manual screen readers remain unchecked.
