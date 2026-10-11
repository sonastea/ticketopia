---
target: event-row and event-context discovery elements
total_score: 29
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 1
target_identity: "file:/Users/johnny/fun/ticketopia/views/home/index.templ"
target_fingerprint: "sha256:42430c66208aad3ea767d00a23212526ca7e46395a7d67f0a480d5bef37d9088"
target_path: /Users/johnny/fun/ticketopia/views/home/index.templ
timestamp: 2026-10-10T22-14-17Z
slug: views-home-index-templ
---
# Critique: event-row + event-context (discovery surface)

Method: dual-agent (A: ses_ed82c4be6ffeqSTor5CHV0yQ32 · B: ses_ed82c4be4ffeJ16BESkKjrdt6S)

## Design Health Score — 29/40 (Good)

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 4 | Selection wash + aria-current; feedback kept out of control geometry |
| 2 | Match System / Real World | 3 | "Event & discussion" toolbar label static across sections |
| 3 | User Control and Freedom | 3 | Compact selection swap recoverable only via small X |
| 4 | Consistency and Standards | 2 | Ghost-button tabs: curved underline + pill hover; .account-nav renders same pattern straight |
| 5 | Error Prevention | 3 | Honest labels; failed writes retain prior state |
| 6 | Recognition Rather Than Recall | 4 | Icon+label everywhere |
| 7 | Flexibility and Efficiency | 2 | No accelerators; ~3.5 rows per viewport; manual Load more |
| 8 | Aesthetic and Minimalist Design | 2 | 5 interactives per row signed-out, 6–7 signed-in; "0 people interested" x20 |
| 9 | Error Recovery | 3 | Plain-language errors with retry; not exercised live |
| 10 | Help and Documentation | 3 | Extensive inline explanation, verging on over-explanation |

## Design Specificity Verdict

Grounded at the token/marker level (dusty-rose date index, pale-plum selection, Manrope roles, enforced no-shadow rule); conventional at the composition level (standard listings row anatomy). Detector: cannot parse .templ (silent skip, proved by control file); rendered-HTML fallback with inlined live stylesheet produced zero surviving CLI findings. Browser overlay added one mechanically accurate item: with selection, main results column ~690% of viewport height (6963px vs 1000px) while context panel fits in 93%. Evidenced false positives: 3 text-occlusions (closed disclosures), "Apollo Theater" (venue proper noun), cramped-padding on .event-row (logical properties unresolved; computed padding 24px/12px is fine). Overlays captured via headless Chromium screenshots; no persistent [Human] tab.

## Overall Impression

User's two instincts target the two real craft failures. Button cluster is a hierarchy absence, not taste; the curved border is a rendering defect (rounded-lg bends the 2px current marker). Biggest opportunity: make the title the row's one primary action and give the section nav a current-state marker built for its shape.

## What's Working

1. Date-index system is a real signature, adapts across breakpoints, never leaks roles.
2. State honesty: labeled signed-out/unavailable/pending states; feedback never shifts geometry; no-JS paths work.
3. Flat border-only depth executed with discipline: no shadows, 44px targets, plum focus outlines, gated hover.

## Priority Issues

- [P1] Active section underline curves ("the smile"). SectionLink = ghost-variant button.Button with rounded-lg; .section-nav a never resets radius, so 2px border-block-end kicks up ~8px at each end, against the glyphs (padding-inline .1rem). Confirmed in @2x capture; .account-nav renders the same pattern straight. Fix (user chose restyle): segmented current-state treatment removing the underline; minimal alternative .section-nav a { border-radius: 0 } + margin-block-end -1px to knit into the nav's 1px rule. No inset box-shadow (reserved for drag markers). Command: $impeccable polish
- [P2] Ghost hover:bg-muted paints pale-plum (--brand-soft, the documented selection color) rounded pill behind non-current tabs; pill metaphor collides with underline tabs. Fix: absorbed by P1 restyle, or scoped background: transparent hover reset. Command: $impeccable polish
- [P3] Row action cluster: 5 co-equal affordances (Save pill, Interested pill, far-right Details link, title, More-info summary), longest labels for signed-out newcomers, repeated x20 rows; 4/8 cognitive-load items fail; corroborated by 690%-viewport column measurement. Fix: quiet inactive pair (--line border, transparent fill), title as primary action, merge/remove Details link, keep on-sale reminder off the row, sign-in links stop wearing the submit-button silhouette. Command: $impeccable distill
- [P4] "0 people interested" rendered unconditionally on every row; repeated zero = noise + anti-social-proof + SR verbosity. Fix: render visually only when count > 0 (keep node for aria-live). Command: $impeccable distill

## Persona Red Flags

- Alex: ~270px rows, 3.5/viewport; no accelerators; most-used target (Details) is smallest/most isolated; .context-jump visually hidden until focus; manual pagination.
- Sam: strong foundation (aria-current, 44px, state in names); Interested accessible name announced fully on all 20 rows; twenty "0 people interested" announcements.
- Casey: ~1 event per screen; accidental title tap swaps away whole list (.has-selection) with recovery only via small top-right X; button pair wraps at <=360px; curved underline reads broken.

## Minor Observations

- "Event & discussion" toolbar label static across sections.
- 2px plum marker + 1px nav rule = adjacent two-tone 3px band.
- .genre-label: static text in actionable plum — mute or make a real filter link.
- Desktop date-marker strip duplicates the .event-date line (documented, but more visible at this breakpoint).
- Title->Discussion vs Details->Overview is a documented product bet ("Event titles select Discussion"), worth revisiting, not a defect.
- Keep .event-actions align-items: start.

## Questions to Consider

1. If the title is the row's primary action, why does it open the conversation instead of the facts?
2. What's lost if zero-interest rows stay silent about interest?
3. Who owns catching a shared utility class contradicting a documented contract: detector, review, or the contract?
