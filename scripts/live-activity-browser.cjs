// Production routes and two replica-local hubs over isolated MariaDB. Synthetic
// transport frames below cover timing/overload independently of publication policy.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.SAVED_BASE_URL, replica = process.env.SAVED_FALLBACK_URL;
const output = path.resolve(__dirname, '../.impeccable/review/live-activity');
fs.mkdirSync(output, { recursive: true });
const checks = [], errors = [];
const check = (label, value) => { assert.ok(value, label); checks.push(label); };
(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    const owner = await browser.newContext();
    await owner.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const me = await (await owner.request.get(base + '/api/v1/me')).json();
    const write = async (method, route, data) => {
      const response = await owner.request[method](base + route, { data, headers: { 'X-CSRF-Token': me.csrf_token } });
      check(method + ' ' + route, response.ok()); return response;
    };
    await write('patch', '/api/v1/me/profile', { display_name: 'Forbidden Full Surname', activity_first_name: 'Alex' });
    const guest = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    const page = await guest.newPage(); page.on('pageerror', e => errors.push(e.message));
    const connected = async p => {
      await p.waitForFunction(() => window.fixtureStreams?.some(s => s.readyState === 1));
    };
    await guest.addInitScript(() => {
      const Native = EventSource; window.fixtureStreams = [];
      window.EventSource = class extends Native { constructor(...args) { super(...args); window.fixtureStreams.push(this); } };
    });
    await page.goto(replica + process.env.SAVED_SEARCH_PATH); await connected(page);
    check('No above-content activity or filter editor', await page.locator('[data-live-details], [data-live-filter-form]').count() === 0);
    check('Initial snapshot is quiet', await page.locator('[data-live-toasts] li').count() === 0);
    check('One document stream', await page.evaluate(() => fixtureStreams.filter(s => s.readyState !== 2).length === 1));
    await write('put', '/api/v1/me/event-recommendations/ticketmaster:Browser_0', { reason: 'Forbidden reason' });
    const toast = page.locator('[data-live-toasts] li'); await toast.waitFor({ timeout: 6000 });
    check('Other replica publishes toast', (await toast.textContent()).includes('Alex recommended'));
    const dom = await toast.innerHTML();
    check('Toast DTO excludes identities/reason', !/Forbidden|\/users\//.test(dom) && !dom.includes(me.item.id));
    await page.screenshot({ path: path.join(output, 'desktop.png'), fullPage: true });
    const prior = page.locator('.desktop-nav a[href="/happening-now"]'); await prior.focus();
    await toast.getByRole('button', { name: /Dismiss/ }).focus(); await page.keyboard.press('Enter');
    check('Dismiss restores prior keyboard control', await prior.evaluate(el => el === document.activeElement));
    check('Dismiss only removes toast', await toast.count() === 0);
    await prior.click(); await page.waitForURL('**/happening-now'); await connected(page);
    const rows = page.locator('[data-live-list] li'); await rows.waitFor();
    check('Dismissed event remains browsable', await rows.count() === 1);
    await write('put', '/api/v1/me/event-recommendations/ticketmaster:Browser_1', { reason: '' });
    await page.locator('[data-live-updates]').waitFor({ state: 'visible', timeout: 6000 });
    check('Page buffers new rows without duplicate toast', await rows.count() === 1 && await toast.count() === 0);
    await write('patch', '/api/v1/me/profile', { activity_first_name: '' });
    await rows.getByText('Someone recommended', { exact: false }).waitFor({ timeout: 6000 });
    check('Buffered name redaction is prompt', await rows.count() === 1);
    await page.locator('[data-live-updates]').click(); check('Explicit page updates', await rows.count() === 2);
    await rows.locator('a').first().click(); await page.waitForURL('**/events/**');
    check('Activity opens Overview', new URL(page.url()).searchParams.get('section') !== 'community');
    await page.getByRole('link', { name: 'Back to Happening Now' }).click(); await page.waitForURL('**/happening-now');
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 2);
    check('Overview/Back restores browsing origin', true);
    await page.locator('[data-live-hide]').click();
    check('Local hide keeps deliberate page browsing', await rows.count() === 2);
    await page.reload(); await page.locator('[data-live-show]').waitFor({ state: 'visible' });
    check('Existing browser-local hide persists', await rows.count() === 2);
    await page.goto(replica + '/community');
    await page.waitForTimeout(500);
    check('Hidden background delivery stops', await page.evaluate(() => !fixtureStreams.length));
    await page.goto(replica + '/happening-now'); await connected(page);
    await page.locator('[data-live-show]').click();
    const removed = page.locator('[data-live-list] a[data-id="ticketmaster:Browser_0"]'); await removed.focus();
    await write('delete', '/api/v1/me/event-recommendations/ticketmaster:Browser_0');
    await page.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('Withdrawal restores page heading focus', await page.locator('h1').evaluate(el => el === document.activeElement));
    for (const width of [1440, 1050, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      check('Navigation reachable at ' + width, await page.locator('nav a[href="/happening-now"]:visible').count() === 1);
      check('No overflow at ' + width, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      if (width === 320) await page.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
    }
    await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
    check('200% text wraps', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
    await page.keyboard.press('Tab');
    await page.locator('[data-live-hide]').focus();
    check('Forced-colors keyboard focus', await page.locator('[data-live-hide]').evaluate(el => getComputedStyle(el).outlineStyle !== 'none'));
    if (process.env.AXE_SCRIPT) {
      await page.addScriptTag({ path: process.env.AXE_SCRIPT });
      const violations = await page.evaluate(async () => (await axe.run()).violations);
      check('Page automated accessibility: ' + JSON.stringify(violations.map(v => v.id)), violations.length === 0);
    }
    const native = await browser.newContext({ javaScriptEnabled: false });
    const np = await native.newPage(); await np.goto(replica + '/happening-now');
    check('Native current snapshot, time and Refresh', await np.locator('[data-live-list] li').count() === 1 && await np.locator('time').count() === 1 && await np.getByRole('link', { name: 'Refresh', exact: true }).count() === 1);
    await np.locator('[data-live-filter-details] summary').click();
    await np.getByLabel('Location', { exact: true }).selectOption('city');
    await np.getByLabel('City', { exact: true }).fill('Berlin'); await np.getByLabel('Two-letter country').fill('DE');
    await np.getByRole('button', { name: 'Apply', exact: true }).click();
    check('Native guest filter form', await np.locator('[data-live-list] li').count() === 0 && np.url().includes('live_city=Berlin'));
    await np.locator('[data-live-empty-reset]').click();
    check('Native guest Reset', await np.locator('[data-live-list] li').count() === 1);
    const signed = await owner.newPage(); await signed.goto(base + '/happening-now');
    await signed.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    await write('patch', '/api/v1/me/preferences', { notifications_paused: true });
    await signed.locator('[data-live-paused]').waitFor({ state: 'visible', timeout: 6000 });
    check('Account pause clears page', await signed.locator('[data-live-list] li').count() === 0);
    await signed.reload(); check('Pause survives page entry', await signed.locator('[data-live-paused]').isVisible());
    await write('patch', '/api/v1/me/preferences', { notifications_paused: false });
    await signed.evaluate(() => window.dispatchEvent(new Event('focus')));
    await signed.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    const preferences = await owner.request.get(base + '/api/v1/me/live-activity-preferences');
    const pref = await preferences.json();
    const disabled = await owner.request.put(base + '/api/v1/me/live-activity-preferences', { headers: { 'X-CSRF-Token': me.csrf_token, 'If-Match': preferences.headers().etag }, data: { version: 1, enabled: false, filters: pref.filters } });
    check('Live-disable saves', disabled.ok()); await signed.locator('[data-live-paused]').waitFor({ state: 'visible' });
    const nativeOwner = await browser.newContext({ javaScriptEnabled: false });
    await nativeOwner.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const no = await nativeOwner.newPage(); await no.goto(base + '/happening-now');
    await no.locator('[data-live-filter-details] summary').click(); await no.getByRole('button', { name: 'Reset filters' }).click();
    await no.waitForURL('**/happening-now');
    check('Native owner Reset preserves disable', await no.locator('[data-live-paused]').isVisible() && !(await (await owner.request.get(base + '/api/v1/me/live-activity-preferences')).json()).enabled);
    await owner.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_OTHER_TOKEN, url: base }]);
    await signed.evaluate(() => window.dispatchEvent(new Event('focus')));
    await signed.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('Account switching discards disabled scope', await signed.locator('[data-live-paused]').isHidden());
    const otherMe = await (await owner.request.get(base + '/api/v1/me')).json();
    check('Session revocation', (await owner.request.delete(base + '/api/v1/me/session', { headers: { 'X-CSRF-Token': otherMe.csrf_token } })).ok());
    await signed.locator('[data-live-paused]').waitFor({ state: 'visible', timeout: 6000 });
    check('Revocation never widens to guest scope', await signed.locator('[data-live-list] li').count() === 0);
    const fallback = await browser.newContext();
    await fallback.addInitScript(() => { window.EventSource = undefined; Object.defineProperty(window, 'localStorage', { get() { throw new Error('blocked'); } }); });
    const fp = await fallback.newPage(); await fp.goto(replica + '/happening-now');
    await fp.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('JSON fallback and denied storage', true);
    await fp.goto(process.env.SAVED_DISABLED_URL + '/happening-now');
    check('Kill switch keeps explanatory native page', await fp.getByText('Live activity is unavailable on this server.').isVisible() && await fp.locator('nav a[href="/happening-now"]').count() === 0);
    await guest.close(); await owner.close(); await native.close(); await nativeOwner.close(); await fallback.close();

    // Deterministic transport clock: burst bounds, lifetime, redaction, focus,
    // quiet baseline/reconnect and expiry use the real production controller.
    const synthetic = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await synthetic.addInitScript(() => {
      window.liveSources = [];
      window.EventSource = class {
        constructor() { this.listeners = {}; liveSources.push(this); }
        addEventListener(type, callback) { this.listeners[type] = callback; }
        emit(type, data) { if (!this.closed) this.listeners[type]?.({ data: JSON.stringify(data) }); }
        close() { this.closed = true; }
      };
    });
    const sp = await synthetic.newPage(); sp.on('pageerror', e => errors.push(e.message)); await sp.clock.install();
    await sp.goto(replica + '/community'); await sp.waitForFunction(() => liveSources.length > 0);
    const item = (id, stamp = 1) => ({ event_id: 'ticketmaster:' + id, event_title: 'Readable public event ' + id, city: 'London', actor_first_name: 'Alex', recent_recommendations: 1, latest_recommended_at: `2099-01-01T00:00:0${stamp}Z`, expires_at: '2099-01-01T00:10:00Z' });
    const emit = async (items, at = 1) => sp.evaluate(data => liveSources.at(-1).emit('activity.snapshot', data), { as_of: `2099-01-01T00:00:0${at}Z`, items });
    await emit([], 0); await emit([1, 2, 3, 4, 5].map(id => item(id)));
    const st = sp.locator('[data-live-toasts] li');
    check('Burst capped at three', await st.count() === 3);
    await st.first().getByRole('button').focus(); await sp.clock.runFor(8100);
    check('Focused toast pauses; other toasts expire without backlog', await st.count() === 1);
    await sp.locator('.desktop-nav a[href="/happening-now"]').focus(); await sp.clock.runFor(8100);
    check('Eight non-interacting seconds dismiss', await st.count() === 0);
    check('Dismiss clears announcement for the next batch', await sp.locator('[data-live-status]').textContent() === '');
    await emit([item('hover', 2)], 2); await st.hover(); await sp.clock.runFor(8100);
    check('Hover suspends lifetime', await st.count() === 1);
    check('Later batch announces availability again', (await sp.locator('[data-live-status]').textContent()).includes('New public recommendations'));
    await sp.mouse.move(0, 900);
    await emit([{ ...item('hover', 3), recent_recommendations: 2 }], 3);
    check('Same event groups in one toast', await st.count() === 1 && (await st.textContent()).includes('2 people'));
    await sp.clock.runFor(8100); check('New contribution does not reset lifetime', await st.count() === 0);
    await emit([{ ...item('hover', 3), recent_recommendations: 2 }], 3);
    check('Dismissed contribution is not replayed', await st.count() === 0);
    await sp.evaluate(() => { const dialog = document.createElement('dialog'); dialog.innerHTML = '<button>Continue task</button>'; document.body.append(dialog); dialog.showModal(); });
    await emit([item('dialog', 4)], 4); check('Dialog defers toast', await st.count() === 0);
    await sp.evaluate(() => document.querySelector('dialog').remove());
    await sp.evaluate(() => window.dispatchEvent(new Event('focus')));
    await sp.waitForFunction(() => liveSources.length > 1);
    await emit([item('reconnect', 1)], 1); check('Reconnect snapshot is quiet', await st.count() === 0);
    await emit([item('redact', 2)], 2); await emit([{ ...item('redact', 2), actor_first_name: null }], 3);
    check('Visible toast redaction updates in place', await st.count() === 1 && (await st.textContent()).includes('Someone recommended'));
    await emit([], 4); check('Withdrawal clears toast', await st.count() === 0);
    await emit([item('old')], 5); check('Old metadata admission is quiet', await st.count() === 0);
    await sp.setViewportSize({ width: 390, height: 900 }); await emit([item('mobile1', 6), item('mobile2', 6)], 6);
    check('Compact burst capped at one', await st.count() === 1);
    await sp.screenshot({ path: path.join(output, 'mobile-toast.png'), fullPage: true });
    await st.first().getByRole('button').focus();
    await emit([{ ...item('mobile1', 6), expires_at: '2099-01-01T00:00:07Z' }], 6);
    await sp.route('**/api/v1/community/activity?*', route => route.fulfill({ json: { as_of: '2099-01-01T00:00:08Z', items: [] } }));
    await sp.clock.runFor(1200); check('Server-relative expiry wins over focus pause', await st.count() === 0);
    await sp.clock.runFor(31000); check('Heartbeat watchdog closes stalled stream', await sp.evaluate(() => liveSources[0].closed));
    await synthetic.close();
    check('No browser runtime errors: ' + errors.join(', '), errors.length === 0);
    fs.writeFileSync(path.join(output, 'checks.json'), JSON.stringify({ checks, errors }, null, 2));
    console.log(JSON.stringify({ checks: checks.length, output }));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
