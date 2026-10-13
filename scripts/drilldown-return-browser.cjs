// Navigation checks against isolated local discussion/MariaDB accounts.
const {chromium, webkit} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const engine = process.env.DRILLDOWN_BROWSER_ENGINE || 'chromium';
const base = process.env.SAVED_BASE_URL;
const out = path.resolve(process.env.DRILLDOWN_REVIEW_DIR || path.join(__dirname, '../.impeccable/review/drilldown-return'), engine);
const report = {checks: [], accessibility: [], screenshots: [], errors: []};
const check = (name, value) => { assert.ok(value, name); report.checks.push(name); };
fs.mkdirSync(out, {recursive: true});
(async () => {
  const browser = await (engine === 'webkit' ? webkit : chromium).launch({headless: true});
  try {
    const context = await browser.newContext();
    const page = await context.newPage();
    page.on('pageerror', error => report.errors.push(error.message));
    const event = '/events/ticketmaster:Browser_0';
    await context.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base}]);
    const me = await (await context.request.get(base + '/api/v1/me')).json();
    const response = await context.request.post(base + '/api/v1/events/ticketmaster:Browser_0/discussions', {data: {body: 'Where is the entrance?'}, headers: {'X-CSRF-Token': me.csrf_token, 'Idempotency-Key': 'drilldown-return-question'}});
    assert.equal(response.status(), 201);
    const root = await response.json();
    await page.goto(base + process.env.SAVED_SEARCH_PATH);
    const paginatedSearch = await page.locator('#pagination a').getAttribute('href');
    check('Use a real filtered discovery cursor', new URL(paginatedSearch, base).searchParams.has('cursor'));
    const origins = [paginatedSearch, '/saved?cursor=saved-page', '/me/interests?cursor=interest-page', '/community?city=Berlin&limit=1', '/users/' + process.env.SAVED_FIXTURE_ID + '?cursor=profile-page', '/radar'];
    const surfaces = [['participants', '/interested-users', 'community', 'Back to Recommendations'], ['recommendations', '/recommendations', 'community', 'Back to Recommendations'], ['conversations', '/discussions', 'discussion', 'Back to Questions & tips'], ['thread', '/discussions/' + root.id, 'discussion', 'Back to Questions & tips']];
    for (const [name, route, section, label] of surfaces) {
      for (const [width, enlarged] of [[390, false], [320, true], [1440, false]]) {
        await page.setViewportSize({width, height: 900});
        await page.goto(base + event + route + '?return_to=' + encodeURIComponent(origins[0]));
        await page.evaluate(async enlarged => { document.documentElement.style.fontSize = enlarged ? '200%' : ''; await document.fonts.ready; }, enlarged);
        const capture = `${name}-${width}`;
        const file = path.join(out, capture + '.png');
        await page.screenshot({path: file, fullPage: true}); report.screenshots.push(file);
        check(capture + ' no overflow', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
        const nav = page.getByRole('navigation', {name: 'Event destinations', exact: true});
        check(capture + ' event destinations grouped', await nav.getByRole('link').count() === 2);
        check(capture + ' links wrap inside 44px targets', await page.locator('.drilldown-return a').evaluateAll(nodes => nodes.length === 3 && nodes.every(node => {
          const rect = node.getBoundingClientRect();
          return rect.height >= 44 && rect.width >= 44 && node.scrollWidth <= node.clientWidth + 1;
        })));
        check(capture + ' collection is visually secondary', await page.locator('.drilldown-return').evaluate(node => parseInt(getComputedStyle(node.querySelector('[data-return-results]')).fontWeight) < parseInt(getComputedStyle(node.querySelector('nav a')).fontWeight)));
        await nav.getByRole('link', {name: label, exact: true}).focus();
        check(capture + ' keyboard focus visible', await nav.getByRole('link', {name: label, exact: true}).evaluate(node => getComputedStyle(node).outlineStyle !== 'none'));
        if (process.env.AXE_SCRIPT) {
          await page.addScriptTag({path: process.env.AXE_SCRIPT});
          const violations = await page.evaluate(async () => (await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']}})).violations);
          report.accessibility.push({name: capture, violations}); check(capture + ' axe', !violations.length);
        }
      }
      for (const origin of origins) {
        await page.goto(base + event + route + '?return_to=' + encodeURIComponent(origin));
        check(name + ' exact collection origin ' + origin, await page.locator('[data-return-results]').getAttribute('href') === origin);
        const nav = page.getByRole('navigation', {name: 'Event destinations', exact: true});
        for (const [text, target] of [[label, section], ['Event Overview', 'overview']]) {
          const href = new URL(await nav.getByRole('link', {name: text, exact: true}).getAttribute('href'), base);
          check(name + ' event destination retains origin ' + text + origin, href.pathname === event && (href.searchParams.get('section') || 'overview') === target && href.searchParams.get('return_to') === origin);
        }
      }
      await page.getByRole('link', {name: label, exact: true}).click();
      await page.waitForURL(url => url.pathname === event);
      check(name + ' section return navigates', new URL(page.url()).searchParams.get('section') === section);
      await page.goto(base + event + route + '?return_to=' + encodeURIComponent('https://example.com/untrusted'));
      check(name + ' unsafe origin falls back to Discover', await page.locator('[data-return-results]').getAttribute('href') === '/');
      await page.goto(base + event + route + '?return_to=' + encodeURIComponent(paginatedSearch));
      await page.locator('[data-return-results]').click();
      await page.waitForURL(url => url.pathname === '/' && url.searchParams.has('cursor'));
      check(name + ' collection return opens the filtered next page', await page.locator('.event-row').count() === 1 && new URL(page.url()).searchParams.get('city') === 'Chicago');
    }
    {
      const native = await browser.newContext({javaScriptEnabled: false, viewport: {width: 390, height: 844}});
      const nativePage = await native.newPage();
      for (const [name, route] of surfaces) {
        await nativePage.goto(base + event + route + '?return_to=' + encodeURIComponent('/community?city=Berlin'));
        await nativePage.getByRole('link', {name: 'Event Overview', exact: true}).click();
        check(name + ' native Overview preserves origin', new URL(nativePage.url()).pathname === event && new URL(nativePage.url()).searchParams.get('return_to') === '/community?city=Berlin');
      }
      await native.close();
    }
    check('No browser errors', !report.errors.length);
  } finally {
    await browser.close();
    fs.writeFileSync(path.join(out, 'report.json'), JSON.stringify(report, null, 2));
  }
  console.log(JSON.stringify(report));
})().catch(error => { console.error(error); process.exit(1); });
