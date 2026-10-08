// Public production routes backed by an isolated SQL catalog, with no provider key.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.HISTORY_BASE_URL;
const out = process.env.HISTORY_REVIEW_DIR;
const report = { checks: [], accessibility: [], consoleErrors: [] };
const check = (name, ok) => { assert.ok(ok, name); report.checks.push(name); };
const query = city => '/?city=' + city + '&country=DE&start_date=2026-10-08&end_date=2026-10-10&limit=3';

(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
    const page = await context.newPage();
    page.on('pageerror', error => report.consoleErrors.push(error.message));
    const capture = async (name, width, enlarged = false) => {
      await page.setViewportSize({ width, height: 1000 });
      await page.evaluate(async enlarged => {
        document.documentElement.style.fontSize = enlarged ? '200%' : '';
        await document.fonts.ready;
      }, enlarged);
      check(name + ': no horizontal overflow', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      if (out) { fs.mkdirSync(out, { recursive: true }); await page.screenshot({ path: path.join(out, 'history-' + name + '.png'), fullPage: true }); }
      if (process.env.AXE_SCRIPT) {
        await page.addScriptTag({ path: process.env.AXE_SCRIPT });
        const scan = await page.evaluate(() => axe.run(document, { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa'] } }));
        report.accessibility.push({ name, violations: scan.violations.map(v => v.id) });
        check(name + ': axe clean', scan.violations.length === 0);
      }
    };
    await page.goto(base + query('Berlin'));
    check('Discover works without provider key', await page.locator('#event-list > li').count() === 3);
    check('Complete collection shown', await page.locator('[data-coverage="complete"]').first().isVisible());
    await page.getByRole('link', { name: 'Load more events' }).click();
    await page.waitForFunction(() => document.querySelectorAll('#event-list > li').length === 6);
    await page.getByRole('link', { name: 'Load more events' }).click();
    await page.waitForFunction(() => document.querySelectorAll('#event-list > li').length === 7);
    check('SQL load more has no duplicates', await page.locator('#event-list > li').evaluateAll(nodes => new Set(nodes.map(n => n.dataset.eventId)).size === nodes.length));
    await page.locator('#filters > summary').click();
    await page.getByLabel('Sort events').selectOption('name_asc');
    await page.getByRole('button', { name: 'Apply filters' }).click();
    check('Native sort survives submission', await page.getByLabel('Sort events').inputValue() === 'name_asc');
    await page.getByLabel('Event, performer, team, or keyword', { exact: true }).fill('Band');
    await page.getByRole('button', { name: 'Find events' }).click();
    check('SQL keyword finds catalog without provider', await page.locator('#event-list > li').count() === 3);
    await page.locator('#filters > summary').click();
    await page.getByLabel('Sort events').focus();
    check('Keyboard focus is outlined', await page.getByLabel('Sort events').evaluate(el => getComputedStyle(el).outlineStyle !== 'none'));
    for (const [name, width, enlarged] of [['desktop',1440,false],['tablet',1024,false],['mobile',390,false],['compact',320,false],['enlarged',390,true]]) await capture(name,width,enlarged);
    await page.evaluate(() => document.documentElement.style.fontSize = '');
    await page.goto(base + '/events/ticketmaster:browser-Berlin-0?section=overview');
    check('First/last seen on event detail', await page.getByText('First seen', { exact: false }).isVisible());
    check('Dated date change', await page.getByText('Previously scheduled for 2026-10-08', { exact: false }).isVisible());
    check('Dated venue change with escaped public name', await page.getByText('Moved from Old <venue>', { exact: false }).isVisible());
    await capture('detail',390);
    const h = await (await context.request.get(base + '/api/v1/events/ticketmaster:browser-Berlin-0/history?limit=1')).json();
    check('API and HTML share changes', h.changes.length === 2 && h.changes.some(c => c.kind === 'venue'));
    for (const [city,status,title] of [['Partial','partial','Partial coverage.'],['Older','stale','Showing older results'],['Unknown','not_collected',"Events haven't been collected yet"],['Empty','complete','No events found for these filters']]) {
      const response = await page.goto(base + query(city));
      check(city + ': usable response', response.status() === 200);
      check(city + ': coverage state', await page.locator('[data-coverage="' + status + '"]').first().isVisible());
      check(city + ': honest state copy', await page.getByText(title, { exact: false }).first().isVisible());
    }
    const native = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 390, height: 1000 } });
    const nativePage = await native.newPage();
    await nativePage.goto(base + query('Berlin'));
    await nativePage.getByRole('link', { name: 'Load more events' }).click();
    check('No-JavaScript pagination', await nativePage.locator('#event-list > li').count() === 3 && nativePage.url().includes('cursor='));
    await native.close();
    check('No browser runtime errors', report.consoleErrors.length === 0);
    if (out) fs.writeFileSync(path.join(out,'history-checks.json'),JSON.stringify(report,null,2));
    console.log(JSON.stringify(report,null,2));
    await context.close();
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
