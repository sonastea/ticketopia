// One batched pass for phase-3 surfaces; screenshots remain ignored review evidence.
const path = require('node:path');
module.exports = async function review({page, name, check, report, out}) {
  if (process.env.DISCUSSION_SKIP_CAPTURES === 'true' || process.env.PARTICIPATION_SKIP_CAPTURES === 'true' || process.env.FOLLOWS_SKIP_CAPTURE) return;
  for (const [suffix,width,height,zoom] of [['desktop',1440,1000],['rail',1024,1000],['tablet',768,1000],['390',390,844],['320',320,640],['short',1440,480],['200-text',390,844,true]]) {
    const label=name+'-'+suffix;
    await page.setViewportSize({width,height});
    await page.evaluate(async zoom=>{document.documentElement.style.fontSize=zoom?'200%':'';await document.fonts.ready;scrollTo(0,0);},!!zoom);
    const overflow=await page.evaluate(()=>({width:innerWidth,scroll:document.documentElement.scrollWidth,nodes:[...document.querySelectorAll('main *')].filter(n=>n.getBoundingClientRect().right>innerWidth+1).map(n=>({tag:n.tagName,classes:n.className,width:n.getBoundingClientRect().width,text:n.textContent.slice(0,60)}))}));
    check(label+' reflow '+(overflow.scroll>overflow.width?JSON.stringify(overflow):''),overflow.scroll<=overflow.width);
    check(label+' action targets',await page.locator('main a:visible, main button:visible, main summary:visible, main select:visible').evaluateAll(nodes=>nodes.every(n=>n.getBoundingClientRect().height>=44)));
    const file=path.join(out,label+'.png');await page.screenshot({path:file,fullPage:true});report.screenshots.push(file);
    if(process.env.AXE_SCRIPT) {
      await page.addScriptTag({path:process.env.AXE_SCRIPT});
      const violations=await page.evaluate(async()=> (await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations);
      report.accessibility.push({name:label,violations});
      check(label+' axe '+JSON.stringify(violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))),!violations.length);
    }
  }
  await page.evaluate(()=>document.documentElement.style.fontSize='');
  await page.emulateMedia({forcedColors:'active',reducedMotion:'reduce'});
  const action=page.locator('main a:visible, main summary:visible, main button:visible').first();
  await action.focus();await page.keyboard.press('Tab');await page.keyboard.press('Shift+Tab');
  check(name+' forced-colors keyboard focus',await action.evaluate(n=>getComputedStyle(n).outlineStyle!=='none'));
  await page.emulateMedia({forcedColors:'none',reducedMotion:'no-preference'});
};
