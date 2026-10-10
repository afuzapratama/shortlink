import { test, expect } from '@playwright/test';

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
  await page.getByLabel('Slug',{exact:true}).fill('launch');await page.getByLabel('URL tujuan').fill('https://destination.example/campaign');
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
  await page.getByLabel('URL tujuan',{exact:true}).fill('https://nusa.example/corrected');await page.getByRole('button',{name:'Simpan draft link'}).click();
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
test('link-specific rule is isolated and inherits mandatory global blocking',async({page})=>{
  await page.goto('/?demo=1#/links');await page.getByRole('button',{name:'Aturan launch',exact:true}).click();await expect(page.getByLabel('Cakupan aturan',{exact:true})).toHaveValue('link');
  await page.getByRole('button',{name:'Tambah aturan',exact:true}).click();const dialog=page.getByRole('dialog');
  await dialog.getByLabel('Nama aturan').fill('Singapura untuk launch');await dialog.getByLabel('Negara',{exact:true}).selectOption('SG');await dialog.getByLabel('Tindakan').selectOption('redirect');await dialog.getByRole('button',{name:'Tambah draft',exact:true}).click();
  await page.getByLabel('Prioritas Singapura untuk launch').fill('1');await page.getByLabel('Negara',{exact:true}).selectOption('SG');await page.getByRole('button',{name:'Jalankan simulasi'}).click();
  await expect(page.locator('.simulation-result')).toContainText('Singapura untuk launch');
  await page.getByLabel('Shortlink simulasi').selectOption('l2');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/catalog');
  await page.getByLabel('Shortlink simulasi').selectOption('l1');await page.getByLabel('Crawler dikenali').check();await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('Crawler yang dikenali');
});
test('OS routing distinguishes Android, iPhone, iPad and unknown',async({page})=>{
  await page.goto('/?demo=1#/rules');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/mobile');
  await page.getByLabel('Sistem operasi',{exact:true}).selectOption('ios');await page.getByLabel('Browser',{exact:true}).selectOption('safari');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/ios');
  await page.getByLabel('Perangkat',{exact:true}).selectOption('tablet');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/ios');
  await page.getByLabel('Sistem operasi',{exact:true}).selectOption('unknown');await page.getByRole('button',{name:'Jalankan simulasi'}).click();await expect(page.locator('.simulation-result')).toContainText('https://nusa.example/launch');
});
test('rule edits preserve OS and combine browser with OS using AND',async({page})=>{
  await page.goto('/?demo=1#/rules');await page.getByLabel('Cakupan aturan',{exact:true}).selectOption('domain');await page.getByRole('button',{name:'Edit aturan Pengunjung mobile Indonesia'}).click();
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
