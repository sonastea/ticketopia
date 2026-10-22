// Browser-visible latency (rollout P1.4): Apply → confirmed view rendered,
// cold starts, and commit → rendered update. Times are wall-clock on one host:
// a rendered frame is the animation frame after the DOM change.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.LATENCY_BASE_URL, otherReplica = process.env.LATENCY_OTHER_REPLICA_URL, control = process.env.LATENCY_CONTROL_URL;
const results = {}, failures = [];
const record = (key, value) => (results[key] ||= []).push(Math.round(value));
const percentile = (values, p) => { const s = [...values].sort((a, b) => a - b); return s[Math.max(0, Math.ceil(s.length * p / 100) - 1)]; };
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
const instrument = () => {
  window.liveFrames = [];
  const snapshot = () => ({
    ids: [...document.querySelectorAll('[data-live-list] li')].map(li => li.dataset.id),
    badge: document.querySelector('[data-live-badge]')?.textContent || '',
    busy: document.querySelector('[data-live-filter-form]')?.getAttribute('aria-busy') === 'true',
  });
  const observe = () => new MutationObserver(() => requestAnimationFrame(t => window.liveFrames.push({ t: performance.timeOrigin + t, ...snapshot() })))
    .observe(document.documentElement, { childList: true, subtree: true, characterData: true, attributes: true, attributeFilter: ['aria-busy'] });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', observe); else observe();
};
// First rendered frame at or after since (epoch ms) satisfying predicate.
const frameAfter = async (page, since, predicate, timeout = 20000) => {
  const deadline = Date.now() + timeout;
  for (;;) {
    const frames = await page.evaluate(since => window.liveFrames.filter(f => f.t >= since), since);
    const match = frames.find(predicate);
    if (match) return match.t;
    if (Date.now() > deadline) throw new Error('no matching rendered frame within ' + timeout + 'ms');
    await sleep(25);
  }
};
const same = (a, b) => a.length === b.length && a.every((v, i) => v === b[i]);
const expected = async (page, url) => page.evaluate(async url => (await (await fetch(url, { cache: 'no-store' })).json()).items.map(i => i.event_id).slice(0, 20), url);
const signed = async (browser, url, token, init) => {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  await context.addCookies([{ name: 'ticketopia_session', value: token, url }]);
  if (init) await context.addInitScript(init);
  await context.addInitScript(instrument);
  return context;
};
const guest = async (browser, filter) => {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  if (filter) await context.addInitScript(f => localStorage.setItem('ticketopia-live-filters-v1', JSON.stringify(f)), filter);
  await context.addInitScript(instrument);
  return context;
};
const writer = async (browser, url, token) => {
  const context = await browser.newContext();
  await context.addCookies([{ name: 'ticketopia_session', value: token, url }]);
  const me = await (await context.request.get(url + '/api/v1/me')).json();
  return async (method, route, data) => {
    const response = await context.request[method](url + route, { data, headers: { 'X-CSRF-Token': me.csrf_token } });
    if (!response.ok()) throw new Error(`${method} ${route}: ${response.status()} ${await response.text()}`);
    return Date.now();
  };
};
const open = async (context, url = base) => {
  const page = await context.newPage();
  page.on('pageerror', error => failures.push('page error: ' + error.message));
  await page.goto(url + '/happening-now');
  await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length > 0, null, { timeout: 20000 });
  return page;
};
const publicFilter = category => ({ location: { mode: 'all' }, category_ids: category ? [category] : [], artist_ids: [], venue_ids: [] });
// Apply through the real form; returns click → first frame showing the
// confirmed server view for the new filter.
const apply = async (page, { categories, sources, verify }) => {
  const form = page.locator('[data-live-filter-form]');
  if (!await form.locator('summary').evaluate(summary => summary.parentElement.open)) await form.locator('summary').click();
  await form.getByLabel('Music', { exact: true }).waitFor();
  for (const id of ['Music', 'Sports', 'Arts']) {
    const box = form.getByLabel(id, { exact: true });
    if ((await box.isChecked()) !== categories.includes(id)) await box.setChecked(categories.includes(id));
  }
  for (const [label, id] of [['Saved', 'saved'], ['Interested', 'interested']]) {
    const box = form.getByLabel(label, { exact: true });
    if (sources && await box.count() && (await box.isChecked()) !== sources.includes(id)) await box.setChecked(sources.includes(id));
  }
  const clicked = await page.evaluate(() => { const t = performance.timeOrigin + performance.now(); [...document.querySelectorAll('[data-live-filter-form] button')].find(button => button.textContent.trim() === 'Apply').click(); return t; });
  await page.waitForFunction(() => document.querySelector('[data-live-filter-form]').getAttribute('aria-busy') !== 'true', null, { timeout: 20000 });
  await sleep(300);
  const want = await expected(page, verify);
  const rendered = await frameAfter(page, clicked, f => !f.busy && same(f.ids, want));
  return rendered - clicked;
};

