// k6 baseline for web, /api/v1 and /mcp traffic against a local review server
// (make review-server scenario=discover-last-known, then make load). See
// docs/load-testing.md. It refuses non-loopback targets: never point load at
// production, a shared deployment or real Ticketmaster.
import http from 'k6/http';
import { check, fail } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate } from 'k6/metrics';

const manifest = JSON.parse(open(__ENV.MANIFEST || fail('set MANIFEST to a review server manifest.json')));
const base = manifest.base_url;
if (!/^http:\/\/(127\.0\.0\.1|\[::1\]):\d+$/.test(base)) {
	fail(`refusing non-loopback target ${base}`);
}
// Its stored Discover lists and loopback upstream serve searches without a
// provider key; other scenarios have no searchable events.
if (manifest.scenario !== 'discover-last-known') {
	fail(`start the review server with scenario=discover-last-known, not ${manifest.scenario}`);
}
// Paths must stay on the target: no scheme-relative or absolute URLs.
for (const path of [manifest.paths.discover, manifest.paths.mcp]) {
	if (typeof path !== 'string' || !path.startsWith('/') || path.startsWith('//')) {
		fail(`unexpected manifest path ${path}`);
	}
}
const events = manifest.event_ids;
const owner = manifest.accounts.owner.api_token;
const other = manifest.accounts.other.api_token;
// The writer's event: saved and reminded repeatedly, idempotently.
const written = events[5];

const duration = __ENV.DURATION || '1m';
// RATE scales every scenario's arrival rate (iterations per second) and its
// VU budget, so drops reflect the server rather than too few generators.
const scale = Number(__ENV.RATE || 1);
if (!(scale > 0)) {
	fail(`RATE must be a positive number, not ${__ENV.RATE}`);
}
const arrivals = (rate, vus) => ({
	executor: 'constant-arrival-rate',
	rate: Math.max(1, Math.round(rate * scale)),
	timeUnit: '1s',
	duration,
	preAllocatedVUs: Math.ceil(vus * scale),
	maxVUs: Math.ceil(vus * 4 * scale),
});

// Weights follow an agent session's flow: search, open, save, set a reminder.
export const options = {
	// Redirects could leave loopback; the app's responses here never redirect.
	maxRedirects: 0,
	scenarios: {
		web: { ...arrivals(10, 10), exec: 'web' },
		api: { ...arrivals(10, 10), exec: 'api' },
		mcp_public: { ...arrivals(10, 10), exec: 'mcpPublic' },
		mcp_signed_in: { ...arrivals(2, 5), exec: 'mcpSignedIn' },
	},
	thresholds: {
		http_req_failed: ['rate<0.01'],
		checks: ['rate>0.99'],
		mcp_tool_errors: ['count<1'],
		dropped_iterations: ['count<1'],
		'http_req_duration{scenario:web}': ['p(95)<300', 'p(99)<1000'],
		'http_req_duration{scenario:api}': ['p(95)<300', 'p(99)<1000'],
		'http_req_duration{scenario:mcp_public}': ['p(95)<300', 'p(99)<1000'],
		'http_req_duration{scenario:mcp_signed_in}': ['p(95)<300', 'p(99)<1000'],
	},
};

const toolErrors = new Counter('mcp_tool_errors');
// /mcp admission refusals by cause, over /mcp requests only: 429 is a rate
// limit, busy a 503 from the concurrency cap. Other failures are not refusals.
const limited = new Rate('mcp_limited_429');
const busy = new Rate('mcp_busy_503');

function get(path, name, token) {
	const headers = token ? { Authorization: `Bearer ${token}` } : {};
	const res = http.get(base + path, { headers, tags: { name } });
	check(res, { [`${name} 200`]: (r) => r.status === 200 });
	return res;
}

const isObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);

// Stateless MCP: each tools/call is one plain POST, without initialize. The
// call passes only with a JSON-RPC result whose structured content satisfies
// valid; it returns that content or null, and never throws.
function tool(name, args, token, valid = () => true) {
	const headers = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' };
	if (token) headers.Authorization = `Bearer ${token}`;
	const body = JSON.stringify({ jsonrpc: '2.0', id: exec.scenario.iterationInTest, method: 'tools/call', params: { name, arguments: args } });
	const res = http.post(base + manifest.paths.mcp, body, { headers, tags: { name: `mcp ${name}` } });
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

const hasItems = (c) => Array.isArray(c.items) && c.items.length > 0 && c.items.every(isObject);
// Search results are used afterwards, so every item needs a usable ID.
const hasEvents = (c) => hasItems(c) && c.items.every((e) => typeof e.id === 'string' && e.id !== '');
const pick = () => events[exec.scenario.iterationInTest % events.length];

export function web() {
	get(manifest.paths.discover, 'page discover');
	get(`/events/${pick()}`, 'page event');
}

export function api() {
	get('/api/v1/events?city=Chicago&country=US', 'api events');
	get(`/api/v1/events/${pick()}`, 'api event');
	get('/api/v1/me/saved-events', 'api saved', owner);
}

export function mcpPublic() {
	const found = tool('find_events', { city: 'Chicago', country: 'US', limit: 5 }, null, hasEvents);
	const id = found ? found.items[exec.scenario.iterationInTest % found.items.length].id : pick();
	tool('event_details', { event_id: id }, null, (c) => isObject(c.item) && c.item.id === id);
}

export function mcpSignedIn() {
	tool('my_saved_events', {}, owner, hasItems);
	tool('save_event', { event_id: written }, other, (c) => c.saved === true);
	tool('set_reminder', { event_id: written, lead_minutes: 60 }, other, (c) => c.lead_minutes === 60);
	tool('my_reminders', {}, other, hasItems);
}
