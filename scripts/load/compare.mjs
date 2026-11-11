// Compares make benchmark runs (for example at different times of day or with
// different settings) in one self-contained HTML report. See
// docs/load-testing.md#benchmarks.
//
// usage: node scripts/load/compare.mjs <config.json> [out.html]
// The config names the runs and, per scenario, the benchmark directory that
// holds its steps (<scenario>-<rps>.json and .log); see the docs for fields.
import { existsSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join, resolve } from 'node:path';

const [configPath, outArg] = process.argv.slice(2);
if (!configPath) {
	console.error('usage: node scripts/load/compare.mjs <config.json> [out.html]');
	process.exit(2);
}
const outPath = outArg || (/\.json$/i.test(configPath) ? configPath.replace(/\.json$/i, '.html') : `${configPath}.html`);
if (resolve(outPath) === resolve(configPath)) {
	console.error('the report would overwrite the config; name another output file');
	process.exit(2);
}
const config = JSON.parse(readFileSync(configPath, 'utf8'));
const positive = (v, fallback, name) => {
	if (v === undefined) return fallback;
	if (!(typeof v === 'number' && v > 0)) {
		console.error(`${name} must be a positive number`);
		process.exit(2);
	}
	return v;
};
const stepSeconds = positive(config.stepSeconds, 30, 'stepSeconds');
const p95Limit = positive(config.p95, 1000, 'p95');
for (const run of config.runs) {
	for (const key of ['readyz', 'discover']) {
		if (run.probe?.[key] != null) positive(run.probe[key], 0, `probe.${key}`);
	}
}
const scenarioOrder = ['web', 'api', 'mcp_public', 'web+api+mcp_public'];
const names = {
	web: 'Pages',
	api: 'JSON API',
	mcp_public: 'Agent tools (MCP)',
	'web+api+mcp_public': 'Mixed traffic',
};
const descriptions = {
	web: 'Discover page and event pages, rendered HTML.',
	api: '<code>GET /api/v1/events</code> and event details, JSON.',
	mcp_public: '<code>find_events</code> and <code>event_details</code> over <code>/mcp</code>.',
	'web+api+mcp_public': 'Pages, API and agent tools together, sharing each step evenly.',
};
const scenarios = scenarioOrder.filter((s) => config.runs.some((r) => r.sources[s]));

// Reads one scenario's steps from a benchmark directory. A step's outcome comes
// from the benchmark's saved result; older runs without one are judged from
// the log, and a step that cannot be judged is "unknown", never a pass.
function load(dir, scenario) {
	if (!dir || !existsSync(dir)) return [];
	const prefix = `${scenario}-`;
	return readdirSync(dir)
		.filter((f) => f.startsWith(prefix) && f.endsWith('.json') && !f.endsWith('.result.json') && /^\d+(\.\d+)?$/.test(f.slice(prefix.length, -5)))
		.map((f) => {
			const base = f.slice(0, -5);
			const rps = Number(base.slice(prefix.length));
			const m = JSON.parse(readFileSync(join(dir, f), 'utf8')).metrics;
			const log = existsSync(join(dir, `${base}.log`)) ? readFileSync(join(dir, `${base}.log`), 'utf8') : '';
			const saved = existsSync(join(dir, `${base}.result.json`)) ? JSON.parse(readFileSync(join(dir, `${base}.result.json`), 'utf8')) : null;
			const together = scenario.split('+');
			const trend = (together.length > 1 ? m.http_req_duration : m[`http_req_duration{scenario:${scenario}}`]) || {};
			const aborted = saved ? saved.aborted : /stopping test prematurely/.test(log);
			const count = together.reduce((sum, n) => sum + (m[`http_reqs{scenario:${n}}`]?.count || 0), 0);
			const crossed = /thresholds on metrics .* (have been|were) crossed/.test(log);
			// Without a saved outcome, a pass needs k6's final summary and no error
			// or interruption in the log; anything less is unknown.
			const completed = /TOTAL RESULTS/.test(log) && !/level=error|interrupt|context canceled|GoError/i.test(log);
			const outcome = saved ? (saved.interrupted ? 'unknown' : saved.passed ? 'pass' : 'fail') : crossed ? 'fail' : completed && count > 0 ? 'pass' : 'unknown';
			const statuses = Object.fromEntries(Object.entries(m)
				.filter(([k, v]) => k.startsWith('failed_status{status:') && v.count > 0)
				.map(([k, v]) => [k.slice('failed_status{status:'.length, -1), v.count]));
			return {
				rps,
				achieved: aborted || !count ? m.http_reqs?.rate || 0 : count / stepSeconds,
				p50: trend.med,
				p95: trend['p(95)'],
				p99: trend['p(99)'],
				failed: (m.http_req_failed?.value ?? m.http_req_failed?.rate ?? 0) * 100,
				busy: (m.mcp_busy_503?.value ?? 0) * 100,
				dropped: m.dropped_iterations?.count || 0,
				statuses,
				aborted,
				outcome,
				reason: saved?.reason || '',
				passed: outcome === 'pass',
			};
		})
		.sort((a, b) => a.rps - b.rps);
}

