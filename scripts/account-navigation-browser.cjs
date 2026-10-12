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
    page.on('pageerror', e=>report.errors.push(e.message));
    await page.goto(base+'/me');
    await page.getByLabel('Display name', {exact:true}).fill('Local explorer with a long public display name');
    await page.getByLabel(/^Bio /).fill('LongProfileWord'.repeat(30));
    await page.getByRole('button',{name:'Save profile & privacy',exact:true}).click();
    check('Profile saved',await page.getByText('Your profile and privacy settings were saved.',{exact:true}).isVisible());
    const nav = page.getByRole('navigation',{name:'Your space',exact:true});
    check('Current account links direct',await nav.getByRole('link',{name:'Preferences',exact:true}).isVisible());
    check('Secondary families disclosed',!await nav.getByRole('link',{name:'Artists & venues',exact:true}).isVisible());
    await nav.locator('summary').click();
    check('All existing families reachable',await nav.getByRole('link',{name:'Artists & venues',exact:true}).isVisible() && await nav.getByRole('link',{name:'Reply notifications',exact:true}).isVisible());
    check('Unavailable safety not advertised',await nav.getByRole('link',{name:'Your reports',exact:true}).count()===0);
    const capture = async(name,width,height=1000,zoom=false)=>{
      await page.setViewportSize({width,height});
      await page.evaluate(async zoom=>{document.documentElement.style.fontSize=zoom?'200%':'';await document.fonts.ready;scrollTo(0,0);},zoom);
      check(name+' reflow',await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      check(name+' navigation targets',await nav.locator('a, summary').evaluateAll(nodes=>nodes.filter(n=>n.getClientRects().length).every(n=>n.getBoundingClientRect().height>=44)));
      if(name==='account-desktop' || name==='account-390' || zoom) {
        const sideBySide=await page.locator('.account-profile-layout').evaluate(node=>{const [form,preview]=node.children;return Math.abs(form.getBoundingClientRect().top-preview.getBoundingClientRect().top)<2;});
        check(name+' preview arrangement',sideBySide===(name==='account-desktop'));
      }
      const file=path.join(out,name+'.png');await page.screenshot({path:file,fullPage:true});report.screenshots.push(file);
      if(process.env.AXE_SCRIPT){await page.addScriptTag({path:process.env.AXE_SCRIPT});const scan=await page.evaluate(()=>axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}}));report.accessibility.push({name,violations:scan.violations});check(name+' axe '+JSON.stringify(scan.violations.map(v=>({id:v.id,nodes:v.nodes.map(n=>n.target)}))),!scan.violations.length);}
    };
    for(const [name,width,height,zoom] of [['account-desktop',1440],['account-rail',1024],['account-tablet',768],['account-390',390],['account-320',320],['account-short',1440,480],['account-200-text',390,1000,true]]) await capture(name,width,height,zoom);
    await page.evaluate(()=>document.documentElement.style.fontSize='');
    await page.emulateMedia({forcedColors:'active',reducedMotion:'reduce'});
    await nav.locator('summary').focus();
    await page.keyboard.press('Tab');
    await page.keyboard.press('Shift+Tab');
    check('Forced colors keyboard disclosure focus',await nav.locator('summary').evaluate(n=>getComputedStyle(n).outlineStyle!=='none'));
    await page.keyboard.press('Enter');
    check('Keyboard closes native disclosure',!await nav.getByRole('link',{name:'Artists & venues',exact:true}).isVisible());
    await page.emulateMedia({forcedColors:'none',reducedMotion:'no-preference'});
    await nav.getByRole('link',{name:'Preferences',exact:true}).click();
    check('Preferences current visible',await nav.locator('a[aria-current="page"]').textContent()==='Preferences');
    await capture('account-preferences-390',390);
    const native = await browser.newContext({javaScriptEnabled:false,hasTouch:true,viewport:{width:320,height:640}});
    await native.addCookies([{name:'ticketopia_session',value:process.env.ACCOUNT_FIXTURE_TOKEN,url:base}]);
    const np=await native.newPage();await np.goto(base+'/me');
    await np.locator('.account-more summary').tap();
    await np.locator('.account-navigation').getByRole('link',{name:'Artists & venues',exact:true}).tap();
    check('No-JS touch reaches events family',await np.locator('.account-navigation a[aria-current="page"]').textContent()==='Artists & venues');
    check('No browser errors',!report.errors.length);
    fs.writeFileSync(path.join(out,'account-navigation-checks.json'),JSON.stringify(report,null,2));
    console.log(JSON.stringify({checks:report.checks.length,accessibility:report.accessibility.length,screenshots:report.screenshots}));
  } finally {await browser.close();}
})().catch(e=>{console.error(e);process.exit(1);});
