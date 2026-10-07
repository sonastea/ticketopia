# Personal radar and reminders

Status: Core product direction. [City/date/category/genre discovery](../discovery.md) is
implemented with cached Ticketmaster events and metadata. [Private saves](../saved-events.md)
retain bookmarks and last-known details. Follows,
personalized matches, reminders, and human pilot checkpoints remain upcoming work.
Google accounts, profiles/privacy defaults, and private category/location/
notification preferences are [implemented, opt-in](../accounts.md).
The [delivery checklist](delivery.md) aligns backend and frontend tasks.
Discovery now starts with a chosen, remembered, or approximate IP-based city;
see [location behavior](../location.md).

Discovery supports all Ticketmaster event categories. Private category preferences
are stored now; private saved events remain planned for the first release alongside
community interest and recommendations. Follows, personalized ranking, and reminders remain agreed
subsequent goals, not prerequisites for the responsive community MVP. See the
[release boundary](delivery.md#mvp-release-boundary) and
[design guidelines](../design-guidelines.md) for the current delivery direction.

## Goal

Help someone discover relevant events nearby, save the ones they care about,
and remember when tickets go on sale or plans change.

The original live-music audience remains supported, alongside people exploring
sports, arts, and other Ticketmaster categories who miss announcements or do not
want to regularly browse listings. The first experience should be useful to one person:

**Choose a city and interests -> discover -> save -> receive a useful reminder.**

Saving is private personal organization, not an Interested signal or public
recommendation. Reminders are explicitly configured, not implied by any social
action. See [interaction semantics](../design-guidelines.md#interaction-semantics-and-hierarchy).

Deliver that experience through a shared backend that can also serve a mobile
app, another personal app, or an analysis client. See the [API draft](../design/radar-api.md).

[Positive event communities](event-communities.md) are the companion core feature:
people will be able to express interest, discuss events, appreciate helpful contributions,
and connect around a shared event.

## Small goals, one at a time

Each step describes something a person should be able to accomplish. Completion
means observing that outcome with a real person and understanding their feedback.

| Step | Goal | What success looks like |
| --- | --- | --- |
| 1 | Find one event worth considering. | Someone can name a relevant event nearby and understand its date, venue, and known price information. |
| 2 | Keep that discovery for later. | They save an event, leave, and can find it again when they return. |
| 3 | Express an ongoing interest. | They follow one artist or venue and understand why related events appear in their radar. |
| 4 | Receive one useful reminder. | They choose a known upcoming on-sale moment, receive a timely reminder, and can change or stop future reminders. |
| 5 | Have a reason to return. | On a later visit, they find a relevant new match or meaningful update to something they saved. |
| 6 | Join an event community. | They express interest or a plan to attend and find a relevant conversation about the event. |
| 7 | Participate helpfully. | They ask or answer a question and can acknowledge useful contributions with a positive reaction. |
| 8 | Return for useful activity. | They voluntarily return for a reply, relevant discussion, or an experience shared after the event. |

Steps 1-5 cover personal radar and reminders. Steps 6-8 connect that experience
to the [core community goals](event-communities.md), which have their own small
milestones and reporting/moderation expectations. Passport and quest ideas remain
optional discovery experiments.

**First checkpoint:** try steps 1-2 with three people in one well-covered city.
Ask which event they would consider and whether saving it helps across the
chosen categories and device sizes. Expand to a small community MVP pilot of
roughly 10-20 people once its release criteria are met; test reminder steps
separately when delivered. These are learning
checkpoints, not claims of product-market fit.

## What the radar should feel like

- A short list of roughly three to five relevant events, with understandable
  reasons such as an artist, genre, or venue the person likes.
- Enough information to decide whether an event fits: the event date, venue,
  source ticket link, and any known advertised price or public on-sale date.
  Event dates and sale dates are distinct; unknown dates/prices are explicit.
- Useful results early in the first visit, followed by an easy way to keep them.
- A weekly discovery digest and meaningful updates at a pace chosen by the user.

## Reminder promises

- Timing makes sense in the user's time zone, including daylight-saving changes.
- An on-sale reminder requires a known future sale time. Unknown times remain
  pending; saving an already-on-sale event does not create a late sale reminder.
- Date, venue, cancellation, and postponement updates describe what changed.
  Seeing an event for the first time is distinct from detecting a later change.
- Changed sale dates update pending reminders. Cancellation, removal, or a pause
  prevents obsolete reminders from being sent.
- Each notification corresponds to a distinct intended reminder or meaningful
  change. Delivery continues while the app is closed; email is the first channel.

## Reasons to come back or involve someone else

The working hypothesis is that people return for relevant new information,
personal discoveries, or a useful contribution from another person.

The core social purpose is **"help each other decide, prepare, and enjoy an event"**.
Event conversations let people ask questions, share tips, express plans, and
appreciate help. Replies and useful updates give participants a reason to return.
Positive-only reactions and a private reporting process support that experience.

See [event communities](event-communities.md) for the core goals and
[discovery games](../ideas/discovery-games.md) for optional passport and pick-swap
experiments. Aim for meaningful returns alongside timely event updates; event
discovery naturally has quieter periods.

## Learn before expanding

Look for relevant saves, useful reminders, a second discovery session, and
contributions that another person found helpful. Ask what prompted the return
visit. A ticket-link click expresses interest; it does not establish a purchase
or attendance. Earning a stamp alone does not establish a useful discovery.

If relevance is weak, improve the radar. If reminders are the strongest benefit,
refine watchlists. If useful conversations bring people back, strengthen that
community experience. Choose the next small goal from observed value and feedback.

## Related goals and design notes

- **Cross-client continuity:** the same person should be able to access their
  radar, saved events, reminder preferences, and community participation from the
  web and another client.
  Technical proposals live in the [API draft](../design/radar-api.md).
- **Participate and connect:** [event communities](event-communities.md) cover
  positive reactions, conversations, shared plans, and proportionate moderation.
- **Understand the local scene:** event observations may support
  [venue and price insights](../ideas/venue-insights-and-discovery.md).
- **Make discovery playful:** the [game ideas](../ideas/discovery-games.md) are
  hypotheses to try after the core experience offers useful results.