const data = config.runs.map((run) => Object.fromEntries(scenarios.map((s) => [s, load(run.sources[s], s)])));
const best = (rows) => rows.filter((r) => r.passed).at(-1);
const broke = (rows) => rows.find((r) => r.outcome === 'fail');
// A capacity is bracketed when a later step actually failed; otherwise the
// highest passing step is only a lower bound.
const bound = (rows) => {
	const b = best(rows), f = broke(rows);
	return b && !(f && f.rps > b.rps) ? '≥' : '';
};

const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);
const ms = (v) => (v === undefined || v === null ? '–' : v >= 1000 ? `${(v / 1000).toFixed(1)} s` : `${Math.round(v)} ms`);
const num = (v) => (v === undefined ? '–' : Math.round(v).toLocaleString('en-US'));
// Request rates keep their precision: 12.5 stays 12.5.
const rate = (v) => (v === undefined ? '–' : Number.isInteger(v) ? v.toLocaleString('en-US') : v.toLocaleString('en-US', { maximumFractionDigits: 2 }));

function failure(r) {
	if (r.passed) return '';
	if (r.outcome === 'unknown') return r.reason || 'outcome unknown';
	// A saved failure other than crossed thresholds, such as a setup error.
	if (r.reason && !/thresholds crossed/.test(r.reason)) return r.reason;
	const parts = [];
	if (r.busy > 0.05) parts.push(`${r.busy.toFixed(1)}% busy`);
	else if (r.failed >= 0.05) parts.push(`${r.failed.toFixed(1)}% failed`);
	if (r.p95 >= p95Limit) parts.push(`p95 ${ms(r.p95)}`);
	if (r.dropped) parts.push(`${num(r.dropped)} dropped`);
	return parts.join(' · ') || 'thresholds crossed';
}

