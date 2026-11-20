// Shared axe pass for browser journeys. It waits for time-based animations to
// land first. A target covered by the fixed bottom
// navigation or sticky header at the scan's scroll position is a capture
// artifact, not a layout fault: such a target is scrolled clear and checked
// again, and only one that then passes is dropped; everything else is kept.
module.exports = async function scan(page, axeScript) {
  await page.addScriptTag({path: axeScript});
  return page.evaluate(async () => {
    // Fades and slides would be measured mid-way (contrast of half-faded text);
    // wait for time-based ones. Scroll-driven ones follow scrolling, not time.
    const landing = document.getAnimations().filter(a => a.timeline === document.timeline && a.effect?.getComputedTiming().iterations !== Infinity);
    await Promise.race([Promise.allSettled(landing.map(a => a.finished)), new Promise(resolve => setTimeout(resolve, 2000))]);
    const underChrome = el => {
      if (!el) return false;
      const box = el.getBoundingClientRect();
      return [box.top + 2, box.top + box.height / 2, box.bottom - 2].some(y => {
        const top = document.elementFromPoint(box.left + box.width / 2, y);
        if (!top || el.contains(top)) return false;
        for (let node = top; node; node = node.parentElement) {
          if (['fixed', 'sticky'].includes(getComputedStyle(node).position)) return !node.contains(el);
        }
        return false;
      });
    };
    // elementRef: axe's selectors can match a sibling, so re-checks use the node itself.
    const found = (await axe.run(document, {elementRef: true, runOnly: {type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']}})).violations;
    // A covered target is moved clear of the chrome and checked again on its
    // own; only a target that then passes is dropped.
    const frame = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
    const at = scrollY;
    for (const v of found) {
      if (v.id !== 'target-size') continue;
      const kept = [];
      for (const n of v.nodes) {
        const el = n.element;
        // Each finding was measured at the scan's position; judge it from there.
        if (scrollY !== at) { scrollTo(0, at); await frame(); }
        if (!/obscured/.test(n.failureSummary) || !underChrome(el)) { kept.push(n); continue; }
        el.scrollIntoView({block: 'center'}); await frame();
        const again = underChrome(el) || (await axe.run(el, {runOnly: {type: 'rule', values: ['target-size']}})).violations.length > 0;
        if (again) kept.push(n);
      }
      v.nodes = kept;
    }
    scrollTo(0, at); await frame();
    for (const v of found) for (const n of v.nodes) delete n.element;
    return found.filter(v => v.nodes.length);
  });
};
