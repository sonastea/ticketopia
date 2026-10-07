---
version: 1
slug: "views-account-settings-templ"
primary_target: "views/account/settings.templ"
related_targets: ["views/account/presentation.go"]
---

# Account destinations

Mode: Operate. Extend the existing Profile/Interests destinations and established
visual world; no redesign. The user selected direct Google OAuth2 sign-in.

## Direction contract

**THESIS:** A usable personal space with unmistakable public/private boundaries,
not a promotional sign-up page or a simulation of undelivered saved/community work.

**OWN-WORLD:** Inherit DESIGN.md's Manrope, plum actions, light-stone navigation,
flat surfaces, labeled native forms, responsive shell, and visible focus.

**STORY:** Sign in with Google, return to the original local task, choose a public
display name/bio, inspect the public projection, save private preferences, and
sign out or revoke credentials. No Google real name is imported by default.

**FIRST VIEWPORT:** Existing global navigation around a clear heading and real
Profile/Preferences/Interests links. Profile editing leads, with a distinct public
preview alongside on wide screens and beneath on mobile. Security follows;
technical API controls stay disclosed until needed. Forms work without JavaScript.

**FORM:** Narrow extension of the incumbent account destinations; user-pinned
Google-only implementation, inherited seed `6b2b9e52`, code-led.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries

MariaDB owns accounts and hashed credentials. Interest identity defaults private.
Preferences do not create event interest, personalization, or delivered reminders.
Public profiles contain only the local ID, display name, and bio. No new global
design tokens ship. Google sign-in is a scoped provider-brand exception using
Google's unmodified gradient G raster and self-hosted Google Sans Medium; the
surrounding account UI retains Manrope and the incumbent world.