// Capacity: the highest passing step per scenario and run, as grouped bars.
function capacityChart() {
	const w = 760, h = 300, left = 56, right = 16, top = 24, bottom = 56;
	const max = Math.max(...data.flatMap((d) => scenarios.map((s) => best(d[s])?.rps || 0)), 100);
	const ceiling = Math.ceil(max / 200) * 200;
	const group = (w - left - right) / scenarios.length;
	const bar = Math.min(34, (group - 28) / config.runs.length);
	const y = (v) => top + (1 - v / ceiling) * (h - top - bottom);
	let svg = '';
	for (let v = 0; v <= ceiling; v += ceiling / 4) {
		svg += `<line x1="${left}" x2="${w - right}" y1="${y(v)}" y2="${y(v)}" class="grid"/><text x="${left - 8}" y="${y(v) + 4}" class="tick" text-anchor="end">${v}</text>`;
	}
	scenarios.forEach((s, i) => {
		const x0 = left + i * group + (group - bar * config.runs.length) / 2;
		config.runs.forEach((run, j) => {
			const b = best(data[j][s]);
			const x = x0 + j * bar;
			if (!data[j][s].length) {
				svg += `<text x="${x + bar / 2 - 1}" y="${y(0) - 6}" class="na" text-anchor="middle">n/a</text>`;
				return;
			}
			const v = b?.rps || 0;
			svg += `<rect x="${x + 2}" y="${y(v)}" width="${bar - 4}" height="${y(0) - y(v)}" rx="3" class="s${j}"><title>${esc(run.label)}: ${bound(data[j][s])}${v} req/s</title></rect>`;
			svg += `<text x="${x + bar / 2}" y="${y(v) - 6}" class="value" text-anchor="middle">${bound(data[j][s])}${v}</text>`;
		});
		svg += `<text x="${left + i * group + group / 2}" y="${h - bottom + 22}" class="axis-label" text-anchor="middle">${esc(names[s])}</text>`;
	});
	svg += `<text x="14" y="${top + (h - top - bottom) / 2}" class="tick" transform="rotate(-90 14 ${top + (h - top - bottom) / 2})" text-anchor="middle">requests / second</text>`;
	return `<svg viewBox="0 0 ${w} ${h}" role="img" aria-label="Highest request rate held cleanly, by scenario and time of day">${svg}</svg>`;
}

// p95 latency against the held rate, one line per run, on a log scale.
function latencyChart(s) {
	const w = 760, h = 300, left = 56, right = 20, top = 20, bottom = 44;
	const steps = [...new Set(data.flatMap((d) => d[s].map((r) => r.rps)))].sort((a, b) => a - b);
	if (!steps.length) return '';
	const values = data.flatMap((d) => d[s].map((r) => r.p95).filter((v) => Number.isFinite(v) && v > 0));
	if (!values.length) return '<p class="muted">No latency measurements.</p>';
	// Decade bounds that include every measurement and the limit, at least one decade apart.
	const lo = 10 ** Math.floor(Math.log10(Math.min(...values, p95Limit)));
	const hi = Math.max(lo * 10, 10 ** Math.ceil(Math.log10(Math.max(...values, p95Limit * 1.5))));
	const x = (rps) => left + (steps.indexOf(rps) / Math.max(1, steps.length - 1)) * (w - left - right);
	const y = (v) => top + (1 - (Math.log10(v) - Math.log10(lo)) / (Math.log10(hi) - Math.log10(lo))) * (h - top - bottom);
	let svg = '';
	for (let v = lo; v <= hi; v *= 10) {
		svg += `<line x1="${left}" x2="${w - right}" y1="${y(v)}" y2="${y(v)}" class="grid"/><text x="${left - 8}" y="${y(v) + 4}" class="tick" text-anchor="end">${ms(v)}</text>`;
	}
	svg += `<line x1="${left}" x2="${w - right}" y1="${y(p95Limit)}" y2="${y(p95Limit)}" class="limit"/><text x="${left + 6}" y="${y(p95Limit) - 6}" class="limit-label">p95 limit ${ms(p95Limit)}</text>`;
	steps.forEach((rps) => {
		svg += `<text x="${x(rps)}" y="${h - bottom + 18}" class="tick" text-anchor="middle">${rps}</text>`;
	});
	svg += `<text x="${(left + w - right) / 2}" y="${h - 6}" class="axis-label" text-anchor="middle">held request rate (requests / second)</text>`;
	data.forEach((d, j) => {
		const rows = d[s].filter((r) => Number.isFinite(r.p95) && r.p95 > 0);
		if (!rows.length) return;
		svg += `<path d="${rows.map((r, i) => `${i ? 'L' : 'M'}${x(r.rps).toFixed(1)},${y(r.p95).toFixed(1)}`).join(' ')}" class="line s${j}"/>`;
		for (const r of rows) {
			const cx = x(r.rps).toFixed(1), cy = y(r.p95).toFixed(1);
			const tip = `<title>${esc(config.runs[j].label)} · ${r.rps}/s: p95 ${ms(r.p95)}${r.passed ? '' : ` — ${esc(failure(r))}`}</title>`;
			svg += r.passed
				? `<circle cx="${cx}" cy="${cy}" r="4.5" class="dot s${j}">${tip}</circle>`
				: r.outcome === 'unknown'
				? `<circle cx="${cx}" cy="${cy}" r="4.5" class="unknown-mark s${j}">${tip}</circle>`
				: `<g class="fail-mark s${j}"><path d="M${cx - 5},${cy - 5}L${+cx + 5},${+cy + 5}M${+cx + 5},${cy - 5}L${cx - 5},${+cy + 5}"/>${tip}</g>`;
		}
	});
	return `<svg viewBox="0 0 ${w} ${h}" role="img" aria-label="p95 latency by held request rate for ${esc(names[s])}">${svg}</svg>`;
}

