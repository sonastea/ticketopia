---
target: profile page /me and sibling account pages
total_score: 22
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 2
target_identity: "file:/Users/johnny/fun/ticketopia/views/account/settings.templ"
target_fingerprint: "sha256:c8a71c29ee755940e492907b9c7b083a3fb912af608028b654234c07baeefcca"
target_path: /Users/johnny/fun/ticketopia/views/account/settings.templ
timestamp: 2026-11-18T10-18-08Z
slug: views-account-settings-templ
---
# Critique: /me (profile) and sibling account pages

Score 22/40 (Acceptable). Heuristics: 1=2, 2=3, 3=2, 4=2, 5=2, 6=2, 7=2, 8=1, 9=3, 10=3.

Verdict: /me is a generic settings form. The clutter comes from its structure (three pages in one, 15 field-notes, privacy explained five times), not from its styling. Automated scan: CLI clean. Browser: line-length hits at settings.templ:151, :218, connect.templ:97, agents.templ:66, :95. 19 text-occlusion hits were false positives (collapsed details).

## Priority issues
- [P1] /me is three pages in one (settings.templ:123-244). Split it into /me (profile), /me/privacy, /me/security and /me/preferences, each with one form and one Save.
- [P1] "Sign out everywhere…" sits next to Sign out with no confirmation (settings.templ:175-192). Rename it "Sign out of all devices", place it last on /me/security, and require confirmation.
- [P2] Too many field-notes, with privacy restated five times (settings.templ:101,128,151,168,174). Use Public/Private labels, one comparison list, and at most one note per section.
- [P2] Account nav shifts between pages (account_nav.templ:25-70, settings.templ:102, agents.templ:6). Use one helper, family tabs (max 4), and one name per destination.
- [P2] The profile lacks identity, and on phone the preview comes after Save (settings.templ:124-170). Add a profile header with monogram, counts and recent public events, with the edit form below.

## Proposed IA
Families: Account (Profile, Privacy, Preferences, Security & apps); Your events (Radar, Interests, Artists & venues); Updates (Agent drafts, On-sale reminders, Followed discussions, Reply notifications); Safety (Your reports, Moderation outcomes, Review queue).
Shared AccountPage template: two widths (about 44rem for forms, about 70rem for collections), status messages next to the section that triggered them, destructive actions last and confirmed.
