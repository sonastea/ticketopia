// Opt-in fixture harness; see docs/saved-events.md. No production credentials.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.SAVED_BASE_URL;
const fallback = process.env.SAVED_FALLBACK_URL;
const out = process.env.SAVED_REVIEW_DIR || path.resolve(__dirname, '../.impeccable/review');
const axePath = process.env.AXE_SCRIPT;
const report = { checks: [], accessibility: [], screenshots: [], consoleErrors: [] };
const check = (name, result) => { assert.ok(result, name); report.checks.push(name); };
fs.mkdirSync(out, { recursive: true });

(async () => {
  const browser = await chromium.launch({ headless: true });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    await context.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const page = await context.newPage();
    page.on('pageerror', error => report.consoleErrors.push(error.message));
    const actionLayout = async name => {
      const result = await page.evaluate(() => {
        const failures = [];
        const groups = [...document.querySelectorAll('[data-event-actions]')]
          .filter(group => group.getBoundingClientRect().width > 0);
        for (const [index, group] of groups.entries()) {
          const row = group.querySelector('.event-actions');
          const controls = [...row.querySelectorAll('button, a')];
          const geometry = () => {
            const origin = group.getBoundingClientRect();
            return controls.map(control => {
              const rect = control.getBoundingClientRect();
              return { x: rect.x - origin.x, y: rect.y - origin.y, width: rect.width, height: rect.height };
            });
          };
          const before = geometry();
          for (const rect of before) {
            if (rect.height < 44 || rect.x < -1 || rect.x + rect.width > row.getBoundingClientRect().width + 1) failures.push(`group ${index}: clipped or undersized control`);
          }
          for (let i = 0; i < before.length; i++) {
            for (const other of before.slice(i + 1)) {
              const overlap = Math.min(before[i].y + before[i].height, other.y + other.height) - Math.max(before[i].y, other.y);
              if (overlap > 1 && Math.abs(before[i].y - other.y) > 1) failures.push(`group ${index}: controls on the same line are misaligned`);
            }
          }
          const feedback = group.querySelector('[data-save-feedback]');
          if (!feedback) continue;
          const original = { hidden: feedback.hidden, text: feedback.textContent, error: feedback.getAttribute('data-error') };
          for (const message of ['', 'Updating your private save…', 'Event saved privately.', 'Event removed from Saved.', 'Your save could not be confirmed. Try again; repeating it is safe. ' + 'long-retry-guidance-'.repeat(12)]) {
            feedback.hidden = !message;
            feedback.textContent = message;
            feedback.dataset.error = String(message.startsWith('Your save'));
            const after = geometry();
            if (after.some((rect, i) => Object.keys(rect).some(key => Math.abs(rect[key] - before[i][key]) > 1))) failures.push(`group ${index}: feedback moved a control: ${message}`);
            if (message && feedback.getBoundingClientRect().top < row.getBoundingClientRect().bottom) failures.push(`group ${index}: feedback overlaps controls`);
            if (message && feedback.scrollWidth > group.getBoundingClientRect().width + 1) failures.push(`group ${index}: feedback overflows`);
          }
          feedback.hidden = original.hidden;
          feedback.textContent = original.text;
          if (original.error === null) feedback.removeAttribute('data-error');
          else feedback.setAttribute('data-error', original.error);
        }
        if (document.documentElement.scrollWidth > innerWidth) failures.push('page overflows horizontally: ' + JSON.stringify([...document.querySelectorAll('body *')].filter(node => node.getBoundingClientRect().right > innerWidth + 1).map(node => ({ tag: node.tagName, class: node.getAttribute('class'), text: node.textContent.slice(0, 60), width: node.getBoundingClientRect().width, parent: node.parentElement.getAttribute('class'), parentText: node.parentElement.textContent.slice(0, 60) })).slice(-8)));
        return { groups: groups.length, failures };
      });
      check('Stable action layout: ' + name + ' ' + JSON.stringify(result.failures), result.groups > 0 && result.failures.length === 0);
    };
    const actionLayoutViews = async name => {
      for (const [width, zoom] of [[1440, false], [1024, false], [390, false], [320, false], [390, true]]) {
        await page.setViewportSize({ width, height: 1000 });
        await page.evaluate(async textZoom => {
          document.documentElement.style.fontSize = textZoom ? '200%' : '';
          await document.fonts.ready;
        }, zoom);
        await actionLayout(name + ' ' + width + (zoom ? ' 200% text' : ''));
      }
      await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
      await page.setViewportSize({ width: 1440, height: 1000 });
    };
    const capture = async (name, width, textZoom = false) => {
      if (process.env.SAVED_CAPTURE === 'false') return;
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate(async zoom => {
        document.documentElement.style.fontSize = zoom ? '200%' : '';
        await document.fonts.ready;
        scrollTo(0, 0);
      }, textZoom);
      const file = path.join(out, name + '.png');
      await page.screenshot({ path: file, fullPage: true });
      report.screenshots.push(file);
      const overflow = await page.evaluate(() => [...document.querySelectorAll('body *')].filter(node => node.getBoundingClientRect().right > innerWidth + 1).map(node => ({ tag: node.tagName, class: node.className, text: node.textContent.slice(0, 80), width: node.getBoundingClientRect().width })).slice(-12));
      check('No horizontal overflow: ' + name + ' ' + JSON.stringify(overflow), await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      if (axePath) {
        await page.addScriptTag({ path: axePath });
        const violations = await page.evaluate(async () => (await axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] } })).violations);
        report.accessibility.push({ name, violations });
        check('Accessibility: ' + name, violations.length === 0);
      }
    };
    const states = async () => (await context.request.get(base + '/api/v1/me/saved-events')).json();
    await page.goto(base + '/saved');
    check('Empty collection explains private saving', await page.getByRole('heading', { name: 'Keep something worth coming back to.' }).count() === 1);
    await capture('saved-empty', 390);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(base + process.env.SAVED_SEARCH_PATH);
    check('Discovery fixture has two rows', await page.locator('.event-row').count() === 2);
    await page.locator('.event-link').first().click();
    await page.locator('[data-context-id="ticketmaster:Browser_0"]').waitFor();
    await actionLayoutViews('discovery and preview, unsaved');
    let releaseSave;
    const pendingSave = new Promise(resolve => { releaseSave = resolve; });
    let observeSave;
    const saveStarted = new Promise(resolve => { observeSave = resolve; });
    await page.route('**/saved/ticketmaster:Browser_0', async route => {
      observeSave();
      await pendingSave;
      await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ detail: 'Your saved events are temporarily unavailable. Try again.' }) });
    });
    await page.locator('.event-row').first().locator('[data-save-form] button').click();
    await saveStarted;
    check('Pending save keeps row and preview busy', await page.locator('[data-save-id="ticketmaster:Browser_0"][aria-busy="true"]').count() === 2);
    await actionLayout('pending save');
    releaseSave();
    await page.getByRole('status').filter({ hasText: 'Your saved events are temporarily unavailable.' }).waitFor();
    check('Save failure leaves state unchanged', (await states()).items.length === 0);
    check('Save failure keeps retry available', await page.locator('.event-row').first().locator('[data-save-form] button').isEnabled());
    await actionLayout('failed save');
    await capture('saved-action-error-mobile', 320);
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.unroute('**/saved/ticketmaster:Browser_0');
    await page.locator('.event-row').first().locator('[data-save-form] button').click();
    await page.waitForFunction(() => [...document.querySelectorAll('[data-save-id="ticketmaster:Browser_0"] button')].every(button => button.dataset.saved === 'true'));
    check('Preview and row synchronized', await page.locator('[data-save-id="ticketmaster:Browser_0"] button[data-saved="true"]').count() === 2);
    check('Selection and city preserved after save', new URL(page.url()).searchParams.get('selected_event') === 'ticketmaster:Browser_0' && new URL(page.url()).searchParams.get('city') === 'Chicago');
    check('Save retains keyboard focus', await page.locator('.event-row').first().locator('[data-save-form] button').evaluate(button => button === document.activeElement));
    check('Save is stored in authenticated collection', (await states()).items.length === 1);
    await actionLayoutViews('discovery and preview, saved');
    await capture('saved-discovery-desktop', 1440);
    await page.locator('[data-close-context]').click();
    await page.locator('[data-context-id="ticketmaster:Browser_0"]').waitFor({ state: 'detached' });
    await actionLayoutViews('discovery rows without preview');
    await page.locator('.event-link').first().click();
    await page.locator('[data-context-id="ticketmaster:Browser_0"]').waitFor();
    await page.getByRole('link', { name: 'Load more events' }).click();
    await page.locator('.event-row').nth(2).waitFor();
    await page.locator('.event-row').nth(2).locator('[data-save-form] button').click();
    await page.waitForFunction(() => document.querySelector('[data-save-id="ticketmaster:Browser_2"] button').dataset.saved === 'true');
    check('Load-more save controls work', (await states()).items.length === 2);
    await actionLayout('htmx-loaded results');
    await page.goto(base + '/saved');
    check('Saved navigation is current', await page.locator('.desktop-nav a[href="/saved"][aria-current="page"]').count() === 1);
    check('Newest save leads the collection', await page.locator('.event-row').first().getAttribute('data-event-id') === 'ticketmaster:Browser_2');
    check('Saved collection has no public interest action', await page.getByRole('button', { name: 'Interested', exact: true }).count() === 0);
    await actionLayoutViews('Saved collection');
    await capture('saved-desktop', 1440);
    await capture('saved-tablet', 1024);
    await capture('saved-mobile', 390);
    await capture('saved-small', 320);
    await capture('saved-text-zoom', 390, true);
    await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
    await page.goto(base + '/saved?limit=1');
    await page.getByRole('link', { name: 'More saved events' }).click();
    check('Saved pagination is deterministic', await page.locator('.event-row').first().getAttribute('data-event-id') === 'ticketmaster:Browser_0');
    await page.locator('.event-link').first().click();
    check('Detail returns to Saved page', await page.getByRole('link', { name: 'Back to Saved' }).count() === 1);
    await actionLayoutViews('full event');
    await capture('saved-action-event-mobile', 320);
    await page.getByRole('link', { name: 'Back to Saved' }).click();
    check('Return retains Saved cursor', new URL(page.url()).searchParams.has('cursor'));
    await page.goto(fallback + '/events/ticketmaster:Browser_0?return_to=%2Fsaved');
    check('Event remains readable without cache or provider', await page.getByRole('heading', { name: 'A good night in the city', exact: true }).count() === 1);
    check('Fallback has honest freshness notice', (await page.locator('body').innerText()).includes('Current provider data is unavailable'));
    check('Last-known provenance precedes sale status', await page.locator('.event-heading').evaluate(heading => heading.querySelector('.freshness').compareDocumentPosition(heading.querySelector('.event-status')) & Node.DOCUMENT_POSITION_FOLLOWING));
    await capture('saved-fallback-mobile', 390);
    // Bearer client sees the same bookmarks and can remove independently.
    await page.goto(base + '/me');
    const me = await (await context.request.get(base + '/api/v1/me')).json();
    const issued = await context.request.post(base + '/api/v1/me/tokens', { data: { name: 'Saved browser fixture' }, headers: { 'X-CSRF-Token': me.csrf_token, Origin: base } });
    check('Personal API token issued', issued.status() === 201);
    const token = (await issued.json()).token;
    const apiList = await context.request.get(base + '/api/v1/me/saved-events', { headers: { Authorization: 'Bearer ' + token } });
    check('Another authenticated client has same saves', (await apiList.json()).items.length === 2);
    const removed = await context.request.delete(base + '/api/v1/me/saved-events/ticketmaster:Browser_2', { headers: { Authorization: 'Bearer ' + token } });
    check('Bearer unsave works without CSRF', removed.status() === 204);
    await page.goto(base + '/saved');
    await context.request.delete(base + '/api/v1/me/saved-events/ticketmaster:Browser_0', { headers: { Authorization: 'Bearer ' + token } });
    await page.evaluate(() => window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })));
    await page.waitForFunction(() => document.querySelector('[data-save-id="ticketmaster:Browser_0"] button').dataset.saved === 'false');
    await page.getByRole('button', { name: 'Save — A good night in the city', exact: true }).click();
    await page.waitForFunction(() => document.getElementById('saved-notice').textContent.includes('saved privately'));
    check('Revalidated Saved controls can resave after another client removes', (await states()).items.length === 1);
    // Restore loaded discovery pages, but refresh stale action state changed by
    // a second client while the browser was on a dedicated event page.
    await page.setViewportSize({ width: 1440, height: 1000 });
    await page.goto(base + process.env.SAVED_SEARCH_PATH);
    await page.getByRole('link', { name: 'Load more events' }).click();
    await page.locator('.event-row').nth(2).waitFor();
    await page.locator('.event-link').first().click();
    await page.locator('[data-context-id="ticketmaster:Browser_0"]').waitFor();
    await page.getByRole('link', { name: 'Open full event details' }).click();
    await page.locator('.event-heading h1').waitFor();
    await context.request.delete(base + '/api/v1/me/saved-events/ticketmaster:Browser_0', { headers: { Authorization: 'Bearer ' + token } });
    await page.getByRole('link', { name: 'Back to results' }).click();
    await page.waitForFunction(() => document.querySelectorAll('.event-row').length === 3 && document.querySelector('[data-save-id="ticketmaster:Browser_0"] button').dataset.saved === 'false');
    check('Browser Back retains loaded results and refreshes private save state', await page.locator('.event-row').count() === 3);
    await page.locator('.event-row').first().locator('[data-save-form] button').click();
    await page.waitForFunction(() => document.querySelector('[data-save-id="ticketmaster:Browser_0"] button').dataset.saved === 'true');
    check('Restored controls keep usable CSRF credentials', (await states()).items.length === 1);
    // Ownership and public projections use real SQL and production handlers.
    const otherContext = await browser.newContext();
    await otherContext.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_OTHER_TOKEN, url: base }]);
    const otherList = await otherContext.request.get(base + '/api/v1/me/saved-events');
    check('Another owner cannot see saves', (await otherList.json()).items.length === 0);
    const publicProfile = await otherContext.request.get(base + '/api/v1/users/' + process.env.SAVED_FIXTURE_ID);
    check('Public profile excludes bookmark activity', !JSON.stringify(await publicProfile.json()).includes('Browser_0'));
    const guest = await browser.newContext();
    const guestPage = await guest.newPage();
    await guestPage.goto(base + '/saved');
    check('Guest resumes Saved through Google sign-in', guestPage.url().includes('/auth/sign-in?return_to=%2Fsaved'));
    await guestPage.goto(base + '/events/ticketmaster:Browser_0');
    check('Guest event offers sign in to save', await guestPage.getByRole('link', { name: 'Sign in to save A good night in the city' }).count() === 1);
    await guestPage.goto(process.env.SAVED_DISABLED_URL + '/saved');
    check('Disabled deployment explains account requirement', (await guestPage.locator('body').innerText()).includes('Accounts not enabled'));
    // Native no-JavaScript POST, including the hidden CSRF field.
    const noJS = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 320, height: 1000 } });
    await noJS.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
    const plain = await noJS.newPage();
    await plain.goto(base + '/events/ticketmaster:Browser_1?return_to=%2Fsaved');
    await plain.getByRole('button', { name: /Save — An event/ }).click();
    check('No-JavaScript save returns to event', plain.url().includes('/events/ticketmaster:Browser_1'));
    check('No-JavaScript save persisted', (await states()).items.length === 2);
    await plain.getByRole('button', { name: /Remove from Saved — An event/ }).click();
    check('No-JavaScript remove persisted', (await states()).items.length === 1);
    await context.request.put(base + '/api/v1/me/saved-events/ticketmaster:Browser_2', { headers: { Authorization: 'Bearer ' + token } });
    await page.goto(base + '/saved');
    await page.getByRole('button', { name: 'Remove from Saved — Something new for next weekend' }).click();
    await page.waitForFunction(() => document.querySelector('[data-save-id="ticketmaster:Browser_0"] button') === document.activeElement);
    check('Removal restores focus to a remaining event', await page.locator('.event-row').count() === 1);
    check('Removal keeps a visible named confirmation', (await page.locator('#saved-notice').innerText()).includes('Something new for next weekend removed from Saved.'));
    await page.goto(base + '/saved');
    await page.getByRole('button', { name: 'Remove from Saved — A good night in the city' }).click();
    await page.getByRole('heading', { name: 'Keep something worth coming back to.' }).waitFor();
    await page.waitForFunction(() => document.querySelector('.destination-state a[href="/"]') === document.activeElement);
    check('Final removal focuses the empty-state Discover action', true);
    check('Removing final bookmark restores empty state', (await states()).items.length === 0);
    check('No client runtime errors', report.consoleErrors.length === 0);
    fs.writeFileSync(path.join(out, process.env.SAVED_CAPTURE === 'false' ? 'saved-behavior-checks.json' : 'saved-checks.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify({ checks: report.checks.length, accessibility: report.accessibility.length, screenshots: report.screenshots.length }));
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
