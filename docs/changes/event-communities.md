# Event community changes

[Older history](archive/event-communities/).

## Unreleased

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

### 2026-10-29 — Batched moderation and community page reads

- The moderator review queue reads a page's public post projections in one query
  instead of one per row (default page: 22 statements -> 2, about 23 ms -> 3.5 ms
  locally). Order, cursors, pending counts, own-case exclusion and redaction are
  unchanged. See [moderation](../moderation.md).
- Discussion and recommendation pages send and decode each event snapshot once
  per page instead of once per row, still in one statement. With 64 KB snapshots
  a 21-row page fell by 43–56% (thread 3.8 -> 2.2 ms, community 5.8 -> 2.5 ms,
  recommendations 3.3 -> 1.5 ms; 3.0 MB -> 0.2 MB). At the 2.6 KB fixture size,
  community pages are 37% faster and recommendations 4% faster, while thread
  pages are about 5% (0.09 ms) slower. All allocate about half the bytes.

### 2026-10-29 — Optional category filters wait at most one second

- Community, discussions, Happening Now and interest pages give the optional
  category catalog Discover's one-second budget instead of the ten-second refresh.

### 2026-10-28 — Concurrent interest and recommendation reads

- A PUT that omits visibility applies the account default only when it inserts
  the choice. An explicit choice committed while that request fetched event
  details keeps its visibility, so a delayed default can no longer publish it.
- An event's recommendation page and total count share one read snapshot, so a
  concurrent withdrawal can no longer report fewer recommendations than it lists.
