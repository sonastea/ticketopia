// Benchmarks each traffic-mix scenario on its own: holds it at increasing
// request rates, one short k6 run per step, stops the scenario at its first
// failing step, and writes a self-contained HTML report. See
// docs/load-testing.md#benchmarks. Run through make benchmark.
//
// Environment: TARGET (remote origin) or MANIFEST (local review server),
// SCENARIOS (default web,api,mcp_public), STEPS (requests/s, comma-separated),
// STEP (each step's duration), PAUSE (seconds between steps), P95, TOKEN and
// CITY/COUNTRY/EVENTS pass through to k6. K6 is the k6 command.
import { spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const env = process.env;
const scenarios = (env.SCENARIOS || 'web,api,mcp_public').split(',').map((s) => s.trim()).filter(Boolean);
const steps = (env.STEPS || '25,50,100,200,300,400,500,600,800,1000').split(',').map(Number);
const step = env.STEP || '30s';
const pause = Number(env.PAUSE ?? 30);
// K6 is a shell command prefix, such as k6 or "mise exec k6@2.3.0 -- k6".
const k6 = env.K6 || 'k6';
const runK6 = (args, extra = {}) => spawnSync('/bin/sh', ['-c', `${k6} "$@"`, 'k6', ...args], { encoding: 'utf8', ...extra });
const seconds = (d) => {
	const m = /^(\d+(?:\.\d+)?)(s|m|h)$/.exec(d);
	return m ? Number(m[1]) * { s: 1, m: 60, h: 3600 }[m[2]] : NaN;
};
if (steps.some((s) => !(s > 0)) || !(pause >= 0) || !(seconds(step) > 0)) {
	console.error('STEPS must be positive requests/s, STEP a duration such as 30s or 1m, and PAUSE a non-negative number of seconds');
	process.exit(2);
}
const stamp = new Date().toISOString().replace(/[-:]/g, '').replace(/\.\d+Z$/, 'Z');
const out = join('.local', 'load', `benchmark-${stamp}`);
mkdirSync(out, { recursive: true });

const target = env.TARGET || (() => {
	try {
		return JSON.parse(readFileSync(env.MANIFEST, 'utf8')).base_url;
	} catch {
		return 'local review server';
	}
})();
const run = (cmd, args) => spawnSync(cmd, args, { encoding: 'utf8' }).stdout?.trim() || '';
const meta = {
	target,
	started: new Date().toISOString(),
	commit: run('git', ['rev-parse', '--short', 'HEAD']),
	k6: (runK6(['version']).stdout || '').trim().split(' (')[0],
	step,
	pause,
	p95: Number(env.P95 || (env.TARGET ? 1000 : 300)),
};

const results = Object.fromEntries(scenarios.map((s) => [s, []]));
// Ctrl-C reaches k6 too; a step it interrupted, or Ctrl-C during a pause,
// ends the benchmark with a partial report.
let interrupted = false;
process.on('SIGINT', () => {
	interrupted = true;
});
const sleep = async (s) => {
	for (let left = s * 1000; left > 0 && !interrupted; left -= 250) {
		await new Promise((resolve) => setTimeout(resolve, Math.min(250, left)));
	}
};

for (const scenario of scenarios) {
	for (const [i, rps] of steps.entries()) {
		if (interrupted) break;
		const file = join(out, `${scenario}-${rps}`);
		process.stdout.write(`${scenario} at ${rps} requests/s... `);
		const k = runK6(['run', '-q', '--summary-export', `${file}.json`,
			'--summary-trend-stats', 'avg,min,med,p(90),p(95),p(99),max', 'scripts/load/traffic-mix.js'], {
			env: { ...env, SCENARIOS: scenario, RPS: String(rps), RAMP: '0', DURATION: step, STATUS_BREAKDOWN: '1' },
		});
		writeFileSync(`${file}.log`, `${k.stdout}\n${k.stderr}`);
		if (k.signal === 'SIGINT' || k.status === 105 || k.status === 130) interrupted = true;
		if (interrupted) {
			console.log('interrupted');
			break;
		}
		const aborted = /stopping test prematurely/.test(k.stderr) || /stopping test prematurely/.test(k.stdout);
		const result = summarize(scenario, rps, `${file}.json`, k.status, aborted);
		results[scenario].push(result);
		console.log(result.passed ? `ok, ${result.achieved.toFixed(0)}/s, p95 ${ms(result.p95)}` : `failed (${result.reason})`);
		writeReport();
		if (!result.passed) break;
		if (i < steps.length - 1) await sleep(pause);
	}
	if (scenario !== scenarios.at(-1) && !interrupted) await sleep(pause);
}
writeReport();
console.log(`report: ${join(out, 'report.html')}`);

function summarize(scenario, rps, file, status, aborted) {
	let metrics = {};
	try {
		metrics = JSON.parse(readFileSync(file, 'utf8')).metrics;
	} catch {
		// k6 failed before exporting: setup failure or a bad option
	}
	const trend = metrics[`http_req_duration{scenario:${scenario}}`] || {};
	const statuses = Object.fromEntries(Object.entries(metrics)
		.filter(([name, m]) => name.startsWith('failed_status{status:') && m.count > 0)
		.map(([name, m]) => [name.slice('failed_status{status:'.length, -1), m.count]));
	const failed = metrics.http_req_failed || {};
	// The scenario's requests over the held window, excluding setup requests and
	// time. A step aborted early ran for less than its window, so only the whole
	// run's average (approximate) is known.
	const result = {
		rps,
		aborted,
		achieved: aborted ? metrics.http_reqs?.rate || 0 : (metrics[`http_reqs{scenario:${scenario}}`]?.count || 0) / seconds(step),
		requests: metrics.http_reqs?.count || 0,
		p50: trend.med,
		p95: trend['p(95)'],
		p99: trend['p(99)'],
		max: trend.max,
		failedRate: failed.value ?? failed.rate ?? 0,
		checks: metrics.checks?.value ?? metrics.checks?.rate,
		dropped: metrics.dropped_iterations?.count || 0,
		statuses,
		passed: status === 0,
	};
	if (!result.passed) {
		result.reason = status === 99 ? (aborted ? 'aborted: thresholds crossed' : 'thresholds crossed') : status === 107 ? 'script error (often setup)' : `k6 exit ${status}`;
	}
	return result;
}

function ms(v) {
	return v === undefined ? '–' : v >= 1000 ? `${(v / 1000).toFixed(2)} s` : `${v.toFixed(0)} ms`;
}

function escape(s) {
	return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
}

// An inline SVG: achieved requests/s against the held rate, and p95 latency.
function chart(rows) {
	if (rows.length === 0) return '';
	const w = 560, h = 220, pad = 44;
	const maxX = Math.max(...rows.map((r) => r.rps));
	const maxY = Math.max(...rows.map((r) => Math.max(r.rps, r.achieved)));
	const maxL = Math.max(...rows.map((r) => r.p95 || 0), meta.p95);
	const x = (v) => pad + (v / maxX) * (w - 2 * pad);
	const y = (v, max) => h - pad + 8 - (v / max) * (h - 2 * pad);
	const line = (pts) => pts.map(([a, b], i) => `${i ? 'L' : 'M'}${a.toFixed(1)},${b.toFixed(1)}`).join(' ');
	const ideal = line([[x(0), y(0, maxY)], [x(maxX), y(maxX, maxY)]]);
	const achieved = line(rows.map((r) => [x(r.rps), y(r.achieved, maxY)]));
	const latency = line(rows.map((r) => [x(r.rps), y(Math.min(r.p95 || 0, maxL), maxL)]));
	const dots = rows.map((r) => `<circle cx="${x(r.rps).toFixed(1)}" cy="${y(r.achieved, maxY).toFixed(1)}" r="4" class="${r.passed ? 'pass' : 'fail'}"><title>${r.rps}/s held: ${r.achieved.toFixed(0)}/s achieved, p95 ${ms(r.p95)}</title></circle>`).join('');
	return `<svg viewBox="0 0 ${w} ${h}" role="img" aria-label="Achieved requests per second and p95 latency by held rate">
<line x1="${pad}" y1="${h - pad + 8}" x2="${w - pad}" y2="${h - pad + 8}" class="axis"/>
<line x1="${y(meta.p95, maxL) > 0 ? pad : pad}" y1="${y(meta.p95, maxL).toFixed(1)}" x2="${w - pad}" y2="${y(meta.p95, maxL).toFixed(1)}" class="limit"/>
<path d="${ideal}" class="ideal"/><path d="${latency}" class="latency"/><path d="${achieved}" class="achieved"/>${dots}
<text x="${pad}" y="16" class="label">requests/s (0–${maxY})</text>
<text x="${w - pad}" y="16" text-anchor="end" class="label latency-label">p95 (0–${ms(maxL)}; dashed: ${ms(meta.p95)} limit)</text>
<text x="${w / 2}" y="${h - 6}" text-anchor="middle" class="label">held rate, requests/s (to ${maxX})</text></svg>`;
}

function writeReport() {
	const sections = scenarios.map((scenario) => {
		const rows = results[scenario];
		const best = rows.filter((r) => r.passed).at(-1);
		const broke = rows.find((r) => !r.passed);
		const verdict = best
			? `Held <strong>${best.rps} requests/s</strong> (p95 ${ms(best.p95)})${broke ? `; failed at ${broke.rps}/s (${escape(broke.reason)})` : rows.length === steps.length ? '; never failed: raise STEPS to find the limit' : ''}.`
			: rows.length ? `Failed at the first step, ${rows[0].rps}/s (${escape(rows[0].reason)}).` : 'Not run.';
		const table = rows.map((r) => `<tr class="${r.passed ? '' : 'fail'}"><td>${r.rps}</td><td>${r.aborted ? '≈' : ''}${r.achieved.toFixed(1)}</td><td>${ms(r.p50)}</td><td>${ms(r.p95)}</td><td>${ms(r.p99)}</td><td>${ms(r.max)}</td>
<td>${(r.failedRate * 100).toFixed(2)}%</td><td>${Object.entries(r.statuses).map(([s, n]) => `${s === '0' ? 'no response' : escape(s)}: ${n}`).join(', ') || '–'}</td><td>${r.dropped}</td><td>${r.passed ? 'pass' : escape(r.reason)}</td></tr>`).join('');
		return `<section><h2><code>${escape(scenario)}</code></h2><p>${verdict}</p>${chart(rows)}
<div class="scroll"><table><thead><tr><th>Held req/s</th><th>Achieved req/s (held window)</th><th>p50</th><th>p95</th><th>p99</th><th>max</th><th>Failed</th><th>Failed by status</th><th>Dropped</th><th>Result</th></tr></thead><tbody>${table}</tbody></table></div></section>`;
	}).join('');
	const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Ticketopia benchmark — ${escape(meta.target)}</title><style>
:root { color-scheme: light; font-family: system-ui, sans-serif; line-height: 1.5; color: oklch(25% 0.02 260); background: oklch(98.5% 0.005 260); }
body { max-inline-size: 72rem; margin-inline: auto; padding: clamp(1rem, 0.5rem + 2vw, 2.5rem); }
h1 { font-size: clamp(1.5rem, 1.2rem + 1vw, 2rem); margin-block-end: 0.25rem; } h2 { margin-block: 2rem 0.5rem; }
dl { display: grid; grid-template-columns: max-content 1fr; gap: 0.25rem 1rem; } dt { font-weight: 600; } dd { margin: 0; }
section { background: oklch(100% 0 none); border: 1px solid oklch(90% 0.01 260); border-radius: 0.75rem; padding: 1rem 1.25rem; margin-block: 1rem; }
svg { inline-size: 100%; max-inline-size: 36rem; block-size: auto; display: block; margin-block: 0.5rem; }
.axis { stroke: oklch(70% 0.01 260); } .ideal { stroke: oklch(80% 0.01 260); stroke-dasharray: 4 4; fill: none; }
.limit { stroke: oklch(65% 0.15 25); stroke-dasharray: 2 4; } .achieved { stroke: oklch(55% 0.15 250); stroke-width: 2.5; fill: none; }
.latency { stroke: oklch(65% 0.15 60); stroke-width: 2; fill: none; } .label { font-size: 11px; fill: oklch(45% 0.02 260); } .latency-label { fill: oklch(55% 0.15 60); }
circle.pass { fill: oklch(55% 0.15 250); } circle.fail { fill: oklch(55% 0.2 25); }
.scroll { overflow-x: auto; } table { border-collapse: collapse; inline-size: 100%; font-variant-numeric: tabular-nums; font-size: 0.9rem; }
th, td { text-align: end; padding: 0.35rem 0.6rem; border-block-end: 1px solid oklch(92% 0.01 260); white-space: nowrap; } th:nth-child(8), td:nth-child(8) { text-align: start; white-space: normal; }
tr.fail td { color: oklch(45% 0.18 25); } .note { color: oklch(45% 0.02 260); font-size: 0.9rem; }
</style></head><body><h1>Ticketopia benchmark</h1><p class="note">${interrupted ? 'Interrupted; partial results.' : ''}</p>
<dl><dt>Target</dt><dd>${escape(meta.target)}</dd><dt>Started</dt><dd>${escape(meta.started)}</dd><dt>Commit</dt><dd>${escape(meta.commit)}</dd><dt>k6</dt><dd>${escape(meta.k6)}</dd>
<dt>Steps</dt><dd>${steps.join(', ')} requests/s, ${escape(step)} each at full rate, ${pause}s apart</dd><dt>Pass</dt><dd>p95 under ${ms(meta.p95)}, under 1% failed requests and checks, no dropped iterations</dd></dl>
<p class="note">Each scenario runs alone, read-only, repeating one cached search and a few of its events (see docs/load-testing.md), so these are the cheapest requests the site serves. Remote runs abort a failing step after about 30 seconds.</p>
${sections}</body></html>`;
	writeFileSync(join(out, 'report.html'), html);
}
