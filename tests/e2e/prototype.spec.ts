import { test, expect } from '@playwright/test';

async function fillDestination(page: import('@playwright/test').Page, value:string){await page.getByLabel('URL tujuan',{exact:true}).fill(value);const fallback=page.getByLabel('URL fallback',{exact:true});if(await fallback.isVisible()&&await fallback.evaluate(el=>(el as HTMLInputElement).required)&&!(await fallback.inputValue()))await fallback.fill('https://destination.example/fallback')}

test('keyboard can skip navigation and modal keeps focus then restores its trigger',async({page})=>{
  await page.goto('/?demo=1');
  await expect(page.getByRole('link',{name:'Lewati navigasi'})).toBeVisible();
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link',{name:'Lewati navigasi'})).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.locator('#main-content')).toBeFocused();
  const trigger=page.getByRole('button',{name:'Buat shortlink',exact:true});
  await trigger.focus();await page.keyboard.press('Enter');
  const dialog=page.getByRole('dialog');await expect(dialog).toBeVisible();
  for(let i=0;i<16;i++){
    await page.keyboard.press('Tab');
    expect(await dialog.evaluate(el=>el.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press('Shift+Tab');
  expect(await dialog.evaluate(el=>el.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);await expect(trigger).toBeFocused();
});

test('primary pages remain usable at a narrow 320 pixel viewport',async({page})=>{
  await page.setViewportSize({width:320,height:740});
  for(const route of ['','links','domains','rules','traffic','system','agent/setup']){
    await page.goto('/?demo=1#/'+route);
    await expect(page.getByRole('main')).toBeVisible();
    expect(await page.evaluate(()=>document.documentElement.scrollWidth<=(window.visualViewport?.width??innerWidth)+1)).toBe(true);
  }
});

async function navigate(page: import('@playwright/test').Page, name: string) {
  const toggle=page.getByRole('button',{name:'Buka navigasi'});
  if(await toggle.isVisible())await toggle.click();
  await page.getByRole('navigation').getByRole('link',{name,exact:true}).click();
}
test('dashboard, domain detail, and responsive navigation',async({page})=>{
  const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
  await page.goto('/?demo=1');await expect(page.getByRole('heading',{name:'Setiap link, dalam kendali.'})).toBeVisible();
  await navigate(page,'Domain & Agent');await page.getByRole('button',{name:'s.karya.example',exact:true}).click();
  await expect(page.getByText('Belum ada aktivitas (bukan offline)')).toBeVisible();
  await expect(page.getByText('Diproses di pusat; lokal tidak diperlukan')).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=(window.visualViewport?.width??innerWidth)+1)).toBe(true);
  expect(errors).toEqual([]);
});
test('link draft validation, duplicate slug, and publish pending acknowledgement',async({page})=>{
  await page.goto('/?demo=1');await page.getByRole('button',{name:'Buat shortlink',exact:true}).click();
  await page.getByLabel('Slug',{exact:true}).fill('launch');await fillDestination(page,'https://destination.example/campaign');
  await page.getByRole('button',{name:'Simpan draft link'}).click();await expect(page.getByRole('alert')).toContainText('Slug sudah dipakai');
  await page.getByLabel('Slug',{exact:true}).fill('new-campaign');await page.getByRole('button',{name:'Simpan draft link'}).click();
  const row=page.getByRole('row').filter({hasText:'new-campaign'});await expect(row).toContainText('Draft');await row.getByRole('button',{name:'Publish demo'}).click();await expect(row).toContainText('Published');
  await navigate(page,'Domain & Agent');await page.getByRole('button',{name:'go.nusa.example',exact:true}).click();
  await expect(page.getByText('Menunggu acknowledgement.',{exact:false})).toBeVisible();
});
test('simulator explains crawler block and mobile routing',async({page})=>{
  await page.goto('/?demo=1#/rules');await page.getByRole('button',{name:'Jalankan simulasi'}).click();
  await expect(page.getByRole('status')).toContainText('Pengunjung mobile Indonesia');
  await page.getByLabel('Crawler dikenali').check();await page.getByRole('button',{name:'Jalankan simulasi'}).click();
  await expect(page.getByRole('status')).toContainText('Crawler yang dikenali');await expect(page.getByRole('status')).toContainText('block');
});
test('browser setup prevents unsafe path, completes, and clears token',async({page})=>{
  await page.goto('/?demo=1#/agent/setup');await page.getByText('Kontrol pengujian prototipe').click();
  await page.getByLabel('Skenario contoh').selectOption('private');await page.getByLabel('Token / key instalasi').fill('DEMO-INSTALL');await page.getByRole('button',{name:'Hubungkan',exact:true}).click();
  await expect(page.getByRole('button',{name:'Lanjutkan'})).toBeDisabled();await expect(page.getByRole('alert')).toContainText('Aktivasi belum bisa');
  await page.getByLabel('Skenario contoh').selectOption('success');await page.getByRole('button',{name:'Lanjutkan'}).click();await page.getByRole('button',{name:'Aktifkan demo'}).click();
  await expect(page.getByRole('heading',{name:'Domain terhubung pada demo.'})).toBeVisible();await expect(page.getByLabel('Skenario contoh')).toBeDisabled();
  expect(await page.locator('input[type=password]').count()).toBe(0);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=(window.visualViewport?.width??innerWidth)+1)).toBe(true);
});
test('expired token and empty traffic results are recoverable',async({page})=>{
  await page.goto('/?demo=1#/agent/setup');await page.getByText('Kontrol pengujian prototipe').click();await page.getByLabel('Skenario contoh').selectOption('expired');
  await page.getByLabel('Token / key instalasi').fill('DEMO-INSTALL');await page.getByRole('button',{name:'Hubungkan',exact:true}).click();await expect(page.getByRole('alert')).toContainText('kedaluwarsa');
  await page.goto('/?demo=1#/traffic');await page.getByLabel('Filter tindakan').selectOption('challenge');await expect(page.getByRole('heading',{name:'Tidak ada event'})).toBeVisible();
});
test('domain draft rejects URL and preserves detail route after save',async({page})=>{
  await page.goto('/?demo=1');await page.getByRole('button',{name:'Tambah domain',exact:true}).click();
  await page.getByLabel('Hostname').fill('https://go.example.com/path');await page.getByRole('button',{name:'Simpan draft domain'}).click();
  await expect(page.getByRole('alert')).toContainText('hostname tanpa');
  await page.getByLabel('Hostname').fill('go.new.example');await page.getByRole('button',{name:'Simpan draft domain'}).click();
  await expect(page.getByRole('heading',{name:'go.new.example',exact:true})).toBeVisible();
  await expect(page.getByText('Menunggu verifikasi',{exact:true})).toBeVisible();
});
test('duplicate priorities block publish and modal supports keyboard dismiss',async({page})=>{
  await page.goto('/?demo=1#/rules');await page.getByLabel('Prioritas Tujuan umum').fill('10');
  await page.getByRole('button',{name:'Publish draft demo'}).click();await expect(page.getByRole('status')).toContainText('prioritas aturan harus unik');
  await page.getByRole('button',{name:'Tambah aturan'}).click();await expect(page.getByRole('dialog')).toBeVisible();
  await page.keyboard.press('Escape');await expect(page.getByRole('dialog')).toHaveCount(0);
});
test('published link destination stays active until republish and URL identity is unchanged',async({page})=>{
  await page.goto('/?demo=1#/links');const row=page.getByRole('row').filter({hasText:'go.nusa.example/launch'});
  await row.getByRole('button',{name:'Edit tujuan launch'}).click();
  await expect(page.getByLabel('Domain',{exact:true})).toBeDisabled();await expect(page.getByLabel('Slug',{exact:true})).toBeDisabled();
  await fillDestination(page,'https://nusa.example/corrected');await page.getByRole('button',{name:'Simpan draft link'}).click();
  await expect(row).toContainText('Published: https://nusa.example/launch');await expect(row).toContainText('Draft: https://nusa.example/corrected');await expect(row).toContainText('12.480');
  await navigate(page,'Aturan Traffic');await page.getByLabel('Perangkat',{exact:true}).selectOption('desktop');await page.getByRole('button',{name:'Jalankan simulasi'}).click();
  await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/launch');
  await navigate(page,'Shortlink');await page.getByRole('row').filter({hasText:'go.nusa.example/launch'}).getByRole('button',{name:'Publish demo'}).click();
  await expect(page.getByRole('row').filter({hasText:'go.nusa.example/launch'})).not.toContainText('Perubahan draft');
  await navigate(page,'Aturan Traffic');await page.getByLabel('Perangkat',{exact:true}).selectOption('desktop');await page.getByRole('button',{name:'Jalankan simulasi'}).click();
  await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/corrected');
});
test('domain rule does not leak to another domain',async({page})=>{
  await page.goto('/?demo=1#/rules');await page.getByLabel('Shortlink simulasi').selectOption('l3');await page.getByRole('button',{name:'Jalankan simulasi'}).click();
  await expect(page.locator('.simulation-result')).toContainText('https://karya.example/work');await expect(page.locator('.simulation-result')).not.toContainText('Pengunjung mobile Indonesia');
  await page.getByLabel('Crawler dikenali').check();await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('Crawler yang dikenali');
});
test('link audience and fallback are edited inline and published together',async({page})=>{
 await page.goto('/?demo=1#/links');const row=page.getByRole('row').filter({hasText:'go.nusa.example/launch'});
 await row.getByRole('button',{name:'Aturan launch',exact:true}).click();await expect(page.getByRole('dialog')).toBeVisible();await page.getByLabel('URL fallback').fill('https://alternate.example/fallback');await page.getByText('Atur pengunjung tujuan utama',{exact:true}).click();await page.getByLabel('Negara yang diizinkan').selectOption('SG');await page.getByRole('button',{name:'Simpan draft link'}).click();
 await expect(row).toContainText('Draft fallback: https://alternate.example/fallback');await expect(row.getByRole('link',{name:'Buka shortlink launch'})).toHaveAttribute('href','https://go.nusa.example/launch');await expect(row.getByRole('link',{name:'Buka shortlink launch'})).toHaveAttribute('rel','noopener noreferrer');
 await row.getByRole('button',{name:'Publish demo'}).click();await expect(row).toContainText('Fallback: https://alternate.example/fallback');await expect(row).not.toContainText('Perubahan draft');
 await navigate(page,'Aturan Traffic');await page.getByLabel('Shortlink simulasi').selectOption('l1');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://alternate.example/fallback');await page.getByLabel('Crawler dikenali').check();await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('fallback');
});
test('OS routing distinguishes Android, iPhone, iPad and unknown',async({page})=>{
  await page.goto('/?demo=1#/rules');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/mobile');
  await page.getByLabel('Sistem operasi',{exact:true}).selectOption('ios');await page.getByLabel('Browser',{exact:true}).selectOption('safari');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/ios');
  await page.getByLabel('Perangkat',{exact:true}).selectOption('tablet');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/ios');
  await page.getByLabel('Sistem operasi',{exact:true}).selectOption('unknown');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/launch');
});
test('rule edits preserve OS and combine browser with OS using AND',async({page})=>{
  await page.goto('/?demo=1#/domains/d1');await page.getByRole('button',{name:'Edit aturan Pengunjung mobile Indonesia'}).click();
  const dialog=page.getByRole('dialog');await expect(dialog.getByLabel('Sistem operasi',{exact:true})).toHaveValue('android');await dialog.getByLabel('Browser',{exact:true}).selectOption('firefox');await dialog.getByRole('button',{name:'Simpan draft aturan'}).click();
  await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/launch');
  await page.getByLabel('Browser',{exact:true}).selectOption('firefox');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/mobile');
  await page.getByRole('button',{name:'Edit aturan Pengunjung mobile Indonesia'}).click();await expect(page.getByRole('dialog').getByLabel('Sistem operasi',{exact:true})).toHaveValue('android');await expect(page.getByRole('dialog').getByLabel('Browser',{exact:true})).toHaveValue('firefox');
});
test('OS browser and device log filters show hardware metadata without inventing models',async({page})=>{
  await page.goto('/?demo=1#/traffic');await page.getByLabel('Filter OS').selectOption('ios');await page.getByLabel('Filter browser').selectOption('safari');await page.getByLabel('Filter perangkat').selectOption('tablet');
  await expect(page.locator('.pagination')).toContainText('4 event');await page.getByRole('button',{name:'Lihat',exact:true}).first().click();
  await expect(page.getByRole('dialog')).toContainText('Apple');await expect(page.getByRole('dialog')).toContainText('iPad');await page.getByRole('button',{name:'Tutup dialog'}).click();
  await page.getByLabel('Filter OS').selectOption('unknown');await expect(page.getByRole('heading',{name:'Tidak ada event'})).toBeVisible();await page.getByLabel('Filter browser').selectOption('all');await page.getByLabel('Filter perangkat').selectOption('all');await page.getByRole('button',{name:'Lihat',exact:true}).first().click();
  await expect(page.getByRole('dialog').getByText('Unknown · tidak tersedia',{exact:true})).toHaveCount(2);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=(window.visualViewport?.width??innerWidth)+1)).toBe(true);
});


test('proxy choice uses the link fallback for positive and unknown results',async({page})=>{
 await page.goto('/?demo=1#/links');const row=page.getByRole('row').filter({hasText:'s.karya.example/portfolio'});await row.getByRole('button',{name:'Edit tujuan portfolio'}).click();await page.getByLabel('URL fallback').fill('https://alternate.example/proxy');await page.getByText('Atur pengunjung tujuan utama',{exact:true}).click();await page.getByLabel('Periksa IP dengan BEON-IPQuality',{exact:true}).check();await expect(page.getByLabel('Arahkan hasil IP yang belum diketahui ke fallback',{exact:true})).toBeChecked();await page.getByRole('button',{name:'Simpan draft link'}).click();await row.getByRole('button',{name:'Publish demo'}).click();
 await navigate(page,'Aturan Traffic');await page.getByLabel('Shortlink simulasi').selectOption('l3');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://karya.example/work');await page.getByLabel('Proxy/VPN terdeteksi (simulasi)',{exact:true}).check();await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://alternate.example/proxy');await page.getByLabel('Hasil proxy simulasi').selectOption('unavailable');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://alternate.example/proxy');
});
