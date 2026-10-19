// Two replica-local services and separate SQL pools, using production routes.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.SAVED_BASE_URL, replica = process.env.SAVED_FALLBACK_URL;
const checks = [], errors = [];
const check = (label, value) => { assert.ok(value, label); checks.push(label); };
(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    const owner = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await owner.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const me = await (await owner.request.get(base + '/api/v1/me')).json();
    const write = async (method, route, data) => {
      const response = await owner.request[method](base + route, { data, headers: { 'X-CSRF-Token': me.csrf_token } });
      assert.ok(response.ok(), `${method} ${route}: ${response.status()} ${await response.text()}`);
    };
    for (let i = 0; i < 4; i++) await write('delete', '/api/v1/me/saved-events/ticketmaster:LiveFilter_' + i);
    const page = await owner.newPage(); page.on('pageerror', e => errors.push(e.message));
    await page.addInitScript(() => {
      const Native = EventSource;
      window.activityFrames = [];
      window.EventSource = class extends Native {
        constructor(...args) { super(...args); this.addEventListener('activity.snapshot', event => window.activityFrames.push(JSON.parse(event.data))); }
      };
    });
    await page.goto(replica + '/happening-now');
    const form = page.locator('[data-live-filter-form]');
    const count = n => page.waitForFunction(n => document.querySelectorAll('[data-live-list] li').length === n, n, { timeout: 10000 });
    await count(20); await form.locator('summary').click();
    await form.getByLabel('Saved', { exact: true }).check();
    await form.getByLabel('Interested', { exact: true }).check();
    await form.getByRole('button', { name: 'Apply', exact: true }).click(); await count(0);
    await page.waitForFunction(() => document.querySelector('[data-live-connection]').textContent.includes('match'));
    check('Honest zero-match view', (await page.locator('[data-live-connection]').textContent()).includes('match'));
    const second = await owner.newPage(); await second.goto(replica + '/happening-now');
    await write('put', '/api/v1/me/saved-events/ticketmaster:LiveFilter_0'); await count(1);
    await second.waitForFunction(() => document.querySelectorAll('[data-live-list] li').length === 1);
    check('Save on A reaches both B tabs without reload', (await page.locator('[data-live-list]').textContent()).includes('Entity match 0'));
    await write('put', '/api/v1/me/event-interests/ticketmaster:LiveFilter_0', { visibility: 'private' });
    await write('delete', '/api/v1/me/saved-events/ticketmaster:LiveFilter_0');
    await page.waitForTimeout(2500); await count(1);
    check('Any selected source retains private Interested after unsave', true);
    await write('delete', '/api/v1/me/event-interests/ticketmaster:LiveFilter_0'); await count(0);
    check('Removal on A removes B membership', true);
    await form.getByLabel('Followed artists', { exact: true }).check();
    await form.getByLabel('Followed venues', { exact: true }).check();
    await form.getByRole('button', { name: 'Apply', exact: true }).click();
    await write('put', '/api/v1/me/follows/artist/ticketmaster:A'); await count(2);
    check('Artist follow matches full lineup', true);
    const activityDOM = await page.locator('[data-live-list]').innerHTML();
    check('Activity DOM has no private membership metadata', !/account_id|membership|visibility|data-saved|data-followed/.test(activityDOM));
    await write('put', '/api/v1/me/follows/venue/ticketmaster:Hall'); await count(4);
    check('Artist OR venue matches with duplicate suppression', true);
    await write('delete', '/api/v1/me/follows/artist/ticketmaster:A'); await count(3);
    await write('delete', '/api/v1/me/follows/venue/ticketmaster:Hall'); await count(0);
    check('Both unfollows propagate across replicas', true);
    check('Membership replacements are quiet', await page.locator('[data-live-badge]').textContent() === '' && await page.locator('[data-live-toasts] li').count() === 0);
    const other = await browser.newContext();
    await other.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_OTHER_TOKEN, url: base }]);
    const otherView = await (await other.request.get(replica + '/api/v1/me/community/activity')).json();
    check('Other account remains independent', otherView.items.length === 20);
    const payload = JSON.stringify(await page.evaluate(() => window.activityFrames));
    check('Actual SSE has no private flags or owner identifiers', !/account_id|sources|membership|visibility|followed_artists/.test(payload) && !payload.includes(process.env.SAVED_FIXTURE_ID) && !payload.includes(process.env.SAVED_OTHER_ID));
    const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }));
    check('Browser storage contains no personal connection selections', !/followed_artists|followed_venues|membership|Entity match/.test(storage));
    const guest = await browser.newContext(); const gp = await guest.newPage(); await gp.goto(base + '/happening-now');
    await gp.locator('[data-live-filter-form] summary').click();
    check('Guest gets sign-in explanation', await gp.getByText('Sign in to filter by your connections.', { exact: true }).isVisible());
    check('Guest private route remains unauthorized', (await guest.request.get(base + '/api/v1/me/community/activity')).status() === 401);
    check('Public route rejects private fields', (await guest.request.get(base + '/api/v1/community/activity?filter=' + encodeURIComponent(JSON.stringify({ connections: { mode: 'any', sources: ['saved'] } })))).status() === 400);
    const native = await browser.newContext({ javaScriptEnabled: false });
    await native.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const np = await native.newPage(); await np.goto(base + '/me/preferences');
    const nf = np.locator('form[action="/me/live-activity-preferences"]'); await nf.locator('summary').click();
    check('Native account editor persists sources', await nf.getByLabel('Saved', { exact: true }).isChecked());
    for (const label of ['Interested', 'Followed artists', 'Followed venues']) await nf.getByLabel(label, { exact: true }).uncheck();
    await nf.getByRole('button', { name: 'Apply', exact: true }).click();
    const nativePrefs = await (await owner.request.get(base + '/api/v1/me/live-activity-preferences')).json();
    check('Native Apply commits the selected private source', JSON.stringify(nativePrefs.filters.connections) === JSON.stringify({ mode: 'any', sources: ['saved'] }));
    await nf.locator('summary').click();
    const nativeSave = np.waitForResponse(response => response.url().endsWith('/me/live-activity-preferences') && response.request().method() === 'POST');
    await nf.getByRole('button', { name: 'Reset filters' }).click();
    assert.equal((await nativeSave).status(), 303, await np.locator('main').textContent());
    await np.goto(base + '/happening-now');
    check('Native Reset restores all events', await np.locator('[data-live-list] li').count() === 20);
    await page.goto(replica + '/happening-now'); await form.locator('summary').click();
    const output = path.resolve(__dirname, '../.impeccable/review/live-connections'); fs.mkdirSync(output, { recursive: true });
    for (const width of [1440, 390, 320]) {
      await page.setViewportSize({ width, height: 1000 });
      check('Connection controls wrap at ' + width, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      if (width !== 390) await form.screenshot({ path: path.join(output, width + '.png') });
    }
    await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
    check('Connection controls support enlarged text', await form.evaluate(el => [...el.querySelectorAll('*')].every(n => !n.getBoundingClientRect().width || n.getBoundingClientRect().right <= innerWidth + 1)));
    await page.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
    await page.keyboard.press('Tab');
    await form.getByLabel('Saved', { exact: true }).focus();
    check('Keyboard focus remains visible', await form.getByLabel('Saved', { exact: true }).evaluate(el => getComputedStyle(el).outlineStyle !== 'none'));
    if (process.env.AXE_SCRIPT) { await page.addScriptTag({ path: process.env.AXE_SCRIPT }); check('Whole-page accessibility', (await page.evaluate(async () => (await axe.run()).violations)).length === 0); }
    await second.close();
    check('No browser exceptions', errors.length === 0);
    fs.writeFileSync(path.join(output, 'checks.json'), JSON.stringify(checks, null, 2));
    console.log(JSON.stringify({ checks: checks.length, passed: checks }, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
