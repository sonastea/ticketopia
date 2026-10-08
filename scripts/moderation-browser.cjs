// Synthetic, isolated MariaDB fixture; real authorization, native forms and CSRF.
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.SAVED_BASE_URL;
const out = path.resolve(__dirname, '../.impeccable/review');
const report = {checks: [], screenshots: [], accessibility: [], errors: []};
const check = (name, condition) => {assert.ok(condition, name); report.checks.push(name);};
fs.mkdirSync(out, {recursive: true});
(async () => {
  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE});
  try {
    const context = async (token, native = false) => {
      const c = await browser.newContext({viewport: {width: 1440, height: 1000}, javaScriptEnabled: !native});
      if (token) await c.addCookies([{name: 'ticketopia_session', value: token, url: base}]);
      return c;
    };
    const author = await context(process.env.SAVED_FIXTURE_TOKEN);
    const reporter = await context(process.env.SAVED_OTHER_TOKEN, true);
    const moderator = await context(process.env.MODERATION_FIXTURE_TOKEN);
    const guest = await context('');
    const authorMe = await (await author.request.get(base + '/api/v1/me')).json();
    const reporterMe = await (await reporter.request.get(base + '/api/v1/me')).json();
    const create = await author.request.post(base + '/api/v1/events/ticketmaster:Browser_0/discussions', {
      data: {body: 'A venue tip that needs a moderator to review its context.'},
      headers: {'X-CSRF-Token': authorMe.csrf_token, 'Idempotency-Key': 'moderation-browser-root-01'},
    });
    check('Author publishes fixture contribution', create.status() === 201);
    const root = await create.json();
    const thread = '/events/' + root.event_id + '/discussions/' + root.id;
    const reply = await reporter.request.post(base + '/api/v1/events/' + root.event_id + '/discussions/' + root.id + '/replies', {
      data: {body: 'A useful reply remains readable when the root is hidden.'},
      headers: {'X-CSRF-Token': reporterMe.csrf_token, 'Idempotency-Key': 'moderation-browser-reply-01'},
    });
    check('Reply is created independently', reply.status() === 201);
    const rp = await reporter.newPage();
    await rp.goto(base + thread);
    await rp.locator('#post-' + root.id).getByRole('link', {name: 'Report privately'}).click();
    check('Report page keeps event and contribution context', await rp.getByRole('heading', {name: 'A good night in the city'}).count() === 1 && (await rp.locator('blockquote').innerText()).includes('venue tip'));
    await rp.getByLabel('Reason', {exact: true}).selectOption('private_information');
    await rp.getByLabel(/Additional context/).fill('PRIVATE REPORT CONTEXT: please review whether this venue tip exposes someone’s information. ' + 'LongContext'.repeat(20));
    const formPath = new URL(rp.url()).pathname;
    // Capture the interactive state in a JS-enabled read of the same native form.
    const captureContext = await context(process.env.SAVED_OTHER_TOKEN);
    const capturePage = await captureContext.newPage();
    const capture = async (page, name, width, zoom = false) => {
      if (process.env.MODERATION_SKIP_CAPTURES === 'true') return;
      await page.setViewportSize({width, height: 1000});
      await page.evaluate(async zoom => {document.documentElement.style.fontSize = zoom ? '200%' : ''; await document.fonts.ready; scrollTo(0, 0);}, zoom);
      check('No horizontal overflow: ' + name, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      const file = path.join(out, name + '.png');
      await page.screenshot({path: file, fullPage: true}); report.screenshots.push(file);
      if (process.env.AXE_SCRIPT) {
        await page.addScriptTag({path: process.env.AXE_SCRIPT});
        const violations = await page.evaluate(async () => (await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations);
        report.accessibility.push({name, violations}); check('Accessibility: ' + name, !violations.length);
      }
    };
    await capturePage.goto(base + formPath);
    await capturePage.getByLabel('Reason', {exact: true}).selectOption('private_information');
    await capturePage.getByLabel(/Additional context/).fill('Please review this concern privately. ' + 'LongContext'.repeat(20));
    await capture(capturePage, 'moderation-report-desktop', 1440);
    await capture(capturePage, 'moderation-report-mobile', 390);
    const failed = await reporter.request.post(base + formPath, {form: {csrf_token: 'wrong', reason: 'spam', context: '<script>PRIVATE DRAFT</script>'}});
    check('Rejected native report preserves escaped draft', failed.status() === 403 && (await failed.text()).includes('&lt;script&gt;PRIVATE DRAFT&lt;/script&gt;'));
    await rp.getByRole('button', {name: 'Send private report'}).click();
    await rp.waitForURL('**/me/reports');
    check('Native report receipt is visible', await rp.getByText('Awaiting moderator review', {exact: true}).count() === 1);
    check('Personal receipt identifies event and exact contribution', await rp.getByRole('heading', {name: root.event.name, exact:true}).count() === 1 && (await rp.getByRole('link', {name: 'Open reported contribution'}).getAttribute('href')).endsWith('#post-' + root.id));
    await capturePage.goto(base + '/me/reports');
    await capture(capturePage, 'moderation-receipts-desktop', 1440);
    await capture(capturePage, 'moderation-receipts-mobile', 390);
    const retry = await reporter.request.post(base + '/api/v1/posts/' + root.id + '/reports', {data: {reason: 'spam'}, headers: {'X-CSRF-Token': reporterMe.csrf_token}});
    check('Report retry returns existing receipt', retry.status() === 200);
    check('Reports do not hide content automatically', !(await (await guest.request.get(base + '/api/v1/events/' + root.event_id + '/discussions/' + root.id)).json()).hidden);
    check('Moderator-author cannot read own case evidence', (await author.request.get(base + '/api/v1/moderation/posts/' + root.id)).status() === 403 && (await author.request.get(base + '/moderation/posts/' + root.id)).status() === 403);
    const selfQueue = await (await author.request.get(base + '/api/v1/moderation?state=all')).json();
    check('Moderator-author queue omits own case activity', selfQueue.items.length === 0);
    const mp = await moderator.newPage();
    mp.on('pageerror', e => report.errors.push(e.message));
    await mp.goto(base + '/me');
    await mp.getByRole('link', {name: 'Moderator review queue'}).click();
    await capture(mp, 'moderation-queue-desktop', 1440);
    await capture(mp, 'moderation-queue-mobile', 390);
    await mp.getByRole('link', {name: 'Review contribution', exact: true}).click();
    await mp.locator('#reported-concerns + details summary').click();
    await mp.getByLabel('Action', {exact: true}).selectOption('hide');
    await mp.getByLabel(/Decision reason/).fill('Hidden while we review potentially private information.');
    await mp.getByLabel(/Private moderator notes/).fill('PRIVATE MODERATOR NOTES: check the full conversation, not report volume.');
    for (const [name, width, zoom] of [['moderation-case-desktop',1440,false],['moderation-case-tablet',1024,false],['moderation-case-mobile',390,false],['moderation-case-small-mobile',320,false],['moderation-case-text-zoom',390,true]]) await capture(mp, name, width, zoom);
    await mp.evaluate(() => document.documentElement.style.fontSize = '');
    const edited = await author.request.patch(base + '/api/v1/posts/' + root.id, {data: {body: 'An edited venue tip needing contextual review.'}, headers: {'X-CSRF-Token': authorMe.csrf_token}});
    check('Author can edit before moderator action', edited.status() === 200);
    await mp.getByRole('button', {name: 'Record decision'}).click();
    check('Stale native review preserves drafts and shows new context', (await mp.locator('[role="alert"]').innerText()).includes('changed') && await mp.getByLabel(/Private moderator notes/).inputValue() === 'PRIVATE MODERATOR NOTES: check the full conversation, not report volume.' && await mp.getByText('An edited venue tip needing contextual review.', {exact:true}).count() === 1);
    await mp.getByRole('button', {name: 'Record decision'}).click();
    await mp.waitForLoadState('networkidle');
    check('Moderator decision is recorded in history', await mp.getByText('Contribution hidden', {exact: true}).count() === 1);
    const gp = await guest.newPage();
    await gp.goto(base + thread);
    check('Public hidden placeholder preserves replies', await gp.getByText('This contribution was hidden by a moderator. Replies are kept.', {exact:true}).count() === 1 && await gp.getByText('A useful reply remains readable when the root is hidden.', {exact:true}).count() === 1);
    check('Hidden text and private evidence stay out of public page', !(await gp.content()).includes('An edited venue tip') && !(await gp.content()).includes('PRIVATE REPORT CONTEXT') && !(await gp.content()).includes('PRIVATE MODERATOR NOTES'));
    const ap = await author.newPage();
    await ap.goto(base + '/me/moderation');
    check('Author sees shared reason without report evidence', (await ap.innerText('main')).includes('Hidden while we review') && !(await ap.content()).includes('PRIVATE REPORT CONTEXT') && !(await ap.content()).includes('PRIVATE MODERATOR NOTES'));
    check('Personal outcome identifies event and exact contribution', await ap.getByRole('heading', {name:root.event.name,exact:true}).count() === 1 && (await ap.getByRole('link', {name:'Open affected contribution'}).getAttribute('href')).endsWith('#post-' + root.id));
    await ap.getByText('Request another review', {exact: true}).first().click();
    await ap.getByLabel(/Why should this decision/).fill('Please reconsider the venue context; this is not a person’s information.');
    await capture(ap, 'moderation-outcome-desktop', 1440);
    await capture(ap, 'moderation-outcome-mobile', 390);
    await ap.getByRole('button', {name: 'Request another review'}).click();
    await ap.waitForLoadState('networkidle');
    check('Author review request acknowledged', (await ap.innerText('main')).includes('It is awaiting review.'));
    await mp.goto(base + '/moderation');
    check('Author request reopens review queue', (await mp.innerText('main')).includes('1 author review requests'));
    await mp.getByRole('link', {name: 'Review contribution', exact: true}).click();
    await mp.getByLabel('Action', {exact: true}).selectOption('restore');
    await mp.getByLabel(/Decision reason/).fill('Restored after reviewing the venue context.');
    await mp.getByRole('button', {name: 'Record decision'}).click();
    await mp.waitForLoadState('networkidle');
    const visible = await (await guest.request.get(base + '/api/v1/events/' + root.event_id + '/discussions/' + root.id)).json();
    check('Restoration returns retained content', !visible.hidden && visible.body === 'An edited venue tip needing contextual review.');
    await rp.goto(base + '/me/reports');
    check('Reporter sees reviewed resolution', await rp.getByText('Contribution hidden', {exact:true}).count() === 1 && !(await rp.content()).includes('PRIVATE MODERATOR NOTES'));
    const reporterResult = await (await reporter.request.get(base + '/api/v1/me/reports')).json();
    check('Reporter resolution omits private author appeal metadata', !JSON.stringify(reporterResult).includes('appealed'));
    check('Disabled reporting is unavailable', (await guest.request.get(process.env.SAVED_DISABLED_URL + '/api/v1/me/reports')).status() === 503);
    // Keyboard focus and forced colors use the same native controls; no new motion.
    await mp.goto(base + '/moderation/posts/' + root.id);
    await mp.emulateMedia({forcedColors: 'active', reducedMotion: 'reduce'});
    await mp.getByLabel('Action', {exact:true}).focus();
    check('Forced-colors keyboard focus retains an outline', await mp.getByLabel('Action', {exact:true}).evaluate(el => getComputedStyle(el).outlineStyle !== 'none'));
    check('No browser runtime errors', report.errors.length === 0);
    fs.writeFileSync(path.join(out, 'moderation-checks.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify({checks: report.checks.length, screenshots: report.screenshots.length, accessibility: report.accessibility.length}));
  } finally {await browser.close();}
})().catch(e => {console.error(e); process.exitCode = 1;});
