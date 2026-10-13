# B19 — hasil pengujian publik

Pengujian 11 Oktober 2026 melengkapi [hasil lokal](CAPACITY_B19.md). **Instalasi/fungsi VPS ARM, HTTPS publik, PHP aaPanel dan cPanel lulus. Go 300 RPS/15 menit lulus. Gate kapasitas PHP/API belum lulus; B19 nyata belum ditutup. B20 dimulai untuk review rilis atas arahan owner; gate ini tetap terbuka.** Angka latency publik mencakup jaringan workstation, bukan waktu pemrosesan server. Domain pada laporan Git memakai contoh peran; alamat dan akses aktual tetap pada catatan operator privat.

## Lingkungan dan deployment

| Peran | Domain | Lingkungan yang diuji |
|---|---|---|
| Pusat | `control.example.com` | aaPanel/Nginx, Ubuntu 26.04.1 ARM64, Standard_D2ps_v6, 2 vCPU/sekitar 8 GiB, PostgreSQL 18.0 existing |
| Agent CLI | `arm-agent.example.com` | Ubuntu 26.04.1 ARM64 bersih, Standard_D2pls_v6, 2 vCPU/sekitar 4 GiB; Docker tidak ada sebelum installer |
| Agent PHP aaPanel | `php-aapanel.example.com` | Nginx/PHP-FPM 8.4, pada VPS pusat |
| Agent PHP cPanel | `php-cpanel.example.com` | LiteSpeed/shared hosting terpisah, PHP **web dan CLI 8.2.34** |

Pusat memakai binary ARM64 dari toolchain project Go 1.27.1, berjalan native melalui systemd. Go 1.25 pada daftar aaPanel tidak dipakai untuk build. Dashboard merupakan build React statis; Node/Vite tidak dijalankan sebagai server publik. Login HTTPS Chromium desktop/mobile lulus tanpa error JavaScript atau horizontal overflow.

Path pusat: binary `/opt/shortlink-test/bin/control`, dashboard `/opt/shortlink-test/dashboard`, data `/var/lib/shortlink-test`, env `/etc/shortlink-test/control.env` (root:root 0600). User service `shortlink-test`; API bind `127.0.0.1:8086`; PostgreSQL bind loopback. Database/role baru `shortlink_b19`, terpisah dari database/owner lokal. Pusat test mempunyai signing key sendiri: gunakan public key bootstrap dari **pusat ini**, bukan file key pusat lokal. Sertifikat pusat memakai Certbot/webroot, renewal timer dan reload hook Nginx.

Database GeoLite valid disalin ke staging pusat dan dipublish dengan key pusat; Go agent mengunduh MMDB melalui API terautentikasi. PHP menggunakan GeoLite pusat. **geoipupdate/license/jadwal download rutin belum dipasang** sesuai arahan untuk fokus pengujian dahulu. Env, token, key, MMDB, dump dan screenshot tidak masuk Git.

## Instalasi dan fungsi

