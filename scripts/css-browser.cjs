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
    const capture = async (name, width, route, enlarged = false, height = 900) => {
      await page.setViewportSize({ width, height });
      await page.goto(base + route);
      await page.evaluate(async enlarged => {
        document.documentElement.style.fontSize = enlarged ? '200%' : '';
        await document.fonts.ready;
        scrollTo(0, 0);
      }, enlarged);
      check('No horizontal overflow: ' + name, await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth));
      if (await page.locator('.bottom-nav').isVisible()) {
        check('Navigation labels stay inside 44px targets: ' + name, await page.locator('.bottom-nav .nav-link').evaluateAll(nodes => nodes.every(node => {
          const control = node.getBoundingClientRect(), label = node.querySelector('.nav-label').getBoundingClientRect();
          // Single-word labels stay on one line rather than breaking mid-word.
          const words = document.createRange(); words.selectNodeContents(node.querySelector('.nav-label'));
          return words.getClientRects().length === 1 && control.width >= 44 && control.height >= 44 && control.top >= 0 && control.bottom <= innerHeight + 1 && label.left >= control.left - 1 && label.right <= control.right + 1 && label.top >= control.top && label.bottom <= control.bottom && node.scrollWidth <= node.clientWidth + 1;
        })));
        check('Compact navigation reserves its actual height: ' + name, await page.evaluate(() => {
          const nav = document.querySelector('.bottom-nav');
          if (getComputedStyle(nav).position !== 'sticky') return true; // The intermediate rail is fixed.
          scrollTo(0, document.documentElement.scrollHeight);
          const clear = document.querySelector('.app-workspace').getBoundingClientRect().bottom <= nav.getBoundingClientRect().top + 1;
          scrollTo(0, 0);
          return clear;
        }));
      }
      check('44px action targets: ' + name, await page.locator('main button:visible, main [data-slot="button"]:visible').evaluateAll(nodes => nodes.every(node => node.getBoundingClientRect().height >= 44)));
      check('Event action labels fit their controls: ' + name, await page.locator('.event-actions [data-slot="button"]:visible').evaluateAll(nodes => nodes.every(node => node.scrollWidth <= node.clientWidth + 1 && node.scrollHeight <= node.clientHeight + 1)));
      if (await page.locator('.reminder-button:visible').count()) {
        check('Reminder matches the button group, without an underline: ' + name, await page.locator('.reminder-button:visible').evaluateAll(nodes => nodes.every(node => {
          const css = getComputedStyle(node);
          return node.matches('a[href]') && node.querySelector('svg[aria-hidden="true"]') && css.borderStyle === 'solid' && parseFloat(css.borderWidth) >= 1 && !css.textDecorationLine.includes('underline');
        })));
      }
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
        await capture(name + '-200-text-320', 320, route, true);
      }
      await capture('profile-short', 390, '/me', false, 480);
      await capture('profile-short-200-text', 320, '/me', true, 480);
    }
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto(base + '/');
    check('Desktop sidebar/context thresholds unchanged', await page.locator('.sidebar').isVisible() && await page.locator('.event-context').isVisible() && !await page.locator('.bottom-nav').isVisible());
    await page.setViewportSize({ width: 1024, height: 900 });
    check('Intermediate rail/context thresholds unchanged', !await page.locator('.sidebar').isVisible() && await page.locator('.event-context').isVisible() && await page.locator('.bottom-nav').isVisible());
    await page.setViewportSize({ width: 390, height: 900 });
    check('Mobile navigation/document scrolling unchanged', !await page.locator('.sidebar').isVisible() && !await page.locator('.event-context').isVisible() && await page.locator('.bottom-nav').isVisible() && await page.locator('body').evaluate(node => getComputedStyle(node).overflowY === 'visible'));
    check('Phone navigation floats inset from the edges with a bordered current pill', await page.locator('.bottom-nav').evaluate(nav => {
      const rect = nav.getBoundingClientRect(), current = nav.querySelector('[aria-current="page"]');
      const css = getComputedStyle(nav), pill = getComputedStyle(current);
      return rect.left >= 8 && rect.right <= innerWidth - 8 && rect.bottom <= innerHeight - 8 && parseFloat(css.borderTopLeftRadius) >= 24 && css.fontSize === '12px' && pill.borderTopColor !== getComputedStyle(nav.querySelector('.nav-link:not([aria-current="page"])')).borderTopColor;
    }));
    // The wordmark steps once at the stick threshold; scroll anchoring must not
    // pull the page back above it, which would unstick and regrow the header.
    for (const enlarged of [false, true]) check('Compact header sticks, wordmark steps to the headline size' + (enlarged ? ' with 200% text' : ''), await page.evaluate(async enlarged => {
      document.documentElement.style.fontSize = enlarged ? '200%' : '';
      const header = document.querySelector('.compact-header'), brand = header.querySelector('.brand');
      const frames = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      const size = () => parseFloat(getComputedStyle(brand).fontSize);
      try {
        if (!CSS.supports('container-type: scroll-state')) return getComputedStyle(header).position === 'static';
        scrollTo(0, 0); await frames();
        const before = size(), rem = parseFloat(getComputedStyle(document.documentElement).fontSize);
        scrollTo(0, rem / 2); await frames();
        const unstuck = size();
        scrollTo(0, rem + 2); await frames(); await frames(); await frames();
        const stuck = header.getBoundingClientRect(), stepped = size(), held = Math.abs(scrollY - (rem + 2)) < 1;
        scrollTo(0, 400); await frames();
        return getComputedStyle(header).position === 'sticky' && unstuck === before && stepped === 1.25 * rem && size() === stepped && held && Math.round(stuck.top) === -rem;
      } finally {
        scrollTo(0, 0); document.documentElement.style.fontSize = '';
      }
    }, enlarged));
    await page.emulateMedia({ reducedMotion: 'reduce' });
    check('Reduced motion keeps the compact header static', await page.locator('.compact-header').evaluate(node => getComputedStyle(node).position === 'static'));
    await page.emulateMedia({ reducedMotion: 'no-preference' });

    await page.goto(base + '/?more=true');
    check('Pagination skeleton stays hidden until a load starts', await page.locator('.pagination-skeleton').evaluate(node => getComputedStyle(node).display === 'none' && node.getAttribute('aria-hidden') === 'true'));
    await page.locator('#pagination').evaluate(node => node.classList.add('htmx-request'));
    check('Pagination loads show shimmering placeholder rows', await page.locator('.pagination-skeleton').evaluate(node => {
      const block = node.querySelector('[data-slot="skeleton"]');
      return getComputedStyle(node).display === 'grid' && node.querySelectorAll('.skeleton-row').length === 3 && block.getBoundingClientRect().height > 0 && getComputedStyle(block, '::after').animationName === 'skeleton-shimmer';
    }));
    await page.emulateMedia({ reducedMotion: 'reduce' });
    check('Reduced motion keeps pagination placeholders still', await page.locator('.pagination-skeleton [data-slot="skeleton"]').first().evaluate(node => getComputedStyle(node).animationName === 'none' && getComputedStyle(node, '::after').animationName === 'none'));
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    if (out) {
      await page.locator('#pagination').scrollIntoViewIfNeeded();
      const file = path.join(out, 'pagination-loading-390.png');
      await page.screenshot({ path: file }); report.screenshots.push(file);
    }

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
      const pairs = [['ink', 'paper'], ['muted', 'paper'], ['muted', 'brand-soft'], ['muted-warm', 'surface'], ['muted-warm', 'paper'], ['muted-warm', 'brand-soft'], ['muted-warm', 'nav-surface'], ['brand', 'brand-soft'], ['ink', 'date-surface'], ['surface', 'brand'], ['surface', 'brand-dark'], ['surface', 'brand-active'], ['error', 'error-bg'], ['warning', 'warning-bg'], ['surface', 'google-brand'], ['surface', 'google-hover'], ['surface', 'google-active']];
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
    const save = page.locator('.event-actions .save-button').first();
    const interest = page.locator('.event-actions .interest-button').first();
    const actionStyle = node => {
      const css = getComputedStyle(node);
      return { fill: css.backgroundColor, color: css.color, border: css.borderColor };
    };
    await save.evaluate(node => node.dataset.saved = 'false');
    const unselected = await save.evaluate(actionStyle);
    check('Save, Interested, and reminder share unselected styling', JSON.stringify(unselected) === JSON.stringify(await interest.evaluate(actionStyle)) && JSON.stringify(unselected) === JSON.stringify(await page.locator('.reminder-button').first().evaluate(actionStyle)));
    await save.evaluate(node => node.dataset.saved = 'true');
    await interest.evaluate(node => node.setAttribute('aria-pressed', 'true'));
    const selected = await save.evaluate(actionStyle);
    check('Save and Interested share distinct selected styling', selected.fill !== unselected.fill && selected.border !== unselected.border && JSON.stringify(selected) === JSON.stringify(await interest.evaluate(actionStyle)));
    await save.hover();
    const selectedHover = await save.evaluate(actionStyle);
    check('Selected hover remains distinct from rest', selectedHover.fill !== selected.fill);
    await page.mouse.down();
    check('Selected press remains distinct from hover', (await save.evaluate(actionStyle)).fill !== selectedHover.fill);
    await page.mouse.move(0, 0); await page.mouse.up();
    await save.evaluate(node => node.disabled = true);
    check('Unavailable action retains muted styling', (await save.evaluate(actionStyle)).color !== selected.color);
    await save.evaluate(node => node.closest('form').setAttribute('aria-busy', 'true'));
    check('Pending action retains its plum state', JSON.stringify(await save.evaluate(actionStyle)) === JSON.stringify(selected));
    await save.evaluate(node => { node.disabled = false; node.closest('form').removeAttribute('aria-busy'); });
    const actionBoxes = await page.locator('.event-actions').first().evaluate(node => [...node.children].map(child => child.getBoundingClientRect().top));
    await page.locator('[data-save-feedback]').first().evaluate(node => { node.hidden = false; node.textContent = 'Saved. Return to this event whenever you need its details.'; });
    check('Feedback does not move the controls', JSON.stringify(actionBoxes) === JSON.stringify(await page.locator('.event-actions').first().evaluate(node => [...node.children].map(child => child.getBoundingClientRect().top))));
    check('Reduced motion disables geometry transitions', await page.locator('.app-shell').evaluate(node => getComputedStyle(node).transitionDuration === '0s'));
    await page.locator('.sidebar-toggle').click();
    // Focus-visible follows the last input modality, not programmatic focus alone.
    await page.keyboard.press('Tab');
    await page.locator('.sidebar .nav-link').first().focus();
    check('Collapsed tooltip remains available to keyboard focus', await page.locator('.sidebar .nav-link').first().evaluate(node => getComputedStyle(node, '::after').display === 'block'));
    await page.emulateMedia({ forcedColors: 'active' });
    check('Forced-colors focus retains a real outline', await page.locator('.sidebar .nav-link').first().evaluate(node => { const css = getComputedStyle(node); return css.outlineStyle === 'solid' && parseFloat(css.outlineWidth) >= 2; }));
    await save.focus();
    check('Event action focus remains visible in forced colors', await save.evaluate(node => { const css = getComputedStyle(node); return css.outlineStyle === 'solid' && parseFloat(css.outlineWidth) >= 2; }));
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
    await nativePage.evaluate(async () => { document.documentElement.style.fontSize = '200%'; await document.fonts.ready; });
    check('Native enlarged navigation keeps complete labels', await nativePage.locator('.bottom-nav .nav-label').evaluateAll(nodes => nodes.every(node => node.scrollWidth <= node.clientWidth + 1)));
    await nativePage.locator('.bottom-nav').getByRole('link', { name: 'Profile', exact: true }).click();
    await nativePage.waitForURL(base + '/me');
    check('Native compact navigation reaches its destination', (await nativePage.locator('.bottom-nav a[aria-current="page"]').textContent()).trim() === 'Profile');
    await nativePage.emulateMedia({ forcedColors: 'active', reducedMotion: 'reduce' });
    await nativePage.keyboard.press('Tab');
    const compactLink = nativePage.locator('.bottom-nav').getByRole('link', { name: 'Saved', exact: true });
    await compactLink.focus();
    check('Compact keyboard focus remains visible in forced colors', await compactLink.evaluate(node => {
      const css = getComputedStyle(node);
      return node.matches(':focus-visible') && css.outlineStyle === 'solid' && parseFloat(css.outlineWidth) >= 2;
    }));
    await native.close();
    check('No browser runtime errors', report.errors.length === 0);
    console.log(JSON.stringify(report, null, 2));
  } finally { await browser.close(); }
})().catch(error => { console.error(JSON.stringify({ ...report, error: error.stack }, null, 2)); process.exitCode = 1; });
