// Live replies load: many people taking part in open conversations at once
// (docs/load-testing.md#live-replies). Each participant VU holds one thread
// open and checks it for new replies at the browser's pace, as discussions.js
// does, while writers add replies across the threads. It measures the live
// check's latency and how long a new reply takes to be noticed.
//
// Local populated review server only (it writes), started with a raised
// per-account limit, because participants share the 40 member sessions:
//   DISCUSSION_LIVE_CHECKS_PER_MINUTE=6000 make review-server scenario=populated
//
// Environment: MANIFEST, PARTICIPANTS (VUs, default 50), THREADS (default 10),
// POLL (seconds between a participant's checks, default 10), WRITE_EVERY
// (seconds between replies across all threads, default 10), RAMP (default
// 10s), DURATION (held, default 1m), P95 (live check limit in ms, default 300).
// The benchmark also sets THREADS_JSON (threads it seeded once for every step)
// and WRITE_OFFSET (replies earlier steps wrote, so writers keep rotating).
import http from 'k6/http';
import { check, fail, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Trend } from 'k6/metrics';

const manifest = JSON.parse(open(__ENV.MANIFEST || fail('set MANIFEST to a populated review server manifest.json')));
const base = manifest.base_url;
if (!/^http:\/\/(127\.0\.0\.1|\[::1\]):\d+$/.test(base)) {
	fail(`refusing non-loopback target ${base}: the live replies load writes`);
}
if (manifest.scenario !== 'populated' || !Array.isArray(manifest.members) || manifest.members.length === 0) {
	fail(`start the review server with scenario=populated, not ${manifest.scenario}`);
}
const members = manifest.members;
const number = (name, fallback, min) => {
	const v = Number(__ENV[name] || fallback);
	if (!(v >= min)) fail(`${name} must be at least ${min}, not ${__ENV[name]}`);
	return v;
};
const participants = number('PARTICIPANTS', 50, 1);
const threads = Math.min(number('THREADS', 10, 1), members.length);
const poll = number('POLL', 10, 1);
const writeEvery = number('WRITE_EVERY', 10, 1);
const p95 = number('P95', 300, 1);

export const options = {
	maxRedirects: 0,
	scenarios: {
		participants: {
			executor: 'ramping-vus',
			exec: 'participant',
			startVUs: 0,
			stages: [{ target: participants, duration: __ENV.RAMP || '10s' }, { target: participants, duration: __ENV.DURATION || '1m' }],
			gracefulRampDown: '5s',
		},
		writers: {
			executor: 'constant-arrival-rate',
			exec: 'writer',
			rate: 1,
			timeUnit: `${writeEvery}s`,
			duration: __ENV.DURATION || '1m',
			startTime: __ENV.RAMP || '10s',
			preAllocatedVUs: 2,
			maxVUs: 8,
		},
	},
	thresholds: {
		// Capacity gates apply to checks started while the load was held, not
		// the ramp before it or the grace period after it.
		'http_req_duration{kind:live,phase:held}': [`p(95)<${p95}`],
		'http_req_failed{kind:live,phase:held}': ['rate<0.01'],
		'http_reqs{kind:live}': ['count>=0'],
		'http_req_failed{kind:write}': ['rate<0.01'],
		// A reply should be noticed within one check interval (jittered up to
		// 1.2×) plus a few seconds of slack.
		live_noticed_after: [`p(95)<${Math.round((poll * 1.2 + 3) * 1000)}`],
		// Replies were written and noticed; a run that wrote or noticed none
		// measured nothing.
		live_replies_seen: ['count>0'],
		'dropped_iterations{scenario:writers}': ['count<1'],
		'http_reqs{kind:write}': ['count>0'],
		// Checks made while the load was held, for the benchmark's achieved rate.
		live_checks_held: ['count>=0'],
		checks: ['rate>0.99'],
	},
};

// Time from each reply's creation to the check that first listed it.
const noticed = new Trend('live_noticed_after', true);
const seen = new Counter('live_replies_seen');
const held = new Counter('live_checks_held');
const durationMs = (value, fallback) => {
	const m = /^(\d+(?:\.\d+)?)(s|m)$/.exec(value || fallback);
	return m ? Number(m[1]) * (m[2] === 'm' ? 60000 : 1000) : NaN;
};
const rampMs = durationMs(__ENV.RAMP, '10s');
const holdMs = durationMs(__ENV.DURATION, '1m');
// Whether now is within the participants' held load, from their scenario's start.
const holding = () => {
	const t = Date.now() - exec.scenario.startTime;
	return t >= rampMs && t < rampMs + holdMs;
};