- CLI memasang Docker **29.9.0 ARM64** dan Compose **5.6.0** langsung dari VPS bersih. Tidak melakukan pemasangan Docker terpisah sebelum installer. Ubuntu 26.04 didukung [Docker resmi](https://docs.docker.com/engine/install/ubuntu/); allowlist installer dan test ditambah untuk `resolute`.
- Rilis immutable baru `v0.19.1-b19` menambahkan dukungan distro. `v0.19.2-b19` memakai kode sama dengan metadata versi berbeda untuk uji update. Signed manifest/checksum binary ARM dan ZIP PHP diverifikasi dari source tepercaya sebelum dijalankan. Enrollment token diberikan lewat terminal tersembunyi.
- Rerun memakai identitas/data lama tanpa token baru. Restart ready kembali. Update 19.1 → 19.2 dan rollback → 19.1 lulus. Rilis current pusat 19.2; agent tetap 19.1 setelah uji rollback. Paket PHP tetap immutable `v0.17.0-b17`.
- PHP kedua hosting lulus self-preflight, browser enrollment/activation, dan setup tetap terkunci setelah reload. PHP cURL/OpenSSL/fsync/flock dan private writable dikonfirmasi pada runtime web, termasuk minimum PHP 8.2.
- Ketiga agent lulus authenticated public readiness: DNS, TLS/proof, binding domain, revision dan GeoLite yang relevan. HTTP dialihkan ke HTTPS (301 pusat/PHP, 308 Caddy).
- Redirect, draft destination yang tidak memengaruhi link aktif, publish destination baru dan slug tidak dikenal lulus pada ketiga jalur. Rule iPhone/mobile/iOS/Safari block lulus; Android/mobile tetap mengikuti published rules. Event iPhone tercatat brand `Apple`, model `iPhone`, OS `ios`, browser `safari`, country `ID`.
- Header `X-Forwarded-For` palsu tidak mengubah visitor menjadi US. Request dari hosting dengan GeoLite country US menghasilkan fallback `https://example.org/b19-us` pada ketiga jalur. Rules hanya diterapkan pada slug test `b19-device`.

## Konfigurasi hosting

aaPanel: public root `/www/wwwroot/php-aapanel.example.com/public`, private `/var/lib/shortlink-php-aapanel/private`, owner `www-data`, directory 0700/file 0600. FastCGI menetapkan `SHORTLINK_PRIVATE_DIR` dan `PHP_ADMIN_VALUE open_basedir`. Pool khusus domain uji `shortlink-b19` di `127.0.0.1:9001`, max 24 worker; pool `www` situs lain tidak diubah. Location exact readiness ditetapkan sebelum penolakan dotfile. Vhost sebelumnya dibackup privat.

cPanel: document root existing `/home/account/php-cpanel.example.com` berisi file **public** ZIP; seluruh source/config private di `/home/account/shortlink-private/phpagent`, directory 0700/file 0600. `.htaccess` menetapkan `SetEnv SHORTLINK_PRIVATE_DIR`. UAPI `SubDomain changedocroot` memakai `docroot`; perubahan metadata ke `/public` sempat belum terlihat pada TLS vhost, sehingga root dikembalikan ke existing dan digunakan layout custom private yang didukung. Secret tidak diamankan hanya dengan deny `.htaccess`. Probe PHP web sementara sudah dihapus.

Satu TXT cPanel yang tersimpan sebagai `verification=...` diperbaiki melalui UAPI `DNS mass_edit_zone`, dengan serial check dan backup zona privat; TTL menjadi 300, record lain dipertahankan. Query TCP authoritative/DoH membaca nilai baru; resolver Azure masih mempunyai cache TTL lama 14400. Verifikasi normal dilakukan dengan resolver Cloudflare sementara, lalu DNS VPS dikembalikan ke `168.63.129.16`. Tidak melewati TXT dengan mengubah verified langsung dalam database.

## Hasil beban publik

| Jalur | Beban/durasi | Hasil | p95 HTTP publik | Gate error/drop <0,1% |
|---|---|---|---|---|
| Go ARM | 50 RPS / 60 detik | 3.000 redirect, error/drop 0 | 98,57 ms | Lulus diagnostic |
| Go ARM | 300 RPS / 60 detik | 18.000 redirect, error/drop 0 | 98,44 ms | Lulus diagnostic |
| **Go ARM** | **300 RPS / 15 menit** | **270.000 redirect, error/drop 0** | **99,88 ms** | **Lulus** |
| PHP aaPanel | 50 RPS / 60 detik | 3.000 redirect, error/drop 0 | 131,27 ms | Lulus diagnostic |
| PHP aaPanel + Go300 | 50 RPS / 5 menit | 15.000 redirect, error/drop 0 | 247,99 ms | Lulus diagnostic gabungan |
| PHP aaPanel + Go300 | 100 RPS / 5 menit | 29.398 redirect, 7 HTTP503, 595 jadwal terlewat / 30.000 | 1.147,10 ms | Gagal (2,01%) |
| PHP aaPanel, tanpa Go load | 100 RPS / 5 menit | 29.753 redirect, tanpa HTTP error, 247 jadwal terlewat / 30.000 | 1.024,69 ms | Gagal (0,823%) |
| PHP aaPanel pool24 | 300 RPS / 60 detik | 7.647 redirect, 10.353 jadwal terlewat / 18.000 | 874,71 ms | Gagal (57,52%) |
| cPanel, pusat stress gabungan | 10 RPS / 60 detik | 598 redirect, 2 HTTP503 / 600 | 941,84–1.085,65 ms | Gagal (0,333%) |
| cPanel, tanpa Go load | 10 RPS / 60 detik | 600 redirect, error/drop 0 | 742,35 ms | Lulus diagnostic |

Saat Go soak, sebagian waktu bersamaan dengan PHP100/PHP50 dan sample cPanel. Saat cPanel terakhir, PHP aaPanel100 juga berjalan. Tujuan redirect diperiksa **tanpa mengikuti redirect**, sehingga destination website tidak menerima load.

Generator memakai 96 worker dan maksimum 128 pekerjaan outstanding; `generatorDrops` adalah request terjadwal yang belum dikirim, bukan respons error server. Ini tetap menggagalkan acceptance jumlah request; jangan menyatakan kapasitas server 100/300 lulus hanya dari HTTP302 yang selesai. Sebelum menyimpulkan kapasitas maksimum PHP, lakukan pengukuran ulang dengan generator yang sesuai latency/antrean. PHP/API 300 juga gagal pada diagnostic native terpisah berikut, sehingga batas tidak hanya berasal dari generator publik.

## Diagnostic native API dan batasnya

Fixture API pada database terpisah `shortlink_b19_capacity` memakai disk/PG nyata, schema test otomatis dibersihkan. 300 RPS/30 detik: 7.163 HTTP200 dan 1.837 `decision_busy` / 9.000 (20,41%), p95 loopback 129,47 ms; events/clicks 7.163 cocok, pending habis. p95 pool wait 34,74 ms, event lock wait 43,34 ms, commit 4,32 ms, write batch 1,15 ms. **Gate API 300 pada VPS ini gagal.** Fixture memakai GeoLite synthetic, bukan measurement keputusan public PHP.

Sampel pusat saat Go300+PHP100 menunjukkan CPU hampir penuh. `pg_test_fsync` pada file disposable, bukan WAL, mengukur fdatasync satu write 8 KiB sekitar 1,879 ms/op. fsync/synchronous_commit tetap on; PostgreSQL global tidak diubah. Eksperimen pipeline query baca diuji pada fixture/regresi, tidak memberi peningkatan stabil dan mengalami worker timeout; perubahan source dibatalkan, tidak dipasang ke pusat aktif.

Limit account cPanel yang dibaca dari UAPI: CPU 200%, EP 30, RAM 2 GiB, IOPS 1024, I/O 50 MiB/s, proses 200. Ini limit account, bukan jaminan throughput. Status fungsional hosting tidak sama dengan kelulusan kapasitas 100–300 RPS.

## Outage dan antrean

Dua uji pusat tidak merespons dilakukan melalui SIGSTOP/SIGCONT dengan timer pemulihan independen **10 detik**. Uji terakhir mencakup kedua PHP: masing-masing 503 tanpa Location dan kembali redirect setelah pulih; lima request Go selama outage tetap redirect. Ini bukan cold restart atau database outage.

Native ARM queue lulus `SQLITE_FULL`, SIGKILL sesudah event durable, dan syscall ENOSPC/recovery. Container disposable tanpa network/capabilities, filesystem read-only, tmpfs khusus 1 MiB; volume agent aktif dan disk VPS tidak diisi. Wrapper ENOSPC kini memilih arsitektur Docker dan menerima `SHORTLINK_QUEUE_TEST_IMAGE` untuk image cached yang cocok; wrapper lokal juga lulus.

Accounting sesudah semua load dan outage: **388.790 events = unique IDs = event keys = jumlah aggregate minute = jumlah aggregate hour**, pending nol. Counter setiap link cocok dengan events redirect/fallback; block tetap dihitung di log/aggregate. `b19-load` Go mempunyai 291.001 klik (seluruh soak/smoke + satu warm-up), tanpa dropped/write errors dan queue habis. Pemeriksaan Android sesudah snapshot menambah tiga event; backup di bawah tetap merupakan snapshot sebelumnya yang konsisten.

## Backup/restore nyata

Snapshot PostgreSQL test 388.790 event: archive **65.721.091 byte**, backup **5,17 detik**, restore **10,37 detik** ke database baru `shortlink_b19_restore`. Fingerprint data **seluruh 27 tabel** cocok dengan exported snapshot source, termasuk owner/password hash, signing seed, agents dan sessions. Target/source aktif tidak ditukar; ini bukan measurement RTO cutover seluruh VPS.

Backup remote `/var/backups/shortlink-test/b19-after-public-tests`, directory 0700/file 0600. Salinan off-host berada dalam storage operator privat `~/.local/state/shortlinkSimple/remote/native-backup-b19`; SHA256 diperiksa. MMDB, environment dan config agent tidak masuk dump, sehingga tetap perlu disuplai terpisah pada pemulihan deployment.

## Gate berikutnya

Pekerjaan kapasitas PHP/API B19 tetap terbuka saat review B20: generator/latency, CPU/DB/lock, dan workload campuran sesuai target total 100–300 RPS. Jangan memindahkan hasil tmpfs developer sebagai kapasitas VPS. Full baseline publik 50 RPS/30 menit dan stress 600 RPS/5 menit belum diulang; hasil full lokal tetap terpisah.

Ubuntu 22.04/24.04/Debian dan VPS amd64 bersih belum diuji native. Konflik port/gagal download live, full VPS rebuild/cold restart, maintenance rotation/migration live, production MFA/setup/updater/scheduled backup dan konfigurasi aaPanel produksi tetap tahap berikutnya. Tidak menutup gate yang tidak dijalankan hanya karena instalasi berhasil.
