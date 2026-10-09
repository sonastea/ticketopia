// Synthetic isolated MariaDB fixture; real cookie authentication and native forms.
const {chromium} = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const base = process.env.SAVED_BASE_URL;
const out = path.resolve(__dirname, '../.impeccable/review');
const report = {checks: [], screenshots: [], accessibility: [], errors: []};
const check = (name, value) => {assert.ok(value, name); report.checks.push(name);};
fs.mkdirSync(out, {recursive: true});
(async () => {
  const browser = await chromium.launch({headless: true, executablePath: process.env.CHROMIUM_EXECUTABLE});
  try {
    const owner = await browser.newContext({viewport: {width: 1440, height: 1000}});
    const other = await browser.newContext();
    const guest = await browser.newContext();
    for (const [context, value] of [[owner,process.env.SAVED_FIXTURE_TOKEN],[other,process.env.SAVED_OTHER_TOKEN]]) {
      await context.addCookies([{name:'ticketopia_session',value,url:base}]);
    }
    const me = await (await owner.request.get(base+'/api/v1/me')).json();
    const them = await (await other.request.get(base+'/api/v1/me')).json();
    const write = (context,method,endpoint,data,csrf,key) => context.request[method](base+endpoint, {data,headers:{'X-CSRF-Token':csrf,...(key?{'Idempotency-Key':key}:{})}});
    check('Explicit account-wide resume', (await write(owner,'patch','/api/v1/me/preferences',{notifications_paused:false},me.csrf_token)).status()===200);
    const endpoint='/api/v1/events/ticketmaster:Browser_0/discussions';
    const rootResponse=await write(other,'post',endpoint,{body:'Where should we meet before the show? Practical advice for people arriving by public transport.'},them.csrf_token,'follow-browser-root-0001');
    check('Publish synthetic thread',rootResponse.status()===201);
    const root=await rootResponse.json();
    const thread='/events/'+root.event_id+'/discussions/'+root.id;
    const page=await owner.newPage();
    page.on('pageerror',e=>report.errors.push(e.message));
    await page.goto(base+thread);
    await page.getByText('Follow this conversation',{exact:true}).click();
    await page.getByLabel('Reply notification frequency').selectOption('immediate');
    await page.getByRole('button',{name:'Follow conversation',exact:true}).click();
    await page.waitForURL(base+thread);
    check('Native follow confirmed on thread',(await page.locator('summary').allTextContents()).some(t=>t.includes('Following · immediate')));
    await page.goto(base+'/me/discussions');
    check('Private collection retains event context',await page.getByRole('heading',{name:'A good night in the city'}).count()===1);
    check('Guest cannot read private follows',(await guest.request.get(base+'/api/v1/me/discussions')).status()===401);
    // More than one reply page verifies selected contribution links.
    let latest;
    for(let i=0;i<23;i++) {
      const response=await write(i<5?owner:other,'post',endpoint+'/'+root.id+'/replies',{body:'Synthetic reply '+i+': meet near the station entrance.'},i<5?me.csrf_token:them.csrf_token,'follow-browser-reply-'+String(i).padStart(4,'0'));
      check('Reply publication '+i,response.status()===201);latest=await response.json();
    }
    await page.goto(base+'/me/notifications?limit=1');
    check('Reply notification avoids excerpts',!(await page.locator('main').innerText()).includes('station entrance'));
    check('Notification pagination',await page.getByRole('link',{name:'More notifications'}).count()===1);
    const selectedURL=await page.getByRole('link',{name:'Open conversation at reply'}).getAttribute('href');
    check('Correct event/thread and reply target',selectedURL.includes('/'+root.id+'?post_id='+latest.id+'#post-'+latest.id));
    await page.getByRole('link',{name:'Open conversation at reply'}).click();
    check('Off-page reply selected exactly once',await page.locator('#post-'+latest.id).count()===1 && await page.getByRole('heading',{name:'Selected contribution',exact:true}).count()===1);
    await page.goto(base+'/me/notifications?limit=1');
    await page.getByRole('button',{name:'Mark read',exact:true}).click();
    check('Native read confirmation',(await page.locator('main').innerText()).includes('Read') && !await page.getByRole('button',{name:'Mark read'}).count());
    const capture=async(name,width,zoom=false)=>{
      await page.setViewportSize({width,height:1000});
      await page.evaluate(async zoom=>{document.documentElement.style.fontSize=zoom?'200%':'';await document.fonts.ready;scrollTo(0,0);},zoom);
      check('Reflow '+name,await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      const file=path.join(out,name+'.png');await page.screenshot({path:file,fullPage:true});report.screenshots.push(file);
      if(process.env.AXE_SCRIPT){await page.addScriptTag({path:process.env.AXE_SCRIPT});const violations=await page.evaluate(async()=> (await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations);report.accessibility.push({name,violations});check('Accessibility '+name,!violations.length);}
    };
    await page.goto(base+'/me/notifications');
    await capture('discussion-notifications-desktop',1440);
    await capture('discussion-notifications-mobile',390);
    await page.goto(base+'/me/discussions');
    await page.locator('.recommendation-editor summary').click();
    const frequency=page.getByLabel('Reply notification frequency');
    check('Frequency control has a 44px target',(await frequency.boundingBox()).height>=44);
    await frequency.focus();
    check('Keyboard focus is outlined',await frequency.evaluate(el=>{const s=getComputedStyle(el);return s.outlineStyle!=='none' && parseFloat(s.outlineWidth)>0;}));
    await page.emulateMedia({forcedColors:'active',reducedMotion:'reduce'});
    check('Forced-colors focus remains outlined',await frequency.evaluate(el=>{const s=getComputedStyle(el);return s.outlineStyle!=='none' && parseFloat(s.outlineWidth)>0;}));
    await page.emulateMedia({forcedColors:'none',reducedMotion:'no-preference'});
    await capture('discussion-follows-desktop',1440);
    await capture('discussion-follows-mobile',390);
    await capture('discussion-follows-small',320);
    await capture('discussion-follows-text-zoom',390,true);
    await page.evaluate(()=>document.documentElement.style.fontSize='');
    await page.getByLabel('Reply notification frequency').selectOption('muted');
    await page.getByRole('button',{name:'Save frequency',exact:true}).click();
    check('Muted choice persists',(await page.locator('summary').allTextContents()).some(t=>t.includes('Following · muted')));
    check('Mute cancels unread updates',(await (await owner.request.get(base+'/api/v1/me/notifications')).json()).items.length===0);
    const native=await browser.newContext({javaScriptEnabled:false});
    await native.addCookies([{name:'ticketopia_session',value:process.env.SAVED_FIXTURE_TOKEN,url:base}]);
    const nativePage=await native.newPage();await nativePage.goto(base+'/me/discussions');
    await nativePage.locator('.recommendation-editor summary').click();await nativePage.getByRole('button',{name:'Unfollow',exact:true}).click();
    check('Unfollow works without JavaScript',await nativePage.getByText('No followed conversations yet.',{exact:false}).count()===1);
    check('No browser runtime errors',!report.errors.length);
    fs.writeFileSync(path.join(out,'discussion-follows-checks.json'),JSON.stringify(report,null,2));
    console.log(JSON.stringify({checks:report.checks.length,screenshots:report.screenshots.length,accessibility:report.accessibility.length}));
  }finally{await browser.close();}
})().catch(error=>{console.error(error);process.exit(1);});
