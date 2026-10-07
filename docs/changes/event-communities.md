# Event community changes

## Unreleased

### 2026-10-06 — Interested visibility and public recommendations

- Implement durable, reversible event interest independently from saves, category
  preferences, endorsements and attendance. Keep identity private by default;
  profile defaults affect only new choices. See [event interest](../event-interest.md).
- Deliver shared owner APIs, public aggregate counts, own collection/visibility
  management, and public-opt-in participants/profile activity with bounded keysets.
- Add native/enhanced discovery, preview, Saved and event controls with distinct
  selected state/counts, pending/error feedback, and immediate initiating-panel
  refresh after visibility revocation. Preserve private saves and retained details.
- Verify ownership/CSRF/validation, concurrency/rollback, privacy/keysets, migration
  preservation/restarts, Chromium desktop/tablet/mobile/native/error journeys and
  automated accessibility. Discussions/moderation remain planned.
- Resolve the independent review's label-in-name and participant-return findings;
  verify 54 Chromium checks and seven zero-violation accessibility captures, the
  full Go race suite with real MariaDB, vet, production build, and OpenAPI lint.
- Simplify repeated interest state: keep privacy on the button, place **Change
  visibility** beside a more readable count, and close the editor after saving.
  Announce success without lingering paragraphs; show one dismissible error and
  clear stale feedback on retry/new actions/history restore. Preserve native forms,
  per-event visibility, private defaults, and focus continuity.
- Verify the refinement with 84 Chromium checks/ten zero-violation axe captures,
  targeted view/API race tests and vet, production build, and in-thread responsive
  inspection. Only pre-existing detector notices remain.
- Deliver public recommendations independently of Save/Interested: one per
  account/event, optional 500-character reason, stable publication/edit times,
  ownership/CSRF, and retry-safe edit/withdrawal. See [recommendations](../event-recommendations.md).
- Add event/public-profile attribution, explicit counts, recent city/category
  browsing, native/enhanced forms with preserved drafts, and paginated event/return
  context. Keep discussions/moderation truthful and require moderation before a pilot.
- Verify SQL/API race tests, concurrency/rollback, migration preservation/restarts,
  scoped keysets/privacy, and 62 Chromium checks with eight accessibility scans
  across desktop/tablet/phone/200% text. Fix enlarged-text recommendation wrapping.
- Address the independent review's direct-open, native credential-failure draft,
  and obsolete-availability findings; verify rejected writes remain unapplied and
  native credential recovery retains safe event/return context. The reviewer scores
  all four listed fixes resolved; existing ticket-button clipping at 200% remains
  outside that scoped verdict.

### 2026-10-03 — Distinct social signals and responsive participation

- Align [community goals](../goals/event-communities.md) with separate private
  saves, private-by-default interest identity, public recommendations, and
  post-level Helpful reactions.
- Specify asynchronous event threads and simple city/category community scopes
  in the [design guidelines](../design-guidelines.md) and proposed
  [API contracts](../design/radar-api.md#proposed-mvp-community-resources).
- Retain pilot moderation; place Going/Went and discussion notifications after
  the MVP. These are design decisions, not implemented community features.

### 2026-10-02 — Positive communities as a core product goal

- Add [event communities](../goals/event-communities.md) as a core planned
  feature, with small goals for participation, discussion, connection, and return
  visits.
- Define Interested/Going, comments and replies, positive Helpful reactions,
  and self-reported post-show experiences. Exclude thumbs-down reactions.
- Describe private user reports, proportionate moderator review, and clear
  outcomes for participants.
- Align the radar goals, optional game ideas, and shared API direction with the
  core community experience.

The older entries document product direction, not delivered discussions/moderation.