(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    // Warm public Apply: other tabs keep both cohorts active.
    const holders = [];
    for (const category of ['Music', 'Sports']) holders.push(await open(await guest(browser, publicFilter(category))));
    const page = await open(await guest(browser));
    for (let i = 0; i < 21; i++) {
      const category = i % 2 ? 'Sports' : 'Music';
      const ms = await apply(page, { categories: [category], verify: '/api/v1/community/activity?filter=' + encodeURIComponent(JSON.stringify(publicFilter(category))) });
      if (i) record('warm public Apply', ms);
    }
    // Warm personal Apply: membership is assembled and held by a second tab;
    // only the public dimension changes.
    const viewer = await signed(browser, base, process.env.LATENCY_VIEWER_TOKEN);
    const holder = await open(viewer);
    const personal = await open(viewer);
    for (let i = 0; i < 21; i++) {
      const category = ['Music', 'Sports'][i % 2];
      const ms = await apply(personal, { categories: i === 20 ? [] : [category], sources: ['saved', 'interested'], verify: '/api/v1/me/community/activity' });
      if (i && i < 20) record('warm personal Apply', ms);
    }
    await holder.close();
    // Cold personal Apply: accounts whose membership was never assembled.
    for (const token of process.env.LATENCY_COLD_TOKENS.split(',')) {
      const context = await signed(browser, base, token);
      const cold = await open(context);
      record('cold personal Apply', await apply(cold, { categories: [], sources: ['saved', 'interested'], verify: '/api/v1/me/community/activity' }));
      await context.close();
    }
    // Commit → rendered: publication (pending update) and withdrawal.
    const publish = await writer(browser, otherReplica, process.env.LATENCY_OTHER_TOKEN);
    const watch = await open(await guest(browser));
    for (let i = 0; i < 20; i++) {
      const id = 'ticketmaster:LiveCap_' + i;
      const committed = await publish('put', '/api/v1/me/event-recommendations/' + id, { reason: '' });
      record('publication commit → rendered', await frameAfter(watch, committed, f => f.badge !== '') - committed);
      await watch.locator('[data-live-updates]').click();
      await watch.waitForFunction(id => [...document.querySelectorAll('[data-live-list] li')].some(li => li.dataset.id === id), id);
      const removed = await publish('delete', '/api/v1/me/event-recommendations/' + id);
      record('removal commit → rendered', await frameAfter(watch, removed, f => f.ids.length > 0 && !f.ids.includes(id)) - removed);
    }
    // Membership commit → rendered on the viewer's personal page. Writes on the
    // other replica use revision polling; same-replica writes close the stream
    // for the browser's quiet reconnect.
    for (const [label, url] of [['other replica', otherReplica], ['same replica', base]]) {
      const member = await writer(browser, url, process.env.LATENCY_VIEWER_TOKEN);
      for (let i = 1; i <= 10; i++) {
        const id = 'ticketmaster:LiveCap_' + (i * 2 + 1);
        const added = await member('put', '/api/v1/me/event-interests/' + id, { visibility: 'private' });
        record(`membership addition (${label}) commit → rendered`, await frameAfter(personal, added, f => f.ids.includes(id)) - added);
        // A local change first clears the view (unavailable); only a confirmed
        // non-empty view without the item counts as the rendered removal.
        const removed = await member('delete', '/api/v1/me/event-interests/' + id);
        record(`membership removal (${label}) commit → rendered`, await frameAfter(personal, removed, f => f.ids.length > 0 && !f.ids.includes(id)) - removed);
      }
    }
    await Promise.all([...holders, page, personal, watch].map(p => p.close()));
    // Cold start after replica restart: navigation → confirmed first view.
    const firstView = async (context, verify) => {
      const started = Date.now();
      const p = await context.newPage();
      await p.goto(base + '/happening-now');
      await p.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length > 0, null, { timeout: 30000 });
      await sleep(300);
      const want = await expected(p, verify);
      const ms = await frameAfter(p, 0, f => same(f.ids, want)) - started;
      await p.close();
      return ms;
    };
    for (let i = 0; i < 10; i++) {
      await fetch(control + '/restart', { method: 'POST' });
      record('restart cold start (public)', await firstView(await guest(browser), '/api/v1/community/activity'));
      await fetch(control + '/restart', { method: 'POST' });
      record('restart cold start (personal)', await firstView(viewer, '/api/v1/me/community/activity'));
    }
    // Cold start after idle eviction (30 s without subscribers).
    for (let i = 0; i < 4; i++) {
      await sleep(35000);
      if (i % 2) record('eviction cold start (public)', await firstView(await guest(browser), '/api/v1/community/activity'));
      else record('eviction cold start (personal)', await firstView(viewer, '/api/v1/me/community/activity'));
    }
  } finally { await browser.close(); }
  const summary = Object.fromEntries(Object.entries(results).map(([k, v]) => [k, { n: v.length, p50: percentile(v, 50), p95: percentile(v, 95), max: Math.max(...v) }]));
  for (const key of ['warm public Apply', 'warm personal Apply']) if (summary[key].p95 > 1000) failures.push(`${key} p95 ${summary[key].p95}ms > 1000ms`);
  for (const [key, value] of Object.entries(summary)) if (/commit → rendered/.test(key) && value.p95 > 5000) failures.push(`${key} p95 ${value.p95}ms > 5000ms`);
  console.log(JSON.stringify({ milliseconds: summary, failures }, null, 2));
  if (failures.length) process.exit(1);
})().catch(error => { console.error(error); process.exit(1); });
