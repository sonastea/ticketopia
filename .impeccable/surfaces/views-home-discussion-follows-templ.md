---
version: 1
slug: "views-home-discussion-follows-templ"
primary_target: "views/home/discussion_follows.templ"
related_targets: ["views/home/discussions.templ", "views/account/settings.templ"]
---

# Followed discussions

Mode: Operate. Continue the existing discussion/account composition, not a new
visual identity. In-app delivery proceeds after the user's instruction to continue.

## Direction contract

**THESIS:** A private way back to the exact event conversation, not a generic
social feed or live chat.

**OWN-WORLD:** Inherit Manrope/plum, flat rule-separated records, native labeled
selects, disclosures, and the account navigation. No new global tokens or assets.

**STORY:** Follow an individual conversation, choose reply frequency, see due
updates, open the exact reply, and unfollow. Global pause is explicit.

**FIRST VIEWPORT:** Event context leads threads; a compact follow disclosure sits
before the root. Private collection pages lead with title, account navigation,
pause state, then event-named records and direct reply links.

**FORM:** Narrow code-led Operate extension inheriting discussion seed `6b2b9e52`.
Compact and enlarged-text layouts wrap naturally. Native controls work without JS.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Finish documentation — 2026-10-08

Checked `views/home/discussion_follows.templ`, `views/home/discussions.templ`,
`views/account/settings.templ`, the existing app stylesheet and `DESIGN.md` against
`docs/followed-discussions.md` and `.impeccable/review/discussion-follows-{desktop,mobile}.png`
plus the corresponding `discussion-notifications-{desktop,mobile}.png` captures.
Recorded private follow controls, frequency/mute/global pause, the flat event-named
in-app inbox and exact visible-reply links in `DESIGN.md`; only external email/push
delivery remains planned. No tokens, fonts, CSS, motion, assets or generated output
changed in this documentation pass. Existing design sidecar drift is left untouched.
Builds/tests were not run; finish-review scoring and UI ship approval remain pending.

## Finish verification — 2026-10-08

The independent reviewer scored the missing design-documentation fix resolved:
**ship for that fix only**, not whole-application approval. The browser journey
passes 52 checks and six zero-violation axe scans; existing discussion behavior
passes 24 regression checks without recapturing unrelated surfaces. Real-MariaDB
race tests, vet, production build and OpenAPI lint pass. Manual screen-reader,
physical-device, cross-browser and production-delivery certification remain outside
this fixture review. No new visual tokens or shipping rasters were introduced.
