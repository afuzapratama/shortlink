# Backend pusat — B03–B05

Backend di `services/control` menggunakan Go 1.27.1, PostgreSQL, [pgx/v5](https://pkg.go.dev/github.com/jackc/pgx/v5/pgxpool) dan [Argon2id](https://github.com/golang/crypto/blob/master/argon2/argon2.go). Versi dependency dikunci pada go.mod/go.sum. Database tidak berisi fixture dashboard.

## Menjalankan lokal

Prasyarat: Go sesuai go.mod, Docker Compose untuk database, serta Bash untuk contoh prompt password di bawah. API berjalan pada host; Compose B03 hanya menjalankan PostgreSQL. Updater GeoLite tetap memakai compose.geoip.yml secara terpisah.

Dari root project:

```sh
cp deploy/.env.control.example deploy/.env.control
# Edit password database dan DATABASE_URL agar sesuai; URL-encode password pada URL.
docker compose --env-file deploy/.env.control -f deploy/compose.control.yml up -d --wait
```

Buka Bash (`bash` bila shell saat ini zsh), lalu dari root:

```bash
set -a
source deploy/.env.control
set +a
cd services/control
go run ./cmd/control migrate
read -rp 'Email owner: ' OWNER_EMAIL
read -rsp 'Password owner (12–256 byte): ' OWNER_PASSWORD
printf '\n'
export OWNER_EMAIL OWNER_PASSWORD
go run ./cmd/control bootstrap-owner
unset OWNER_EMAIL OWNER_PASSWORD
go run ./cmd/control serve
```

Bootstrap hanya melalui CLI, tanpa endpoint HTTP pembuatan akun. Password tidak dimasukkan dalam argumen/history atau file contoh. Password di-hash Argon2id dengan salt acak, bukan disimpan langsung. Constraint singleton PostgreSQL menjamin hanya satu owner walaupun bootstrap dijalankan bersamaan. Bootstrap ulang gagal dan tidak mengganti password owner yang sudah ada.

Migration dibundel dalam binary, dijalankan eksplisit dengan transaksi dan advisory lock. Rerun aman; checksum migration yang sudah diterapkan harus tetap sama. Server menolak startup bila migrasi/owner belum siap. Perubahan schema berikutnya memakai file migration baru.

Untuk menghentikan database lokal tanpa menghapus volume:

```sh
docker compose --env-file deploy/.env.control -f deploy/compose.control.yml stop
```

## Endpoint yang tersedia

| Endpoint | Perilaku saat ini |
| --- | --- |
| GET /health/live | Liveness minimal, tanpa diagnosis private |
| GET /health/ready | Koneksi database, migration awal dan owner tersedia; bukan status agent |
| POST /v1/auth/login | JSON email/password, Origin persis sesuai konfigurasi; session cookie dan header X-CSRF-Token |
| GET /v1/auth/session | Owner terautentikasi dan csrfToken untuk mutation |
| POST /v1/auth/logout | Origin + CSRF wajib, session dicabut durable dan cookie dihapus |
| GET/POST /v1/admin/domains | List dengan cursor / buat domain draft |
| GET /v1/admin/domains/{id} | Rincian domain |
| GET/POST /v1/admin/links | List cursor + domainId / buat link draft |
| GET/PATCH /v1/admin/links/{id} | Baca link / simpan draft dengan expectedRevision |
| Operasi admin lainnya | Anonim 401; setelah guard, operasi belum tersedia menghasilkan 501 |

Login berhasil mengembalikan status authenticated dan challengeId null. Session cookie `shortlink_session` memakai HttpOnly, SameSite=Strict, Path=/, tanpa Domain. Session acak 32 byte disimpan hanya sebagai hash SHA-256 di PostgreSQL; lifetime absolut default 12 jam (configurable 5 menit–24 jam). Login ulang mengganti session cookie yang disertakan pada request; logout/expiry membuat session lama tidak valid. CSRF dihasilkan dari token session dengan HMAC dan diambil frontend melalui endpoint session; tidak membutuhkan localStorage. B04 memakai koneksi same-origin/proxy untuk dashboard, tanpa CORS terbuka.

Mode development hanya menerima listener IP loopback dan origin localhost/loopback; cookie boleh HTTP lokal. Production mewajibkan origin HTTPS dan cookie Secure; TLS/reverse proxy dikelola saat deployment. Jangan mengganti mode development agar bind publik bisa berjalan. Di belakang reverse proxy, rate limit B03 memakai socket IP proxy karena forwarded header belum dipercaya; konfigurasi trusted proxies disiapkan pada batch terkait.

Rate limit login durable: 30 request per socket IP dan 10 per email ternormalisasi per jendela 15 menit, ditambah maksimal dua pekerjaan hashing bersamaan per proses. Salah email/password memakai respons identik dan dummy hash. Kegagalan database menghasilkan 503 dan tidak meloloskan autentikasi. Cleanup session expired/bucket lama berjalan setiap lima menit. Body login dibatasi 8 KiB, JSON tambahan/field tak dikenal ditolak, dan log tidak mencatat password, cookie, CSRF, query atau body. Semua error memakai code/message/requestId; 501 memakai not_implemented.

B18 menyediakan TOTP/recovery dan provisioning di halaman Keamanan. Password hanya menghasilkan challenge pada akun MFA; sesi harus mempunyai flag MFA verified. Aktivasi membatalkan sesi lain. Lihat [SECURITY.md](SECURITY.md) untuk enrollment, recovery dan maintenance agent; gate produksi tetap meliputi HTTPS publik dan kapasitas.

## Verifikasi

Dari root:

```sh
./scripts/test-control.sh
```

Script memakai PostgreSQL lokal sementara, hanya Unix socket pada direktori private, menjalankan `go test -race -count=1 ./...`, lalu membersihkan instance miliknya. Memerlukan initdb/pg_ctl di PATH dan user non-root. Tidak memakai database development. Alternatif: set CONTROL_TEST_DATABASE_URL ke URL PostgreSQL database khusus pengujian dengan izin membuat schema; setiap test membuat schema unik lalu menghapus hanya schema tersebut. Tanpa URL tersebut, menjalankan `go test ./...` langsung akan melewati integration tests dengan pesan skip.

Cakupan: bootstrap concurrent/sekali pakai, migration rerun/checksum, session hash/expiry/rotation/logout dan persistensi, CSRF/origin, batas JSON, isolasi admin/agent, MFA fail-closed, rate limit atomik/durable dan spoofed forwarded IP, serta database unavailable tanpa meloloskan login/session. Smoke test membangun binary, menjalankan CLI migrate/bootstrap/serve, login/session lewat HTTP loopback, lalu menghentikan server secara graceful. Compose PostgreSQL 18.6 telah diuji terpisah sampai healthy dan query berhasil pada project/volume sementara yang dibersihkan setelah uji.

```sh
cd services/control
go vet ./...
go build -o /tmp/shortlink-control ./cmd/control
```

## Dashboard B04

Setelah API berjalan, jalankan `npm run dev -- --port 5173 --strictPort` dari root di terminal lain. Buka http://127.0.0.1:5173; CONTROL_ORIGIN dalam env contoh sekarang adalah origin dashboard ini, bukan port API. Untuk env lama, ubah nilai Origin lalu restart API. [Proxy Vite](https://vite.dev/config/server-options.html#server-proxy) meneruskan /v1 dan /health tanpa membuka CORS. CONTROL_API_TARGET dapat mengganti target default http://127.0.0.1:8080; hanya dipakai proses Vite, bukan credential frontend. Build static produksi memerlukan reverse proxy same-origin yang sesuai; proxy Vite hanya untuk development.

Login/session, logout, domain list/detail/create, serta link list/create/edit memakai API nyata. Ringkasan berisi resource tersimpan dan agregat traffic aktual B14; tidak mengisi analytics dengan angka contoh. Credential/password/CSRF tidak disimpan ke localStorage. Preview fitur lanjutan terpisah lewat ?demo=1; tidak ada fallback otomatis ke fixture saat API gagal.

Domain dibuat sebagai draft dengan koneksi unknown, appliedRevision null, dan capabilities false hingga runtime benar-benar melapor. Hostname dinormalisasi, URL/path/port/IP dan nama lokal/reserved ditolak; verifikasi DNS TXT sudah B05; probe HTTPS/routing tetap B11. Slug unik per domain; tujuan HTTP/HTTPS tanpa credential dan loop langsung ke domain yang sama. Tidak ada request jaringan ke tujuan saat menyimpan. ID dan slug otomatis dibuat server bila slug kosong. List API memakai cursor + limit 1–100; dashboard mengambil semua halaman management (traffic pagination server B14).

Revision link adalah versi resource untuk concurrency, berbeda dari desired/applied revision snapshot domain. PATCH selalu meminta expectedRevision dan menambah versi resource; draft yang belum published memperbarui destination. Untuk link published, destination/ID/slug/clicks dipertahankan dan URL baru masuk draftDestination, atau pending draft dihapus bila tujuan kembali sama. Domain/slug published tidak dapat diganti. Snapshot enrollment sudah tersedia, publish link belum tersedia; redirect belum dilayani dari link draft. Field fallback/expiry/query allowlist akan diperluas saat implementasi fiturnya; saat ini ditolak sebagai field tak dikenal, tidak diam-diam diabaikan.

Uji browser nyata:

```sh
npm run test:live
```

Script membuat PostgreSQL private sementara, migrate/bootstrap, API di port 8085 dan Vite port 5175, lalu menjalankan Playwright desktop/mobile dan membersihkan semuanya. Port harus kosong; tidak menggunakan database development atau credentials owner sebenarnya. Mencakup persistensi setelah refresh, edit, duplicate hostname, konflik tab lain, session expiry/logout, empty state, detail 404, dan retry error tanpa contoh sintetis menggantikan data pusat.

Kelompok desktop/mobile dijalankan berurutan. Penghitung rate limit direset hanya dalam database sementara di antara kelompok agar keduanya menguji login secara independen; batas autentikasi aplikasi tetap sama.

Runtime agent Go, snapshot lokal, host/slug dan redirect dasar tersedia pada B06. Distribusi GeoLite tersedia B07; batch berikutnya B08 untuk aturan/publish. Worker, installer dan 2FA produksi mengikuti batch masing-masing.

## B05 — verifikasi dan enrollment

Pada detail domain, klik Tampilkan instruksi DNS, buat record TXT _shortlink.<hostname> dengan nilai yang diberikan, lalu Periksa DNS sekarang. Record challenge berlaku 24 jam. Pemeriksaan memakai resolver DNS server pusat; propagation belum selesai akan memberi status fail tanpa mengizinkan token. Berhasil menjadi Menunggu instalasi. Pada lokal, DNS domain nyata tetap perlu dikelola pemilik; tidak ada tombol bypass verifikasi.

Buat token instalasi setelah domain verified. Metode terikat local→cli atau api→php_zip. Token 32 byte berlaku 30 menit dan ditampilkan hanya sekali; token baru mencabut unused token sebelumnya. Installation ID adalah metadata provisioning untuk paket/perintah installer nanti. Installer/ZIP masih unreleased dan belum dapat didownload. URL pusat di bootstrap berasal dari CONTROL_ORIGIN; pada development 127.0.0.1 hanya untuk pengujian lokal.

POST /v1/agents/enroll menerima token, installationId, installationProof (32 random bytes canonical base64url), hostname, method dan version. Agent menghasilkan/stores proof sebelum request pertama. Request pertama ditukar atomik; retry identik mendapatkan credential/agent sama hingga 10 menit. Token/proof hanya disimpan hash; credential derived dengan HMAC atas binding memakai private seed pusat, sehingga hasil recovery tidak perlu menyimpan credential plaintext. Recovery tidak berlaku setelah revoke/proof mismatch/window habis. Satu domain hanya satu agent aktif; revoke dahulu untuk enrollment pengganti. Enrollment menjadi preparing dan config revision naik; domain belum siap redirect.

GET /v1/agents/config dengan bearer credential mengembalikan snapshot signed + ETag. [Ed25519](https://pkg.go.dev/crypto/ed25519) menandatangani byte JSON dalam signedPayload (base64url tanpa padding); verifikasi menggunakan manifestVerificationKey dari bootstrap trusted. Agent harus mengevaluasi decoded signedPayload, bukan menyalin envelope unsigned. Lease 24 jam, renew pada sisa <12 jam. If-None-Match sesuai memberi 304 tanpa body dan tanpa lease baru. Snapshot awal mengecualikan link draft/pending destination, rules kosong dan geo/feed null sampai batch terkait.

POST /v1/agents/heartbeat hanya CLI, applied revision tidak boleh menurun/melebihi desired. Version, queue counters dan geo/feed metadata dicatat; acknowledgement audited sekali per perubahan revision. Heartbeat tidak mengklaim localGeo capability/readiness ready. PHP memakai aktivitas/probe pada batch terkait. CLI online dibaca offline bila last heartbeat lebih dari 90 detik.

Owner dapat melihat GET /v1/admin/domains/{id}/agent dan /audit (50 terbaru), serta POST /v1/admin/agents/{agentId}/revoke. Revoke langsung menolak akses pusat/recovery credential lama; local snapshot offline masih mengikuti lease. Revoke idempotent, termasuk ketika agent pengganti sudah ada. Audit transaksi tidak mengandung token/proof/credential. Seed signing/recovery disimpan pada control_keys; backup/restore harus mencakup tabel ini. Rotasi key/trust B18 belum tersedia.

Migration 003 diterapkan pada database lokal tanpa reset owner/domain/link dan API direstart. Untuk menjalankan manual pada env lama, migrate dahulu sebelum serve. Backend lokal saat ini 127.0.0.1:8086, dashboard 127.0.0.1:5173.

Verifikasi B05: PostgreSQL race tests termasuk enrollment concurrent/recovery/expiry/revoke, signature/tampering, lease/304/renew dan ack; DNS resolver positive/negative dikontrol dalam test backend. Dua belas E2E desktop/mobile lulus; kegagalan DNS browser memakai respons terkontrol. Kepemilikan DNS domain produksi dan runtime agent belum diuji/deploy.

## B06 — runtime agent dan pengujian lokal

Runtime Go agent, demo tanpa DNS dan konfigurasi private dijelaskan di [AGENT.md](AGENT.md). `./scripts/test-agent.sh` menjalankan vet dan race tests. `./scripts/test-control.sh` juga menjalankan binary agent terhadap API pusat/PostgreSQL test terisolasi, termasuk snapshot signed, redirect, heartbeat revision dan outage. Agent tidak mengubah draft menjadi published dan tidak menandai domain siap produksi. Pusat lokal pengguna tetap berjalan di 8086, dashboard di 5173. Batch berikutnya B08 untuk mesin aturan/publish.

## B07 — GeoLite publisher/assets dan status Sistem

Panduan [GEOIP.md](GEOIP.md). GEOIP_ROOT host absolut atau mount container /var/lib/shortlink/geoip; GEOIP_PUBLISH_ENABLED=true menjalankan publisher bersama API, false untuk API read-only dengan proses watch-geoip terpisah. CLI publish-geoip/watch-geoip memakai seed pusat dari PostgreSQL. Tidak ada migrasi schema baru. GET /v1/assets/geoip/manifest dan releases/{release}/{edition} memakai owner session atau credential agent; GET /v1/admin/system/geoip hanya owner. Kontrak sekarang 40 operations.

Agent melaporkan rilis MMDB aktif melalui heartbeat; halaman Sistem menampilkan status pusat dan agent stale. Test binary pusat/agent dengan GeoLite required, tiga database sintetis, stream terotorisasi, aktivasi/restart/outage/revoke serta race lulus. Browser berjumlah 14 test desktop/mobile. Docker image pusat berhasil dibuild; Compose profiles divalidasi. GeoLite asli belum tersedia; layanan lokal menampilkan Menunggu database setelah GEOIP_ROOT disiapkan.

Database lokal dari /usr/share/GeoIP telah disalin ke data/geoip/staging untuk diagnostik pada 10 Oktober 2026. Lookup IPv4/IPv6 valid, tetapi build November/Desember 2025 ditolak publisher karena freshness. Detail dan inspector ada di [GEOIP.md](GEOIP.md).

Sumber lokal diperbarui dari DatabaseDirectory /mnt/DataDrive/project-beon/BEON-IPQuality/data/mmdb: build Oktober 2026 telah disalin, divalidasi dan dipublish. Halaman Sistem sekarang Rilis tersedia; catatan Menunggu database/build lama di atas merupakan histori.

## B08 — aturan, visitor, publish dan rollback

Migration `004_policy.sql` menambahkan rule_sets, rule_history dan link_history; migration lama tidak diubah. Jalankan migrate sebelum serve binary B08. Dependency lokal packages/policy digunakan control dan agent, serta disalin dalam Dockerfile pusat. Owner, domain, link, seed signing dan release GeoLite yang sudah ada dipertahankan.

Menu Aturan Traffic dan tombol publish/riwayat shortlink memakai API pusat. Simpan draft tidak mengubah konfigurasi published. Publish dan rollback memakai transaksi, optimistic revision, audit serta snapshot signed; global publish mengembalikan revision masing-masing domain dan pending agent IDs. Heartbeat tetap menunjukkan applied terpisah, tanpa mengklaim readiness produksi. Kontrak 47 operations; endpoint yang belum tersedia tetap mengikuti batch.

Rincian alur, batas payload, deteksi visitor dan pengujian ada di [POLICY.md](POLICY.md). Test binary nyata membuktikan redirect, edit/publish/rollback tujuan, block mobile iOS dan redirect iPad, dengan simulator shared core. Backend/PostgreSQL dan agent race tests lulus; 16 live browser test lulus. Data pengujian memakai database sementara, bukan database pengguna.

Lingkungan lokal sudah menjalankan migration 004 dan binary B08 pada 127.0.0.1:8086; dashboard tetap 127.0.0.1:5173. Fingerprint owner/domain/link/signing sebelum-sesudah migration identik. Login owner asli, menu aturan, countries API dan GeoLite Rilis tersedia diverifikasi, lalu logout. Docker pusat shortlink-control:local berhasil dibuild dengan module shared policy; demo terisolasi 8091 tetap tersedia.

## B09 — distribusi agent dan origin bootstrap

AGENT_ASSET_ROOT menunjuk filesystem rilis private, AGENT_RELEASE_INPUT menunjuk output builder untuk CLI publish-agent. Command agent-key mengekspor public verification key, tanpa seed private. Publisher mengambil seed existing dari PostgreSQL, tanpa migration schema baru. API root bisa read-only; image pusat menyertakan module packages/artifacts. Mount contoh ada di compose.geoip.yml profile central-api.

CONTROL_PUBLIC_URL adalah origin API bagi agent, default CONTROL_ORIGIN yang tetap menjadi allowed browser origin. Bootstrap/install-token/configUrl memakai public URL configurable. Lokal diset http://127.0.0.1:8086; dashboard tetap http://127.0.0.1:5173. Production public URL wajib HTTPS. Endpoint /v1/assets/agent/manifest dan releases/{version}/{artifact} sudah operasional untuk owner session atau active CLI credential; token enrollment/PHP credential tidak mendapat akses release Go.

Rilis v0.9.0-b09 sudah dipublish di data/agent-assets dan backend lokal menjalankan B09. Enam file diverifikasi oleh Go serta Python/OpenSSL; artifact version/key bootstrap dari pusat existing. Ownership/seed/GeoLite dipertahankan. Docker pusat berhasil dibuild, image agent dua arch tersedia; Docker amd64 memakai API/PostgreSQL sementara saat integrasi. Tutorial [AGENT_RELEASES.md](AGENT_RELEASES.md).

## B10 — konteks installer dan rilis CLI

POST /v1/agents/install-context menerima token di JSON body, dibatasi 30 request per IP bucket, memvalidasi expiry/revoke/method CLI dan window recovery, lalu mengembalikan installationId serta bootstrap. Token tidak dikonsumsi, proof/credential tidak dikembalikan; enrollment existing tetap memerlukan proof dan bound ID. Tidak ada migration schema baru. Kontrak sekarang 48 operations.

Rilis v0.10.0-b10 memuat CLI dan installerReady true, menggunakan seed signing existing. Older release immutable/capability false tetap terbaca. Backend lokal B10 tetap 8086 dan dashboard 5173; owner/domain/link/key/GeoLite dipertahankan. Test integrasi menggunakan PostgreSQL/GeoLite terisolasi dan runner Docker CLI root hanya pada root test; tidak memasang apt, mengubah service host atau menguji TLS produksi. [INSTALL_AGENT.md](INSTALL_AGENT.md).

## B11 — progress instalasi dan readiness

Jalankan migrate sebelum binary B11: migration 005_readiness wajib untuk health/ready. GET /v1/admin/domains/{id}/installation (owner) mengembalikan Domain, metadata agent tanpa credential/hash, lima prasyarat dan hasil probe sebelumnya. POST /probe (owner + CSRF) memakai fixed HTTPS endpoint /.well-known/shortlink-ready, nonce/proof HMAC dengan purpose request/response yang terpisah. Public resolver harus hanya memberi IP publik; dial dipin ke hasil DNS, sertifikat TLS diverifikasi, redirect/proxy environment ditolak dan payload dibatasi 16 KiB. Rate limit 10/domain/15 menit, dua probe concurrent, timeout delapan detik.

Progress dibaca lagi setelah network dan commit memeriksa revision serta agent aktif; hasil lama tidak memberi Siap saat konfigurasi berubah/revoke. Readiness Siap berlaku maksimal lima menit, membutuhkan heartbeat <=90 detik serta revision sesuai. Tidak ada loop public probing otomatis; polling UI read-only. Audit readiness_probed tidak menyimpan nonce/proof/credential. Tabel domains mendapat probe_checked_at/probe_result nullable. Local API diperbarui pada 8086 dan dashboard 5173. Rilis agent v0.11.1-b11 mendukung protocol; B10 perlu update. Gate VPS owner di [TEST_MATRIX.md](TEST_MATRIX.md).

## Event B12

Migration 006_events wajib; control serve menjalankan worker agregasi otomatis. POST /v1/events/batch kini menerima credential CLI, commit durable/dedup dan binding; GET /v1/admin/system/events menyediakan kesehatan antrean/worker untuk owner. Retensi EVENT_RAW_DAYS=7 dan EVENT_AGGREGATE_DAYS=90. [Detail durabilitas dan operasi](EVENTS.md). Kontrak kini 50 operasi.

## Proteksi B13

Migration 007_protection wajib. GET/PATCH /v1/admin/domains/{id}/protection, GET /v1/admin/system/feeds dan signed GET /v1/assets/feeds/manifest tersedia; OpenAPI kini 53 operasi. PATCH owner+CSRF/revision/audit; risk publish/enforce memerlukan active CLI protectionVersion=1/heartbeat90s. RISK_FEED_ROOT dan optional RISK_TOR_ENABLED mengatur publisher/refresh-feeds fixed Tor URL; event worker turut mengagregasi risk evidence. [Runbook](PROTECTION.md).

## Analytics B14

[Analytics dan log](ANALYTICS.md) memakai agregat minute/hour serta raw terpartisi. Session owner melindungi listing/summary; export wajib Origin/CSRF dan dibatasi. Migrasi 008 diperlukan sebelum menjalankan binary baru; artefak agent B13 tetap kompatibel.

## Keputusan pusat B15

Migrasi 009 menambah api_decisions untuk respons idempotent; POST /v1/decisions dan /v1/agents/probe memakai credential PHP/API domain. Publish API menggunakan snapshot pusat/revision; detector GeoLite/feed/rules tetap Go. [Request, timeout/fallback, privacy, probe dan semantik idle](DECISIONS.md). Worker mengagregasi source PHP bersama Go; response generation tidak membuktikan delivery redirect ke browser. Paket ZIP bukan bagian B15.

B17 menambahkan `PHP_ASSET_ROOT`, `PHP_RELEASE_INPUT` dan command `control publish-php`. Build PHP dengan `scripts/build-php-release.py`, kemudian publish memakai signing seed yang sudah tersimpan. Root/input harus terpisah, source ZIP harus layout allowlist tanpa active secret/fixture, dan versi yang sama tidak boleh berubah. Mount root PHP read-only pada API container bila memakai Docker; publisher membutuhkan mount writable terpisah. Download owner `/v1/assets/php/manifest` dan `/v1/assets/php/releases/{version}/{artifact}` tersedia, tanpa migration DB tambahan. Local B17 memakai PHP v0.17.0-b17 dan Go v0.13.0-b13; [panduan](INSTALL_PHP.md).


Penutupan B19 lokal: [CAPACITY_B19.md](CAPACITY_B19.md) dan [OPERATIONS.md](OPERATIONS.md) berisi hasil full-duration, scope, failure/restore dan batas yang masih terbuka. API 50/30m,300/15m,600/5m memenuhi gate lokal; Go lokal 300/15m,600/5m tanpa error/loss; real batch ingest 300 events/s menerima 270k dalam 15m dengan accounting cocok. Restore dataset 90k, crash queue SIGKILL, SQLITE_FULL/recovery dan actual ENOSPC Docker tmpfs lulus. Backend/agent race, GeoLite cleanup/retensi, dashboard/contract dan Docker image build lulus. Source Dockerfile pusat sekarang menyertakan risk module. Riwayat status B19 berjalan sebelumnya bukan status final; B20/gate lingkungan nyata belum selesai.

### Penghapusan domain

`DELETE /v1/admin/domains/{domainId}` memerlukan sesi owner, Origin/CSRF,
serta JSON `hostname` dan `expectedRevision` (desired revision domain).
Hostname harus sama persis; revision yang berubah menghasilkan 409.
Respons sukses 200 berisi `{"deleted":true}`. Domain yang tidak ada menghasilkan 404.
Penghapusan memakai transaksi dan lock publish, membersihkan shortlink,
aturan domain/link dan riwayat konfigurasinya, token, snapshot, agent serta
credential/manage/replay data terkait. Audit dan traffic historis dipertahankan.
Hostname bisa didaftarkan lagi dengan identitas baru. Agent Go offline dapat
memakai snapshot lama sampai lease berakhir; file instalasi harus dihentikan
atau dibuang di VPS/hosting untuk penghentian langsung.

Validasi: suite backend race, pengujian khusus konfirmasi/CSRF/credential,
retensi traffic dan isolasi domain lain, 30 pengujian browser desktop/mobile,
penghapusan domain sementara di staging, serta pemeriksaan UI production
secara baca saja. Domain production yang ada tidak dihapus saat pengujian.
