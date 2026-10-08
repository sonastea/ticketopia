// Isolated MariaDB accounts and server-owned event fixtures; production auth/CSRF.
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.SAVED_BASE_URL;
const out = path.resolve(__dirname, '../.impeccable/review');
const report = {checks: [], screenshots: [], accessibility: [], errors: []};
const check = (name, condition) => { assert.ok(condition, name); report.checks.push(name); };
fs.mkdirSync(out, {recursive: true});
(async () => {
  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE});
  try {
    const owner = await browser.newContext({viewport: {width: 1440, height: 1000}});
    const other = await browser.newContext();
    const guest = await browser.newContext();
    await owner.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base}]);
    await other.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_OTHER_TOKEN, url: base}]);
    const me = await (await owner.request.get(base + '/api/v1/me')).json();
    const otherMe = await (await other.request.get(base + '/api/v1/me')).json();
    const page = await owner.newPage();
    page.on('pageerror', e => report.errors.push(e.message));
    const id = 'ticketmaster:Browser_0';
    const endpoint = '/api/v1/events/' + id + '/discussions';
    await page.goto(base + process.env.SAVED_SEARCH_PATH);
    await page.locator('.event-link').first().click();
    const preview = page.locator('[data-context-id="' + id + '"]');
    await preview.getByRole('link', {name: 'Discussion', exact: true}).click();
    await preview.locator('.discussion-editor summary').click();
    const composer = preview.locator('.discussion-composer');
    await composer.locator('textarea').fill('How is the balcony view?');
    await preview.getByRole('link', {name: 'Overview', exact: true}).click();
    await preview.getByRole('link', {name: 'Discussion', exact: true}).click();
    await preview.locator('.discussion-editor summary').click();
    check('Draft survives selected-event section changes', await preview.locator('textarea').inputValue() === 'How is the balcony view?');
    await page.setViewportSize({width: 390, height: 900});
    check('Draft survives responsive transition', await preview.locator('textarea').inputValue() === 'How is the balcony view?');
    await page.route('**/discussion-actions', route => route.fulfill({status: 503, contentType: 'application/problem+json', body: JSON.stringify({detail: 'Storage unavailable. Try again.'})}));
    await preview.getByRole('button', {name: 'Post question or tip'}).click();
    await preview.locator('[data-discussion-error]').waitFor({state: 'visible'});
    check('Failed publication retains draft', await preview.locator('textarea').inputValue() === 'How is the balcony view?');
    await page.unroute('**/discussion-actions');
    const retryKey = await preview.locator('[name="idempotency_key"]').inputValue();
    await page.route('**/discussion-actions', route => route.abort('failed'));
    await preview.getByRole('button', {name: 'Post question or tip'}).click();
    await preview.locator('[data-discussion-error]').waitFor({state:'visible'});
    check('Network failure explains confirmation uncertainty', (await preview.locator('[data-discussion-error]').innerText()) === 'The change couldn’t be confirmed. Your draft is kept; try again.');
    check('Network failure preserves draft and retry identity', await preview.locator('textarea').inputValue() === 'How is the balcony view?' && await preview.locator('[name="idempotency_key"]').inputValue() === retryKey);
    if (process.env.DISCUSSION_SKIP_CAPTURES !== 'true') {
      for (const [name,width] of [['discussion-network-error-desktop',1440],['discussion-network-error-mobile',390]]) {
        await page.setViewportSize({width,height:1000});
        await page.evaluate(async () => { await document.fonts.ready; scrollTo(0,0); });
        if (width >= 1280) {
          // The desktop preview scrolls independently of the document. Show
          // the alert in that scrollport while retaining a document-top capture.
          await preview.locator('[data-discussion-error]').scrollIntoViewIfNeeded();
          await page.evaluate(() => scrollTo(0,0));
        }
        const file = path.join(out,name+'.png'); await page.screenshot({path:file,fullPage:true}); report.screenshots.push(file);
      }
    }
    await page.unroute('**/discussion-actions');
     await preview.getByRole('button', {name: 'Post question or tip'}).click();
     await page.waitForURL(url => url.searchParams.has('selected_thread'));
     await preview.getByRole('link', {name: 'Open full conversation', exact: true}).click();
     await page.waitForURL('**/discussions/*');
    check('Publishing opens a shareable event thread', await page.locator('.discussion-post').first().innerText().then(text => text.includes('How is the balcony view?')));
    const list = await (await owner.request.get(base + endpoint)).json();
    check('Read-after-write root appears immediately', list.items && list.items.length === 1);
    const root = list.items[0];
    const thread = '/events/' + id + '/discussions/' + root.id;
    const replyResponse = await other.request.post(base + endpoint + '/' + root.id + '/replies', {data: {body: 'The balcony has a clear view. Arrive early.'}, headers: {'X-CSRF-Token': otherMe.csrf_token, 'Idempotency-Key': 'browser-reply-000001'}});
    check('Another person can answer', replyResponse.status() === 201);
    const reply = await replyResponse.json();
    await page.goto(base + thread);
    await Promise.all([page.waitForNavigation({waitUntil:'networkidle'}),page.locator('#post-' + reply.id).getByRole('button', {name: 'Helpful'}).click()]);
    const helpful = await (await owner.request.get(base + endpoint + '/' + root.id + '/replies')).json();
    check('Helpful acknowledgment is confirmed', helpful.items[0].helpful && helpful.items[0].helpful_count === 1);
    await page.locator('#post-' + reply.id).getByRole('link', {name: 'Reply', exact: true}).click();
    check('Reply target retains same-thread context', new URL(page.url()).searchParams.get('reply_to') === reply.id);
    await page.locator('#discussion-composer textarea').fill('Thank you, that helps!');
    await Promise.all([page.waitForNavigation({waitUntil:'networkidle'}),page.getByRole('button', {name: 'Publish reply'}).click()]);
    await page.goto(base + thread);
    check('Reply-to-reply names its target with one visual level', await page.getByRole('link', {name: /In reply to/}).count() === 1 && await page.locator('.discussion-replies .discussion-replies').count() === 0);
    const capture = async (name, width, zoom = false) => {
      if (process.env.DISCUSSION_SKIP_CAPTURES === 'true') return;
      await page.setViewportSize({width, height: 1000});
      await page.evaluate(async zoom => {document.documentElement.style.fontSize = zoom ? '200%' : ''; await document.fonts.ready; scrollTo(0,0);}, zoom);
      check('No horizontal overflow: ' + name, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      const file = path.join(out, name + '.png');
      await page.screenshot({path: file, fullPage: true}); report.screenshots.push(file);
      if (process.env.AXE_SCRIPT) {
        await page.addScriptTag({path: process.env.AXE_SCRIPT});
        const violations = await page.evaluate(async () => (await axe.run(document, {runOnly: {type: 'tag', values: ['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations);
        report.accessibility.push({name, violations}); check('Accessibility: ' + name, !violations.length);
      }
    };
    await capture('discussion-desktop', 1440);
    await capture('discussion-tablet', 1024);
    await capture('discussion-mobile', 390);
    await capture('discussion-small-mobile', 320);
    await capture('discussion-text-zoom', 390, true);
    await page.evaluate(() => document.documentElement.style.fontSize = '');
    const guestPage = await guest.newPage();
    await guestPage.goto(base + thread);
    check('Guests read threads without seeing owner controls', await guestPage.getByText('The balcony has a clear view. Arrive early.', {exact: true}).count() === 1 && !await guestPage.getByText('Edit your contribution', {exact: true}).count());
    const publicReplies = await (await guest.request.get(base + endpoint + '/' + root.id + '/replies')).json();
    check('Public reads do not leak private accounts or viewer reactions', !publicReplies.items[0].helpful && !JSON.stringify(publicReplies).includes(me.item.email));
    check('Other users cannot edit root', (await other.request.patch(base + '/api/v1/posts/' + root.id, {data: {body: 'Hijacked'}, headers: {'X-CSRF-Token': otherMe.csrf_token}})).status() === 404);
    await page.goto(base + '/community/discussions?city=Berlin&category_id=sports&limit=1');
    check('City/category conversation browsing', await page.locator('.discussion-list li').count() === 1);
    await page.getByRole('link', {name: /Open conversation/}).click();
    check('Direct thread retains community return', new URL(page.url()).searchParams.get('return_to') === '/community/discussions?city=Berlin&category_id=sports&limit=1');
    const fallback = await owner.newPage();
    await fallback.goto(process.env.SAVED_FALLBACK_URL + thread);
    check('Thread works without provider/cache data', (await fallback.locator('.discussion-post > .discussion-body').first().innerText()) === 'How is the balcony view?');
    await page.goto(base + thread);
    await page.locator('#post-' + root.id).getByText('Edit your contribution', {exact: true}).click();
    await page.locator('#post-' + root.id + ' textarea').fill('How is the BALCONY view?');
    await Promise.all([page.waitForNavigation({waitUntil:'networkidle'}),page.locator('#post-' + root.id).getByRole('button', {name: 'Save changes'}).click()]);
    check('Owner edit preserves publication time', (await (await owner.request.get(base + endpoint + '/' + root.id)).json()).created_at === root.created_at);
    await page.locator('#post-' + root.id).getByText('Remove your contribution', {exact: true}).click();
    await Promise.all([page.waitForNavigation({waitUntil:'networkidle'}),page.locator('#post-' + root.id).getByRole('button', {name: 'Remove contribution', exact: true}).click()]);
    check('Removal keeps replies and withholds removed content', await page.getByText('This contribution was removed.', {exact: true}).count() === 1 && await page.getByText('The balcony has a clear view. Arrive early.', {exact: true}).count() === 1 && !await page.getByText('How is the BALCONY view?', {exact: true}).count());
    const native = await browser.newContext({javaScriptEnabled: false});
    await native.addCookies([{name: 'ticketopia_session', value: process.env.SAVED_FIXTURE_TOKEN, url: base}]);
    const nativePage = await native.newPage();
    await nativePage.goto(base + '/events/' + id + '?section=discussion');
    await nativePage.locator('.discussion-editor summary').click();
    await nativePage.locator('textarea').fill('A question posted without JavaScript.');
    await nativePage.getByRole('button', {name: 'Post question or tip'}).click();
    check('Native forms publish and redirect to the thread', await nativePage.locator('.discussion-post').first().innerText().then(text => text.includes('A question posted without JavaScript.')));
    const failed = await guest.request.post(base + '/discussion-actions', {form: {action:'create', event_id:id, body:'<script>preserved draft</script>', idempotency_key:'native-session-failure', return_to:thread}});
    check('Native session failure retains escaped draft and sign-in recovery', failed.status() === 401 && (await failed.text()).includes('&lt;script&gt;preserved draft&lt;/script&gt;'));
    check('Rejected cross-origin writes remain protected', (await owner.request.post(base + endpoint, {data:{body:'Rejected'},headers:{'X-CSRF-Token':me.csrf_token,'Idempotency-Key':'bad-origin-request-key','Origin':'https://evil.example'}})).status() === 403);
    const disabled = await guest.request.get(process.env.SAVED_DISABLED_URL + '/api/v1/community/discussions');
    check('Disabled discussions report unavailable, not zero', disabled.status() === 503);
    check('No browser runtime errors', !report.errors.length);
    fs.writeFileSync(path.join(out, 'discussion-checks.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify({checks:report.checks.length,screenshots:report.screenshots.length,accessibility:report.accessibility.length}));
  } finally { await browser.close(); }
})().catch(e => {console.error(e); process.exitCode=1;});
