// Read-only responsive context checks against scripts/css-preview.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.CSS_BASE_URL || 'http://127.0.0.1:18088';
const out = process.env.CSS_SCREENSHOT_DIR;
const report = { checks: [], screenshots: [], errors: [] };
const check = (name, result) => { assert.ok(result, name); report.checks.push(name); };

(async () => {
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE });
  try {
    const page = await browser.newPage();
    page.on('pageerror', error => report.errors.push(error.message));
    for (const [name, width, height, enlarged] of [
      ['desktop', 1440, 900, false], ['short-desktop', 1440, 768, false],
      ['wide', 1920, 1080, false], ['ultrawide', 2560, 1080, false],
      ['tablet', 1024, 900, false], ['mobile', 390, 900, false],
      ['small', 320, 900, false], ['enlarged', 1440, 900, true],
    ]) {
      await page.setViewportSize({ width, height });
      await page.goto(base + '/?selected=true&section=overview');
      await page.evaluate(async enlarged => {
        document.documentElement.style.fontSize = enlarged ? '200%' : '';
        await document.fonts.ready;
      }, enlarged);
      const panel = page.locator('#event-context');
      const geometry = await panel.evaluate(node => {
        const rect = selector => {
          const { x, y, width, height, bottom } = node.querySelector(selector).getBoundingClientRect();
          return { x, y, width, height, bottom };
        };
        return {
          panel: node.getBoundingClientRect().toJSON(),
          discussion: rect('#context-discussion-heading'), community: rect('#context-community-heading'),
          question: rect('.discussion-body'), recommendation: rect('.recommendation-reason'),
          columns: getComputedStyle(node.querySelector('.context-activity')).gridTemplateColumns,
          scrollTop: node.scrollTop,
        };
      });
      const wide = width >= 1440 && !enlarged;
      if (wide) {
        check(name + ': selected context fills the usable desktop height', geometry.panel.height >= height - 49 && geometry.panel.height <= height - 47);
        check(name + ': context gets the larger share of workspace', geometry.panel.width > await page.locator('.discovery-content').evaluate(node => node.getBoundingClientRect().width));
        check(name + ': activity columns share a starting line', Math.abs(geometry.discussion.y - geometry.community.y) < 1 && geometry.community.x > geometry.discussion.x);
        check(name + ': question and recommendation are visible without scrolling', geometry.scrollTop === 0 && geometry.question.bottom <= Math.min(height, geometry.panel.bottom) && geometry.recommendation.bottom <= Math.min(height, geometry.panel.bottom));
      } else {
        check(name + ': activity stacks when columns would be too narrow', geometry.community.y > geometry.discussion.y && Math.abs(geometry.community.x - geometry.discussion.x) < 1);
      }
      check(name + ': no horizontal overflow', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      check(name + ': Overview remains available without displacing activity', await panel.locator('.context-event-details .provider-copy').count() === 1 && !await panel.locator('.context-event-details').evaluate(node => node.open));
      check(name + ': no preview tabs', await panel.locator('.event-section-nav').count() === 0);
      if (out) {
        fs.mkdirSync(out, { recursive: true });
        const file = path.join(out, 'event-context-space-' + name + '.png');
        await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
      }
      await panel.locator('.context-event-details > summary').click();
      check(name + ': native event details remain usable', await panel.locator('.provider-copy').isVisible() && await panel.locator('[data-save-form]').isVisible());
      await panel.locator('.context-event-details > summary').click();
      await page.keyboard.press('Tab');
      await panel.locator('.discussion-editor > summary').focus();
      check(name + ': keyboard focus stays visible', await panel.locator('.discussion-editor > summary').evaluate(node => getComputedStyle(node).outlineStyle === 'solid'));
    }
    // Richer discovery rows remain useful with or without selected context.
    for (const [name, width, enlarged] of [['desktop', 1440, false], ['tablet', 1024, false], ['mobile', 390, false], ['small', 320, false], ['enlarged', 1440, true]]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(base + '/');
      await page.evaluate(async enlarged => {
        document.documentElement.style.fontSize = enlarged ? '200%' : '';
        await document.fonts.ready;
      }, enlarged);
      const first = page.locator('.event-row').first();
      check('Rows ' + name + ': useful description and performers are visible', await first.locator('.row-description').isVisible() && (await first.locator('.row-details').innerText()).includes('The Neighborhood Band'));
      check('Rows ' + name + ': actual question and recommendation previews are visible', await first.locator('.row-activity').isVisible() && (await first.locator('.row-activity').innerText()).includes('Is there a coat check'));
      check('Rows ' + name + ': events without activity do not fabricate engagement', await page.locator('.event-row').nth(1).locator('.row-activity').count() === 0);
      check('Rows ' + name + ': essential event facts are not truncated', await first.locator('.event-link').evaluate(node => getComputedStyle(node).whiteSpace !== 'nowrap' && getComputedStyle(node).textOverflow !== 'ellipsis'));
      check('Rows ' + name + ': added links have touch-sized targets', await first.locator('.row-activity a').evaluateAll(nodes => nodes.every(node => node.getBoundingClientRect().height >= 44)));
      check('Rows ' + name + ': no horizontal overflow', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      check('Rows ' + name + ': extra information uses the full row width', await first.evaluate(node => {
        const style = getComputedStyle(node);
        const available = node.clientWidth - parseFloat(style.paddingInlineStart) - parseFloat(style.paddingInlineEnd);
        return Math.abs(node.querySelector('.event-row-extension').getBoundingClientRect().width - available) < 1;
      }));
      if (enlarged) check('Rows: enlarged copy retains readable width', await first.locator('.event-copy').evaluate(node => node.getBoundingClientRect().width >= 320));
      await first.locator('[data-thread-link]').focus();
      check('Rows ' + name + ': conversation link has visible keyboard focus', await first.locator('[data-thread-link]').evaluate(node => getComputedStyle(node).outlineStyle === 'solid'));
      if (out) {
        const file = path.join(out, 'event-rows-' + name + '.png');
        await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
      }
    }
    await page.goto(base + '/?selected=true&activity=none');
    check('Selected context remains tall even without community activity', await page.locator('#event-context').evaluate(node => node.getBoundingClientRect().height >= innerHeight - 49));
    // Serve production-template fragments locally; exercise client navigation
    // without any provider, database mutation, or OAuth dependency.
    const fragment = async query => {
      const response = await page.request.get(base + query);
      return page.evaluate(html => new DOMParser().parseFromString(html, 'text/html').getElementById('event-context').innerHTML, await response.text());
    };
    const eventFragment = await fragment('/?selected=true');
    const threadFragment = await fragment('/?selected=true&thread=true');
    await page.route('**/events/**', async route => {
      check('Row navigation requests only selected context', route.request().headers()['x-ticketopia-panel'] === 'true');
      await route.fulfill({ contentType: 'text/html', body: route.request().url().includes('/discussions/') ? threadFragment : eventFragment });
    });
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(base + '/');
      await page.locator('.event-row').first().locator('[data-thread-link]').click();
      await page.locator('#event-context .context-thread').waitFor();
      check('Row conversation ' + width + ': thread selection is retained in URL', new URL(page.url()).searchParams.get('selected_thread') === 'css-question');
      check('Row conversation ' + width + ': results are retained', await page.locator('.event-row').count() === 2);
      await page.locator('[data-close-context]').click();
      check('Row conversation ' + width + ': return restores matching event focus', await page.locator('.event-row').first().locator('.event-link').evaluate(node => node === document.activeElement));
      await page.locator('.event-row').first().getByRole('link', { name: 'Read recommendations', exact: true }).click();
      await page.locator('#context-community-heading').waitFor();
      check('Row recommendations ' + width + ': selects community without leaving discovery', new URL(page.url()).pathname === '/' && new URL(page.url()).searchParams.get('section') === 'community');
    }
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(base + '/?selected=true&thread=true');
      const panel = page.locator('#event-context');
      check('Thread ' + width + ': root, replies and community remain available', await panel.locator('.context-thread .discussion-post').count() === 2 && await panel.locator('.recommendation-reason').count() === 1);
      if (width >= 1440) {
        check('Thread: community is visible beside the conversation without scrolling', await panel.evaluate(node => {
          const thread = node.querySelector('.context-thread').getBoundingClientRect();
          const community = node.querySelector('#context-community-heading').getBoundingClientRect();
          const reason = node.querySelector('.recommendation-reason').getBoundingClientRect();
          return community.x > thread.x && reason.bottom <= node.getBoundingClientRect().bottom && node.scrollTop === 0;
        }));
      }
      if (out) {
        const file = path.join(out, 'event-context-space-thread-' + width + '.png');
        await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
      }
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto(base + '/?selected=true&section=discussion');
    await page.locator('.sidebar-toggle').click();
    await page.evaluate(() => document.getAnimations().forEach(animation => animation.finish()));
    check('Collapsed navigation frees space for parallel activity', await page.locator('.context-activity').evaluate(node => getComputedStyle(node).gridTemplateColumns.split(' ').length === 2));
    await page.locator('.discussion-body').evaluate(node => node.textContent = 'LongContributionWord'.repeat(200));
    await page.locator('.recommendation-reason').evaluate(node => node.textContent = 'LongRecommendationWord'.repeat(100));
    check('Long contributions wrap instead of widening the workspace', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
    await page.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
    await page.keyboard.press('Tab');
    await page.locator('.discussion-editor > summary').focus();
    check('Forced-colors focus remains visible without motion', await page.locator('.discussion-editor > summary').evaluate(node => getComputedStyle(node).outlineStyle === 'solid'));
    const native = await browser.newPage({ javaScriptEnabled: false, viewport: { width: 1440, height: 900 } });
    await native.goto(base + '/?selected=true&section=discussion');
    check('Parallel activity works without JavaScript', await native.locator('.context-activity').evaluate(node => getComputedStyle(node).gridTemplateColumns.split(' ').length === 2));
    await native.locator('.discussion-editor > summary').click();
    check('Native discussion composer remains usable', await native.locator('.discussion-composer textarea').isVisible());
    await native.goto(base + '/');
    check('Native rows expose description and public contributions', await native.locator('.row-description').first().isVisible() && await native.locator('.row-activity').isVisible());
    check('Native conversation link retains canonical event and thread destination', (await native.locator('.event-row [data-thread-link]').getAttribute('href')).startsWith('/events/ticketmaster:CSS_0/discussions/css-question'));
    check('No browser runtime errors', report.errors.length === 0);
    if (out) fs.writeFileSync(path.join(out, 'event-context-space-checks.json'), JSON.stringify(report, null, 2));
    console.log(JSON.stringify(report, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(JSON.stringify({ ...report, error: error.stack }, null, 2)); process.exitCode = 1; });
