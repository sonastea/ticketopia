// k6 traffic mix of pages, /api/v1 and /mcp. See docs/load-testing.md.
//
// Local (default): a discover-last-known review server from MANIFEST, at
// steady rates, including idempotent signed-in writes on fixture accounts.
//
// Remote: TARGET=https://host (a dev server or production). Read-only: one
// fixed search, identical for pages, API and tools, and a few of its events,
// so most responses come from the discovery cache; signed-in reads only with
// TOKEN; never writes. Rates are gentle and ramp up, and the run aborts when
// errors, failed checks or latency exceed the thresholds.
import http from 'k6/http';
import { check, fail } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate } from 'k6/metrics';

const remote = Boolean(__ENV.TARGET);
const list = (value) => (value || '').split(',').map((s) => s.trim()).filter(Boolean);

let base, manifest;
if (remote) {
	base = __ENV.TARGET.replace(/\/+$/, '');
	if (!/^https?:\/\/[^/?#@]+$/.test(base)) {
		fail(`TARGET must be an origin such as https://ticketopia.example, not ${__ENV.TARGET}`);
	}
} else {
	manifest = JSON.parse(open(__ENV.MANIFEST || fail('set MANIFEST to a review server manifest.json, or TARGET for a remote server')));
	base = manifest.base_url;
	if (!/^http:\/\/(127\.0\.0\.1|\[::1\]):\d+$/.test(base)) {
		fail(`refusing non-loopback manifest target ${base}; set TARGET to aim at a remote server`);
	}
	// Its stored Discover lists and loopback upstream serve searches without a
	// provider key; other scenarios have no searchable events.
	if (manifest.scenario !== 'discover-last-known') {
		fail(`start the review server with scenario=discover-last-known, not ${manifest.scenario}`);
	}
	// Paths must stay on the target: no scheme-relative or absolute URLs.
	for (const path of [manifest.paths.mcp]) {
		if (typeof path !== 'string' || !path.startsWith('/') || path.startsWith('//')) {
			fail(`unexpected manifest path ${path}`);
		}
	}
}

const mcpPath = remote ? '/mcp' : manifest.paths.mcp;
// One search with explicit dates (today in UTC and 90 days on, the defaults),
// so every request normalizes to the same discovery query and cache entry.
// setup() fixes the dates once; each VU runs init separately, possibly after
// midnight.
function searchFor(now) {
	const day = (offset) => new Date(now + offset * 86400000).toISOString().slice(0, 10);
	return { city: __ENV.CITY || 'Chicago', country: __ENV.COUNTRY || 'US', start_date: day(0), end_date: day(90) };
}
const queryOf = (search) => Object.entries(search).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&');
// Signed-in reads use TOKEN remotely, the owner fixture locally; the
// fixture's other account makes the local idempotent writes.
const reader = remote ? __ENV.TOKEN || '' : manifest.accounts.owner.api_token;
const writer = remote ? '' : manifest.accounts.other.api_token;
// An API token is a full account credential: send it only over HTTPS or to
// this machine, unless explicitly allowed for a trusted network.
if (remote && reader && !/^https:|^http:\/\/(localhost|127\.0\.0\.1|\[::1\])(:\d+)?$/.test(base) && __ENV.ALLOW_HTTP_TOKEN !== '1') {
	fail(`refusing to send TOKEN over plain HTTP to ${base}; use https, or ALLOW_HTTP_TOKEN=1 on a trusted network`);
}
const eventCount = Number(__ENV.EVENTS || 5);
if (!Number.isInteger(eventCount) || eventCount < 1) {
	fail(`EVENTS must be a positive integer, not ${__ENV.EVENTS}`);
}

// Iterations per minute at RATE=1. Remote rates keep /mcp near 40 requests a
// minute from one IP, under the default MCP_RATE_PER_MINUTE of 60.
const rates = remote
	? { web: 30, api: 30, mcp_public: 15, mcp_signed_in: 4 }
	: { web: 600, api: 600, mcp_public: 600, mcp_signed_in: 120 };
const vus = { web: 10, api: 10, mcp_public: 10, mcp_signed_in: 5 };
const exported = { web: 'web', api: 'api', mcp_public: 'mcpPublic', mcp_signed_in: 'mcpSignedIn' };

// SCENARIOS picks a subset, for example web,api where /mcp is not enabled.
const available = Object.keys(rates).filter((name) => name !== 'mcp_signed_in' || reader);
const chosen = __ENV.SCENARIOS ? list(__ENV.SCENARIOS) : available;
for (const name of chosen) {
	if (!rates[name]) fail(`unknown scenario ${name}; choose from ${Object.keys(rates).join(', ')}`);
	if (!available.includes(name)) fail(`${name} needs TOKEN, an API token from the account page`);
}

const duration = __ENV.DURATION || '1m';
const ramp = __ENV.RAMP || (remote ? '1m' : '');
// RATE scales every scenario's arrival rate and its VU budget, so drops
// reflect the server rather than too few generators.
const scale = Number(__ENV.RATE || 1);
if (!(scale > 0)) {
	fail(`RATE must be a positive number, not ${__ENV.RATE}`);
}
function arrivals(name) {
	const rate = Math.max(1, Math.round(rates[name] * scale));
	const budget = { preAllocatedVUs: Math.ceil(vus[name] * scale), maxVUs: Math.ceil(vus[name] * 4 * scale), exec: exported[name] };
	if (!ramp) {
		return { executor: 'constant-arrival-rate', rate, timeUnit: '1m', duration, ...budget };
	}
	return { executor: 'ramping-arrival-rate', startRate: 0, timeUnit: '1m', stages: [{ target: rate, duration: ramp }, { target: rate, duration }], ...budget };
}

const p95 = Number(__ENV.P95 || (remote ? 1000 : 300));
const latency = remote
	? [{ threshold: `p(95)<${p95}`, abortOnFail: true, delayAbortEval: '30s' }, `p(99)<${p95 * 3}`]
	: [`p(95)<${p95}`, `p(99)<${p95 * 3}`];
// Remotely, failed checks (including tool errors, which are HTTP 200) abort too.
const abort = (threshold) => (remote ? [{ threshold, abortOnFail: true, delayAbortEval: '30s' }] : [threshold]);
const thresholds = {
	http_req_failed: abort('rate<0.01'),
	checks: abort('rate>0.99'),
	mcp_tool_errors: ['count<1'],
	dropped_iterations: ['count<1'],
};
for (const name of chosen) {
	thresholds[`http_req_duration{scenario:${name}}`] = latency;
}

// Weights follow an agent session's flow: search, open, save, set a reminder.
export const options = {
	// Redirects could leave the target; give TARGET's canonical origin.
	maxRedirects: 0,
	scenarios: Object.fromEntries(chosen.map((name) => [name, arrivals(name)])),
	thresholds,
};

const toolErrors = new Counter('mcp_tool_errors');
// /mcp admission refusals by cause, over /mcp requests only: 429 is a rate
// limit, busy a 503 from the concurrency cap. Other failures are not refusals.
const limited = new Rate('mcp_limited_429');
const busy = new Rate('mcp_busy_503');

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
const hasItems = (c) => Array.isArray(c.items) && c.items.length > 0 && c.items.every(isObject);
// Search results are used afterwards, so every item needs a usable ID.
const hasEvents = (c) => hasItems(c) && c.items.every((e) => typeof e.id === 'string' && e.id !== '');
// An account may have nothing saved remotely; the list must still be a list.
const isList = (c) => Array.isArray(c.items) && c.items.every(isObject);

function get(path, name, token) {
	const headers = token ? { Authorization: `Bearer ${token}` } : {};
	const res = http.get(base + path, { headers, tags: { name } });
	check(res, { [`${name} 200`]: (r) => r.status === 200 });
	return res;
}

function rpc(method, params, token, name, id) {
	const headers = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' };
	if (token) headers.Authorization = `Bearer ${token}`;
	const body = JSON.stringify({ jsonrpc: '2.0', id, method, params });
	return http.post(base + mcpPath, body, { headers, tags: { name } });
}

// Stateless MCP: each tools/call is one plain POST, without initialize. The
// call passes only with a JSON-RPC result whose structured content satisfies
// valid; it returns that content or null, and never throws.
function tool(name, args, token, valid = () => true) {
	const res = rpc('tools/call', { name, arguments: args }, token, `mcp ${name}`, exec.scenario.iterationInTest);
	limited.add(res.status === 429, { tool: name });
	busy.add(res.status === 503 && String(res.body).includes('busy'), { tool: name });
	let result = null;
	if (res.status === 200) {
		try {
			const reply = res.json();
			result = isObject(reply) && isObject(reply.result) ? reply.result : null;
		} catch (_) {
			// not JSON: fails the check below
		}
	}
	if (result && result.isError) {
		toolErrors.add(1, { tool: name });
	}
	const content = result && !result.isError && isObject(result.structuredContent) ? result.structuredContent : null;
	let ok = false;
	try {
		ok = content !== null && valid(content) === true;
	} catch (_) {
		// a malformed payload fails the check
	}
	check(res, { [`${name} ok`]: () => ok });
	return ok ? content : null;
}

// setup checks the target before load starts and finds the events to open:
// the fixture's locally, the fixed search's first page remotely. It also
// notes whether reminders are enabled, as my_reminders is listed only then.
export function setup() {
	const status = (res) => `${res.request.method} ${res.url.replace(/\?.*/, '')}: ${res.status}`;
	let reminders = false;
	const search = searchFor(Date.now());
	if (chosen.some((name) => name.startsWith('mcp'))) {
		const res = rpc('tools/list', {}, reader, 'setup', 0);
		if (res.status !== 200) {
			fail(`${status(res)}; is MCP_ENABLED=true there? Otherwise set SCENARIOS=web,api`);
		}
		try {
			reminders = res.json().result.tools.some((t) => t.name === 'my_reminders');
		} catch (_) {
			fail(`${status(res)} returned no tool list`);
		}
	}
	if (reader && (chosen.includes('mcp_signed_in') || chosen.includes('api'))) {
		const res = http.get(`${base}/api/v1/me/saved-events`, { headers: { Authorization: `Bearer ${reader}` }, tags: { name: 'setup' } });
		if (res.status !== 200) {
			fail(`${status(res)}; check TOKEN`);
		}
	}
	if (!remote) {
		return { events: manifest.event_ids, reminders, search };
	}
	// Signed-in reads open no events, so they need no search.
	if (!chosen.some((name) => name !== 'mcp_signed_in')) {
		return { events: [], reminders, search };
	}
	const res = http.get(`${base}/api/v1/events?${queryOf(search)}`, { tags: { name: 'setup' } });
	let events = [];
	try {
		events = res.json().items.map((e) => e.id).filter((id) => typeof id === 'string' && id !== '');
	} catch (_) {
		// reported below
	}
	if (res.status !== 200 || events.length === 0) {
		fail(`${status(res)} found no events; set CITY and COUNTRY to a search with results`);
	}
	return { events: events.slice(0, eventCount), reminders, search };
}

const pick = (data) => data.events[exec.scenario.iterationInTest % data.events.length];

export function web(data) {
	get(`/?${queryOf(data.search)}`, 'page discover');
	get(`/events/${encodeURIComponent(pick(data))}`, 'page event');
}

export function api(data) {
	get(`/api/v1/events?${queryOf(data.search)}`, 'api events');
	get(`/api/v1/events/${encodeURIComponent(pick(data))}`, 'api event');
	if (reader) {
		get('/api/v1/me/saved-events', 'api saved', reader);
	}
}

export function mcpPublic(data) {
	const found = tool('find_events', data.search, '', hasEvents);
	// Remotely, open only the setup events, so details stay cached.
	const id = found && !remote ? found.items[exec.scenario.iterationInTest % found.items.length].id : pick(data);
	tool('event_details', { event_id: id }, '', (c) => isObject(c.item) && c.item.id === id);
}

export function mcpSignedIn(data) {
	tool('my_saved_events', {}, reader, remote ? isList : hasItems);
	if (remote) {
		if (data.reminders) {
			tool('my_reminders', {}, reader, isList);
		}
		return;
	}
	// The writer's event: saved and reminded repeatedly, idempotently.
	const written = data.events[5];
	tool('save_event', { event_id: written }, writer, (c) => c.saved === true);
	tool('set_reminder', { event_id: written, lead_minutes: 60 }, writer, (c) => c.lead_minutes === 60);
	tool('my_reminders', {}, writer, hasItems);
}
