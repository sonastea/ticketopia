# Feature backlog

Status: candidate product features for when new feature work resumes, ordered
by recommendation. The [delivery checklist](delivery.md#current-focus-use-refine-and-verify)
still owns verification priorities; nothing here is implemented until its guide
says so. Check an item only when its user-facing behavior ships and is verified.

## 1. Agents and other clients

Ticketopia's users find events in consumer chat apps (Claude, ChatGPT) more
than in editors, and those apps connect only through OAuth. See the
[App MCP design](../design/app-mcp.md).

- [ ] **OAuth sign-in for `/mcp` with Client ID Metadata Documents (CIMD)** and
  `read`/`write` scopes: authorization-server and protected-resource metadata,
  authorization code + PKCE (S256) through the existing Google sign-in, a consent
  page, rotating refresh tokens, and connected-agent revocation on the account page.
  Implemented, on by default with accounts, with sign-in required for every
  `/mcp` request ([guide](../app-mcp.md#oauth-sign-in));
  check this once real clients connect to a public deployment (below).
- [ ] Dynamic Client Registration as a deprecated compatibility fallback, only if a
  client we need (for example Cursor) cannot use CIMD or a pre-registered client.
- [ ] Token revocation endpoint (RFC 7009) so clients can disconnect themselves.
  Implemented at `/oauth/revoke` ([guide](../app-mcp.md#oauth-sign-in)); check
  this once a real client disconnects through it on a public deployment.
- [ ] Agent tools for personalized radar (`my_radar`) and artist/venue follows,
  then Interested; the APIs exist, so each tool wraps a shared service.
- [ ] Default `find_events` to the signed-in account's remembered city.
- [ ] Connect Claude (web and Code), ChatGPT, VS Code, Cursor and OpenCode to a
  public HTTPS deployment before calling agent sign-in done.
- [ ] Close the "manage personal and community activity from another client" goal:
  add `/events/{id}/community` and `/users/{id}/activity`, then exercise the
  community API from a second client.

## 2. Community (optional, wait for a need)

From [event communities](event-communities.md#small-goals-one-at-a-time).

- [ ] Going/Went records with visibility controls, distinct from Interested and
  never implying verified attendance.
- [ ] Post-event Went prompts and structured reflections in the same discussions.

## 3. Discovery explorations

- [ ] [Discovery passport and pick swaps](../ideas/discovery-games.md) as a
  return-visit experiment.
- [ ] [Venue and price insights](../ideas/venue-insights-and-discovery.md) from
  dated event history, with the documented caveats about partial observations.

## Out of scope

Weekly digests, price or change alerts, direct messages and real-time chat,
managed groups and map discovery stay out unless the product scope changes.