function headers(member, extra = {}) {
	return { Cookie: `${manifest.session_cookie}=${member.session}`, 'Accept-Encoding': 'gzip, deflate, br, zstd', ...extra };
}
function csrfFor(member) {
	const res = http.get(`${base}/api/v1/me`, { headers: headers(member, { Accept: 'application/json' }), tags: { kind: 'setup' } });
	return res.status === 200 ? res.json('csrf_token') : '';
}
function post(member, csrf, fields, kind) {
	const form = Object.entries({ ...fields, csrf_token: csrf }).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&');
	return http.post(`${base}/discussion-actions`, form, {
		headers: headers(member, { Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded', Origin: base, 'Sec-Fetch-Site': 'same-origin' }),
		tags: { kind, name: kind === 'write' ? 'reply' : 'thread' },
	});
}
const key = () => `${Date.now().toString(36)}${Math.random().toString(36).slice(2)}`.padEnd(16, '0').slice(0, 32);

// One fresh thread per member up to THREADS, on the manifest's events, unless
// the benchmark seeded them once for every step.
export function setup() {
	if (__ENV.THREADS_JSON) return { threads: JSON.parse(__ENV.THREADS_JSON), csrf: members.map(csrfFor) };
	const out = [];
	for (let i = 0; i < threads; i++) {
		const member = members[i];
		const csrf = csrfFor(member);
		const event = manifest.event_ids[i % manifest.event_ids.length];
		const res = post(member, csrf, { action: 'create', event_id: event, body: `Live replies load thread ${i + 1}`, idempotency_key: key(), return_to: `/events/${encodeURIComponent(event)}/discussions` }, 'setup');
		const location = res.status === 200 ? res.json('location') : '';
		const id = /\/discussions\/([0-9a-f]{32})/.exec(location || '');
		if (!id) fail(`could not start thread ${i + 1}: ${res.status} ${res.body}`);
		out.push({ event, id: id[1] });
	}
	return { threads: out, csrf: members.map(csrfFor) };
}

// Per-VU state: the open thread's as_of and the replies already noticed.
let since = '';
const noticedIDs = new Set();

export function participant(data) {
	const vu = exec.vu.idInTest;
	const member = members[(vu - 1) % members.length];
	const thread = data.threads[(vu - 1) % data.threads.length];
	if (!since) {
		since = new Date(Date.now() - 1000).toISOString();
		// Spread participants over the first interval, as page loads would be.
		sleep(Math.random() * poll);
	}
	const inHold = holding();
	const res = http.get(`${base}/events/${encodeURIComponent(thread.event)}/discussions/${thread.id}/new-replies?since=${encodeURIComponent(since)}`, {
		headers: headers(member, { Accept: 'application/json' }),
		tags: { kind: 'live', name: 'new-replies', phase: inHold ? 'held' : 'ramp' },
	});
	let body = null;
	try {
		body = res.status === 200 ? res.json() : null;
	} catch (_) {
		body = null;
	}
	check(res, { 'live check answered': () => body !== null && Array.isArray(body.replies) && Array.isArray(body.recent) });
	if (inHold) held.add(1);
	for (const reply of [...(body?.recent || []), ...(body?.replies || [])]) {
		if (noticedIDs.has(reply.id)) continue;
		noticedIDs.add(reply.id);
		// Replies from before this participant opened the thread aren't new to it.
		if (Date.parse(reply.created_at) < Date.parse(since)) continue;
		noticed.add(Date.now() - Date.parse(reply.created_at));
		seen.add(1);
	}
	// The browser's active pace with its ±20% jitter.
	sleep(poll * (0.8 + Math.random() * 0.4));
}

export function writer(data) {
	const i = exec.scenario.iterationInTest + Number(__ENV.WRITE_OFFSET || 0);
	const thread = data.threads[i % data.threads.length];
	// Writers rotate through every member, keeping each far under the
	// server's 20 contributions per ten minutes.
	const m = (i * 7 + 3) % members.length;
	const res = post(members[m], data.csrf[m], {
		action: 'create', event_id: thread.event, thread_id: thread.id, parent_id: thread.id,
		body: `Live reply ${i + 1}`, idempotency_key: key(), return_to: `/events/${encodeURIComponent(thread.event)}/discussions/${thread.id}`,
	}, 'write');
	check(res, { 'reply published': (r) => r.status === 200 });
}
