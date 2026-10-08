// Opt-in production-handler/MariaDB fixtures; see docs/radar.md.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.RADAR_BASE_URL;
const out = process.env.RADAR_REVIEW_DIR || path.resolve(__dirname, '../.impeccable/review');
const report = { checks: [], accessibility: [], screenshots: [], consoleErrors: [] };
const check = (name, result) => { assert.ok(result, name); report.checks.push(name); };
fs.mkdirSync(out, { recursive: true });
(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    const context = async (session, options = {}) => {
      const ctx = await browser.newContext(options);
      if (session) await ctx.addCookies([{ name: 'ticketopia_session', value: session, url: base }]);
      return ctx;
    };
    const owner = await context(process.env.RADAR_SESSION, { viewport: { width: 1440, height: 1000 } });
    const page = await owner.newPage();
    page.on('pageerror', error => report.consoleErrors.push(error.message));
    const rows = () => page.locator('#event-list > .event-row');
    await page.goto(base + '/radar?limit=2');
    const first = await (await owner.request.get(base + '/api/v1/me/radar?limit=2')).json();
    check('Web/API identities and ordering', JSON.stringify(await rows().evaluateAll(nodes => nodes.map(n => n.dataset.eventId))) === JSON.stringify(first.items.map(i => i.event.id)));
    for (const item of first.items) for (const reason of item.reasons) {
      check('Same explanation: ' + reason.code, await page.locator(`[data-event-id="${item.event.id}"]`).getByText(reason.text, { exact: true }).isVisible());
    }
    check('Unknown category label uses understandable fallback', await page.getByText('Matches one of your preferred categories', { exact: true }).count() === 2);
    check('Outage retains old data and incomplete-coverage evidence', await page.getByText('Showing last-known details.', { exact: false }).first().isVisible() && await page.locator('[data-coverage="not_collected"]').isVisible());
    check('Unknown price not free', await rows().getByText('Price not listed', { exact: false }).count() === 2);
    check('No nonexistent preview jump', await page.locator('a[href="#event-context"]').count() === 0);
    const capture = async (name, width, enlarged = false) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate(async enlarged => { document.documentElement.style.fontSize = enlarged ? '200%' : ''; await document.fonts.ready; }, enlarged);
      check(name + ': no horizontal overflow', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      const undersized = await page.locator('main button, main summary').evaluateAll(nodes => nodes.filter(n => n.getClientRects().length && n.getBoundingClientRect().height < 44).map(n => ({ text: n.textContent.trim(), height: n.getBoundingClientRect().height })));
      check(name + ': visible form targets at least 44px ' + JSON.stringify(undersized), undersized.length === 0);
      await page.evaluate(() => scrollTo(0, 0));
      const file = path.join(out, 'radar-' + name + '.png');
      await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
      if (process.env.AXE_SCRIPT) {
        await page.addScriptTag({ path: process.env.AXE_SCRIPT });
        const scan = await page.evaluate(() => axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } }));
        report.accessibility.push({ name, violations: scan.violations.map(v => ({ id: v.id, impact: v.impact })) });
        check(name + ': axe clean', scan.violations.length === 0);
      }
    };
    await capture('desktop', 1440); await capture('tablet', 1024); await capture('mobile', 390); await capture('320', 320); await capture('200-text', 390, true);
    await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
    await page.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
    const more = page.getByRole('link', { name: 'More radar events', exact: true }); await more.focus();
    check('Keyboard forced-colors focus remains visible', await more.evaluate(n => getComputedStyle(n).outlineStyle !== 'none'));
    await Promise.all([page.waitForURL(url => url.searchParams.has('cursor')), page.keyboard.press('Enter')]);
    check('Native next page is distinct', await rows().count() === 2 && !(await rows().evaluateAll(nodes => nodes.map(n => n.dataset.eventId))).some(id => first.items.some(item => item.event.id === id)));
    check('End cursor has no next link', await page.getByRole('link', { name: 'More radar events', exact: true }).count() === 0);
    await page.emulateMedia({ forcedColors: 'none', reducedMotion: 'no-preference' });
    await page.goto(base + '/radar?limit=2');
    const nextURL = await page.getByRole('link', { name: 'More radar events', exact: true }).getAttribute('href');
    const me = await (await owner.request.get(base + '/api/v1/me')).json();
    const changed = await owner.request.patch(base + '/api/v1/me/preferences', { headers: { 'X-CSRF-Token': me.csrf_token, Origin: base }, data: { category_ids: ['sports'] } });
    check('Explicit preference change accepted', changed.status() === 200);
    const response = await page.goto(base + nextURL);
    check('Changed set offers first-page recovery', response.status() === 409 && await page.getByText('Your radar changed while you were browsing.', { exact: false }).isVisible());
    await page.getByRole('link', { name: 'Refresh Radar', exact: true }).click();
    check('Refresh recovers matches from follows despite category change', await rows().count() === 3);
    const native = await context(process.env.RADAR_SESSION, { javaScriptEnabled: false }); const nativePage = await native.newPage();
    await nativePage.goto(base + '/radar?limit=2');
    await nativePage.getByRole('button', { name: /^Save — Radar show 0$/ }).click();
    check('No-JS save stays on radar with authoritative state', new URL(nativePage.url()).pathname === '/radar' && await nativePage.getByRole('button', { name: /^Remove from Saved — Radar show 0$/ }).isVisible());
    await nativePage.getByRole('link', { name: 'Details for Radar show 0', exact: true }).click();
    check('Native event expansion retains radar return context', new URL(nativePage.url()).searchParams.get('return_to') === '/radar?limit=2');
    const touch = await context(process.env.RADAR_SESSION, { hasTouch: true, viewport: { width: 390, height: 844 } }); const touchPage = await touch.newPage();
    await touchPage.goto(base + '/radar');
    await Promise.all([touchPage.waitForURL(url => url.pathname.startsWith('/events/')), touchPage.getByRole('link', { name: 'Details for Radar show 0', exact: true }).tap()]);
    check('Touch opens real event details', new URL(touchPage.url()).pathname.startsWith('/events/'));
    for (const [session, text, name] of [
      [process.env.RADAR_NEW_SESSION, 'Where should we look?', 'new-account'],
      [process.env.RADAR_NEARBY_SESSION, 'These are nearby suggestions, not personalized matches yet.', 'nearby'],
      [process.env.RADAR_EMPTY_SESSION, 'No matches in the available event data', 'no-matches']
    ]) {
      const ctx = await context(session); const otherPage = await ctx.newPage(); await otherPage.goto(base + '/radar');
      check(name + ': honest state', await otherPage.getByText(text, { exact: false }).isVisible());
      if (name !== 'nearby') check(name + ': no foreign artist explanation', await otherPage.getByText('You follow Private artist', { exact: true }).count() === 0);
      await ctx.close();
    }
    const guest = await context(); const guestPage = await guest.newPage(); await guestPage.goto(base + '/radar');
    check('Guest sign-in preserves task', guestPage.url().includes('/auth/sign-in?return_to='));
    await guestPage.goto(process.env.RADAR_DISABLED_URL + '/radar');
    check('Disabled accounts explain requirement', await guestPage.getByText("Radar requires accounts, which aren't enabled", { exact: false }).isVisible());
    await page.goto(base + '/me'); await page.locator('main').getByRole('link', { name: 'Radar', exact: true }).click();
    check('Compact Profile navigation reaches Radar', new URL(page.url()).pathname === '/radar');
    check('No JavaScript errors', report.consoleErrors.length === 0);
    fs.writeFileSync(path.join(out, 'radar-report.json'), JSON.stringify(report, null, 2)); console.log(JSON.stringify(report, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
