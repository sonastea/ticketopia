// Isolated MariaDB and synthetic public metadata; never sends live email.
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
    const owner = await browser.newContext({viewport:{width:1440,height:1000}});
    await owner.addCookies([{name:'ticketopia_session',value:process.env.SAVED_FIXTURE_TOKEN,url:base}]);
    const me = await (await owner.request.get(base+'/api/v1/me')).json();
    const event = 'ticketmaster:Browser_1';
    const endpoint = '/api/v1/me/reminders/'+event+'/public-on-sale';
    const write = (method, endpoint, data) => owner.request[method](base+endpoint,{data,headers:{'X-CSRF-Token':me.csrf_token}});
    check('Save synthetic event',(await write('put','/api/v1/me/saved-events/'+event)).status()===201);
    check('Saving does not subscribe',(await owner.request.get(base+endpoint)).status()===404);
    const page = await owner.newPage();
    page.on('pageerror', e=>report.errors.push(e.message));
    const capture = async(name,width,zoom=false,height=1000,target=page)=>{
      await target.setViewportSize({width,height});
      await target.evaluate(async zoom=>{document.documentElement.style.fontSize=zoom?'200%':'';await document.fonts.ready;scrollTo(0,0);},zoom);
      check('Reflow '+name,await target.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
      if(!process.env.REMINDER_CAPTURE_ONLY || process.env.REMINDER_CAPTURE_ONLY===name) {
        const file=path.join(out,name+'.png');await target.screenshot({path:file,fullPage:true});report.screenshots.push(file);
        if(['preferences-desktop','preferences-phone','preferences-small'].includes(name)) {
          const focused=path.join(out,name+'-notifications.png');await target.locator('.notification-preferences').screenshot({path:focused});report.screenshots.push(focused);
        }
      }
      if(process.env.AXE_SCRIPT){await target.addScriptTag({path:process.env.AXE_SCRIPT});const violations=await target.evaluate(async()=> (await axe.run(document,{runOnly:{type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}})).violations);report.accessibility.push({name,violations});check('Accessibility '+name,!violations.length);}
    };
    await page.goto(base+'/saved');
    check('Collections do not repeat reminder controls',await page.locator('.reminder-button').count()===0);
    await page.locator('.event-link[href*="Browser_1"]').first().click();
    await page.locator('[data-save-form] button').click();
    await page.locator('.reminder-button').waitFor({state:'hidden'});
    check('Confirmed unsave hides reminder settings',true);
    await page.locator('[data-save-form] button').click();
    await page.locator('.reminder-button').waitFor({state:'visible'});
    check('Resaving exposes settings without consent',(await owner.request.get(base+endpoint)).status()===404);
    await page.locator('.reminder-button').click();
    check('Explicit pending form',await page.getByRole('button',{name:'Save email reminder'}).count()===1);
    check('Defaults remain paused',(await page.locator('main').innerText()).includes('Delivery is paused'));
    const timing = page.getByLabel('Remind me before public sale', {exact:true});
    const custom = page.getByLabel('Custom lead time in minutes', {exact:true});
    check('Native preset select',await timing.evaluate(el=>el.tagName==='SELECT'));
    check('Default 30 minutes',await timing.inputValue()==='30');
    check('Preset hides Custom',!await custom.isVisible());
    for(const [choice,minutes] of [['60',60],['1440',1440],['30',30]]) {
      await timing.selectOption(choice);
      await page.getByRole('button',{name:'Save email reminder'}).click();
      check('Preset persisted '+choice,(await (await owner.request.get(base+endpoint)).json()).lead_minutes===minutes);
      check('Saved confirmation '+choice,(await page.locator('main').innerText()).includes('Reminder timing saved'));
    }
    await timing.selectOption('custom');
    await custom.fill('10081');
    await page.getByRole('button',{name:'Save email reminder'}).click();
    check('Nearby range error',(await page.locator('#reminder-error').innerText()).includes('10080'));
    check('Invalid custom retained',await custom.inputValue()==='10081' && await timing.inputValue()==='custom');
    check('Invalid choice did not write',(await (await owner.request.get(base+endpoint)).json()).lead_minutes===30);
    await capture('reminders-error-desktop',1440);
    await capture('reminders-error-phone',390);
    await custom.fill('45');
    await page.getByRole('button',{name:'Save email reminder'}).click();
    check('Native form persisted',(await (await owner.request.get(base+endpoint)).json()).lead_minutes===45);
    check('Configuration did not unpause',(await (await owner.request.get(base+'/api/v1/me/preferences')).json()).notifications_paused===true);
    check('Saved non-preset uses Custom',await timing.inputValue()==='custom' && await custom.inputValue()==='45');
    check('Unknown sale remains unscheduled',(await page.locator('main').innerText()).includes('Time not announced') && (await page.locator('[data-reminder-preview]').innerText()).includes('no target is scheduled'));
    await capture('reminders-desktop',1440);
    await capture('reminders-rail',1050);
    await capture('reminders-tablet',768);
    await capture('reminders-phone',390);
    await capture('reminders-small',320);
    await capture('reminders-short',390,false,480);
    await capture('reminders-enlarged-text',390,true);
    await page.evaluate(()=>document.documentElement.style.fontSize='');
    const lead=page.getByLabel('Custom lead time in minutes',{exact:true});
    check('44px input',(await lead.boundingBox()).height>=44);
    await lead.focus();
    check('Keyboard outline',await lead.evaluate(el=>{const s=getComputedStyle(el);return s.outlineStyle!=='none'&&parseFloat(s.outlineWidth)>0;}));
    await page.emulateMedia({forcedColors:'active',reducedMotion:'reduce'});
    check('Forced colors outline',await lead.evaluate(el=>{const s=getComputedStyle(el);return s.outlineStyle!=='none'&&parseFloat(s.outlineWidth)>0;}));
    await page.emulateMedia({forcedColors:'none',reducedMotion:'no-preference'});
    await page.goto(base+'/me/reminders');
    check('Owner collection retains context',(await page.locator('main').innerText()).includes('An event with a very long name'));
    await capture('reminders-collection',390);
    check('Collection has account time zone',(await page.locator('main').innerText()).includes('America/Chicago'));
    for(const [state,label,detail] of [
      ['pending_date','Pending sale time','no target is scheduled'],['scheduled','Scheduled','not guaranteed'],
      ['paused','Paused','Your timing choice is retained'],['refresh_failed','Waiting for fresh evidence','delivery is withheld'],
      ['sending','Sending','already being sent'],['pending','Waiting to retry','original deadline'],
      ['accepted','SES accepted','inbox delivery is not confirmed'],['uncertain','Uncertain','not automatically resent'],
      ['failed','Failed','Operator review'],['expired','Expired','No late email'],['cancelled','Stopped','cancelled/postponed']]) {
      const row=page.locator('.reminder-list li').filter({has:page.getByRole('heading',{name:'Reminder fixture — '+state,exact:true})});
      check('Truthful collection state '+state,(await row.innerText()).includes(label) && (await row.innerText()).includes(detail));
    }
    await capture('reminders-collection-desktop',1440);
    await write('put','/api/v1/me/saved-events/ticketmaster:Browser_0');
    await page.goto(base+'/me/reminders/ticketmaster:Browser_0');
    check('Future sale preview has target',(await page.locator('[data-reminder-preview]').innerText()).includes('Target:'));
    await timing.selectOption('1440');
    check('Elapsed lead explains immediate eligibility',(await page.locator('[data-reminder-preview]').innerText()).includes('eligible immediately'));
    await timing.selectOption('60');
    await capture('reminders-known-sale',1440);
    await write('put','/api/v1/me/saved-events/ticketmaster:Browser_2');
    await page.goto(base+'/me/reminders/ticketmaster:Browser_2');
    check('Past sale has no late reminder',(await page.locator('[data-reminder-preview]').innerText()).includes('no late reminder'));
    await page.goto(base+'/me/preferences');
    check('Digest is not interactive',await page.locator('[name="weekly_digest"]').count()===0);
    check('Preferences do not advertise future digests',!(await page.locator('main').innerText()).includes('Weekly discovery digest'));
    check('Email permission has local help',await page.getByRole('checkbox',{name:'Email me on-sale reminders',exact:true}).getAttribute('aria-describedby')==='email-reminders-help');
    check('Pause explains both channels',(await page.locator('#notification-pause-help').innerText()).includes('on-sale emails and new reply updates'));
    check('Delivery details start collapsed',!await page.locator('details').filter({has:page.getByText('About email delivery',{exact:true})}).evaluate(el=>el.open));
    const emailPermission=page.getByRole('checkbox',{name:'Email me on-sale reminders',exact:true});
    const permissionBefore=await emailPermission.isChecked();
    await page.locator('#email-reminders-help').click();
    check('Grouped email help toggles its checkbox',await emailPermission.isChecked()!==permissionBefore);
    await page.locator('#email-reminders-label').click();
    check('Email label restores its checkbox',await emailPermission.isChecked()===permissionBefore);
    const deliverySummary=page.getByText('About email delivery',{exact:true});
    await deliverySummary.focus();await page.keyboard.press('Enter');
    check('Delivery details open with keyboard',await page.locator('.notification-delivery').evaluate(el=>el.open));
    await page.keyboard.press('Enter');
    check('Delivery details close with keyboard',!await page.locator('.notification-delivery').evaluate(el=>el.open));
    await page.getByLabel('City', {exact:false}).fill('Chicago');
    await page.getByRole('button',{name:'Save preferences',exact:true}).click();
    check('Unrelated preference save retains digest',(await (await owner.request.get(base+'/api/v1/me/preferences')).json()).weekly_digest===true);
    await page.getByLabel('Time zone',{exact:true}).fill('Not/AZone');
    await page.getByRole('button',{name:'Save preferences',exact:true}).click();
    check('Preference validation retains input',await page.getByLabel('Time zone',{exact:true}).inputValue()==='Not/AZone');
    check('Preference validation retains digest',(await (await owner.request.get(base+'/api/v1/me/preferences')).json()).weekly_digest===true);
    await capture('preferences-error-phone',390);
    await page.goto(base+'/me/preferences');
    await capture('preferences-desktop',1440);
    await capture('preferences-rail',1050);
    await capture('preferences-tablet',768);
    await capture('preferences-phone',390);
    await capture('preferences-small',320);
    await capture('preferences-short',390,false,480);
    await capture('preferences-enlarged-text',390,true);
    check('Notification actions are separate from prose',await page.locator('.notification-preferences p a, .notification-copy a').count()===0);
    check('Enlarged notification targets stay usable',await page.locator('.notification-preferences').evaluate(el=>[...el.querySelectorAll('label,a,summary')].every(target=>target.getBoundingClientRect().height>=44)));
    const guest=await browser.newContext();
    check('Guest denied private API',(await guest.request.get(base+'/api/v1/me/reminders')).status()===401);
    const other=await browser.newContext();
    await other.addCookies([{name:'ticketopia_session',value:process.env.SAVED_OTHER_TOKEN,url:base}]);
    check('Another owner cannot read setting',(await other.request.get(base+endpoint)).status()===404);
    const native=await browser.newContext({javaScriptEnabled:false});
    await native.addCookies([{name:'ticketopia_session',value:process.env.SAVED_FIXTURE_TOKEN,url:base}]);
    const nativePage=await native.newPage();await nativePage.goto(base+'/me/reminders/'+event);
    check('Custom visible without JavaScript',await nativePage.getByLabel('Custom lead time in minutes',{exact:true}).isVisible());
    await nativePage.getByLabel('Remind me before public sale',{exact:true}).selectOption('custom');
    await nativePage.getByLabel('Custom lead time in minutes',{exact:true}).fill('75');
    await nativePage.getByRole('button',{name:'Save email reminder',exact:true}).click();
    check('Custom saves without JavaScript',(await (await owner.request.get(base+endpoint)).json()).lead_minutes===75);
    await nativePage.getByRole('button',{name:'Stop this reminder',exact:true}).click();
    check('Stop works without JavaScript',(await owner.request.get(base+endpoint)).status()===404);
    await nativePage.goto(base+'/me/preferences');
    const nativePause=nativePage.getByRole('checkbox',{name:'Pause all notifications',exact:true});
    const pausedBefore=await nativePause.isChecked();
    await nativePage.locator('#notification-pause-help').click();
    check('Grouped pause works without JavaScript',await nativePause.isChecked()!==pausedBefore);
    await nativePage.getByText('About email delivery',{exact:true}).click();
    check('Delivery disclosure works without JavaScript',await nativePage.locator('.notification-delivery').evaluate(el=>el.open));
    const touch=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true});
    await touch.addCookies([{name:'ticketopia_session',value:process.env.SAVED_FIXTURE_TOKEN,url:base}]);
    const touchPage=await touch.newPage();await touchPage.goto(base+'/me/reminders/'+event);
    await Promise.all([touchPage.waitForURL('**?saved=reminder'),touchPage.getByRole('button',{name:'Save email reminder',exact:true}).tap()]);
    check('Touch save retains consent',(await (await owner.request.get(base+'/api/v1/me/preferences')).json()).notifications_paused===true);
    await touchPage.goto(base+'/me/preferences');
    const touchPause=touchPage.getByRole('checkbox',{name:'Pause all notifications',exact:true});
    const touchPausedBefore=await touchPause.isChecked();
    await touchPage.locator('#notification-pause-help').tap();
    check('Grouped pause works on touch',await touchPause.isChecked()!==touchPausedBefore);
    await touchPage.goto(base+'/me/reminders/'+event);
    await touch.addCookies([{name:'ticketopia_session',value:process.env.SAVED_OTHER_TOKEN,url:base}]);
    await Promise.all([touchPage.waitForNavigation(),touchPage.evaluate(()=>window.dispatchEvent(new PageTransitionEvent('pageshow',{persisted:true})))]);
    check('BFCache changed session cannot retain private reminder form',await touchPage.locator('[data-reminder-timing]').count()===0);
    await touchPage.goto(base+'/me/preferences');
    check('Changed session preferences are revalidated',await touchPage.getByLabel('City',{exact:false}).inputValue()==='');
    await touchPage.goto(base+'/me/reminders');
    check('Other owner has an empty collection',(await touchPage.locator('main').innerText()).includes('No on-sale reminders yet'));
    await capture('reminders-empty-phone',390,false,1000,touchPage);
    await page.goto(process.env.SAVED_DISABLED_URL+'/me/reminders');
    check('Disabled server remains usable',(await page.locator('main').innerText()).includes('Email reminders are not enabled'));
    check('No browser runtime errors',report.errors.length===0);
    fs.writeFileSync(path.join(out,'reminders-checks.json'),JSON.stringify(report,null,2));
    console.log(JSON.stringify({checks:report.checks.length,screenshots:report.screenshots.length,accessibility:report.accessibility.length}));
  } finally {await browser.close();}
})().catch(error=>{fs.writeFileSync(path.join(out,'reminders-checks.json'),JSON.stringify(report,null,2));console.error(error);process.exit(1);});