function stepTable(s) {
	const steps = [...new Set(data.flatMap((d) => d[s].map((r) => r.rps)))].sort((a, b) => a - b);
	const head = config.runs.map((r, j) => `<th scope="col" colspan="2"><span class="key s${j}"></span>${esc(r.label)}</th>`).join('');
	const sub = config.runs.map(() => '<th scope="col">p50 / p95</th><th scope="col">Result</th>').join('');
	const rows = steps.map((rps) => `<tr><th scope="row">${rps}</th>${data.map((d) => {
		const r = d[s].find((x) => x.rps === rps);
		if (!r) return '<td class="muted">–</td><td class="muted">–</td>';
		const pill = r.passed ? '<span class="pill pass">Pass</span>' : r.outcome === 'unknown' ? '<span class="pill unknown">Unknown</span>' : `<span class="pill fail">Fail</span> <span class="why">${esc(failure(r))}</span>`;
		return `<td>${ms(r.p50)} / ${ms(r.p95)}</td><td>${pill}</td>`;
	}).join('')}</tr>`).join('');
	return `<div class="table-wrap"><table><thead><tr><th scope="col" rowspan="2">Held req/s</th>${head}</tr><tr>${sub}</tr></thead><tbody>${rows}</tbody></table></div>`;
}

const kpis = scenarios.map((s) => `<div class="kpi"><div class="kpi-name">${esc(names[s])}</div><dl>${config.runs.map((run, j) => {
	const rows = data[j][s];
	const b = best(rows);
	return `<div><dt><span class="key s${j}"></span>${esc(run.label)}</dt><dd>${rows.length ? `${b ? `${bound(rows)}${rate(b.rps)}` : '0'}<span class="unit">req/s</span>` : '<span class="muted">not run</span>'}</dd></div>`;
}).join('')}</dl></div>`).join('');

const sections = scenarios.map((s) => {
	const verdicts = config.runs.map((run, j) => {
		const rows = data[j][s];
		if (!rows.length) return '';
		const b = best(rows), f = broke(rows);
		return `<li><span class="key s${j}"></span><strong>${esc(run.label)}:</strong> ${b ? `held ${b.rps} req/s at p95 ${ms(b.p95)}` : 'no step passed'}${f ? `; failed at ${f.rps} req/s (${esc(failure(f))})` : b ? '; no failing step observed, so the limit is higher' : ''}.</li>`;
	}).join('');
	return `<section class="card" id="${esc(s)}"><header class="card-head"><div><h3>${esc(names[s])}</h3><p class="sub">${descriptions[s]}</p></div></header>
<ul class="verdicts">${verdicts}</ul><figure>${latencyChart(s)}<figcaption>p95 latency at each held rate (log scale). Filled circles pass, crosses mark the failing step, and hollow circles mark steps whose outcome is unknown.</figcaption></figure>${stepTable(s)}</section>`;
}).join('');

