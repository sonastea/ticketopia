// Read-only styling regressions against scripts/css-preview; no account writes.
const browsers = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.CSS_BASE_URL || 'http://127.0.0.1:18088';
const out = process.env.CSS_SCREENSHOT_DIR;
const report = { checks: [], screenshots: [], errors: [] };
const check = (name, result) => { assert.ok(result, name); report.checks.push(name); };

(async () => {
  const browserType = browsers[process.env.CSS_BROWSER || 'chromium'];
  const browser = await browserType.launch({ headless: true, executablePath: process.env.CSS_BROWSER_EXECUTABLE || process.env.CHROMIUM_EXECUTABLE });
  try {
    const context = await browser.newContext();
    const page = await context.newPage();
    page.on('pageerror', error => report.errors.push(error.message));
    // Isolated fixtures intentionally do not implement mutations.
    await page.addInitScript(() => document.addEventListener('submit', event => event.preventDefault()));
    const capture = async (name, width, route, enlarged = false) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto(base + route);
      await page.evaluate(async enlarged => {
        document.documentElement.style.fontSize = enlarged ? '200%' : '';
        await document.fonts.ready;
        scrollTo(0, 0);
      }, enlarged);
      check('No horizontal overflow: ' + name, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      check('44px action targets: ' + name, await page.locator('main button:visible, main [data-slot="button"]:visible').evaluateAll(nodes => nodes.every(node => node.getBoundingClientRect().height >= 44)));
      check('16px input floor: ' + name, await page.locator('main input:visible, main select:visible, main textarea:visible').evaluateAll(nodes => nodes.every(node => parseFloat(getComputedStyle(node).fontSize) >= 16)));
      if (out) {
        fs.mkdirSync(out, { recursive: true });
        const file = path.join(out, name + '.png');
        await page.screenshot({ path: file, fullPage: true }); report.screenshots.push(file);
      }
    };
    if (process.env.CSS_SKIP_LAYOUT !== 'true') {
      for (const [name, route] of [['discovery', '/'], ['preview', '/?selected=true'], ['event', '/events/ticketmaster:CSS_0'], ['profile', '/me'], ['sign-in', '/auth/sign-in'], ['composer', '/events/ticketmaster:CSS_0?section=community']]) {
        for (const width of [1440, 1024, 390, 320]) await capture(name + '-' + width, width, route);
        await capture(name + '-200-text', 390, route, true);
      }
    }
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(base + '/');
    check('Desktop sidebar/context thresholds unchanged', await page.locator('.sidebar').isVisible() && await page.locator('.event-context').isVisible() && !await page.locator('.bottom-nav').isVisible());
    await page.setViewportSize({ width: 1024, height: 900 });
    check('Intermediate rail/context thresholds unchanged', !await page.locator('.sidebar').isVisible() && await page.locator('.event-context').isVisible() && await page.locator('.bottom-nav').isVisible());
    await page.setViewportSize({ width: 390, height: 900 });
    check('Mobile navigation/document scrolling unchanged', !await page.locator('.sidebar').isVisible() && !await page.locator('.event-context').isVisible() && await page.locator('.bottom-nav').isVisible() && await page.locator('body').evaluate(node => getComputedStyle(node).overflowY === 'visible'));

    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(base + '/?selected=true');
    check('Image wrapper clips without scrolling', await page.locator('.event-image').first().evaluate(node => getComputedStyle(node).overflow === 'clip' && getComputedStyle(node).aspectRatio !== 'auto'));
    check('Preview remains sticky and scrollable', await page.locator('.event-context').evaluate(node => getComputedStyle(node).position === 'sticky' && getComputedStyle(node).overflowY === 'auto' && getComputedStyle(node).overscrollBehavior === 'contain'));
    check('Link text can be selected', await page.locator('.nav-link').first().evaluate(node => getComputedStyle(node).userSelect === 'text'));
    check('App stays light under OS dark preference', await (async () => { await page.emulateMedia({ colorScheme: 'dark' }); return page.locator('html').evaluate(node => getComputedStyle(node).colorScheme === 'light'); })());
    check('Base, derived, and selected palette text retains AA contrast', await page.evaluate(() => {
      const canvas = document.createElement('canvas'); canvas.width = canvas.height = 1;
      const ctx = canvas.getContext('2d', { willReadFrequently: true });
      const luminance = color => {
        ctx.fillStyle = color; ctx.fillRect(0, 0, 1, 1);
        return [...ctx.getImageData(0, 0, 1, 1).data].slice(0, 3).map(v => v / 255).map(v => v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4).reduce((sum, v, i) => sum + v * [.2126, .7152, .0722][i], 0);
      };
      const probe = document.createElement('span'); document.body.append(probe);
      const pairs = [['ink', 'paper'], ['muted', 'paper'], ['muted', 'brand-soft'], ['brand', 'brand-soft'], ['ink', 'date-surface'], ['surface', 'brand'], ['surface', 'brand-dark'], ['surface', 'brand-active'], ['error', 'error-bg'], ['warning', 'warning-bg'], ['surface', 'google-brand'], ['surface', 'google-hover'], ['surface', 'google-active']];
      const passes = pairs.every(([text, fill]) => {
        probe.style.color = `var(--${text})`; probe.style.backgroundColor = `var(--${fill})`;
        const css = getComputedStyle(probe), values = [luminance(css.color), luminance(css.backgroundColor)].sort((a, b) => b - a);
        return (values[0] + .05) / (values[1] + .05) >= 4.5;
      });
      probe.remove(); return passes;
    }));
    check('Library dark tokens resolve within a color-scheme subtree', await page.evaluate(() => {
      const slot = document.createElement('div'); slot.style.backgroundColor = 'var(--card)'; document.body.append(slot);
      const light = getComputedStyle(slot).backgroundColor; slot.className = 'dark';
      const changed = getComputedStyle(slot).backgroundColor !== light && getComputedStyle(slot).colorScheme === 'dark';
      slot.remove(); return changed;
    }));
    const hoveredRules = await page.evaluate(() => {
      const failures = [];
      const walk = (rules, media = '') => {
        for (const rule of rules) {
          const conditions = media + ' ' + (rule.conditionText || '');
          if (rule.selectorText?.includes(':hover') && !(/hover:\s*hover/.test(conditions) && /pointer:\s*fine/.test(conditions))) failures.push(rule.selectorText);
          if (rule.cssRules) walk(rule.cssRules, conditions);
        }
      };
      for (const sheet of document.styleSheets) walk(sheet.cssRules);
      return failures;
    });
    check('All compiled hover rules require a fine, hover-capable pointer: ' + hoveredRules.join(', '), hoveredRules.length === 0);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.locator('.search-bar button').hover();
    const hoverFill = await page.locator('.search-bar button').evaluate(node => getComputedStyle(node).backgroundColor);
    await page.mouse.move(0, 0);
    check('Mouse hover changes primary fill', hoverFill !== await page.locator('.search-bar button').evaluate(node => getComputedStyle(node).backgroundColor));
    check('Reduced motion disables geometry transitions', await page.locator('.app-shell').evaluate(node => getComputedStyle(node).transitionDuration === '0s'));
    await page.locator('.sidebar-toggle').click();
    // Focus-visible follows the last input modality, not programmatic focus alone.
    await page.keyboard.press('Tab');
    await page.locator('.sidebar .nav-link').first().focus();
    check('Collapsed tooltip remains available to keyboard focus', await page.locator('.sidebar .nav-link').first().evaluate(node => getComputedStyle(node, '::after').display === 'block'));
    await page.emulateMedia({ forcedColors: 'active' });
    check('Forced-colors focus retains a real outline', await page.locator('.sidebar .nav-link').first().evaluate(node => { const css = getComputedStyle(node); return css.outlineStyle === 'solid' && parseFloat(css.outlineWidth) >= 2; }));
    await page.emulateMedia({ forcedColors: 'none', reducedMotion: 'no-preference', colorScheme: 'light' });

    await page.goto(base + '/me');
    await page.locator('textarea').fill(Array(40).fill('Useful practical event advice.').join('\n'));
    check('Textarea remains bounded and scrolls long input', await page.locator('textarea').evaluate(node => { const css = getComputedStyle(node); return node.getBoundingClientRect().height <= parseFloat(css.maxBlockSize) + 1 && node.scrollHeight > node.clientHeight && css.resize === 'vertical'; }));
    await page.evaluate(() => { document.documentElement.dir = 'rtl'; });
    check('Logical sidebar moves to inline start in RTL', await page.locator('.sidebar').evaluate(node => node.getBoundingClientRect().left > innerWidth / 2));
    check('Long profile remains shrink-safe in RTL', await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));

    const touch = await browser.newContext({ viewport: { width: 390, height: 900 }, isMobile: true, hasTouch: true });
    const touchPage = await touch.newPage();
    await touchPage.emulateMedia({ reducedMotion: 'reduce' });
    await touchPage.goto(base + '/events/ticketmaster:CSS_0');
    const secondary = touchPage.locator('.button-quiet:not(:disabled)').first();
    const rest = await secondary.evaluate(node => getComputedStyle(node).color);
    await secondary.hover();
    check('Coarse-pointer hover does not stick', await secondary.evaluate((node, rest) => !matchMedia('(hover: hover) and (pointer: fine)').matches && getComputedStyle(node).color === rest, rest));
    // Playwright's mouse down still exposes :active on a coarse-pointer context.
    const box = await secondary.boundingBox();
    await touchPage.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await touchPage.mouse.down();
    check('Touch press feedback needs no hover or movement', await secondary.evaluate((node, rest) => { const css = getComputedStyle(node); return css.color !== rest && css.translate === 'none' && css.transform === 'none'; }, rest));
    await touchPage.mouse.move(0, 0); await touchPage.mouse.up();
    await touch.close();

    const native = await browser.newContext({ javaScriptEnabled: false, viewport: { width: 320, height: 900 } });
    const nativePage = await native.newPage();
    await nativePage.goto(base + '/');
    await nativePage.locator('#filters summary').click();
    check('Native filters work without JavaScript', await nativePage.locator('#city').isVisible());
    check('Open native filter fields retain the 16px floor', await nativePage.locator('input:visible, select:visible').evaluateAll(nodes => nodes.every(node => parseFloat(getComputedStyle(node).fontSize) >= 16)));
    await native.close();
    check('No browser runtime errors', report.errors.length === 0);
    console.log(JSON.stringify(report, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(JSON.stringify({ ...report, error: error.stack }, null, 2)); process.exitCode = 1; });
