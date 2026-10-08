// End-to-end responsive discovery/participation against isolated MariaDB fixtures.
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.SAVED_BASE_URL;
const out = path.resolve(__dirname, '../.impeccable/review');
const report = {checks: [], screenshots: [], geometry: [], accessibility: [], errors: []};
const check = (name, condition) => { assert.ok(condition, name); report.checks.push(name); };
fs.mkdirSync(out, {recursive: true});

(async () => {
  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE});
  try {
    const owner = await browser.newContext({viewport: {width: 1440, height: 1000}});
    const other = await browser.newContext();
    const guest = await browser.newContext();
    for (const [context, token] of [[owner, process.env.SAVED_FIXTURE_TOKEN], [other, process.env.SAVED_OTHER_TOKEN]]) {
      await context.addCookies([{name: 'ticketopia_session', value: token, url: base}]);
    }
    const me = await (await owner.request.get(base + '/api/v1/me')).json();
    const them = await (await other.request.get(base + '/api/v1/me')).json();
    const id = 'ticketmaster:Browser_0';
    const endpoint = '/api/v1/events/' + id + '/discussions';
    let serial = 0;
    const create = async (context, csrf, url, body) => {
      const response = await context.request.post(base + url, {data: {body}, headers: {'X-CSRF-Token': csrf, 'Idempotency-Key': 'responsive-fixture-' + (++serial)}});
      assert.equal(response.status(), 201, await response.text());
      return response.json();
    };
    const root = await create(other, them.csrf_token, endpoint, 'Is the balcony a good place to watch?');
    const answer = await create(other, them.csrf_token, endpoint + '/' + root.id + '/replies', 'Yes. Arrive early for a clear view.');
    const page = await owner.newPage();
    page.on('pageerror', e => report.errors.push(e.message));
    const panel = page.locator('#event-context');
    const selected = () => page.locator('[data-context-id="' + id + '"]');
    const settled = async () => {
      await page.waitForFunction(() => {
        const panel = document.getElementById('event-context');
        return panel?.querySelector('.context-heading h2') && !panel.hasAttribute('aria-busy') && !panel.querySelector('[data-discussion-form][aria-busy="true"]');
      });
    };
    const activate = async (locator, touch = false) => {
      if (touch) await locator.tap();
      else { await locator.focus(); await page.keyboard.press('Enter'); }
    };
    const capture = async (name, width, zoom = false) => {
      if (process.env.RESPONSIVE_SKIP_CAPTURES === 'true') return;
      await page.setViewportSize({width, height: 1000});
      await page.evaluate(async zoom => {document.documentElement.style.fontSize = zoom ? '200%' : ''; await document.fonts.ready; scrollTo(0, 0); const context = document.getElementById('event-context'); if (context) context.scrollTop = 0;}, zoom);
      await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
      await page.evaluate(() => scrollTo(0, 0));
      await page.waitForFunction(() => scrollY === 0);
      report.geometry.push({name, ...await page.evaluate(() => {
        const rect = selector => {
          const el = document.querySelector(selector);
          if (!el || !el.getBoundingClientRect().width) return null;
          const {x, y, width, height} = el.getBoundingClientRect();
          return {x, y, width, height};
        };
        return {scrollY, focus: document.activeElement?.outerHTML.slice(0, 200), sidebar: rect('.sidebar'), navigation: rect('.bottom-nav'), context: rect('#event-context')};
      })});
      check('No horizontal overflow: ' + name, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      const file = path.join(out, name + '.png');
      await page.screenshot({path: file, fullPage: width < 992 || zoom}); report.screenshots.push(file);
      if (process.env.AXE_SCRIPT) {
        await page.addScriptTag({path: process.env.AXE_SCRIPT});
        const violations = await page.evaluate(async () => (await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations);
        report.accessibility.push({name, violations}); check('Accessibility: ' + name, !violations.length);
      }
    };

    for (const [name, width, zoom] of [['desktop', 1440, false], ['tablet', 1024, false], ['mobile', 390, false], ['small-mobile', 320, false], ['text-zoom', 390, true]]) {
      await page.setViewportSize({width, height: 1000});
      await page.goto(base + process.env.SAVED_SEARCH_PATH);
      await page.evaluate(zoom => document.documentElement.style.fontSize = zoom ? '200%' : '', zoom);
      const more = page.getByRole('link', {name: /Load more events/});
      if (await more.count()) await activate(more);
      await page.locator('.event-row').nth(2).waitFor();
      check(name + ': pagination keeps all results', await page.locator('.event-row').count() === 3);
      await activate(page.locator('.event-link').first()); await settled();
      check(name + ': selection stays alongside/in discovery', new URL(page.url()).pathname === '/' && new URL(page.url()).searchParams.get('section') === 'discussion');
      check(name + ': layout has one readable primary view', width >= 992 && !zoom ? await page.locator('.discovery-content').isVisible() : !await page.locator('.discovery-content').isVisible());
      if (width < 992 || zoom) check(name + ': selection focus enters context', await panel.locator('.context-heading h2').evaluate(el => el === document.activeElement));
      await activate(panel.locator('.context-event-details > summary'));
      const save = panel.locator('[data-save-form] button');
      const initialSaved = await save.getAttribute('data-saved') === 'true';
      await activate(save);
      await page.waitForFunction(([id, saved]) => [...document.querySelectorAll('[data-save-form]')].filter(f => f.dataset.saveId === id).every(f => f.querySelector('button').dataset.saved === String(!saved)), [id, initialSaved]);
      check(name + ': private Save confirms and synchronizes rows/context', (await owner.request.get(base + '/api/v1/me/saved-events/' + id)).status() === (initialSaved ? 404 : 200));
      const interest = panel.locator('[data-interest-form]:not([data-interest-edit]) button');
      await activate(interest);
      await page.waitForFunction(id => [...document.querySelectorAll('[data-interest-form]')].filter(f => f.dataset.interestId === id).every(f => f.getAttribute('aria-busy') !== 'true'), id);
      check(name + ': Interested remains distinct and private', (await panel.locator('[data-interest-count]').innerText()).includes('interested') && (await interest.innerText()).includes('Private'));
      await activate(panel.getByRole('link', {name: 'Recommend', exact: true}));
      await panel.locator('[data-recommendation-form] textarea').waitFor();
      await panel.locator('[data-recommendation-form] textarea').fill('A practical recommendation from ' + name + '.');
      await activate(panel.locator('[data-recommendation-form] button[value="set"]'));
      await panel.locator('[data-recommendation-editor]:not([open])').waitFor();
      check(name + ': public recommendation confirms without changing view', new URL(page.url()).pathname === '/' && await panel.getByText('A practical recommendation from ' + name + '.', {exact: true}).count() >= 1);
      await activate(panel.getByRole('link', {name: 'Discussion', exact: true}));
      await panel.locator('.discussion-editor > summary').waitFor();
      check(name + ': event action disclosure survives section changes', await panel.locator('.context-event-details').evaluate(el => el.open));
      await activate(panel.locator('.context-event-details > summary'));
      await activate(panel.getByRole('link', {name: /Open conversation/}).filter({hasText: 'Open conversation'}).first());
      await panel.locator('#post-' + root.id).waitFor();
      check(name + ': thread selection retains results and event identity', new URL(page.url()).searchParams.get('selected_thread') === root.id && await page.locator('.event-row').count() === 3);
      await activate(panel.locator('#post-' + answer.id).getByRole('button', {name: /Helpful/}));
      await settled();
      await panel.locator('#post-' + answer.id).waitFor();
      check(name + ': Helpful is confirmed in the panel', new URL(page.url()).pathname === '/' && await panel.locator('#post-' + answer.id + ' button[aria-pressed]').count() === 1);
      check(name + ': Helpful keeps action focus without a new navigation', await panel.locator('#post-' + answer.id + ' button[aria-pressed]').evaluate(el => el === document.activeElement));
      await activate(panel.locator('#post-' + answer.id).getByRole('link', {name: 'Reply', exact: true}));
      await settled();
      const draft = panel.locator('#discussion-composer textarea');
      await draft.waitFor();
      check(name + ': reply target focuses the active composer', new URL(page.url()).searchParams.get('reply_to') === answer.id && await draft.evaluate(el => el === document.activeElement));
      await draft.fill('Thanks for the practical advice, ' + name + '.');
      const key = await panel.locator('#discussion-composer [name="idempotency_key"]').inputValue();
      await page.route('**/discussion-actions', route => route.abort('failed'));
      await activate(panel.getByRole('button', {name: 'Publish reply'}));
      await panel.locator('#discussion-composer [data-discussion-error]').waitFor({state: 'visible'});
      check(name + ': failure keeps scoped draft and retry identity', await draft.inputValue() === 'Thanks for the practical advice, ' + name + '.' && await panel.locator('#discussion-composer [name="idempotency_key"]').inputValue() === key);
      await page.unroute('**/discussion-actions');
      await page.reload(); await settled();
      if (zoom) await page.evaluate(() => document.documentElement.style.fontSize = '200%');
      check(name + ': reload reconstructs selected thread/target/draft', new URL(page.url()).searchParams.get('selected_thread') === root.id && await draft.inputValue() === 'Thanks for the practical advice, ' + name + '.');
      await activate(panel.getByRole('button', {name: 'Publish reply'}));
      await panel.locator('.discussion-body').filter({hasText: 'Thanks for the practical advice, ' + name + '.'}).waitFor();
      check(name + ': reply confirms without leaving discovery', new URL(page.url()).pathname === '/');
      await capture('responsive-' + name, width, zoom);
      // Expand and return through real history, preserving appended result pages.
      await activate(panel.getByRole('link', {name: 'Open full conversation', exact: true}));
      await page.locator('.discussion-page').waitFor();
      check(name + ': thread expansion preserves selected return URL', new URL(page.url()).searchParams.get('return_to').includes('selected_thread=' + root.id));
      await activate(page.getByRole('link', {name: 'Back to browsing', exact: true}));
      await settled();
      check(name + ': return restores selected thread and appended results', new URL(page.url()).searchParams.get('selected_thread') === root.id && await page.locator('.event-row').count() === 3);
      await activate(panel.getByRole('link', {name: 'Open full event details', exact: true}));
      await page.locator('.event-page').waitFor();
      check(name + ': event expansion leads with real details', new URL(page.url()).pathname === '/events/' + id && await page.getByRole('heading', {name: 'About the event', exact: true}).count() === 1);
      await activate(page.locator('[data-return-results]')); await settled();
      check(name + ': details return retains thread/filters', new URL(page.url()).searchParams.get('selected_thread') === root.id && new URL(page.url()).searchParams.get('city') === 'Chicago');
      await activate(panel.locator('[data-close-context]'));
      check(name + ': close restores result focus', !new URL(page.url()).searchParams.has('selected_event') && await page.locator('.event-link').first().evaluate(el => el === document.activeElement));
    }

    await page.evaluate(() => document.documentElement.style.fontSize = '');
    await page.setViewportSize({width: 1440, height: 1000});
    await page.goto(base + process.env.SAVED_SEARCH_PATH);
    await page.locator('.event-link').first().click(); await settled();
    await panel.locator('.discussion-editor > summary').click();
    await panel.locator('.discussion-composer textarea').fill('A new question from results.');
    await page.locator('.event-link').nth(1).click(); await settled();
    await panel.locator('.discussion-editor > summary').click();
    check('Root drafts are event-scoped, not copied to another event', await panel.locator('.discussion-composer textarea').inputValue() === '');
    await page.goBack(); await settled();
    await panel.locator('.discussion-editor > summary').click();
    check('Back restores the original event-scoped root draft', await panel.locator('.discussion-composer textarea').inputValue() === 'A new question from results.');
    await panel.getByRole('button', {name: 'Post question or tip'}).click();
    await panel.locator('.discussion-body').filter({hasText: 'A new question from results.'}).waitFor();
    const ownThread = new URL(page.url()).searchParams.get('selected_thread');
    check('Root publication opens a selected, shareable thread in context', Boolean(ownThread) && new URL(page.url()).pathname === '/');
    await panel.locator('#post-' + ownThread).getByText('Edit your contribution', {exact: true}).click();
    await panel.locator('#post-' + ownThread + ' textarea').fill('An edited question from results.');
    await panel.getByRole('button', {name: 'Save changes', exact: true}).click();
    await panel.locator('.discussion-body').filter({hasText: 'An edited question from results.'}).waitFor();
    check('Editing stays in the same selected thread', new URL(page.url()).searchParams.get('selected_thread') === ownThread);
    // Independent reply pagination: enough retained replies without exceeding creation limits.
    for (let i = 0; i < 21; i++) await create(i < 15 ? other : owner, i < 15 ? them.csrf_token : me.csrf_token, endpoint + '/' + ownThread + '/replies', 'Retained reply ' + i);
    await page.reload(); await settled();
    await panel.getByRole('link', {name: 'More replies', exact: true}).click();
    await panel.locator('.discussion-body').filter({hasText: /^Retained reply 20$/}).waitFor();
    check('Replies paginate independently of discovery', new URL(page.url()).searchParams.has('discussion_cursor') && !new URL(page.url()).searchParams.has('cursor'));
    await page.reload(); await settled();
    check('Reply page reconstructs on reload', await panel.locator('.discussion-body').filter({hasText: /^Retained reply 20$/}).count() === 1);
    await panel.locator('#post-' + ownThread).getByText('Remove your contribution', {exact: true}).click();
    await panel.getByRole('button', {name: 'Remove contribution', exact: true}).click();
    await panel.getByText('This contribution was removed.', {exact: true}).waitFor();
    check('Removal preserves readable replies in context', await panel.locator('.discussion-body').filter({hasText: /^Retained reply 20$/}).count() === 1 && !await panel.getByText('An edited question from results.', {exact: true}).count());

    // Late selection responses and local retry must never replace another identity.
    await page.goto(base + process.env.SAVED_SEARCH_PATH);
    await page.route('**/events/ticketmaster%3ABrowser_0?*', route => route.fulfill({status: 503, body: 'Unavailable'}));
    await page.locator('.event-link').first().click();
    await panel.getByRole('heading', {name: 'Event preview unavailable'}).waitFor();
    check('Failed selection leaves results usable', await page.locator('.event-row').count() === 2);
    await page.unroute('**/events/ticketmaster%3ABrowser_0?*');
    await panel.getByRole('button', {name: 'Retry', exact: true}).click(); await settled();
    check('Retry retains filters and selected identity', await selected().count() === 1 && new URL(page.url()).searchParams.get('city') === 'Chicago');
    let delayed;
    await page.route('**/events/ticketmaster%3ABrowser_0?*', async route => { delayed = route; });
    await panel.getByRole('link', {name: 'Overview', exact: true}).click();
    await page.locator('.event-link').nth(1).click();
    await panel.locator('[data-context-id="ticketmaster:Browser_1"]').waitFor();
    if (delayed) await delayed.continue().catch(() => {});
    check('Obsolete loads cannot replace a different selected event', await panel.locator('[data-context-id]').getAttribute('data-context-id') === 'ticketmaster:Browser_1');
    await page.unroute('**/events/ticketmaster%3ABrowser_0?*');
    await page.goBack(); await settled();
    check('Back restores event/section identity', new URL(page.url()).searchParams.get('selected_event') === id && (new URL(page.url()).searchParams.get('section') || 'overview') === 'overview');
    // Real touch controls, keyboard resizing, and forced-colors/reduced-motion focus.
    const touch = await browser.newContext({viewport: {width: 390, height: 844}, hasTouch: true, isMobile: true});
    await touch.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base}]);
    const touchPage = await touch.newPage(); await touchPage.goto(base + process.env.SAVED_SEARCH_PATH);
    await touchPage.locator('.event-link').first().tap(); await touchPage.locator('.context-heading h2').waitFor();
    await touchPage.locator('.discussion-editor > summary').tap();
    await touchPage.locator('.discussion-composer textarea').fill('A touch draft with a reduced keyboard viewport.');
    await touchPage.setViewportSize({width: 390, height: 420});
    await touchPage.getByRole('button', {name: 'Post question or tip'}).scrollIntoViewIfNeeded();
    check('Touch composer/action remain reachable above navigation', await touchPage.getByRole('button', {name: 'Post question or tip'}).evaluate(el => el.getBoundingClientRect().bottom <= document.querySelector('.bottom-nav').getBoundingClientRect().top));
    await touchPage.setViewportSize({width: 1024, height: 1000});
    check('Resize keeps active touch draft and selected event', await touchPage.locator('.discussion-composer textarea').inputValue() === 'A touch draft with a reduced keyboard viewport.' && new URL(touchPage.url()).searchParams.get('selected_event') === id);
    await page.emulateMedia({forcedColors: 'active', reducedMotion: 'reduce'});
    await panel.getByRole('link', {name: 'Discussion', exact: true}).focus();
    check('Keyboard focus retains an outline in forced colors', await panel.getByRole('link', {name: 'Discussion', exact: true}).evaluate(el => getComputedStyle(el).outlineStyle !== 'none'));
    await page.emulateMedia({forcedColors: 'none', reducedMotion: 'no-preference'});

    const blocked = await browser.newContext({viewport: {width: 390, height: 844}});
    await blocked.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base}]);
    await blocked.addInitScript(() => { Storage.prototype.getItem = Storage.prototype.setItem = () => { throw new Error('Storage unavailable'); }; });
    const blockedPage = await blocked.newPage(); await blockedPage.goto(base + process.env.SAVED_SEARCH_PATH);
    await blockedPage.locator('.event-link').first().click(); await blockedPage.locator('.context-heading h2').waitFor();
    await blockedPage.locator('.discussion-editor > summary').click();
    await blockedPage.locator('.discussion-composer textarea').fill('An in-page draft when storage is blocked.');
    await blockedPage.getByRole('link', {name: 'Overview', exact: true}).click();
    await blockedPage.getByRole('heading', {name: 'About the event', exact: true}).waitFor();
    await blockedPage.getByRole('link', {name: 'Discussion', exact: true}).click();
    await blockedPage.locator('.discussion-editor > summary').click();
    check('Unavailable browser storage still retains drafts within the page', await blockedPage.locator('.discussion-composer textarea').inputValue() === 'An in-page draft when storage is blocked.');

    const publicPage = await guest.newPage(); await publicPage.goto(base + '/events/' + id + '/discussions/' + root.id);
    check('Guest reads event/thread without private state or owner editors', await publicPage.getByText('Yes. Arrive early for a clear view.', {exact: true}).count() === 1 && !await publicPage.locator('[data-save-form], [data-interest-form], .discussion-editor').count());
    const publicReads = await (await guest.request.get(base + endpoint + '/' + root.id + '/replies')).json();
    check('Guest projection hides private accounts and viewer Helpful', !JSON.stringify(publicReads).includes(me.item.email) && publicReads.items.every(p => !p.helpful));
    await publicPage.setViewportSize({width: 390, height: 844});
    await publicPage.goto(base + process.env.SAVED_SEARCH_PATH);
    await publicPage.locator('.event-link').first().focus();
    await publicPage.keyboard.press('Enter');
    await publicPage.locator('#event-context .discussion-list').waitFor();
    await publicPage.locator('#event-context [data-thread-link][href*="/discussions/' + root.id + '"]').focus();
    await publicPage.keyboard.press('Enter');
    await publicPage.locator('#event-context #post-' + root.id).waitFor();
    await publicPage.locator('#event-context #post-' + answer.id).getByRole('link', {name: 'Reply', exact: true}).focus();
    await publicPage.keyboard.press('Enter');
    const signIn = publicPage.locator('#event-context #discussion-composer a[href^="/auth/sign-in"]');
    await publicPage.waitForFunction(() => {
      const target = document.querySelector('#event-context #discussion-composer a[href^="/auth/sign-in"]');
      return target && target === document.activeElement;
    });
    check('Guest keyboard Reply focuses sign-in instead of a missing composer', await signIn.evaluate(el => el === document.activeElement));
    check('Guest reply sign-in retains thread and browsing origin', new URL(await signIn.getAttribute('href'), base).searchParams.get('return_to').includes('/discussions/' + root.id));

    await touchPage.setViewportSize({width: 390, height: 844});
    await touchPage.goto(base + process.env.SAVED_SEARCH_PATH);
    await touchPage.route('**/events/ticketmaster%3ABrowser_0?*', route => route.fulfill({status: 503, body: 'Unavailable'}));
    await touchPage.locator('.event-link').first().focus();
    await touchPage.keyboard.press('Enter');
    const firstRetry = touchPage.getByRole('button', {name: 'Retry', exact: true});
    await firstRetry.waitFor();
    check('Compact first-selection failure hands keyboard focus to Retry', await firstRetry.evaluate(el => el === document.activeElement));
    await touchPage.unroute('**/events/ticketmaster%3ABrowser_0?*');
    await touchPage.keyboard.press('Enter');
    await touchPage.locator('#event-context .context-heading h2').waitFor();
    check('Compact keyboard retry loads the same event with usable focus', await touchPage.locator('#event-context .context-heading h2').evaluate(el => el === document.activeElement) && new URL(touchPage.url()).searchParams.get('selected_event') === id);
    const participants = await (await guest.request.get(base + '/api/v1/events/' + id + '/interested-users')).json();
    check('Private interest identity is not listed publicly', !JSON.stringify(participants).includes(me.item.id));
    const anotherSaves = await (await other.request.get(base + '/api/v1/me/saved-events')).json();
    check('Another account cannot read owner saves', anotherSaves.items.length === 0);
    const fallback = await guest.request.get(process.env.SAVED_FALLBACK_URL + '/events/' + id + '/discussions/' + root.id, {headers: {'X-Ticketopia-Panel': 'true'}});
    check('Context thread works without provider/cache data', fallback.status() === 200 && (await fallback.text()).includes('Is the balcony a good place to watch?'));
    const native = await browser.newContext({javaScriptEnabled: false});
    await native.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base}]);
    const nativePage = await native.newPage(); await nativePage.goto(base + process.env.SAVED_SEARCH_PATH);
    await nativePage.locator('.event-link').first().click(); await nativePage.locator('.discussion-editor > summary').click();
    await nativePage.locator('.discussion-composer textarea').fill('A native question without JavaScript.');
    await nativePage.getByRole('button', {name: 'Post question or tip'}).click();
    check('No-JavaScript discovery links and native publication work', await nativePage.locator('.discussion-post').first().innerText().then(text => text.includes('A native question without JavaScript.')));
    check('No browser runtime errors', !report.errors.length);
    fs.writeFileSync(path.join(out, 'responsive-checks.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify({checks: report.checks.length, screenshots: report.screenshots.length, accessibility: report.accessibility.length}));
  } finally { await browser.close(); }
})().catch(e => {console.error(e); process.exitCode = 1;});
