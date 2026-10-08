---
version: 1
slug: "views-home-moderation-templ"
primary_target: "views/home/moderation.templ"
related_targets: ["views/home/discussions.templ", "views/account/settings.templ"]
---

# Private reporting and moderator review

Mode: Operate. Confirmed reporting/review loop; durable moderator roles with an
operator grant/revoke CLI, not an administrator web UI or notification delivery.

## Direction contract

**THESIS:** A private concern gets an accountable review without becoming a
public downvote or exposing its reporter.

**OWN-WORLD:** Inherit Manrope/plum, the existing shell, flat rule-separated
contributions, labeled native selects/textareas, and outline focus. No new tokens.

**STORY:** Report with a reason; check receipt and outcome in the account area.
Moderators open a case, compare reported and current text, read the conversation,
then keep/hide/restore with a shared reason and separate private notes.
Own receipts/outcomes retain recognizable event and contribution identity, with
direct links that select off-page replies. Moderators cannot read their own cases'
private evidence or see those cases in their queue.

**FIRST VIEWPORT:** Reporting leads with privacy and event/contribution context.
The queue leads with pending cases; a case leads with event context and current
visibility. Author outcomes offer a native disclosure for another review.

**FORM:** User-confirmed, code-led local extension of the existing discussion and
account surfaces, inheriting seed `6b2b9e52`. All tasks remain single-column,
usable without JavaScript, with escaped drafts preserved on rejected writes.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
