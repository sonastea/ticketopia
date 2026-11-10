// Simulated signed-in users against a local populated review server
// (make review-server scenario=populated, then make simulate-users). See
// docs/load-testing.md. Each VU is one member with its own browser
// session; visits browse and act through the same form posts the browser's
// scripts send (session cookie, CSRF token, same-origin headers), with think
// time between steps. It writes, so it refuses anything but a loopback
// populated review server.
import http from 'k6/http';
import { check, fail, sleep } from 'k6';
import exec from 'k6/execution';
import { Counter, Rate } from 'k6/metrics';

const manifest = JSON.parse(open(__ENV.MANIFEST || fail('set MANIFEST to a populated review server manifest.json')));
const base = manifest.base_url;
if (!/^http:\/\/(127\.0\.0\.1|\[::1\]):\d+$/.test(base)) {
	fail(`refusing non-loopback target ${base}: simulated users write`);
}
if (manifest.scenario !== 'populated' || !Array.isArray(manifest.members) || manifest.members.length === 0) {
	fail(`start the review server with scenario=populated, not ${manifest.scenario}`);
}
const members = manifest.members;
const users = Number(__ENV.USERS || 20);
if (!Number.isInteger(users) || users < 1 || users > members.length) {
	fail(`USERS must be an integer from 1 to ${members.length}, the number of members`);
}
// Think time between steps, in seconds, varied from half to one and a half times.
const think = Number(__ENV.THINK || 3);
if (!(think >= 0)) {
	fail(`THINK must be a non-negative number of seconds, not ${__ENV.THINK}`);
}

export const options = {
	maxRedirects: 0,
	scenarios: {
		users: {
			executor: 'ramping-vus',
			startVUs: 0,
			stages: [{ target: users, duration: __ENV.RAMP || '30s' }, { target: users, duration: __ENV.DURATION || '2m' }],
			gracefulRampDown: '10s',
		},
	},
	thresholds: {
		http_req_failed: ['rate<0.01'],
		checks: ['rate>0.99'],
		'http_req_duration{kind:page}': ['p(95)<500'],
		'http_req_duration{kind:action}': ['p(95)<300'],
		'http_req_duration{kind:read}': ['p(95)<300'],
		// Always-true thresholds, so the summary counts each action.
		...Object.fromEntries(['save', 'interest', 'recommend', 'follow venue', 'new thread', 'reply', 'helpful', 'follow thread']
			.map((action) => [`user_actions{action:${action}}`, ['count>=0']])),
		posts_limited: ['count>=0'],
		posts_refused: ['rate<0.5'],
	},
};

const actions = new Counter('user_actions');
// Posts refused by the per-account limit (code rate_limited), as a person
// posting too often would be; expected when runs follow each other within ten
// minutes. A member stops posting once refused, so a healthy run refuses few
// attempts; posts_refused over every attempt catches posting that is broken.
const postsLimited = new Counter('posts_limited');
const postsRefused = new Rate('posts_refused');
const postStatuses = http.expectedStatuses(200, 429);

// Per-VU state: the member, their CSRF token, and their recent posts,
// kept under the server's 20 contributions per 10 minutes per account.
const me = () => members[(exec.vu.idInTest - 1) % members.length];
let csrf = '';
const posted = [];
const postBudget = { count: 15, window: 10 * 60 * 1000 };

function headers(extra = {}) {
	return { Cookie: `${manifest.session_cookie}=${me().session}`, ...extra };
}

function page(path, name) {
	const res = http.get(base + path, { headers: headers({ Accept: 'text/html', 'Sec-Fetch-Site': 'same-origin' }), tags: { name, kind: 'page' } });
	check(res, { [`${name} 200`]: (r) => r.status === 200 });
	return res;
}

function read(path, name) {
	const res = http.get(base + path, { headers: headers({ Accept: 'application/json' }), tags: { name, kind: 'read' } });
	const body = res.status === 200 ? json(res) : null;
	check(res, { [`${name} 200`]: () => body !== null });
	return body;
}

// What each JSON action must return, as the browser's scripts read it.
const confirms = {
	save: (b) => typeof b.saved === 'boolean' && typeof b.event_id === 'string',
	interest: (b) => typeof b.event_id === 'string' && 'state' in b,
	recommend: (b) => typeof b.event_id === 'string' && typeof b.notice === 'string',
	'new thread': (b) => typeof b.location === 'string' && b.location.includes('/discussions/'),
	reply: (b) => typeof b.location === 'string' && b.location.includes('post_id='),
	helpful: (b) => typeof b.notice === 'string',
};

const json = (res) => {
	try {
		const body = res.json();
		return body !== null && typeof body === 'object' ? body : null;
	} catch (_) {
		return null;
	}
};

