// Live replies in a real browser (docs/load-testing.md#live-replies): how long
// another person's reply takes to be offered and then shown, and that nothing
// moves or is lost on the way. The owner holds their own thread open (so
// checks start at once) with a draft in its reply composer; the other account
// replies over the API; the script times creation → "Show N new replies"
// rendered → the reply rendered and focused after the person asks.
//
// MANIFEST=<review manifest.json> PLAYWRIGHT_MODULE=<…/node_modules/playwright>
// [CHROME_PATH=<browser>] [ROUNDS=5] [REPLIER=other] node scripts/live-replies-browser.cjs
// REPLIER is the manifest account that replies; each may post 20 times in ten minutes.
// Run it against an idle review server, or alongside make live-replies to see
// the same under load.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const manifest = JSON.parse(fs.readFileSync(process.env.MANIFEST, 'utf8'));
const base = manifest.base_url;
const rounds = Number(process.env.ROUNDS || 5);
const dir = path.dirname(process.env.MANIFEST);
const results = { offered: [], shown: [] }, failures = [];
const percentile = (values, p) => { const s = [...values].sort((a, b) => a - b); return s[Math.max(0, Math.ceil(s.length * p / 100) - 1)]; };

async function reply(event, thread, body) {
  const response = await fetch(`${base}/api/v1/events/${encodeURIComponent(event)}/discussions/${thread}/replies`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${manifest.accounts[process.env.REPLIER || 'other'].api_token}`, 'Content-Type': 'application/json', 'Idempotency-Key': crypto.randomUUID().replaceAll('-', '') },
    body: JSON.stringify({ body }),
  });
  if (response.status !== 201) throw new Error(`reply failed: ${response.status} ${await response.text()}`);
  return response.json();
}

(async () => {
  const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {});
  const context = await browser.newContext({ storageState: path.join(dir, 'owner.json'), viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();
  const event = manifest.event_ids[0];
  await page.goto(base + manifest.paths.discussion);
  for (let round = 1; round <= rounds; round++) {
    const draft = `Draft kept through round ${round}`;
    await page.locator('#discussion-composer textarea').fill(draft);
    // Read from the top, as someone partway through the conversation would,
    // with focus out of the composer (a focused composer is kept in view).
    await page.evaluate(() => { document.activeElement?.blur(); scrollTo(0, 0); });
    const before = await page.evaluate(() => scrollY);
    // Replies arrive at any point in the check interval.
    await new Promise(resolve => setTimeout(resolve, Math.random() * 10000));
    const posted = await reply(event, manifest.discussion_thread_id, `Live reply round ${round}`);
    const created = Date.parse(posted.created_at);
    const show = page.locator('[data-live-replies-show]:not([hidden])');
    await show.waitFor({ timeout: 30000 });
    const offered = await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => resolve(performance.timeOrigin + performance.now()))));
    results.offered.push(Math.round(offered - created));
    if (await page.evaluate(() => scrollY) !== before) failures.push(`round ${round}: the page moved before the person asked`);
    // Only this round's reply is new: what an earlier round showed is never re-offered.
    const label = await page.locator('[data-live-replies-label]').textContent();
    if (label !== 'Show 1 new reply') failures.push(`round ${round}: offered "${label}"`);
    const status = await page.locator('[data-live-replies-status]').textContent();
    if (!/new repl/.test(status)) failures.push(`round ${round}: nothing was announced (${status})`);
    const clicked = Date.now();
    await show.click();
    const article = page.locator(`#post-${posted.id}`);
    await article.waitFor({ timeout: 15000 });
    await page.waitForFunction(id => document.activeElement?.closest('#post-' + id), posted.id, { timeout: 5000 })
      .catch(() => failures.push(`round ${round}: focus did not move to the new reply`));
    results.shown.push(Date.now() - clicked);
    if (await page.locator('#discussion-composer textarea').inputValue() !== draft) failures.push(`round ${round}: the reply draft was lost`);
    if (!(await page.locator('[data-live-replies-show]').isHidden())) failures.push(`round ${round}: the control stayed after showing the replies`);
  }
  await browser.close();
  const summary = Object.fromEntries(Object.entries(results).map(([k, v]) => [k, { n: v.length, p50: percentile(v, 50), p95: percentile(v, 95), max: Math.max(...v) }]));
  console.log(JSON.stringify({ base, rounds, ms: summary, failures }, null, 2));
  process.exit(failures.length ? 1 : 0);
})().catch(error => { console.error(error); process.exit(1); });
