# B20 — review private release

Status: **berjalan, kembali ke dev lokal untuk revisi owner**, dimulai atas arahan owner 11 Oktober 2026. Review dapat dilakukan sekarang; release production belum dinyatakan siap. [B19 publik](PUBLIC_TESTS_B19.md) tetap menjadi sumber hasil kapasitas dan batas lingkungan nyata.

## Paket pekerjaan

| Tahap | Hasil yang harus tersedia | Status |
|---|---|---|
| B20.1 | Review dashboard desktop/mobile, keyboard, form/error dan alur install | Otomatis lokal lulus; review owner dan audit aksesibilitas menyeluruh tersisa |
| B20.2 | Panduan update/rollback pusat, Go dan PHP dengan identitas/data tetap | Runbook tersedia; Go live sudah diuji B19, PHP upgrade live belum |
| B20.3 | Review owner terhadap fungsi dan tampilan pada lingkungan test | Menunggu review owner |
| B20.4 | Freeze kandidat, hasil acceptance dan keputusan go/no-go | Menunggu gate tersisa |

## Review owner

Gunakan pusat test HTTPS yang sudah dipasang dan akun owner pada catatan privat. Kredensial tidak dimasukkan ke dokumen Git. Perubahan dashboard B20 harus dipasang pada lingkungan test sebelum dianggap telah direview publik.

1. Login, periksa navigasi desktop/mobile, lalu logout. Periksa pesan salah login dan koneksi pusat terputus.
2. Pada domain test, periksa verifikasi, mode lokal/API, status probe, bootstrap serta tutorial CLI dan ZIP. Setup PHP aktif harus tetap terkunci.
3. Buat link test, publish, buka redirect; edit URL tujuan sebagai draft dan pastikan URL aktif berubah hanya setelah publish. Domain/slug link yang sama tetap dipertahankan.
4. Periksa aturan global/domain/link, negara, device, OS dan browser. Gunakan simulator sebelum publish; jangan mengaktifkan enforce pada seluruh domain hanya untuk review.
5. Periksa log/filter dan analytics setelah klik test; bedakan block, fallback dan redirect serta waktu sinkronisasi Go.
6. Navigasikan dengan Tab/Shift+Tab/Enter/Escape. Fokus dialog harus tetap berada pada kontrol dialog dan kembali ke pemicunya setelah tutup.

Catat halaman, langkah, hasil yang diharapkan dan hasil aktual untuk setiap temuan. Gunakan slug khusus review agar tidak mengubah fixture kapasitas B19. Data test tidak otomatis dibawa sebagai data production.

## Upgrade dan rollback

**Pusat:** catat commit/build, versi migrasi, checksum artifact dan konfigurasi service; buat backup database yang diuji restore serta salinan privat environment/signing key. Siapkan binary/dashboard sebelumnya. Periksa kompatibilitas schema sebelum memasang kandidat. Deploy pada test terlebih dahulu, cek health, login, publish, redirect dan accounting. Jika gagal, hentikan rollout dan kembalikan binary/dashboard yang kompatibel; rollback binary tidak otomatis menurunkan schema. Restore database hanya sebagai pemulihan terencana karena dapat kehilangan klik setelah snapshot. Ukur restart nyata; uji freeze 10 detik B19 bukan bukti cold restart 10 detik.

**Go:** gunakan `agent status`, `agent update` dan `agent rollback` pada [tutorial CLI](INSTALL_AGENT.md). Verifikasi signed manifest dan origin/key yang dipin; pertahankan config, credential, queue, MMDB dan image sebelumnya. Jangan menghapus volume untuk upgrade. Update/rollback ARM live sudah lulus B19; mengulangnya diperlukan jika kandidat membawa perubahan runtime.

