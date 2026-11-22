// Steps the live replies load (scripts/load/live-replies.js) through
// increasing numbers of participants, one k6 run per step, until a step fails
// its thresholds or falls short of the checks it should make, and writes a
// self-contained HTML report. It seeds the threads once for every step and
// keeps the writers rotating across steps, within each member's posting
// limit. Each step also samples the review server's and MariaDB's CPU while
// the load is held, so the report shows which one ran out first. See
// docs/load-testing.md#live-replies.
//
// Environment: MANIFEST (local populated review server), STEPS (participants,
// comma-separated), STEP (held duration), RAMP, PAUSE (seconds between steps),
// THREADS, POLL, WRITE_EVERY and P95 pass through to k6. APP_PID (the review
// server's process, found automatically) and DB_CONTAINER (default
// ticketopia-db-mariadb-1) name what to sample. K6 is the k6 command.
import { spawn, spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const env = process.env;
const steps = (env.STEPS || '50,100,200,400,800,1600,3200').split(',').map(Number);
const step = env.STEP || '1m';
const ramp = env.RAMP || '15s';
const pause = Number(env.PAUSE ?? 20);
const poll = Number(env.POLL || 10);
const k6 = env.K6 || 'k6';
const threadCount = Number(env.THREADS || 10);
const writeEvery = Number(env.WRITE_EVERY || 10);
const seconds = (d) => {
	const m = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(d);
	return m ? Number(m[1]) * { s: 1, m: 60, h: 3600 }[m[2]] : NaN;
};
if (steps.some((s) => !(s > 0)) || !(seconds(step) > 0) || !(seconds(ramp) >= 0) || !(pause >= 0)) {
	console.error('STEPS must be positive participant counts, STEP and RAMP durations such as 1m or 15s, and PAUSE seconds');
	process.exit(2);
}
const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z');
const out = join('.local', 'load', `live-replies-${stamp}`);
mkdirSync(out, { recursive: true });

const sh = (cmd) => spawnSync('/bin/sh', ['-c', cmd], { encoding: 'utf8' }).stdout?.trim() || '';
const manifest = (() => {
	try {
		return JSON.parse(readFileSync(env.MANIFEST, 'utf8'));
	} catch {
		console.error('set MANIFEST to a running populated review server manifest.json');
		process.exit(2);
	}
})();
// It writes, so like the k6 script it runs only against a local populated
// review server, checked before any credentials or writes are sent.
if (!/^http:\/\/(127\.0\.0\.1|\[::1\]):\d+$/.test(manifest.base_url) || manifest.scenario !== 'populated' || !Array.isArray(manifest.members) || manifest.members.length === 0) {
	console.error(`refusing ${manifest.base_url} (${manifest.scenario}): start a local review server with scenario=populated`);
	process.exit(2);
}
// The review server runs as the persistence test binary that listens on its port.
const port = new URL(manifest.base_url).port;
const appPID = env.APP_PID || sh(`lsof -nP -iTCP:${port} -sTCP:LISTEN -t | head -1`);
const db = env.DB_CONTAINER || 'ticketopia-db-mariadb-1';
const meta = {
	target: manifest.base_url,
	started: new Date().toISOString(),
	commit: sh('git rev-parse --short HEAD') + (sh('git status --porcelain') ? ' (with uncommitted changes)' : ''),
	k6: sh(`${k6} version`).split(' (')[0],
	host: `${sh('sysctl -n machdep.cpu.brand_string') || sh('uname -m')}, ${sh('sysctl -n hw.ncpu') || '?'} CPUs`,
	threads: Number(env.THREADS || 10),
	poll,
	writeEvery: Number(env.WRITE_EVERY || 10),
	p95: Number(env.P95 || 300),
};

// The threads every step's participants hold open, started once as the first
// members, as the k6 script's own setup would.
async function seedThreads() {
	const out = [];
	for (let i = 0; i < Math.min(threadCount, manifest.members.length); i++) {
		const cookie = `${manifest.session_cookie}=${manifest.members[i].session}`;
		const me = await (await fetch(`${manifest.base_url}/api/v1/me`, { redirect: 'error', headers: { Cookie: cookie, Accept: 'application/json' } })).json();
		const event = manifest.event_ids[i % manifest.event_ids.length];
		const form = new URLSearchParams({ action: 'create', event_id: event, body: `Live replies benchmark thread ${i + 1}`,
			idempotency_key: crypto.randomUUID().replaceAll('-', ''), return_to: `/events/${encodeURIComponent(event)}/discussions`, csrf_token: me.csrf_token });
		const res = await fetch(`${manifest.base_url}/discussion-actions`, { method: 'POST', body: form, redirect: 'error',
			headers: { Cookie: cookie, Accept: 'application/json', Origin: manifest.base_url, 'Sec-Fetch-Site': 'same-origin' } });
		const id = /\/discussions\/([0-9a-f]{32})/.exec((await res.json().catch(() => ({}))).location || '');
		if (!id) {
			console.error(`could not start benchmark thread ${i + 1}: ${res.status}`);
			process.exit(1);
		}
		out.push({ event, id: id[1] });
	}
	return out;
}
const threadsJSON = JSON.stringify(await seedThreads());
let writeOffset = 0;

let interrupted = false;
process.on('SIGINT', () => {
	interrupted = true;
});
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// Output of a command, without blocking the event loop k6's output streams through.
const shAsync = (cmd) => new Promise((resolve) => {
	const child = spawn('/bin/sh', ['-c', cmd], { stdio: ['ignore', 'pipe', 'ignore'] });
	let out = '';
	child.stdout.on('data', (d) => (out += d));
	child.on('close', () => resolve(out.trim()));
});
// CPU samples once the ramp is over, while the step holds its load: the app's
// process and MariaDB's container.
async function sampleWhile(running) {
	const app = [], mariadb = [];
	const start = Date.now() + seconds(ramp) * 1000 + 3000;
	while (running.active) {
		if (Date.now() >= start) {
			const [cpu, stats] = await Promise.all([appPID ? shAsync(`ps -o %cpu= -p ${appPID}`) : '', shAsync(`docker stats --no-stream --format '{{.CPUPerc}}' ${db} 2>/dev/null`)]);
			if (cpu !== '' && Number(cpu) >= 0) app.push(Number(cpu));
			if (stats) mariadb.push(Number.parseFloat(stats));
		}
		await sleep(2000);
	}
	const peak = (a) => (a.length ? Math.max(...a) : undefined);
	const avg = (a) => (a.length ? a.reduce((x, y) => x + y, 0) / a.length : undefined);
	return { app: { avg: avg(app), peak: peak(app) }, mariadb: { avg: avg(mariadb), peak: peak(mariadb) } };
}

const results = [];
for (const [i, participants] of steps.entries()) {
	if (interrupted) break;
	const file = join(out, `participants-${participants}`);
	process.stdout.write(`${participants} participants... `);
	const running = { active: true };
	const samples = sampleWhile(running);
	const status = await new Promise((resolve) => {
		const child = spawn('/bin/sh', ['-c', `${k6} "$@"`, 'k6', 'run', '-q', '--summary-export', `${file}.json`,
			'--summary-trend-stats', 'avg,min,med,p(90),p(95),p(99),max', 'scripts/load/live-replies.js'], {
			env: { ...env, PARTICIPANTS: String(participants), DURATION: step, RAMP: ramp, THREADS_JSON: threadsJSON, WRITE_OFFSET: String(writeOffset) },
			stdio: ['ignore', 'pipe', 'pipe'],
		});
		let log = '';
		child.stdout.on('data', (d) => (log += d));
		child.stderr.on('data', (d) => (log += d));
		child.on('close', (code, signal) => {
			writeFileSync(`${file}.log`, log);
			resolve(signal === 'SIGINT' ? 130 : code);
		});
	});
	running.active = false;
	const cpu = await samples;
	if (status === 130 || interrupted) {
		interrupted = true;
		console.log('interrupted');
		break;
	}
	writeOffset += Math.ceil(seconds(step) / writeEvery);
	const result = summarize(participants, `${file}.json`, status, cpu);
	results.push(result);
	console.log(result.passed
		? `ok, ${result.rate.toFixed(1)} checks/s, p95 ${ms(result.p95)}, noticed p95 ${ms(result.noticedP95)}`
		: `failed (${result.reason})`);
	writeReport();
	if (!result.passed) break;
	if (i < steps.length - 1) await sleep(pause * 1000);
}
writeReport();
console.log(`report: ${join(out, 'report.html')}`);

function summarize(participants, file, status, cpu) {
	let metrics = {};
	try {
		metrics = JSON.parse(readFileSync(file, 'utf8')).metrics;
	} catch {
		// k6 failed before exporting, usually in setup.
	}
	const live = metrics['http_req_duration{kind:live,phase:held}'] || {};
	const noticed = metrics.live_noticed_after || {};
	const failed = metrics['http_req_failed{kind:live,phase:held}'] || {};
	const checks = metrics.live_checks_held?.count || 0;
	const rate = checks / seconds(step);
	const result = {
		participants,
		// Checks made while the load was held.
		rate,
		expected: participants / poll,
		// People at the browser's ten-second pace (0.1 checks/s each) the
		// achieved checks stand for, not the configured VUs.
		equivalent: Math.round(rate * 10),
		checks,
		p50: live.med,
		p95: live['p(95)'],
		p99: live['p(99)'],
		max: live.max,
		noticedP50: noticed.med,
		noticedP95: noticed['p(95)'],
		seen: metrics.live_replies_seen?.count || 0,
		failedRate: failed.value ?? failed.rate ?? 0,
		writeFailed: metrics['http_req_failed{kind:write}']?.value ?? 0,
		cpu,
		// A step must also hold its pace: participants waiting on slow checks
		// make fewer of them, which would otherwise look like a pass.
		passed: status === 0 && rate >= 0.85 * participants / poll,
	};
	if (!result.passed) {
		result.reason = status === 99 ? 'thresholds crossed' : status === 107 ? 'script error (often setup)' : status !== 0 ? `k6 exit ${status}`
			: `held ${rate.toFixed(0)} checks/s, under 85% of the ${(participants / poll).toFixed(0)}/s pace`;
	}
	return result;
}

function ms(v) {
	return v === undefined ? '–' : v >= 1000 ? `${(v / 1000).toFixed(2)} s` : `${v.toFixed(0)} ms`;
}
function pct(v) {
	return v === undefined ? '–' : `${v.toFixed(0)}%`;
}
function escape(s) {
	return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
}

function writeReport() {
	const best = results.filter((r) => r.passed).at(-1);
	const broke = results.find((r) => !r.passed);
	const verdict = best
		? `Held <strong>${best.participants} participants</strong> (${best.equivalent} at the browser's ten-second pace) (${best.rate.toFixed(1)} checks/s, p95 ${ms(best.p95)}, new replies noticed within ${ms(best.noticedP95)} at p95)${broke ? `; failed at ${broke.participants} (${escape(broke.reason)})` : results.length === steps.length ? '; never failed: raise STEPS to find the limit' : ''}.`
		: results.length ? `Failed at the first step, ${results[0].participants} participants (${escape(results[0].reason)}).` : 'Not run.';
	const rows = results.map((r) => `<tr class="${r.passed ? '' : 'fail'}"><td>${r.participants}</td><td>${r.equivalent}</td><td>${r.rate.toFixed(1)} / ${r.expected.toFixed(1)}</td>
<td>${ms(r.p50)}</td><td>${ms(r.p95)}</td><td>${ms(r.p99)}</td><td>${ms(r.max)}</td><td>${ms(r.noticedP50)}</td><td>${ms(r.noticedP95)}</td><td>${r.seen}</td>
<td>${(r.failedRate * 100).toFixed(2)}%</td><td>${pct(r.cpu.app.avg)} / ${pct(r.cpu.app.peak)}</td><td>${pct(r.cpu.mariadb.avg)} / ${pct(r.cpu.mariadb.peak)}</td><td>${r.passed ? 'pass' : escape(r.reason)}</td></tr>`).join('');
	const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Ticketopia live replies benchmark</title><style>
:root { color-scheme: light; font-family: system-ui, sans-serif; line-height: 1.5; color: oklch(25% 0.02 260); background: oklch(98.5% 0.005 260); }
body { max-inline-size: 76rem; margin-inline: auto; padding: clamp(1rem, 0.5rem + 2vw, 2.5rem); }
h1 { font-size: clamp(1.5rem, 1.2rem + 1vw, 2rem); margin-block-end: 0.25rem; }
dl { display: grid; grid-template-columns: max-content 1fr; gap: 0.25rem 1rem; } dt { font-weight: 600; } dd { margin: 0; }
section { background: oklch(100% 0 none); border: 1px solid oklch(90% 0.01 260); border-radius: 0.75rem; padding: 1rem 1.25rem; margin-block: 1rem; }
.scroll { overflow-x: auto; } table { border-collapse: collapse; inline-size: 100%; font-variant-numeric: tabular-nums; font-size: 0.9rem; }
th, td { text-align: end; padding: 0.35rem 0.6rem; border-block-end: 1px solid oklch(92% 0.01 260); white-space: nowrap; }
tr.fail td { color: oklch(45% 0.18 25); } .note { color: oklch(45% 0.02 260); font-size: 0.9rem; }
</style></head><body><h1>Live replies benchmark</h1><p class="note">${interrupted ? 'Interrupted; partial results.' : ''}</p>
<dl><dt>Target</dt><dd>${escape(meta.target)}</dd><dt>Host</dt><dd>${escape(meta.host)}</dd><dt>Started</dt><dd>${escape(meta.started)}</dd><dt>Commit</dt><dd>${escape(meta.commit)}</dd><dt>k6</dt><dd>${escape(meta.k6)}</dd>
<dt>Load</dt><dd>${meta.threads} threads, each participant checking every ${meta.poll}s (±20%), one reply every ${meta.writeEvery}s across the threads</dd>
<dt>Steps</dt><dd>${steps.join(', ')} participants, ${escape(ramp)} ramp then ${escape(step)} held, ${pause}s apart</dd>
<dt>Pass</dt><dd>live check p95 under ${ms(meta.p95)}, under 1% failed checks and writes, no dropped writes, replies noticed (each within ${ms((meta.poll * 1.2 + 3) * 1000)} at p95), and at least 85% of the checks the participants' pace calls for while held</dd></dl>
<section><p>${verdict}</p><div class="scroll"><table><thead><tr><th>Participants</th><th>At 10s pace (from achieved checks)</th><th>Checks/s held (achieved / at pace)</th><th>p50</th><th>p95</th><th>p99</th><th>max</th>
<th>Noticed p50</th><th>Noticed p95</th><th>Replies noticed</th><th>Failed checks</th><th>App CPU avg / peak</th><th>MariaDB CPU avg / peak</th><th>Result</th></tr></thead><tbody>${rows}</tbody></table></div></section>
<p class="note">k6, the app and MariaDB share one machine, so this is a baseline for comparing changes and spotting the limit's shape, not a production capacity. CPU is percent of one core, sampled every few seconds while the step ran.</p>
</body></html>`;
	writeFileSync(join(out, 'report.html'), html);
	writeFileSync(join(out, 'results.json'), JSON.stringify({ meta, steps, results }, null, 2));
}
