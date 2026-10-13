# Pemisahan preview, development, staging dan production

## Dashboard

| Lingkungan | Cara menjalankan/build | Alamat/output | Data |
| --- | --- | --- | --- |
| Development nyata | `npm run dev` | localhost:5173 | API/database lokal |
| Prototype/preview UI | `npm run dev:prototype` | localhost:5174/?demo=1 | Fixture, tanpa database/API nyata |
| Build prototype | `npm run build:prototype` | apps/dashboard/dist-prototype | Khusus preview privat |
| Staging | `npm run build:staging` | apps/dashboard/dist-staging | API/database staging |
| Production | `npm run build` | apps/dashboard/dist | API/database production |

Dashboard development biasa, staging dan production mengabaikan `?demo=1`. Modul preview dan fixture dibuang dari bundle staging/production; komponen domain bersama telah dipisahkan dari PrototypeApp. Link preview tidak ditampilkan pada login dashboard nyata. Build preview tidak boleh disalin ke web root staging/production.

Verifikasi bundle: `node scripts/check-dashboard-build.mjs apps/dashboard/dist apps/dashboard/dist-staging`. Tes prototype memakai port 5174 tersendiri; tes live tetap memakai port 5175 dan PostgreSQL sementara.

## Pusat dan aaPanel

Pusat test yang terpasang saat ini: `shortlink-test.service` systemd, binary `/opt/shortlink-test/bin/control serve`, user shortlink-test, env privat `/etc/shortlink-test/control.env`, dashboard statis `/opt/shortlink-test/dashboard`, database test shortlink_b19. Nginx aaPanel menyajikan dashboard dan meneruskan API ke loopback 8086. Node 24 tersedia di direktori aaPanel, tetapi dashboard tidak mempunyai proses Node saat serving. Go berjalan dari binary, sehingga tidak memerlukan SDK Go terpasang pada server runtime.

Daftar proyek Node/Go aaPanel hanya menampilkan proyek yang didaftarkan lewat pengelolaan proyek panel; menjalankan binary melalui systemd tidak mendaftarkan proyek tersebut. Karena itu pusat test tidak tampak sebagai Go Project dan dashboard statis tidak tampil sebagai Node Project. Ini berbeda dari ketersediaan runtime pada server.