const probes = `<div class="table-wrap"><table><thead><tr><th scope="col">Run</th><th scope="col">Time</th><th scope="col"><code>/readyz</code> to first byte</th><th scope="col">Discover page to first byte</th><th scope="col">Note</th></tr></thead><tbody>${config.runs.map((r, j) => `<tr><th scope="row"><span class="key s${j}"></span>${esc(r.label)}</th><td>${esc(r.when)}</td><td>${r.probe?.readyz ? `${num(r.probe.readyz)} ms` : '–'}</td><td>${r.probe?.discover ? `${num(r.probe.discover)} ms` : '–'}</td><td class="muted">${esc(r.probe?.note || '')}</td></tr>`).join('')}</tbody></table></div>`;

const conditions = `<div class="table-wrap"><table><thead><tr><th scope="col">Run</th><th scope="col">Time</th><th scope="col">Application settings</th></tr></thead><tbody>${config.runs.map((r, j) => `<tr><th scope="row"><span class="key s${j}"></span>${esc(r.label)}</th><td>${esc(r.when)}</td><td>${esc(r.settings)}</td></tr>`).join('')}</tbody></table></div>`;

// One colour per run: the first three are fixed, later runs step around the hue circle.
const palette = ['oklch(72% 0.15 70)', 'oklch(58% 0.18 330)', 'oklch(52% 0.14 240)'];
const seriesColor = (j) => palette[j] || `oklch(58% 0.14 ${(150 + j * 67) % 360})`;
const seriesVars = config.runs.map((_, j) => `--s${j}: ${seriesColor(j)};`).join(' ');
const seriesRules = config.runs.map((_, j) => `.key.s${j} { background: var(--s${j}); } rect.s${j}, .dot.s${j} { fill: var(--s${j}); } .line.s${j}, .fail-mark.s${j} path, .unknown-mark.s${j} { stroke: var(--s${j}); }`).join('\n');

const list = (items) => (items?.length ? `<ul class="prose-list">${items.map((i) => `<li>${i}</li>`).join('')}</ul>` : '');