**PHP:** ikuti [prosedur update ZIP](INSTALL_PHP.md#update-dan-rollback-paket-php). Update source berbeda dari enrollment/rotasi credential. PHP tidak mempunyai auto-updater. Rilis baru harus kompatibel dengan API/config sebelum mengganti source, dan harus diuji pada kedua runtime web hosting.

## Gate sebelum production

| Gate | Bukti / pekerjaan tersisa |
|---|---|
| Fungsi dan instalasi publik | Go ARM, PHP aaPanel 8.4 dan cPanel 8.2 lulus B19 |
| Kapasitas target | Go 300 RPS/15 menit lulus; PHP/API 100–300 belum memenuhi acceptance. Perbaiki pengukuran dan bottleneck, lalu uji ulang workload total yang ditargetkan |
| UI dan aksesibilitas | Review keyboard/responsive otomatis serta owner; audit kontras dan assistive technology belum menyeluruh |
| Upgrade/rollback | Go live lulus; kandidat pusat/PHP serta kompatibilitas migrasi masih perlu diuji |
| Operasi production | Tentukan path/domain/service final, aktifkan MFA owner, GeoLite updater berlisensi, backup terjadwal/off-host dan pemantauan sesuai [OPERATIONS.md](OPERATIONS.md) |
| Recovery | Restore database snapshot lulus; rebuild/cold restart/cutover penuh belum diuji |
| Artifact | Freeze commit dan versi immutable, checksum/signature, catatan perubahan, batas platform yang benar-benar diuji |

Keputusan saat ini: **belum go production** untuk target seluruh jalur 100–300 klik/detik. Owner dapat mereview B20 tanpa menunggu tuning selesai. Pemasangan konfigurasi production dilakukan setelah review dan gate yang sesuai scope rilis terpenuhi.

## Verifikasi B20

11 Oktober 2026: build TypeScript/Vite lulus dan 69 operasi OpenAPI valid. Suite UI demo **30/30 lulus**, termasuk skip navigation, Tab/Shift+Tab dalam dialog, Escape, pengembalian fokus dan pemeriksaan overflow halaman utama pada lebar 320 px. Suite API/PostgreSQL disposable **26/26 lulus** (13 desktop + 13 mobile), meliputi login/expiry, draft/revision, rules/publish/rollback, analytics, paket PHP dan MFA. Ini tidak menggantikan acceptance HTTPS hosting nyata atau audit screen reader/kontras.

Temuan: Tab pada batas kontrol dialog dapat meninggalkan fokus di browser. Komponen dialog bersama kini melingkarkan fokus dan memulihkan pemicu saat tutup; ID judul unik per dialog. Folder hasil demo/live dipisahkan agar dua suite tidak menghapus artifact satu sama lain.

Dashboard build B20 dipasang pada **pusat test** tanpa restart backend/Nginx. File index diganti atomik setelah assets baru tersedia; assets lama tetap ada untuk browser yang masih membuka halaman lama. Salinan dashboard sebelumnya tersedia pada `/opt/shortlink-test/dashboard-before-b20`. Deployment ini bukan cutover production, perubahan schema atau rilis runtime Go/PHP baru.

## Review owner: revisi lokal

Setelah memeriksa dashboard publik, owner meminta kembali ke dev lokal untuk menyederhanakan alur link dan aturan. [LINK_ROUTING.md](LINK_ROUTING.md) menjadi acuan revisi. Dashboard yang dipasang pada pusat test sebelumnya belum memuat revisi fallback/audience ini; bukti publik B19 tidak dianggap sebagai acceptance fitur baru.

Revisi ProxyCheck tersedia di dev lokal: key dashboard terenkripsi, lookup pusat dengan cache/budget dan pilihan proxy/unknown fallback per link. [Spesifikasi dan hasil test](PROXYCHECK.md). Koneksi dengan key akun nyata dan acceptance public/kapasitas belum dilakukan; VPS tetap memakai kandidat sebelumnya.

Revisi lokal 12 Oktober 2026: root dan slug tidak ditemukan memakai URL pembuka domain (302); kosong menghasilkan 503. Detail domain menyediakan editor opening, daftar link domain dan key manage sekali pakai. Go/PHP manage scoped-domain memakai hash sesi di pusat, cookie aman, Origin/CSRF; [AGENT_MANAGE.md](AGENT_MANAGE.md). PHP kandidat v0.20.1-b20, Go dev routingVersion 3. Belum dipasang ke VPS/hosting publik.

Revisi dev 13 Oktober 2026: provider IP dapat dipilih antara ProxyCheck dan BEON-IPQuality milik owner. URL HTTPS dan key client dapat diganti dari Sistem; key terenkripsi hanya di pusat. Flag proxy/VPN/Tor/botnet mengarahkan link yang mengaktifkan fitur ke fallback, crawler tetap memakai deteksi UA. Unknown mengikuti pilihan link. Konfigurasi dan smoke API nyata hanya dilakukan di dev lokal; gate kapasitas dan production tetap terbuka. [Detail integrasi BEON](IP_QUALITY.md).

Revisi owner: fokus BEON-IPQuality saja, provider eksternal ProxyCheck dihapus. Delapan flag boolean dapat dipilih per shortlink; flag terpilih true menuju fallback. Datacenter yang tidak dipilih tidak menolak akses. Simulator dan runtime Go/API memakai pilihan yang sama; Go memerlukan routingVersion 4 untuk daftar eksplisit. Pemeriksaan koneksi publik ARM/PHP aaPanel/cPanel online/ready dan applied revision cocok pada 13 Oktober 2026; kode flag baru tetap hanya dev lokal. [Detail BEON](IP_QUALITY.md).

Persiapan staging/production: prototype UI dipisahkan ke mode prototype/port 5174 dan build dist-prototype. Dashboard nyata lokal/staging/production mengabaikan demo=1; bundle real tidak membawa fixture atau link preview. Build staging dist-staging dan production dist terpisah, contoh environment database/path/hostname terpisah tersedia. Pusat aaPanel test saat ini dikelola systemd (binary Go + dashboard statis Nginx), bukan Node/Go Project panel. Cutover publik menunggu penetapan hostname dan penyelesaian gate rilis; [ENVIRONMENTS.md](ENVIRONMENTS.md).

Cutover 13 Oktober 2026 disetujui owner dengan mempertahankan data/agent: production dashort telah memakai build tanpa preview dan backend baru; staging terpisah tetap aktif. Restore/migrasi 28 tabel dan fingerprint identitas lulus. Go ARM v0.20.4 serta PHP aaPanel/cPanel v0.20.1-b20 terpasang tanpa mengganti credential; redirect/probe ketiganya lulus. Lookup BEON final known. Gate kapasitas/operasi tetap terbuka; [catatan cutover dan rollback](ENVIRONMENTS.md).
