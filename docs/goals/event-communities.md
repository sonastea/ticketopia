# Positive event communities

Status: Core planned product feature, alongside
[personal radar and reminders](personal-radar.md). These are user-outcome goals
for upcoming work.

## Goal

Give each event a place where people can participate, discuss, connect, and
discover together. Help someone decide whether an event is for them, feel prepared
to go, and share their experience afterward.

**Discover -> express interest -> discuss -> appreciate help -> make a plan ->
return and share.**

## Core experience

- **Interested:** reversible event interest, distinct from a private Save and a
  public Recommend. Interest identity is private by default, with explicit opt-in
  for public profiles/participant lists; aggregate counts may include private
  interests. Interest never establishes ticket ownership or confirmed attendance.
- **Recommend:** a public event endorsement with an optional short reason; people
  can edit or withdraw it. Show who endorsed an event without turning their
  endorsement into a post reaction, save, or attendance declaration.
- **Comments and replies:** each event has a conversation for questions, venue
  tips, recommendations, and shared experiences. People can edit or remove their
  own contributions.
- **Helpful thumbs-up:** a positive reaction thanks someone for a useful comment
  or reply. A person can add or remove their reaction. There are no thumbs-down
  reactions or negative voting scores.
- **Later — Follow the conversation:** people can return to replies and useful activity
  on discussions they choose to follow, with control over notifications.
- **Later — Going, Went and reflections:** people can declare plans and, after an event, record self-reported
  attendance and share memories or advice that may help future visitors.

Event-specific threads are asynchronous, not live chat. City/category community
spaces collect recommendations and conversations around existing events; group
membership/administration is not required. MVP includes basic reporting and owner
moderation before a public pilot. See [design guidelines](../design-guidelines.md)
for screen structures, responsive parity, and the authoritative action hierarchy.

## Small goals, one at a time

| Step | Goal | What success looks like |
| --- | --- | --- |
| 1 | Express interest in one show. | Someone marks Interested, sees their choice, and can change or remove it. |
| 2 | Recommend an event. | Someone publishes an explicit endorsement, optionally explains why, and can edit or withdraw it independently of Interested. |
| 3 | Ask one useful question. | A question appears in the right event conversation with enough context for others to answer. |
| 4 | Help another person. | Someone replies with relevant information that the question's author finds useful. |
| 5 | Acknowledge that help. | The recipient gives a Helpful thumbs-up, and the contributor can see that their answer helped. |
| 6 | Report a concern privately. | Someone reports a comment or reply, gives a reason, and receives acknowledgment. |
| 7 | Resolve a reported concern. | A moderator reviews the context, makes a proportionate decision, and communicates the outcome appropriately. |
| 8 | Return for a conversation. | Someone returns through an event or direct thread link; followed-discussion notifications are a later enhancement. |
| 9 | Contribute after attending. | Someone shares a reflection or venue tip in a discussion; structured Went prompts are later. |
| Later | Share attendance plans. | Someone marks Going/Went and understands visibility and that attendance is self-reported. |

Verify these outcomes on mobile as well as desktop; tablet context must collapse
without losing event/thread identity. A private save should never publish social
activity, and Helpful should never increment event interest/recommendations.

**First community milestone:** a useful question gets an answer and a positive
acknowledgment. Reporting and moderator review should also be usable for the
first public pilot. Start around a small set of shows in one well-covered city
so participants encounter each other in the same conversations.

## A positive culture

Encourage curiosity, practical help, encouragement, and respectful conversation.
Useful prompts include:

- "Has anyone seen this artist live?"
- "What is the balcony view like?"
- "Anyone else planning to go solo?"
- "What would help a first-time visitor to this venue?"
- "What was your favourite moment?"

Honest mixed experiences and constructive criticism are welcome. Community rules
focus on respectful treatment of people and relevant contributions. Comments
should avoid harassment, spam, scams, and sharing another person's private
information.

## Moderation, starting small

Use a simple, primarily report-driven review process. Ordinary comments appear
without routine pre-approval; the site owner can act as the initial moderator.

1. **Report:** a person flags a comment or reply with a reason such as spam,
   abusive behaviour, private information, or another concern, plus optional
   context. The report and reporter's identity are private to the reporter and
   authorized moderators.
2. **Review:** a moderator sees the contribution, its conversation, and the
   reported concern. Reports request review; they are not negative votes, and
   report volume alone does not determine removal.
3. **Respond proportionately:** keep the contribution when it fits the rules;
   otherwise hide/remove it with a reason. Repeated spam or abusive behaviour
   can lead to a warning or a temporary participation restriction.
4. **Close the loop:** the reporter can see that the report was reviewed. An
   affected author receives the reason for an action and can request a second
   review. Reporter identities and private case notes stay out of public threads.

Keep a record of moderation decisions so mistakes can be reviewed and hidden
content restored when appropriate. A removed contribution should leave enough
context for the remaining replies to make sense.

## Reasons to return and connect

- A question received a useful answer.
- Someone appreciated a contribution.
- Relevant participants shared a plan or a new tip in a followed discussion.
- A saved event changed, or its on-sale moment is approaching.
- After attending, there is a shared experience to discuss.

Participation should help someone decide, prepare, discover, or feel connected.
Look for useful answers, repeat contributors, and voluntary returns to a
conversation. Ask people what they gained from interacting. Review whether
reported concerns receive clear and fair outcomes as part of the pilot.

## Connection to other goals

The radar brings relevant people and shows together; event communities give
them a reason to participate. Both should be available through the shared
[client API](../design/radar-api.md).

The [discovery passport and game ideas](../ideas/discovery-games.md) can later
recognize meaningful exploration and helpful contributions. These remain
optional experiments around the core community experience.
