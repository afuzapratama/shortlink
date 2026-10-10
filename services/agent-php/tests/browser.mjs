import { chromium } from '@playwright/test';
import { mkdir } from 'node:fs/promises';
let input='';for await (const chunk of process.stdin) input+=chunk;
const {origin,token,artifactDir}=JSON.parse(input);
await mkdir(artifactDir,{recursive:true});
const browser=await chromium.launch({headless:true});
try {
 const mobile=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true});
 const mp=await mobile.newPage();await mp.goto(origin+'/__agent/setup');await mp.getByRole('heading',{name:'Hubungkan domain ke pusat'}).waitFor();
 if(await mp.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth)) throw Error('Mobile setup overflows');
 await mp.screenshot({path:artifactDir+'/php-setup-mobile.png',fullPage:true});await mobile.close();
 const context=await browser.newContext({viewport:{width:1280,height:900}});const page=await context.newPage();
 await page.goto(origin+'/__agent/setup');await page.screenshot({path:artifactDir+'/php-setup-desktop.png',fullPage:true});
 await page.getByLabel('Token instalasi PHP').fill(token);
 await Promise.all([page.waitForNavigation(),page.getByRole('button',{name:'Periksa hosting dan koneksi'}).click()]);
 if(!(await page.getByRole('heading',{name:'Siap untuk aktivasi'}).count())) {console.log('Setup check notice:',await page.locator('.notice').textContent());throw Error('Browser hosting check failed');}
 if((await page.content()).includes(token)) throw Error('Token leaked in returned HTML');
 await page.screenshot({path:artifactDir+'/php-setup-activation-desktop.png',fullPage:true});
 await Promise.all([page.waitForNavigation(),page.getByRole('button',{name:'Aktifkan agent'}).click()]);
 await page.getByRole('heading',{name:'Agent sudah terpasang'}).waitFor();
 if(await page.locator('input[name=token]').count()) throw Error('Setup is not locked');
 await page.goto(origin+'/__agent/setup');await page.reload();await page.getByRole('heading',{name:'Agent sudah terpasang'}).waitFor();
 const finished=await browser.newContext({viewport:{width:390,height:844},isMobile:true,hasTouch:true});const fp=await finished.newPage();
 await fp.goto(origin+'/__agent/setup');await fp.getByRole('heading',{name:'Agent sudah terpasang'}).waitFor();
 await fp.screenshot({path:artifactDir+'/php-setup-complete-mobile.png',fullPage:true});
 console.log('PHP browser checks passed: desktop/mobile, token check, recovered activation, setup lock and reload.');
} finally {await browser.close();}
