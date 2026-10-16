// Opt-in real MariaDB, two replica-local hubs, production shell and API contracts.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.SAVED_BASE_URL;
const replica = process.env.SAVED_FALLBACK_URL;
const output = path.resolve(__dirname, '../.impeccable/review/live-activity');
fs.mkdirSync(output, { recursive: true });
const checks = [];
const check = (label, value) => { assert.ok(value, label); checks.push(label); };
(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    const owner = await browser.newContext();
    await owner.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const me = await (await owner.request.get(base + '/api/v1/me')).json();
    const write = async (method, route, data) => {
      const response = await owner.request[method](base + route, { data, headers: { 'X-CSRF-Token': me.csrf_token } });
      check(`${method} ${route}`, response.ok()); return response;
    };
    await write('patch', '/api/v1/me/profile', { display_name: 'Forbidden Full Surname', bio: 'Forbidden biography', activity_first_name: 'Alex' });
    const guest = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    const page = await guest.newPage();
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.goto(replica + '/community');
    const root = page.locator('[data-live-activity]');
    await root.waitFor({ state: 'visible' });
    await page.waitForFunction(() => document.querySelector('[data-live-connection]').textContent.includes('No recent'));
    check('Default collapsed', !await root.locator('details').evaluate(element => element.open));
    await write('put', '/api/v1/me/event-recommendations/ticketmaster:Browser_0', { reason: 'Forbidden reason' });
    await page.waitForFunction(() => document.querySelector('[data-live-badge]').textContent === '1 new', null, { timeout: 5000 });
    await root.locator('summary').click();
    await root.locator('[data-live-list]').getByText('Alex recommended', { exact: false }).waitFor();
    const serialized = await root.evaluate(element => element.outerHTML);
    for (const secret of ['Forbidden', me.item.id, me.item.email, '/users/']) check('DOM excludes ' + secret, !serialized.includes(secret));
    check('Opening consumes new indicator', await root.locator('[data-live-badge]').textContent() === '');
    await write('put', '/api/v1/me/event-recommendations/ticketmaster:Browser_1', { reason: '' });
    await root.locator('[data-live-updates]').waitFor({ state: 'visible', timeout: 5000 });
    check('New row buffered while reading', await root.locator('li').count() === 1);
    await write('patch', '/api/v1/me/profile', { activity_first_name: '' });
    await root.locator('[data-live-list]').getByText('Someone recommended', { exact: false }).waitFor({ timeout: 5000 });
    check('Name cleared while updates buffered', await root.locator('li').count() === 1 && await root.locator('[data-live-updates]').isVisible());
    await root.locator('[data-live-updates]').click();
    check('Explicit updates reveal both events', await root.locator('li').count() === 2);
    const link = root.locator('a[data-id="ticketmaster:Browser_0"]');
    await link.focus();
    await write('delete', '/api/v1/me/event-recommendations/ticketmaster:Browser_0');
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('Focused withdrawal restores local disclosure focus', await root.locator('summary').evaluate(element => element === document.activeElement));
    await write('put', '/api/v1/me/event-recommendations/ticketmaster:Browser_0', { reason: '' });
    const snapshot = await (await owner.request.get(base + '/api/v1/community/activity')).json();
    check('Republish does not announce again', snapshot.items.length === 1);
    for (const width of [1440, 768, 390, 320]) {
      await page.setViewportSize({ width, height: 900 });
      check('No horizontal overflow at ' + width, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      check('44px actions at ' + width, await root.locator('summary, button:visible, a:visible').evaluateAll(elements => elements.every(element => element.getBoundingClientRect().height >= 44)));
      if (process.env.PARTICIPATION_SKIP_CAPTURES !== 'true') await page.screenshot({ path: path.join(output, `${width}.png`), fullPage: true });
    }
    await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
    check('200% text wraps', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
    await page.keyboard.press('Tab');
    await root.locator('summary').focus();
    check('Forced-colors focus visible', await root.locator('summary').evaluate(element => getComputedStyle(element).outlineStyle !== 'none'));
    if (process.env.AXE_SCRIPT) {
      await page.addScriptTag({ path: process.env.AXE_SCRIPT });
      const violations = await page.evaluate(async () => (await axe.run(document.querySelector('[data-live-activity]'))).violations);
      check('Live region automated accessibility', violations.length === 0);
    }
    await root.locator('[data-live-list] a').click();
    await page.waitForURL('**/events/**');
    check('Live link opens Overview', new URL(page.url()).searchParams.get('section') !== 'community');
    await page.locator('[data-return-results]').click();
    await page.waitForURL('**/community');
    check('Back preserves Community origin', true);
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    if (!await root.locator('details').evaluate(element => element.open)) await root.locator('summary').click();
    await root.locator('[data-live-hide]').click();
    check('Hide clears attribution', await root.locator('li').count() === 0);
    await page.reload(); await root.locator('[data-live-show]').waitFor({ state: 'visible' });
    check('Hide preference persists', await root.locator('details').isHidden());
    await root.locator('[data-live-show]').click();
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('Reconnect is quiet', await root.locator('[data-live-badge]').textContent() === '');
    await guest.setOffline(true);
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 0);
    check('Offline immediately clears attribution', await root.locator('[data-live-connection]').textContent() === 'Live activity is reconnecting');
    await guest.setOffline(false);
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    // JSON fallback and storage denial are independent of the live transport.
    const fallback = await browser.newContext();
    await fallback.addInitScript(() => { window.EventSource = undefined; Object.defineProperty(window, 'localStorage', { get() { throw new Error('blocked'); } }); });
    const fallbackPage = await fallback.newPage(); await fallbackPage.goto(replica + '/community');
    await fallbackPage.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('JSON fallback works with blocked storage', true);
    await fallback.close();
    // Signed-in account pause wins over the local preference.
    const signedPage = await owner.newPage(); await signedPage.goto(base + '/community');
    await signedPage.locator('[data-live-paused]').waitFor({ state: 'visible' });
    check('Existing account pause suppresses live display', await signedPage.locator('[data-live-details]').isHidden());
    await write('patch', '/api/v1/me/preferences', { notifications_paused: false });
    await signedPage.evaluate(() => window.dispatchEvent(new Event('focus')));
    await signedPage.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('Focus rechecks account preference', true);
    await write('put', '/api/v1/me/event-interests/ticketmaster:Browser_0', { visibility: 'private' });
    await signedPage.goto(base + '/events/ticketmaster:Browser_0?section=community');
    const composer = signedPage.locator('[data-recommendation-form] textarea');
    await signedPage.locator('[data-recommendation-editor] summary').click();
    await composer.fill('Keep this unsubmitted draft');
    const other = await browser.newContext();
    await other.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_OTHER_TOKEN, url: base }]);
    const otherMe = await (await other.request.get(base + '/api/v1/me')).json();
    check('Other viewer private interest write', (await other.request.put(base + '/api/v1/me/event-interests/ticketmaster:Browser_0', { data: { visibility: 'private' }, headers: { 'X-CSRF-Token': otherMe.csrf_token } })).ok());
    await signedPage.waitForFunction(() => document.querySelector('[data-interest-count] strong')?.textContent === '2', null, { timeout: 20000 });
    check('Live counts retain private selected control', await signedPage.locator('[data-interest-form]:not([data-interest-edit]) button').getAttribute('aria-pressed') === 'true');
    check('Live updates preserve composer draft', await composer.inputValue() === 'Keep this unsubmitted draft');
    await other.close();
    await write('patch', '/api/v1/me/preferences', { notifications_paused: true });
    await signedPage.evaluate(() => window.dispatchEvent(new Event('focus')));
    await signedPage.locator('[data-live-paused]').waitFor({ state: 'visible' });
    check('Pause removes attribution', await signedPage.locator('[data-live-list] li').count() === 0);
    const stalled = await browser.newContext();
    await stalled.addInitScript(() => {
      window.liveSources = [];
      window.EventSource = class {
        constructor() { this.listeners = {}; window.liveSources.push(this); }
        addEventListener(type, callback) { this.listeners[type] = callback; }
        emit(type, data) { this.listeners[type]?.({ data: JSON.stringify(data) }); }
        close() { this.closed = true; }
      };
    });
    const stalledPage = await stalled.newPage();
    await stalledPage.clock.install();
    await stalledPage.goto(replica + '/community');
    await stalledPage.waitForFunction(() => window.liveSources.length > 0);
    const future = { as_of: '2099-01-01T00:00:00Z', items: [{ event_id: 'ticketmaster:Clock', event_title: 'Clock-independent event', city: 'London', actor_first_name: 'Alex', recent_recommendations: 1, latest_recommended_at: '2099-01-01T00:00:00Z', expires_at: '2099-01-01T00:10:00Z' }] };
    await stalledPage.evaluate(data => window.liveSources.at(-1).emit('activity.snapshot', data), future);
    check('Server-relative expiry ignores visitor wall clock', await stalledPage.locator('[data-live-list] li').count() === 1);
    await stalledPage.clock.runFor(31000);
    check('Silent heartbeat stall clears attribution', await stalledPage.locator('[data-live-list] li').count() === 0);
    check('Silent heartbeat stall closes old stream', await stalledPage.evaluate(() => window.liveSources[0].closed));
    await stalledPage.clock.runFor(6000);
    await stalledPage.evaluate(data => window.liveSources.at(-1).emit('activity.snapshot', data), future);
    await stalledPage.route('**/api/v1/community/activity', route => route.fulfill({ json: { as_of: '2099-01-01T00:00:01Z', items: [] } }));
    const expiring = structuredClone(future); expiring.items[0].expires_at = '2099-01-01T00:00:01Z';
    await stalledPage.evaluate(data => window.liveSources.at(-1).emit('activity.snapshot', data), expiring);
    await stalledPage.clock.runFor(1200);
    check('Monotonic expiry removes a group without a new stream frame', await stalledPage.locator('[data-live-list] li').count() === 0);
    await stalled.close();
    check('No browser runtime errors', errors.length === 0);
    fs.writeFileSync(path.join(output, 'checks.json'), JSON.stringify({ checks, errors }, null, 2));
    console.log(`${checks.length} live activity checks passed`);
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
