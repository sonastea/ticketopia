// Read-only production-template fixtures; run scripts/css-preview first.
// Writes below are intercepted: no account, provider or email mutation occurs.
const playwright = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.CSS_BASE_URL || 'http://127.0.0.1:18088';
const out = process.env.CSS_SCREENSHOT_DIR;
const report = {checks: [], screenshots: [], accessibility: [], errors: []};
const check = (name, value) => {assert.ok(value, name); report.checks.push(name);};

(async () => {
  const engine = process.env.BROWSER_ENGINE || 'chromium';
  const browser = await playwright[engine].launch({headless: true, executablePath: engine === 'chromium' ? process.env.CHROMIUM_EXECUTABLE : undefined});
  try {
     const page = await browser.newPage();
     let documents = 0;
     page.on('request', request => {if (request.isNavigationRequest() && request.frame() === page.mainFrame()) documents++;});
    page.on('pageerror', error => report.errors.push(error.message));
    const panel = page.locator('#event-context');
    const settled = () => page.waitForFunction(() => document.querySelector('#event-context .context-heading') && !document.querySelector('#event-context').hasAttribute('aria-busy') && !document.querySelector('[data-event-section-region][aria-busy]'));
    for (const [name, width, height, zoom] of [
      ['desktop', 1440, 900, false], ['short-desktop', 1440, 600, false],
      ['wide', 1920, 1080, false], ['tablet', 1024, 900, false],
      ['compact-tablet', 768, 900, false], ['phone', 390, 844, false],
      ['small-phone', 320, 844, false], ['enlarged', 390, 844, true],
    ]) {
      await page.setViewportSize({width, height});
      await page.goto(base + '/');
      await page.evaluate(async zoom => {document.documentElement.style.fontSize = zoom ? '200%' : ''; await document.fonts.ready;}, zoom);
      const row = page.locator('.event-row').first();
      check(name + ': title and image share Overview', await row.locator('.event-art a').getAttribute('href') === await row.locator('.event-link').getAttribute('href'));
      check(name + ': list ticket is direct', await row.getByRole('link', {name: /Tickets on Ticketmaster/}).isVisible());
      check(name + ': compact activity, no secondary participation', await row.locator('.row-activity a').count() === 2 && !await row.locator('.row-activity p, .interest-form, .reminder-button, details').count());
      check(name + ': empty activity is omitted', !await page.locator('.event-row').nth(1).locator('.row-activity').count());
      await row.locator('.event-art a').focus(); await page.keyboard.press('Enter'); await settled();
      check(name + ': default is useful Overview', await panel.getByRole('heading', {name: 'About the event'}).isVisible() && await panel.getByText('Bring photo ID.', {exact: false}).isVisible());
      check(name + ': one section navigation', await panel.locator('.event-section-nav').count() === 1);
      check(name + ': tickets precede artwork', await panel.locator('.ticket-link').evaluate(n => n.getBoundingClientRect().top < document.querySelector('[data-section-body] > .event-image').getBoundingClientRect().top));
      check(name + ': one complete compact view', await panel.locator('.open-event').isVisible() === (width >= 992 && !zoom));
      check(name + ': no horizontal overflow', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      check(name + ': primary and navigation targets are touch sized', await panel.locator('.ticket-link, .save-button, .event-section-nav a').evaluateAll(nodes => nodes.every(n => n.getBoundingClientRect().height >= 44)));
      await panel.locator('.ticket-link').focus();
      check(name + ': keyboard focus remains visible', await panel.locator('.ticket-link').evaluate(n => getComputedStyle(n).outlineStyle === 'solid'));
      if (out) {
        fs.mkdirSync(out, {recursive: true});
        const file = path.join(out, 'redesign-' + name + '.png');
        await page.screenshot({path: file, fullPage: true}); report.screenshots.push(file);
      }
      if (process.env.AXE_SCRIPT) {
        await page.addScriptTag({path: process.env.AXE_SCRIPT});
        const violations = await page.evaluate(async () => (await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']}})).violations);
        report.accessibility.push({name, violations}); check(name + ': automated accessibility', !violations.length);
      }
      const beforeSections = documents;
      await page.evaluate(() => {window.stableEventNodes = [...document.querySelectorAll('#event-context .context-heading, #event-context .ticket-link, #event-context .save-button, #event-context .event-section-nav, #event-context .event-section-nav a, #event-context .event-participation, #event-context .event-provenance')];});
      const stable = () => page.evaluate(() => stableEventNodes.every(n => n.isConnected));
      await panel.getByRole('link', {name: 'Questions & tips', exact: true}).click(); await settled();
      check(name + ': section controls stay mounted and focused', await stable() && await panel.locator('[data-event-section="discussion"]').evaluate(n => n === document.activeElement));
      check(name + ': explicit discussion selection', await panel.locator('.discussion-body').isVisible() && !await panel.getByRole('heading', {name: 'About the event'}).count());
      await panel.getByRole('link', {name: 'Recommendations', exact: true}).click(); await settled();
      check(name + ': recommendations keep public interest distinct', await panel.locator('.recommendation-reason').isVisible() && await panel.getByRole('heading', {name: 'Public interest'}).isVisible());
      await page.goBack(); await settled();
      check(name + ': Back restores named section', new URL(page.url()).searchParams.get('section') === 'discussion');
      await page.goForward(); await settled();
      check(name + ': Forward restores named section', new URL(page.url()).searchParams.get('section') === 'community');
      check(name + ': section/history changes make no document requests', documents === beforeSections && await stable());
      const targetURL = new URL(await panel.locator('[data-save-form] input[name="return_to"]').inputValue(), base);
      const selectionURL = new URL(page.url()); targetURL.searchParams.sort(); selectionURL.searchParams.sort();
      check(name + ': return targets match selection', targetURL.href === selectionURL.href);
      await panel.locator('[data-close-context]').click();
      check(name + ': result and focus restoration', await row.locator('.event-link').evaluate(n => n === document.activeElement) && await page.locator('.event-row').count() === 2);
      await page.locator('.event-link').nth(1).click(); await settled();
      check(name + ': long event title is not clipped', await panel.locator('.context-heading h2').evaluate(n => n.scrollWidth <= n.clientWidth && getComputedStyle(n).textOverflow !== 'ellipsis'));
    }

    for (const standalone of [false, true]) {
      await page.setViewportSize({width: 1440, height: 900});
      await page.goto(base + (standalone ? '/events/ticketmaster:CSS_0' : '/?selected_event=ticketmaster%3ACSS_0'));
      const host = page.locator('[data-event-shell]');
      const region = host.locator('[data-event-section-region]');
      const idle = () => page.waitForFunction(() => !document.querySelector('[data-event-section-region][aria-busy]'));
      const mode = standalone ? 'standalone' : 'Discover';
      const baseline = documents;
      const overviewURL = page.url();
      await page.evaluate(() => {window.stableSectionNodes = [...document.querySelectorAll('[data-shell-facts], .event-primary-actions .ticket-link, .event-primary-actions .save-button, .event-section-nav, .event-section-nav a, .event-participation, .event-provenance')];});
      await host.locator('[data-event-section="discussion"]').click(); await idle();
      await host.locator('[data-event-section="community"]').click(); await idle();
      await page.goBack(); await idle();
      check(mode + ': mounted shell and no document navigation', documents === baseline && await page.evaluate(() => stableSectionNodes.every(n => n.isConnected)));
      await host.locator('[data-event-section="overview"]').click(); await idle();
      check(mode + ': Overview URL returns canonically', page.url() === overviewURL);
      const readingPosition = await page.evaluate(standalone => {
        const panel = document.getElementById('event-context');
        if (standalone) scrollTo(0, 250); else panel.scrollTop = 250;
        return {document: scrollY, panel: panel?.scrollTop || 0};
      }, standalone);
      await host.locator('[data-event-section="discussion"]').evaluate(link => link.click()); await idle();
      await page.goBack(); await idle();
      check(mode + ': Back restores section reading position', await page.evaluate(saved => Math.abs(scrollY - saved.document) <= 2 && Math.abs((document.getElementById('event-context')?.scrollTop || 0) - saved.panel) <= 2, readingPosition));
      let release;
      const gate = new Promise(resolve => {release = resolve;});
      await page.route('**/events/**', async route => {
        if (route.request().headers()['x-ticketopia-section'] && new URL(route.request().url()).searchParams.get('section') === 'discussion') {await gate; await route.fulfill({status: 503, body: 'Unavailable'});}
        else await route.continue();
      });
      await host.locator('[data-event-section="discussion"]').click();
      await region.locator('[data-section-status]').filter({hasText: 'Loading Questions'}).waitFor();
      check(mode + ': loading retains explicitly labeled Overview and URL', await region.getByRole('heading', {name: 'About the event'}).isVisible() && page.url() === overviewURL);
      if (out) {const file = path.join(out, 'sections-loading-' + mode + '.png'); await page.screenshot({path: file, fullPage: true}); report.screenshots.push(file);}
      release(); await region.locator('[data-section-retry]').waitFor(); await idle();
      check(mode + ': failed click keeps content/URL and local recovery', page.url() === overviewURL && await region.getByRole('heading', {name: 'About the event'}).isVisible() && await region.getByRole('link', {name: 'Open Questions & tips'}).isVisible());
      await page.unroute('**/events/**');
      await region.locator('[data-section-retry]').click(); await idle();
      check(mode + ': Retry commits only confirmed section', new URL(page.url()).searchParams.get('section') === 'discussion');
      await host.locator('[data-event-section="community"]').click(); await idle();
      await page.route('**/events/**', route => route.fulfill({status: 200, contentType: 'text/html', body: '<html><body>Sign in</body></html>'}));
      await page.goBack(); await region.locator('[data-section-retry]').waitFor(); await idle();
      check(mode + ': failed Back never mislabels old recommendations', new URL(page.url()).searchParams.get('section') === 'discussion' && !await region.locator('.recommendation-reason').count() && await host.locator('[data-event-section="discussion"]').getAttribute('aria-current') === 'page');
      await page.unroute('**/events/**');
      await region.locator('[data-section-retry]').click(); await idle();
      await host.locator('[data-event-section="overview"]').click(); await idle();
      await host.locator('[data-recommend-intent]').click(); await idle();
      check(mode + ': Recommend opens and focuses composer after transitions', await host.locator('[data-recommendation-form] textarea').evaluate(n => n === document.activeElement));
      await host.locator('[data-recommendation-form] textarea').fill('Retained recommendation draft');
      await host.locator('[data-event-section="overview"]').click(); await idle();
      await host.locator('[data-event-section="community"]').click(); await idle();
      check(mode + ': recommendation draft survives sections', await host.locator('[data-recommendation-form] textarea').inputValue() === 'Retained recommendation draft');
      if (out) {const file = path.join(out, 'sections-' + mode + '.png'); await page.screenshot({path: file, fullPage: true}); report.screenshots.push(file);}
      const noOpHistory = await page.evaluate(() => history.length);
      let fragments = 0;
      const countFragments = request => {if (request.headers()['x-ticketopia-section']) fragments++;};
      page.on('request', countFragments);
      await host.locator('[data-event-section="community"]').click(); await idle();
      check(mode + ': current section is a request/history no-op', fragments === 0 && await page.evaluate(() => history.length) === noOpHistory);
      page.off('request', countFragments);
      await host.locator('[data-event-section="overview"]').click(); await idle();
      let releaseOld;
      const oldGate = new Promise(resolve => {releaseOld = resolve;});
      await page.route('**/events/**', async route => {
        if (new URL(route.request().url()).searchParams.get('section') === 'discussion') {const response = await route.fetch(); await oldGate; await route.fulfill({response});}
        else await route.continue();
      });
      await host.locator('[data-event-section="discussion"]').click();
      await host.locator('[data-event-section="community"]').click(); await idle(); releaseOld();
      await page.unroute('**/events/**', {behavior: 'wait'});
      check(mode + ': obsolete request cannot overwrite rapid selection', new URL(page.url()).searchParams.get('section') === 'community' && await region.getAttribute('data-section') === 'community');
      for (const marker of ['data-event-id', 'data-section', 'data-presentation']) {
        const previousURL = page.url();
        await page.route('**/events/**', async route => {
          const response = await route.fetch();
          const body = (await response.text()).replace(new RegExp(marker + '="[^"]*"'), marker + '="mismatched"');
          await route.fulfill({response, body});
        });
        await host.locator('[data-event-section="overview"]').click(); await region.locator('[data-section-retry]').waitFor(); await idle();
        check(mode + ': rejects mismatched ' + marker + ' before mounting', page.url() === previousURL && await region.getAttribute('data-section') === 'community' && await region.locator('.recommendation-reason').isVisible());
        await page.unroute('**/events/**');
      }
      // Changed sessions are reconciled from server metadata, never inferred
      // from the private controls that happened to be mounted previously.
      await page.route('**/events/**', async route => {
        const url = new URL(route.request().url()); url.searchParams.set('account', 'guest');
        const response = await route.fetch({url: url.href}); await route.fulfill({response});
      });
      await host.locator('[data-event-section="overview"]').click(); await idle();
      check(mode + ': session changes withhold previous private controls', !await host.locator('[data-save-form], [data-interest-form], [data-recommendation-form]').count() && await host.getByRole('link', {name: /^Sign in to save/}).isVisible());
      await page.unroute('**/events/**');
    }

    await page.setViewportSize({width: 1440, height: 900});
    await page.goto(base + '/?selected=true');
    let saved = true;
    await page.route('**/saved/ticketmaster:CSS_0', async route => {saved = !saved; await route.fulfill({json: {event_id: 'ticketmaster:CSS_0', saved}});});
    await panel.locator('[data-save-form] button').click();
    await panel.locator('[data-reminder-id]').waitFor({state: 'hidden'});
    check('Confirmed unsave hides reminder settings', !await panel.locator('[data-reminder-id]').isVisible());
    await panel.locator('[data-save-form] button').click();
    await panel.locator('[data-reminder-id]').waitFor({state: 'visible'});
    check('Confirmed save exposes eligible reminder settings', true);
    await page.unroute('**/saved/ticketmaster:CSS_0');
    await page.route('**/saved/ticketmaster:CSS_0', route => route.fulfill({status: 503, json: {detail: 'Save unavailable. Try again.'}}));
    await panel.locator('[data-save-form] button').click();
    await panel.getByText('Save unavailable. Try again.', {exact: true}).waitFor();
    check('Failed save retains confirmed state and nearby retry', await panel.locator('[data-save-form] button').getAttribute('data-saved') === 'true' && await panel.locator('[data-save-form] button').isEnabled() && await panel.locator('[data-reminder-id]').isVisible());

    for (const status of ['canceled', 'postponed', 'offsale', 'unknown']) {
      await page.goto(base + '/events/ticketmaster:CSS_0?status=' + status + '&stale=true');
      check(status + ': honest status and adjacent stale warning', await page.locator('[data-status="' + status + '"]').isVisible() && await page.getByText('Showing last-known details.', {exact: false}).first().isVisible());
      check(status + ': supplied ticket link remains named', await page.locator('.ticket-link').innerText().then(text => text.includes('Tickets on Ticketmaster')));
    }
    await page.goto(base + '/events/ticketmaster:CSS_0?ticket=missing&account=disabled');
    check('Missing URL has explanation, no fake ticket action', !await page.locator('.ticket-link').count() && await page.getByText("A ticket link hasn't been supplied for this event.").isVisible());
    check('Disabled accounts have no inert action toolbar', !await page.locator('.event-actions button').count());
    await page.goto(base + '/events/ticketmaster:CSS_0?account=guest');
    check('Guest Save keeps sign-in return continuity', (await page.getByRole('link', {name: /^Sign in to save/}).getAttribute('href')).includes('return_to='));
    await page.goto(base + '/events/ticketmaster:CSS_0?account=unavailable');
    check('Unknown saved state is explained', await page.getByText('Saved state could not be loaded. Reload to try again.').isVisible() && !await page.locator('[data-save-form]').count());
    await page.emulateMedia({forcedColors: 'active', reducedMotion: 'reduce'});
    await page.keyboard.press('Tab'); await page.locator('.ticket-link').focus();
    check('Forced colors retain keyboard outline', await page.locator('.ticket-link').evaluate(n => getComputedStyle(n).outlineStyle === 'solid'));

    const touch = await browser.newPage({viewport: {width: 390, height: 844}, hasTouch: true, isMobile: true});
    await touch.goto(base + '/'); await touch.locator('.event-art a').first().tap();
    await touch.locator('#event-context .ticket-link').waitFor();
    check('Touch artwork opens the same complete Overview', await touch.getByRole('heading', {name: 'About the event'}).isVisible());
    const native = await browser.newPage({javaScriptEnabled: false, viewport: {width: 390, height: 844}});
    await native.goto(base + '/'); await native.locator('.event-art a').first().click();
    check('Native event access exposes details and tickets', await native.locator('.ticket-link').isVisible() && await native.getByText('Bring photo ID.', {exact: false}).isVisible());
    await native.getByRole('link', {name: 'Questions & tips', exact: true}).click();
    check('Native section links remain useful', await native.getByRole('heading', {name: 'Questions & tips'}).isVisible());
    check('No browser runtime errors', !report.errors.length);
    if (out) fs.writeFileSync(path.join(out, 'event-discovery-checks.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify({checks: report.checks.length, accessibility: report.accessibility.length, screenshots: report.screenshots.length}));
  } finally {await browser.close();}
})().catch(error => {console.error(error); process.exitCode = 1;});