// form posts as the browser's scripts do; follow forms answer with a
// redirect rather than JSON.
function act(path, name, fields, redirect = false) {
	const form = Object.entries({ ...fields, csrf_token: csrf }).map(([k, v]) => `${k}=${encodeURIComponent(v)}`).join('&');
	const posting = fields.action === 'create';
	const params = {
		headers: headers({ Accept: 'application/json', 'Content-Type': 'application/x-www-form-urlencoded', Origin: base, 'Sec-Fetch-Site': 'same-origin' }),
		tags: { name, kind: 'action' },
	};
	// Set only for posts: an undefined callback would stop failure tracking.
	if (posting) params.responseCallback = postStatuses;
	const res = http.post(base + path, form, params);
	const body = redirect ? null : json(res);
	const limited = posting && res.status === 429 && body !== null && body.code === 'rate_limited';
	if (posting) postsRefused.add(limited);
	if (limited) {
		// The user waits out the limit window before posting again.
		postsLimited.add(1);
		while (posted.length < postBudget.count) posted.push(Date.now());
		return null;
	}
	const ok = redirect ? res.status === 303 : res.status === 200 && body !== null && confirms[name](body);
	check(res, { [`${name} ok`]: () => ok });
	if (!ok) {
		console.warn(`${me().display_name}: ${name} ${res.status} ${body && body.detail ? body.detail : ''}`);
		return null;
	}
	actions.add(1, { action: name });
	return body;
}

const rand = (n) => Math.floor(Math.random() * n);
const pick = (items) => items[rand(items.length)];
const pause = () => sleep(think * (0.5 + Math.random()));
const key = () => `load-${exec.vu.idInTest}-${Date.now()}-${rand(1e9)}`;

function canPost() {
	const now = Date.now();
	while (posted.length && now - posted[0] > postBudget.window) posted.shift();
	return posted.length < postBudget.count;
}

const lines = [
	'Has anyone been to this venue before? Wondering about the sightlines.',
	'Going with a few friends, happy to share tips on parking nearby.',
	'The last show here was great, the sound was much better than expected.',
	'Is there a bag policy for this one?',
	'Doors usually open an hour early, worth getting there on time.',
];

// setup finds each event's venue, for follows, from the public API.
export function setup() {
	const venues = {};
	for (const id of manifest.event_ids) {
		const res = http.get(`${base}/api/v1/events/${encodeURIComponent(id)}`);
		try {
			const venue = res.json().item.venues[0];
			if (venue && venue.id) venues[id] = venue.id;
		} catch (_) {
			// events without venues are not followed
		}
	}
	return { events: manifest.event_ids, venues };
}

// One visit: Discover, an event, then a few things a person might do there.
export default function (data) {
	if (!csrf) {
		const account = read('/api/v1/me', 'me');
		csrf = account && typeof account.csrf_token === 'string' ? account.csrf_token : '';
		check(csrf, { 'session has a CSRF token': (token) => token !== '' });
		if (!csrf) {
			pause();
			return;
		}
	}
	page(manifest.paths.discover, 'discover');
	read(`/api/v1/me/saved-events/states?${data.events.map((id) => `event_id=${encodeURIComponent(id)}`).join('&')}`, 'saved states');
	pause();
	const event = pick(data.events);
	const encoded = encodeURIComponent(event);
	page(`/events/${encoded}`, 'event');
	pause();

	const steps = 2 + rand(4);
	for (let i = 0; i < steps; i++) {
		const roll = Math.random();
		if (roll < 0.2) {
			act(`/saved/${encoded}`, 'save', { action: Math.random() < 0.75 ? 'save' : 'remove' });
		} else if (roll < 0.32) {
			const going = Math.random() < 0.8;
			act(`/interests/${encoded}`, 'interest', going ? { action: 'set', visibility: pick(['public', 'private']) } : { action: 'remove' });
		} else if (roll < 0.4) {
			act(`/recommendations/${encoded}`, 'recommend', { action: 'set', reason: pick(lines) });
		} else if (roll < 0.47 && data.venues[event]) {
			act(`/follows/venue/${encodeURIComponent(data.venues[event])}`, 'follow venue', { action: Math.random() < 0.7 ? 'follow' : 'unfollow', return_to: '/follows' }, true);
		} else if (roll < 0.75) {
			discuss(event, encoded);
		} else if (roll < 0.85) {
			page(pick(['/saved', '/me/notifications', '/follows', '/community']), 'account page');
		} else {
			page(`/events/${encoded}/discussions`, 'discussions');
		}
		pause();
	}
}

// Reads the event's discussions, then joins in: a new thread, a reply, a
// helpful reaction or following a thread.
function discuss(event, encoded) {
	const list = read(`/api/v1/events/${encoded}/discussions`, 'discussion list');
	const threads = list && Array.isArray(list.items) ? list.items : [];
	const roll = Math.random();
	if ((threads.length === 0 || roll < 0.25) && canPost()) {
		posted.push(Date.now());
		act('/discussion-actions', 'new thread', { action: 'create', event_id: event, body: pick(lines), idempotency_key: key(), return_to: `/events/${encoded}/discussions` });
		return;
	}
	if (threads.length === 0) return;
	const thread = pick(threads);
	page(`/events/${encoded}/discussions/${thread.id}`, 'thread');
	if (roll < 0.6 && canPost()) {
		posted.push(Date.now());
		act('/discussion-actions', 'reply', { action: 'create', event_id: event, thread_id: thread.id, parent_id: thread.id, body: pick(lines), idempotency_key: key(), return_to: `/events/${encoded}/discussions/${thread.id}` });
	} else if (roll < 0.85) {
		act('/discussion-actions', 'helpful', { action: Math.random() < 0.8 ? 'helpful' : 'unhelpful', post_id: thread.id, return_to: `/events/${encoded}/discussions/${thread.id}` });
	} else {
		act('/discussion-follow-actions', 'follow thread', { action: 'follow', event_id: event, thread_id: thread.id, frequency: pick(['immediate', 'daily', 'weekly']), return_to: `/events/${encoded}/discussions/${thread.id}` }, true);
	}
}
