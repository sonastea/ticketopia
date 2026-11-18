// Isolated account fixture; native navigation and account forms, no live OAuth.
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.ACCOUNT_BASE_URL;
const out = path.resolve(__dirname, '../.impeccable/review');
const report = {checks: [], accessibility: [], screenshots: [], errors: []};
const check = (name, value) => {assert.ok(value, name); report.checks.push(name);};
fs.mkdirSync(out, {recursive:true});
(async () => {
  const browser = await chromium.launch({headless:true, executablePath:process.env.CHROMIUM_EXECUTABLE});
  try {
    const context = await browser.newContext();
    await context.addCookies([{name:'ticketopia_session',value:process.env.ACCOUNT_FIXTURE_TOKEN,url:base}]);
    const page = await context.newPage();
    // emulateMedia flips the reduced-motion query that opts into cross-document
    // view transitions; Chromium then aborts an in-flight one with this error.
    // People don't change that setting mid-navigation, so it isn't an app fault.
    page.on('pageerror', e=>{ if(!/ViewTransition opt-in disabled/.test(e.message)) report.errors.push(e.message); });
    await page.goto(base+'/me');
    await page.getByLabel('Display name', {exact:true}).fill('Local explorer with a long public display name');
    await page.getByLabel(/^Bio /).fill('LongProfileWord'.repeat(30));
    await page.getByRole('button',{name:'Save profile',exact:true}).click();
    check('Profile saved',await page.getByText('Your profile was saved.',{exact:true}).isVisible());
    const nav = page.getByRole('navigation',{name:'Your space',exact:true});
    for (const name of ['Profile','Privacy','Preferences','Security & Apps']) check('Account tab '+name,await nav.getByRole('link',{name,exact:true}).isVisible());
    check('Other families are one link each',await nav.getByRole('link',{name:'Your events',exact:true}).isVisible() && await nav.getByRole('link',{name:'Updates',exact:true}).isVisible() && await nav.getByRole('link',{name:'Artists & venues',exact:true}).count()===0);
    check('Unavailable safety not advertised',await nav.getByRole('link',{name:'Safety',exact:true}).count()===0);
    const capture = async(name,width,height=1000,zoom=false,center='')=>{
      await page.setViewportSize({width,height});
      await page.evaluate(async zoom=>{document.documentElement.style.fontSize=zoom?'200%':'';await document.fonts.ready;scrollTo(0,0);},zoom);
      check(name+' reflow',await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      check(name+' navigation targets',await nav.locator('a').evaluateAll(nodes=>nodes.filter(n=>n.getClientRects().length).every(n=>n.getBoundingClientRect().height>=44)));
      if(name==='account-desktop' || name==='account-390' || zoom) {
        const sideBySide=await page.locator('.profile-layout').evaluate(node=>{const [overview,form]=node.children;return Math.abs(overview.getBoundingClientRect().top-form.getBoundingClientRect().top)<2;});
        check(name+' preview arrangement',sideBySide===(name==='account-desktop'));
      }
      const file=path.join(out,name+'.png');await page.screenshot({path:file,fullPage:true});report.screenshots.push(file);
      // A target under the floating phone navigation at this scroll position is
      // scanned where a person would scroll it, not where the capture left it.
      if(center) await page.locator(center).evaluate(n=>n.scrollIntoView({block:'center'}));
      if(process.env.AXE_SCRIPT){await page.addScriptTag({path:process.env.AXE_SCRIPT});const scan=await page.evaluate(()=>axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}}));report.accessibility.push({name,violations:scan.violations});check(name+' axe '+JSON.stringify(scan.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))),!scan.violations.length);}
    };
    for(const [name,width,height,zoom] of [['account-desktop',1440],['account-rail',1024],['account-tablet',768],['account-390',390],['account-320',320],['account-short',1440,480],['account-200-text',390,1000,true]]) await capture(name,width,height,zoom);
    await page.evaluate(()=>document.documentElement.style.fontSize='');
    await page.emulateMedia({forcedColors:'active',reducedMotion:'reduce'});
    await nav.getByRole('link',{name:'Your events',exact:true}).focus();
    await page.keyboard.press('Shift+Tab');
    await page.keyboard.press('Tab');
    check('Forced colors keyboard family link focus',await nav.getByRole('link',{name:'Your events',exact:true}).evaluate(n=>getComputedStyle(n).outlineStyle!=='none'));
    await page.emulateMedia({forcedColors:'none',reducedMotion:'no-preference'});
    await nav.getByRole('link',{name:'Preferences',exact:true}).click();
    check('Preferences current visible',await nav.locator('a[aria-current="page"]').textContent()==='Preferences');
    await capture('account-preferences-390',390);
    await nav.getByRole('link',{name:'Privacy',exact:true}).click();
    check('Privacy current visible',await nav.locator('a[aria-current="page"]').textContent()==='Privacy');
    await capture('account-privacy-desktop',1440);
    await capture('account-privacy-390',390);
    await nav.getByRole('link',{name:'Security & Apps',exact:true}).click();
    check('Sign out of all devices starts hidden',!await page.getByRole('button',{name:'Yes, sign out of all devices',exact:true}).isVisible());
    await page.locator('.account-confirm > summary').click();
    await page.getByRole('button',{name:'Yes, sign out of all devices',exact:true}).waitFor({state:'visible'});
    check('Sign out of all devices asks first',true);
    await capture('account-security-desktop',1440);
    await capture('account-security-390',390,1000,false,'#api-access > details > summary');
    const native = await browser.newContext({javaScriptEnabled:false,hasTouch:true,viewport:{width:320,height:640}});
    await native.addCookies([{name:'ticketopia_session',value:process.env.ACCOUNT_FIXTURE_TOKEN,url:base}]);
    const np=await native.newPage();await np.goto(base+'/me');
    await np.locator('.account-navigation').getByRole('link',{name:'Your events',exact:true}).tap();
    await np.locator('.account-navigation').getByRole('link',{name:'Artists & venues',exact:true}).tap();
    check('No-JS touch reaches events family',await np.locator('.account-navigation a[aria-current="page"]').textContent()==='Artists & venues');
    check('No browser errors '+JSON.stringify(report.errors),!report.errors.length);
    fs.writeFileSync(path.join(out,'account-navigation-checks.json'),JSON.stringify(report,null,2));
    console.log(JSON.stringify({checks:report.checks.length,accessibility:report.accessibility.length,screenshots:report.screenshots}));
  } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