const html = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>${esc(config.title)}</title><style>
:root {
	color-scheme: light;
	--ink: oklch(24% 0.02 265); --muted: oklch(50% 0.02 265); --line: oklch(91% 0.01 265); --soft: oklch(97.5% 0.005 265);
	--paper: oklch(100% 0 none); --accent: oklch(45% 0.12 265);
${seriesVars}
	--pass: oklch(52% 0.12 155); --fail: oklch(53% 0.19 25);
	font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif; line-height: 1.55; color: var(--ink); background: var(--soft);
	font-size: clamp(0.95rem, 0.9rem + 0.2vw, 1.05rem);
}
*, *::before, *::after { box-sizing: border-box; min-inline-size: 0; }
body { margin: 0; overflow-wrap: break-word; }
h1, h2, h3 { text-wrap: balance; line-height: 1.2; margin: 0; }
code { font-family: ui-monospace, "SF Mono", Menlo, monospace; font-size: 0.88em; background: var(--soft); padding-inline: 0.3em; border-radius: 0.25rem; }
.masthead { background: linear-gradient(135deg, oklch(26% 0.05 265), oklch(34% 0.09 280)); color: oklch(97% 0.01 265); padding-block: clamp(2rem, 1.5rem + 2vw, 3.5rem); }
.wrap { inline-size: min(100% - 2rem, 68rem); margin-inline: auto; }
.eyebrow { text-transform: uppercase; letter-spacing: 0.12em; font-size: 0.75rem; font-weight: 600; color: oklch(82% 0.06 280); }
.masthead h1 { font-size: clamp(1.75rem, 1.3rem + 1.8vw, 2.6rem); margin-block: 0.4rem 0.6rem; font-weight: 700; }
.masthead p { margin: 0; color: oklch(86% 0.03 265); max-inline-size: 60ch; }
.meta { display: flex; flex-wrap: wrap; gap: 0.5rem 1.5rem; margin-block-start: 1.25rem; font-size: 0.85rem; color: oklch(84% 0.03 265); }
.meta strong { color: oklch(97% 0.01 265); font-weight: 600; }
main { padding-block: 2rem 4rem; display: grid; grid-template-columns: minmax(0, 1fr); gap: 1.5rem; }
h2 { font-size: 1.25rem; margin-block: 1rem 0.25rem; }
.lede { color: var(--muted); margin-block: 0 0.5rem; max-inline-size: 70ch; }
.kpis { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 14rem), 1fr)); gap: 1rem; }
.kpi, .card { background: var(--paper); border: 1px solid var(--line); border-radius: 0.9rem; box-shadow: 0 1px 2px oklch(20% 0.02 265 / 0.04), 0 8px 24px -16px oklch(20% 0.02 265 / 0.18); }
.kpi { padding: 1rem 1.1rem; }
.kpi-name { font-weight: 650; margin-block-end: 0.6rem; }
.kpi dl { margin: 0; display: grid; gap: 0.35rem; }
.kpi dl div { display: flex; justify-content: space-between; align-items: baseline; gap: 1rem; }
.kpi dt { color: var(--muted); font-size: 0.85rem; display: flex; align-items: center; gap: 0.45rem; }
.kpi dd { margin: 0; font-weight: 700; font-size: 1.15rem; font-variant-numeric: tabular-nums; }
.unit { font-size: 0.72rem; font-weight: 500; color: var(--muted); margin-inline-start: 0.25rem; }
.card { padding: clamp(1rem, 0.8rem + 1vw, 1.75rem); }
.card-head h3 { font-size: 1.15rem; }
.sub { margin: 0.2rem 0 0; color: var(--muted); font-size: 0.9rem; }
.findings { display: grid; gap: 0.75rem; }
.prose-list { margin: 0; padding-inline-start: 1.2rem; display: grid; gap: 0.5rem; }
.verdicts { list-style: none; padding: 0; margin: 1rem 0 0.5rem; display: grid; gap: 0.35rem; font-size: 0.92rem; }
.verdicts li { padding-inline-start: 1.1rem; text-indent: -1.1rem; }
.key { display: inline-block; inline-size: 0.7rem; block-size: 0.7rem; border-radius: 0.2rem; flex: none; margin-inline-end: 0.35rem; vertical-align: -0.05rem; }
figure { margin: 1rem 0; }
figcaption { color: var(--muted); font-size: 0.8rem; margin-block-start: 0.25rem; }
svg { inline-size: 100%; block-size: auto; display: block; }
.grid { stroke: var(--line); } .tick { font-size: 11px; fill: var(--muted); font-variant-numeric: tabular-nums; } .axis-label { font-size: 12px; fill: var(--ink); font-weight: 600; }
.value { font-size: 11px; font-weight: 700; fill: var(--ink); font-variant-numeric: tabular-nums; } .na { font-size: 10px; fill: var(--muted); }
.line { fill: none; stroke-width: 2.5; stroke-linejoin: round; } .fail-mark path { stroke-width: 2.5; fill: none; }
.dot { stroke: var(--paper); stroke-width: 1.5; } .unknown-mark { fill: var(--paper); stroke-width: 2; }
${seriesRules}
.limit { stroke: var(--fail); stroke-dasharray: 5 5; stroke-width: 1.2; } .limit-label { font-size: 11px; fill: var(--fail); font-weight: 600; }
.legend { display: flex; flex-wrap: wrap; gap: 0.5rem 1.25rem; font-size: 0.85rem; color: var(--muted); }
.table-wrap { overflow-x: auto; border: 1px solid var(--line); border-radius: 0.6rem; }
table { border-collapse: collapse; inline-size: 100%; font-size: 0.86rem; font-variant-numeric: tabular-nums; }
th, td { padding: 0.55rem 0.75rem; text-align: start; border-block-end: 1px solid var(--line); white-space: nowrap; }
thead th { background: var(--soft); font-weight: 600; color: var(--ink); }
tbody tr:last-child > * { border-block-end: 0; }
tbody th { font-weight: 600; }
.pill { display: inline-block; padding: 0.05rem 0.5rem; border-radius: 999px; font-size: 0.75rem; font-weight: 650; }
.pill.pass { background: oklch(94% 0.05 155); color: var(--pass); } .pill.fail { background: oklch(94% 0.04 25); color: var(--fail); } .pill.unknown { background: var(--soft); color: var(--muted); }
.why { color: var(--fail); font-size: 0.78rem; white-space: normal; }
.muted { color: var(--muted); }
.two { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr)); gap: 1.5rem; }
dl.facts { margin: 0; display: grid; gap: 0.6rem; } dl.facts dt { font-weight: 600; } dl.facts dd { margin: 0; color: var(--muted); }
footer { color: var(--muted); font-size: 0.8rem; border-block-start: 1px solid var(--line); padding-block-start: 1rem; }
@media print {
	@page { size: landscape; margin: 12mm; }
	body { background: var(--paper); } .masthead, .key, rect, .pill { print-color-adjust: exact; -webkit-print-color-adjust: exact; }
	.card, .kpi { box-shadow: none; } figure, .kpi { break-inside: avoid; }
	.table-wrap { overflow: visible; } table { font-size: 0.72rem; } th, td { white-space: normal; padding: 0.3rem 0.4rem; }
}
</style></head><body>
<header class="masthead"><div class="wrap">
<div class="eyebrow">Ticketopia · Load benchmark</div>
<h1>${esc(config.title)}</h1>
<p>${esc(config.summary || '')}</p>
<div class="meta"><span><strong>Target</strong> ${esc(config.target)}</span><span><strong>Date</strong> ${esc(config.date)}</span><span><strong>Runs</strong> ${config.runs.length}</span><span><strong>Pass</strong> p95 &lt; ${ms(p95Limit)}, &lt; 1% failed, no dropped iterations</span></div>
</div></header>
<main class="wrap">
<section aria-labelledby="capacity"><h2 id="capacity">Capacity at a glance</h2><p class="lede">Highest request rate each scenario held cleanly, by run.</p><div class="kpis">${kpis}</div></section>
<section class="card"><h3>Highest rate held, by scenario</h3><p class="sub">Each bar is the highest step that passed; “≥” marks runs where no higher step failed, so the limit may be higher.</p><div class="legend" style="margin-block-start:0.75rem">${config.runs.map((r, j) => `<span><span class="key s${j}"></span>${esc(r.label)} · ${esc(r.when)}</span>`).join('')}</div><figure>${capacityChart()}</figure></section>
${config.findings ? `<section class="card findings"><h3>Key findings</h3>${list(config.findings)}</section>` : ''}
<section aria-labelledby="scenarios"><h2 id="scenarios">Results by scenario</h2><p class="lede">Each scenario ran on its own, holding each request rate for ${num(stepSeconds)} seconds at full rate, and stopped at its first failing step.</p></section>
${sections}
<section class="card"><h3>Network baseline</h3><p class="sub">Unloaded time to first byte through Cloudflare, measured from the test machine around each run. Differences here affect every step, independent of server load.</p>${probes}</section>
<section class="card"><h3>Test conditions</h3><div class="two"><dl class="facts"><dt>Server</dt><dd>${esc(config.host || '')}</dd><dt>Load generator</dt><dd>${esc(config.client || '')}</dd></dl><div>${conditions}</div></div></section>
${config.caveats ? `<section class="card"><h3>Caveats</h3>${list(config.caveats)}</section>` : ''}
<footer>Generated ${esc(new Date().toISOString().slice(0, 16).replace('T', ' '))} UTC by <code>scripts/load/compare.mjs</code> from <code>make benchmark</code> results: ${[...new Set(config.runs.flatMap((r) => Object.values(r.sources)))].map((d) => `<code>${esc(d)}</code>`).join(', ')}.</footer>
</main></body></html>`;
writeFileSync(outPath, html);
console.log(`report: ${outPath}`);
