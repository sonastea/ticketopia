// Isolated SQL fixtures and production handlers; no live OAuth or provider calls.
const { chromium, webkit } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.SAVED_BASE_URL;
const engine = process.env.ACTION_FEEDBACK_BROWSER_ENGINE || 'chromium';
const out = path.resolve(__dirname, '../.impeccable/review/action-feedback', engine);
const report = { checks: [], screenshots: [], accessibility: [], consoleErrors: [] };
const check = (name, result) => { assert.ok(result, name); report.checks.push(name); };
fs.mkdirSync(out, { recursive: true });

(async () => {
  const browser = await (engine === 'webkit' ? webkit : chromium).launch({ headless: true });
  try {
    for (const [mode, width, javaScriptEnabled] of [['desktop', 1440, true], ['phone', 390, true], ['enlarged', 320, true], ['native', 320, false]]) {
      if (process.env.ACTION_FEEDBACK_MODE && process.env.ACTION_FEEDBACK_MODE !== mode) continue;
      const context = await browser.newContext({ viewport: { width, height: 800 }, javaScriptEnabled });
      if (mode === 'enlarged') await context.addInitScript(() => document.addEventListener('DOMContentLoaded', () => { document.documentElement.style.fontSize = '200%'; }));
      await context.addCookies([{ name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base }]);
      const page = await context.newPage();
      page.on('pageerror', error => report.consoleErrors.push(error.message));
      const me = await (await context.request.get(base + '/api/v1/me')).json();
      const headers = { 'X-CSRF-Token': me.csrf_token };
      const id = 'ticketmaster:Browser_0';
      const request = async (verb, route, data) => {
        const response = await context.request[verb](base + '/api/v1/me/' + route, { headers, ...(data ? { data } : {}) });
        assert.ok(response.ok(), route + ': ' + response.status());
        return response;
      };
      const capture = async suffix => {
        await page.evaluate(async () => { await document.fonts.ready; });
        const name = mode + '-' + suffix;
        const file = path.join(out, name + '.png');
        await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
        const overflow = await page.evaluate(() => ({ scrollX, width: innerWidth, scrollWidth: document.documentElement.scrollWidth, nodes: [...document.querySelectorAll('body *')].filter(node => node.getBoundingClientRect().right + scrollX > innerWidth + 1 || node.scrollWidth > node.clientWidth + 1).map(node => ({ tag: node.tagName, class: node.getAttribute('class'), text: node.textContent.slice(0, 90), width: node.getBoundingClientRect().width, scrollWidth: node.scrollWidth })).slice(-12) }));
        check(name + ' no horizontal overflow ' + JSON.stringify(overflow), await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
        if (process.env.AXE_SCRIPT && javaScriptEnabled) {
          await page.addScriptTag({ path: process.env.AXE_SCRIPT });
          const violations = await page.evaluate(async () => (await axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] } })).violations);
          report.accessibility.push({ name, violations });
          check(name + ' axe ' + JSON.stringify(violations.map(item => item.id)), !violations.length);
        }
      };
      // Preserve independently published activity across both private actions.
      await request('put', 'event-recommendations/' + id, { reason: 'Independently published recommendation' });
      await request('put', 'saved-events/' + id);
      await request('put', 'event-interests/' + id, { visibility: 'public' });
      await page.goto(base + '/events/' + id + '?section=community');
      for (const [kind, selector, route] of [['save', '[data-save-form]', 'saved'], ['interest', '[data-interest-form]:not([data-interest-edit])', 'interests']]) {
        const form = page.locator(selector).first();
        const button = form.locator('button');
        await button.focus();
        const state = await button.getAttribute(kind === 'save' ? 'data-saved' : 'aria-pressed');
        if (javaScriptEnabled) {
          let release, observed;
          const pending = new Promise(resolve => { release = resolve; });
          const started = new Promise(resolve => { observed = resolve; });
          await page.route('**/' + route + '/' + id, async route => {
            observed(); await pending;
            await route.fulfill({ status: 503, contentType: 'application/problem+json', body: JSON.stringify({ detail: 'Temporarily unavailable. Try again.' }) });
          });
          await page.keyboard.press('Enter'); await started;
          check(mode + ' ' + kind + ' pending retains confirmed state', await button.getAttribute(kind === 'save' ? 'data-saved' : 'aria-pressed') === state && await form.getAttribute('aria-busy') === 'true');
          check(mode + ' Public readable while pending', await page.locator('.interest-button').innerText() === 'Interested · Public');
          const idle = await button.boundingBox();
          await page.waitForFunction(selector => document.querySelector(selector).querySelector('button').hasAttribute('data-pending'), selector);
          const busy = await button.boundingBox();
          check(mode + ' ' + kind + ' slow write spinner keeps button size', idle.width === busy.width && idle.height === busy.height);
          release();
          await page.getByText('Temporarily unavailable. Try again.', { exact: true }).waitFor();
          await page.waitForFunction(selector => !document.querySelector(selector).querySelector('button').disabled, selector);
          check(mode + ' ' + kind + ' spinner clears after failure', !await button.evaluate(node => node.hasAttribute('data-pending')));
          check(mode + ' ' + kind + ' failure retains state and keyboard action', await button.getAttribute(kind === 'save' ? 'data-saved' : 'aria-pressed') === state && await button.evaluate(node => node === document.activeElement));
          await page.unroute('**/' + route + '/' + id);
        }
        await button.focus(); await page.keyboard.press('Enter');
        await page.waitForFunction(({ selector, kind }) => document.querySelector(selector).querySelector('button').getAttribute(kind === 'save' ? 'data-saved' : 'aria-pressed') === 'false', { selector, kind });
        check(mode + ' ' + kind + ' confirmed removal', true);
        check(mode + ' independent recommendation survives ' + kind, (await (await context.request.get(base + '/api/v1/me/event-recommendations/' + id)).json()).reason === 'Independently published recommendation');
        if (javaScriptEnabled) {
          await page.route('**/' + route + '/' + id, async route => {
            // Commit the explicit write, then lose only its response.
            await route.fetch(); await route.abort('failed');
          });
          await button.focus(); await page.keyboard.press('Enter');
          await page.getByText(/Couldn't connect\. Check your connection and try again; repeating it is safe\./).waitFor();
          check(mode + ' ' + kind + ' uncertain write retains prior control', await button.getAttribute(kind === 'save' ? 'data-saved' : 'aria-pressed') === 'false');
          await page.unroute('**/' + route + '/' + id);
          await button.focus(); await page.keyboard.press('Enter');
          await page.waitForFunction(({ selector, kind }) => document.querySelector(selector).querySelector('button').getAttribute(kind === 'save' ? 'data-saved' : 'aria-pressed') === 'true', { selector, kind });
          const items = (await (await context.request.get(base + '/api/v1/me/' + (kind === 'save' ? 'saved-events' : 'event-interests'))).json()).items;
          check(mode + ' ' + kind + ' retry confirms exactly one choice', items.filter(item => item.event.id === id).length === 1);
          check(mode + ' ' + kind + ' retry preserves public recommendation', (await (await context.request.get(base + '/api/v1/me/event-recommendations/' + id)).json()).reason === 'Independently published recommendation');
        }
      }
      await capture('confirmed-event');

      for (const [collection, api, control, notice] of [['/saved', 'saved-events', '[data-save-form] button', '#saved-notice'], ['/me/interests', 'event-interests', '.interest-button', '[data-interest-notice]']]) {
        const seed = async () => {
          for (let i = 0; i < 6; i++) await request('delete', api + '/ticketmaster:Browser_' + i);
          for (let i = 0; i < 6; i++) await request('put', api + '/ticketmaster:Browser_' + i, api === 'event-interests' ? { visibility: i % 2 ? 'public' : 'private' } : undefined);
        };
        for (const laterPage of [false, true]) {
          for (const index of [0, 1, 2]) {
            await seed();
            await page.goto(base + collection + '?limit=3');
            if (laterPage) await page.getByRole('link', { name: api === 'saved-events' ? 'More saved events' : 'More interested events' }).click();
            const origin = page.url();
            const rows = page.locator('.event-row');
            const names = await rows.locator('.event-link').allTextContents();
            const ids = await rows.evaluateAll(nodes => nodes.map(node => node.dataset.eventId));
            const adjacent = index === 2 ? index - 1 : index + 1;
            const target = rows.nth(index).locator(control);
            await target.focus(); await target.scrollIntoViewIfNeeded();
            const before = await rows.nth(adjacent).evaluate(node => node.getBoundingClientRect().top);
            await page.keyboard.press('Enter');
            await page.waitForFunction(id => !document.querySelector('.event-row[data-event-id="' + id + '"]'), ids[index]);
            check(mode + ' ' + collection + ' removes ' + index + ' later=' + laterPage, await rows.count() === (!javaScriptEnabled && !laterPage ? 3 : 2) && page.url() === origin);
            if (javaScriptEnabled) {
              await page.waitForFunction(({ id, control }) => document.querySelector('.event-row[data-event-id="' + id + '"]').querySelector(control) === document.activeElement, { id: ids[adjacent], control });
              check(mode + ' removal names event', (await page.locator(notice).innerText()).includes(names[index].trim()));
              const after = await page.locator('.event-row[data-event-id="' + ids[adjacent] + '"]').evaluate(node => node.getBoundingClientRect().top);
              // Position may clamp at the document start or move just enough to
              // expose the focus target; it must not reset to the page heading.
              check(mode + ' removal retains reading position', index === 0 || Math.abs(after - before) < 4 || await page.evaluate(() => scrollY > 0));
              const rect = await page.locator('.event-row[data-event-id="' + ids[adjacent] + '"]').locator(control).boundingBox();
              check(mode + ' removal focus is visible above compact nav', rect.y >= 0 && rect.y + rect.height <= await page.evaluate(() => document.querySelector('.bottom-nav').getBoundingClientRect().width ? document.querySelector('.bottom-nav').getBoundingClientRect().top : innerHeight));
            }
          }
          // Empty just this page: newer choices still exist on the first page.
          while (await page.locator('.event-row').count()) {
            const wasLastRendered = await page.locator('.event-row').count() === 1;
            const removedID = await page.locator('.event-row').first().getAttribute('data-event-id');
            await page.locator('.event-row').first().locator(control).focus();
            await page.keyboard.press('Enter');
            await page.waitForFunction(id => !document.querySelector('.event-row[data-event-id="' + id + '"]'), removedID);
            // The last rendered row reloads to server-owned empty/refill state.
            await page.waitForLoadState('networkidle');
            if (javaScriptEnabled && wasLastRendered && await page.locator('.event-row').count()) {
              await page.waitForFunction(selector => document.querySelector('.event-row').querySelector(selector) === document.activeElement, control);
              check(mode + ' ' + collection + ' refilled page focuses a remaining choice', true);
            }
          }
          check(mode + ' empty page guidance', laterPage ? await page.getByText(api === 'saved-events' ? 'No saved events on this page.' : 'No event interest on this page.', { exact: true }).isVisible() : await page.getByRole('link', { name: 'Discover events', exact: true }).isVisible());
          if (javaScriptEnabled) {
            const emptyTarget = laterPage ? 'a[href="' + collection + '"]' : (api === 'saved-events' ? '.destination-state a[href="/"]' : '[data-interest-empty]');
            await page.waitForFunction(selector => [...document.querySelectorAll(selector)].some(node => node === document.activeElement), emptyTarget);
            check(mode + ' empty page focuses recovery', true);
          }
          if (laterPage) await capture(api + '-empty-page');
        }
        if (javaScriptEnabled) {
          await seed(); await page.goto(base + collection + '?limit=3');
          const row = page.locator('.event-row').first();
          const removeID = await row.getAttribute('data-event-id');
          const routePattern = '**/' + (api === 'saved-events' ? 'saved' : 'interests') + '/' + removeID;
          let release, observed;
          const pending = new Promise(resolve => { release = resolve; });
          const started = new Promise(resolve => { observed = resolve; });
          await page.route(routePattern, async route => { observed(); await pending; await route.continue(); });
          await row.locator(control).focus(); await page.keyboard.press('Enter'); await started;
          // Another reader action while the write is pending must not be stolen.
          const elsewhere = page.locator('.event-row').nth(1).locator('.event-link');
          await elsewhere.focus();
          const focusedName = await elsewhere.innerText();
          release();
          await page.waitForFunction(id => !document.querySelector('.event-row[data-event-id="' + id + '"]'), removeID);
          check(mode + ' ' + collection + ' slow removal preserves focus moved elsewhere', await page.evaluate(name => document.activeElement.textContent.trim() === name.trim(), focusedName));
          await page.unroute(routePattern);
        }
        if (mode === 'phone') {
          await seed(); await page.goto(base + collection + '?limit=3');
          await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
          await capture(api + '-enlarged');
        }
      }
      await context.close();
    }
    check('No browser runtime errors', !report.consoleErrors.length);
  } finally {
    await browser.close();
    fs.writeFileSync(path.join(out, 'checks.json'), JSON.stringify(report, null, 2));
  }
  console.log(JSON.stringify({ engine, checks: report.checks.length, accessibility: report.accessibility.length, screenshots: report.screenshots.length }));
})().catch(error => { console.error(error); process.exitCode = 1; });