Jika pusat production ingin dikelola aaPanel Go Project, daftarkan executable Go yang sudah dibuild, perintah `control serve`, port loopback dan run user. Environment rahasia harus dimuat dari file privat melalui launcher; pastikan module/versi panel mendukungnya. Matikan supervisor systemd untuk instance yang sama saat berpindah manager, agar dua supervisor tidak memperebutkan port. Panel Go versi terbatas bukan batas versi binary yang dibuild di mesin lain. Dokumentasi resmi: [Go Project](https://www.aapanel.com/docs/Function/go.html), [Node Project](https://www.aapanel.com/docs/Function/Node.html).

Jangan membuat Node Project kosong untuk dashboard statis. Bila tetap memakai systemd, gunakan aaPanel untuk domain/Nginx/SSL dan systemd untuk proses API; log melalui `journalctl -u shortlink-test` untuk pusat test. Pemilihan manager production harus dicatat sebelum cutover.

## Pemisahan deployment

Staging dan production membutuhkan hostname, database/role, owner/session, signing seed, asset/MMDB/risk root, domain-agent binding, cookie, backup dan service yang terpisah. `CONTROL_MODE=production` berlaku pada keduanya karena HTTPS publik; staging bukan development mode. Database/signing key test tidak otomatis dipakai sebagai production dan agent tidak otomatis dipindahkan ke pusat baru.

Path yang disarankan: `/opt/shortlink-staging` dan `/opt/shortlink-production`; env privat `/etc/shortlink-staging/control.env` dan `/etc/shortlink-production/control.env`. Port staging 8087, production 8086 jika dipakai pada host yang sama; port tidak boleh bertabrakan dengan service test yang masih berjalan. Template tersedia di deploy/.env.staging.example dan deploy/.env.production.example, tanpa credential nyata.

Pemisahan build sudah diterapkan lokal. Cutover publik belum dilakukan: hostname final staging/production perlu ditentukan, backup/restore dan kompatibilitas artifact/runtime baru perlu diverifikasi. Gate kapasitas PHP/API 100–300 klik/detik, MFA owner, updater GeoLite berlisensi, serta health feed BEON tetap mengikuti RELEASE_B20.md dan IP_QUALITY.md; pemisahan dashboard bukan bukti gate tersebut selesai.

## Staging publik yang sudah dipasang — 13 Oktober 2026

- URL: https://stg-dashort.beon-api.site, DNS VPS aaPanel ARM 20.243.8.202.
- Website terdaftar di aaPanel (static/PHP tanpa PHP runtime); Nginx/SSL memberi dashboard nyata dan proxy API localhost:8087.
- Service shortlink-staging, user shortlink-staging, database/role shortlink_staging, env /etc/shortlink-staging/control.env, data /var/lib/shortlink-staging, executable /opt/shortlink-staging/bin/control.
- Dashboard build dist-staging dipasang di /www/wwwroot/stg-dashort.beon-api.site. Cookie, owner, seed/signature dan asset staging terpisah dari pusat lama.
- Go release v0.20.2-staging (routingVersion 4), PHP v0.20.1-b20, GeoLite dipublish/sign dengan key staging. Versi Go ARM dijalankan native. Manifest download keduanya HTTP 200.
- Sertifikat HTTPS diterbitkan Certbot dengan renewal terjadwal dan reload hook Nginx; symlink cert staging tersedia di folder cert aaPanel.
- HTTPS health/ready, browser login/logout, bundle tanpa preview dan database kosong lulus smoke. BEON client key tersimpan terenkripsi; upstream saat pemeriksaan masih unavailable/stale.
- Login staging disimpan privat lewat secrets/staging-owner.json yang menunjuk file operator di filesystem Linux; tidak dimasukkan Git atau output chat.

Pusat dashort.beon-api.site masih melayani service/data lama dan merespons health 200. Nama itu dipilih untuk production, tetapi cutover versi baru belum dilakukan. Backup konsisten pg_dump pusat lama dibuat sebelum persiapan production dan disalin off-host dengan checksum cocok. Sebelum cutover perlu keputusan apakah identitas/data/agent lama dipertahankan atau production kosong; data tidak dihapus/dipindah tanpa keputusan tersebut.

## Cutover production dengan data dipertahankan — 13 Oktober 2026

Owner memilih mempertahankan akun, link, traffic, signing seed dan identitas/kredensial agent. Cutover versi baru ke https://dashort.beon-api.site telah dilakukan, bukan database kosong atau pemasangan ulang.

- Service aktif: shortlink-production, localhost:8088, executable /opt/shortlink-production/bin/control, dashboard /opt/shortlink-production/dashboard, env privat /etc/shortlink-production/control.env.
- Database shortlink_b19, role/user lama dan /var/lib/shortlink-test tetap digunakan untuk kompatibilitas dan mempertahankan identitas. Nama path legacy bukan instance staging. Staging tetap database shortlink_staging dan localhost:8087, dengan owner/key tersendiri.
- Nginx aaPanel dipindahkan dengan reload setelah kandidat ready. Service shortlink-test dinonaktifkan; binary/config/dashboard lamanya tetap tersedia sebagai bahan rollback. Production dan staging enabled saat reboot.
- Backup sekitar 65,8 MB dipulihkan pada shortlink_upgrade_check. Migrasi kandidat mempertahankan isi/proyeksi 28 tabel lama. Sebelum/sesudah migrasi production, fingerprint owners/signing seed/agent credential/link cocok; fingerprint identitas agent tetap cocok setelah runtime update.
- Go ARM diperbarui menjadi v0.20.4, routingVersion 4, tanpa enrollment/rotasi. Updater sekarang mempertahankan Caddyfile operator dan hanya mengganti image agent pada Compose; layanan/mount/logging custom tetap ada. Konfigurasi Caddy BEON/monitoring terbukti tidak berubah. Tes first-install scaffold dan konfigurasi custom, race/vet agent lulus.
- PHP aaPanel dan cPanel diperbarui dari ZIP signed v0.20.1-b20 yang diverifikasi memakai key pusat lama. Source diganti, private config dan kredensial dipertahankan (checksum cocok), .htaccess custom cPanel tidak diubah. Backup source/config tersimpan di luar web root; PHP aaPanel FPM direload, source cPanel dilint dengan PHP 8.2.
- Dashboard production tidak membawa prototype/fixture atau link preview, termasuk saat ?demo=1. Login/logout dengan akun lama lulus. File login operator tersedia melalui secrets/production-owner.json, privat dan diabaikan Git.
- BEON key client dikonfigurasi terenkripsi; lookup final melalui production kembali known. Pemeriksaan awal sebelumnya unavailable/stale adalah hasil historis, bukan status terakhir.

Hasil smoke final: ketiga agent menghasilkan redirect published HTTP 302, probe ready/online, desired/applied ARM 6/6, PHP aaPanel 6/6, cPanel 9/9. Manage login HTTP 200. Root/slug tidak ditemukan memakai URL pembuka; jika kosong, 503 sesuai konfigurasi. PHP tidak menyediakan /health/ready; gunakan probe .well-known/shortlink-ready dari pusat.

Cutover fungsi selesai. Ini tidak menyelesaikan gate kapasitas PHP/API 100–300 RPS, audit aksesibilitas penuh, MFA owner, jadwal GeoLite berlisensi, atau operasi backup berkala. Hasil kapasitas tetap mengikuti PUBLIC_TESTS_B19.md.

Rollback backend: pastikan konfigurasi yang sudah dipublish kompatibel dengan binary tujuan. Untuk kondisi cutover awal yang masih kompatibel, hidupkan service shortlink-test, validasi ready 8086, pulihkan /var/backups/shortlink-production/nginx-before-cutover.conf, nginx -t lalu reload, dan hentikan production setelah route kembali. Jangan restore database ke snapshot lama untuk rollback binary karena bisa kehilangan klik. Agent dengan aturan routingVersion 4 tidak boleh diturunkan ke runtime lama tanpa menghapus kebutuhan tersebut terlebih dahulu.

Setelah validasi, database sementara shortlink_upgrade_check dan upload sementara pusat dihapus; laporan fingerprint serta backup tetap disimpan. SSH-agent/askpass sementara cPanel ditutup. Akun production tetap akun sebelumnya, tanpa bootstrap ulang.

### Pembersihan dashboard 13 Oktober 2026

Tautan preview pada sidebar dan halaman login dashboard utama telah dihapus.
Label simulator draft memakai “Uji draft”; fitur simulasi aturan tetap tersedia.
Build production dan staging memeriksa tautan demo serta penanda fixture sebelum
pemasangan. Dashboard dipasang dengan aset terlebih dahulu, lalu penggantian
index secara atomik; API, database, dan identitas agent tetap berjalan.
Verifikasi browser production mencakup login dan tujuh halaman navigasi tanpa
tautan demo maupun teks fixture. Prototype tetap terpisah untuk pengembangan lokal.
