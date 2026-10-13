// Opt-in local OAuth/provider fixtures; never contacts Google or uses live accounts.
const { chromium, webkit } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.SAVED_BASE_URL;
const id = 'ticketmaster:Browser_2';
const selectedEngine = process.env.SIGN_IN_BROWSER_ENGINE || 'chromium';
const out = path.join(process.env.SIGN_IN_REVIEW_DIR || path.resolve(__dirname, '../.impeccable/review/sign-in-continuity'), selectedEngine);
const report = { checks: [], accessibility: [], screenshots: [], consoleErrors: [] };
const check = (name, result) => { assert.ok(result, name); report.checks.push(name); };
fs.mkdirSync(out, { recursive: true });

(async () => {
  let fixtureToken = process.env.SAVED_FIXTURE_TOKEN;
  for (const [engine, browserType] of [[selectedEngine, selectedEngine === 'webkit' ? webkit : chromium]]) {
    const browser = await browserType.launch({ headless: true, ...(engine === 'chromium' && process.env.CHROMIUM_EXECUTABLE ? { executablePath: process.env.CHROMIUM_EXECUTABLE } : {}) });
    try {
      for (const [mode, width, javaScriptEnabled] of [['desktop', 1440, true], ['phone', 390, true], ['native', 320, false]]) {
        const name = engine + '-' + mode;
        const context = await browser.newContext({ viewport: { width, height: 900 }, javaScriptEnabled });
        await context.addCookies([{ name: 'ticketopia_session', value: fixtureToken, url: base }]);
        const me = await (await context.request.get(base + '/api/v1/me')).json();
        const headers = { 'X-CSRF-Token': me.csrf_token };
        for (const route of ['saved-events', 'event-interests', 'event-recommendations']) {
          const response = await context.request.delete(base + '/api/v1/me/' + route + '/' + encodeURIComponent(id), { headers });
          assert.equal(response.status(), 204);
        }
        await context.clearCookies();
        const page = await context.newPage();
        page.on('pageerror', error => report.consoleErrors.push(name + ': ' + error.message));
        let cancel = false;
        // Run the real start, retain its Set-Cookie, then substitute a native
        // fixture consent link for the external provider redirect. The real
        // callback consumes the browser-bound flow and creates the SQL session.
        await page.route('**/auth/google/start?**', async route => {
          const response = await route.fetch({ maxRedirects: 0 });
          assert.equal(response.status(), 303);
          const state = new URL(response.headers().location).searchParams.get('state');
          const callback = base + '/auth/google/callback?state=' + encodeURIComponent(state) + (cancel ? '&error=access_denied' : '&code=saved-browser');
          await route.fulfill({ response, status: 200, contentType: 'text/html', body: '<a href="' + callback.replaceAll('&', '&amp;') + '">Continue fixture sign-in</a>' });
        });
        const capture = async suffix => {
          await page.evaluate(async () => {
            await document.fonts.ready;
            scrollTo(0, 0);
          });
          check(name + ' no overflow ' + suffix, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
          const file = path.join(out, name + '-' + suffix + '.png');
          await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
          if (process.env.AXE_SCRIPT && javaScriptEnabled) {
            await page.addScriptTag({ path: process.env.AXE_SCRIPT });
            const violations = await page.evaluate(async () => (await axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa'] } })).violations);
            report.accessibility.push({ name: name + '-' + suffix, violations });
            check(name + ' accessibility ' + suffix + ' ' + JSON.stringify(violations.map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ html: node.html, summary: node.failureSummary })) }))), violations.length === 0);
          }
        };
        const own = async route => (await context.request.get(base + '/api/v1/me/' + route)).json();
        const authenticate = async () => {
          await page.getByRole('link', { name: 'Sign in with Google', exact: true }).click();
          await page.getByRole('link', { name: 'Continue fixture sign-in' }).click();
          await page.waitForURL(url => url.pathname === '/' || url.pathname === '/events/' + id);
          await page.locator('[data-event-shell="' + id + '"]').waitFor();
          // Use a fresh fixture session between cases; the production ten-session
          // cap correctly evicts the original credential during this matrix.
          fixtureToken = (await context.cookies(base)).find(cookie => cookie.name === 'ticketopia_session').value;
        };

        await page.goto(base + process.env.SAVED_SEARCH_PATH);
        const more = await page.getByRole('link', { name: 'Load more events' }).getAttribute('href');
        await page.goto(base + more);
        const origin = new URL(page.url()).pathname + new URL(page.url()).search;
        check(name + ' paginated filtered fixture', new URL(page.url()).searchParams.has('cursor') && await page.locator('.event-row').count() === 1);
        await page.getByRole('link', { name: /Sign in to save/ }).click();
        await page.waitForURL(url => url.pathname === '/auth/sign-in');
        let target = new URL(page.url()).searchParams.get('return_to');
        const selected = new URL(target, base);
        check(name + ' Save retains event and origin', selected.searchParams.get('selected_event') === id && selected.searchParams.get('cursor') === new URL(origin, base).searchParams.get('cursor') && selected.searchParams.get('city') === 'Chicago');
        check(name + ' sign-in names event and explicit action', await page.getByRole('heading', { name: 'Something new for next weekend' }).count() === 1 && await page.getByText(/Choose your action there/).count() === 1);
        await capture('sign-in');
        if (mode === 'phone') {
          await page.evaluate(() => { document.documentElement.style.fontSize = '200%'; });
          await capture('enlarged-sign-in');
          await page.evaluate(() => { document.documentElement.style.fontSize = ''; });
        }
        cancel = true;
        await page.getByRole('link', { name: 'Sign in with Google', exact: true }).click();
        await page.getByRole('link', { name: 'Continue fixture sign-in' }).click();
        await page.waitForURL(url => url.pathname === '/auth/sign-in' && url.searchParams.get('cancelled') === 'true');
        check(name + ' cancelled callback resumes sign-in', await page.getByText(/Sign-in was cancelled/).count() === 1);
        check(name + ' cancellation retains target', new URL(page.url()).searchParams.get('return_to') === target);
        const guestReturn = page.getByRole('link', { name: 'Return to event without signing in' });
        await guestReturn.focus();
        check(name + ' guest return is keyboard reachable with 44px target', await guestReturn.evaluate(link => link === document.activeElement && link.getBoundingClientRect().height >= 44));
        await page.keyboard.press('Enter');
        await page.locator('[data-context-id="' + id + '"]').waitFor();
        check(name + ' guest return keeps selected event', new URL(page.url()).searchParams.get('selected_event') === id);
        await page.locator('#event-context').getByRole('link', { name: /Sign in to save/ }).click();
        await page.waitForURL(url => url.pathname === '/auth/sign-in');
        cancel = false;
        await authenticate();
        check(name + ' sign-in does not save', (await own('saved-events')).items.length === 0);
        check(name + ' sign-in does not mark interest', (await own('event-interests')).items.length === 0);
        check(name + ' sign-in does not publish', (await own('event-recommendations')).items.length === 0);
        const afterSignIn = await (await context.request.get(base + '/api/v1/me')).json();
        check(name + ' sign-in preserves notification and visibility choices', afterSignIn.item.interest_visibility === me.item.interest_visibility && JSON.stringify(afterSignIn.item.preferences) === JSON.stringify(me.item.preferences));
        await page.locator('[data-event-shell] [data-save-form] button').click();
        await page.locator('[data-event-shell] [data-save-form] button[data-saved="true"]').waitFor();
        check(name + ' explicit Save confirmed', (await own('saved-events')).items.length === 1);
        check(name + ' confirmed Save keeps pagination', new URL(page.url()).searchParams.get('cursor') === selected.searchParams.get('cursor'));

        // Expire the ambient session after rendering the controls. A rejected
        // write must leave prior state intact and offer contextual sign-in.
        const interest = page.locator('[data-event-shell] [data-interest-form]').first();
        if (javaScriptEnabled) await interest.locator('button').scrollIntoViewIfNeeded();
        await context.clearCookies();
        await interest.locator('button').click();
        await page.getByRole('link', { name: javaScriptEnabled ? 'Sign in to continue' : 'Sign in to continue here', exact: true }).waitFor();
        await page.getByRole('link', { name: javaScriptEnabled ? 'Sign in to continue' : 'Sign in to continue here', exact: true }).click();
        await page.waitForURL(url => url.pathname === '/auth/sign-in');
        await authenticate();
        check(name + ' expired interest did not write', (await own('event-interests')).items.length === 0);
        await page.locator('[data-event-shell] [data-interest-form]').first().locator('button').click();
        await page.locator('[data-event-shell] .interest-button[aria-pressed="true"]').waitFor();
        check(name + ' explicit Interested confirmed private', (await own('event-interests')).items[0].visibility === 'private');

        // The native Recommend link reaches the same section on both layouts.
        await page.locator('[data-event-shell] [data-recommend-intent]').click();
        await page.locator('[data-event-shell] [data-recommendation-editor][open]').waitFor();
        const reason = 'A reason retained through sign-in: ' + name;
        await page.locator('[data-event-shell] textarea[name="reason"]').fill(reason);
        await context.clearCookies();
        await page.locator('[data-event-shell] [data-recommendation-form] button[value="set"]').click();
        await page.getByRole('link', { name: 'Sign in to continue', exact: true }).waitFor();
        check(name + ' failed recommendation retains reason', await page.locator('textarea[name="reason"]').inputValue() === reason);
        await page.getByRole('link', { name: 'Sign in to continue', exact: true }).click();
        await page.waitForURL(url => url.pathname === '/auth/sign-in');
        check(name + ' recommendation recovery opens composer', new URL(new URL(page.url()).searchParams.get('return_to'), base).searchParams.get('recommend') === 'true');
        await authenticate();
        await page.locator('[data-event-shell] [data-recommendation-editor][open]').waitFor();
        check(name + ' recovery does not publish', (await own('event-recommendations')).items.length === 0);
        if (javaScriptEnabled) check(name + ' same-account draft restored', await page.locator('[data-event-shell] textarea[name="reason"]').inputValue() === reason);
        else await page.locator('[data-event-shell] textarea[name="reason"]').fill(reason); // Native recovery explicitly asks for copy/paste.
        await page.locator('[data-event-shell] [data-recommendation-form] button[value="set"]').click();
        await page.locator('[data-event-shell] .recommendation-list').getByText(reason, { exact: true }).waitFor();
        check(name + ' explicit publication confirmed', (await own('event-recommendations')).items[0].reason === reason);
        await capture('confirmed');

        if (javaScriptEnabled) {
          // Save has its own enhanced error surface, independent of Interested.
          await context.clearCookies();
          await page.locator('[data-event-shell] [data-save-form] button').click();
          await page.locator('[data-event-shell] [data-save-feedback]').getByRole('link', { name: 'Sign in to continue' }).waitFor();
          check(name + ' rejected removal keeps selected Save', await page.locator('[data-event-shell] [data-save-form] button').getAttribute('data-saved') === 'true');
          await page.locator('[data-event-shell] [data-save-feedback]').getByRole('link', { name: 'Sign in to continue' }).click();
          await page.waitForURL(url => url.pathname === '/auth/sign-in');
          check(name + ' expired Save retains Recommendations section', new URL(new URL(page.url()).searchParams.get('return_to'), base).searchParams.get('section') === 'community');
        }
        await context.close();
      }
    } finally { await browser.close(); }
  }
  check('No browser runtime errors', report.consoleErrors.length === 0);
  fs.writeFileSync(path.join(out, 'report.json'), JSON.stringify(report, null, 2));
  console.log(JSON.stringify({ checks: report.checks.length, accessibility: report.accessibility.length, screenshots: report.screenshots.length }));
})().catch(error => {
  fs.writeFileSync(path.join(out, 'report.json'), JSON.stringify(report, null, 2));
  console.error(error); process.exit(1);
});
