# Shortlink Private

Project shortlink private dengan server pusat dan agent domain.

Rencana arsitektur: [docs/PLAN.md](docs/PLAN.md). Alur dashboard/setup browser: [docs/UI_UX.md](docs/UI_UX.md). Fase dan status batch: [docs/PHASES.md](docs/PHASES.md). Kontrak API: [docs/CONTRACTS.md](docs/CONTRACTS.md).

Status: **B01–B09 selesai; B10–B19 terimplementasi dan diuji lokal**. Dashboard utama memakai login/session dan data domain/link dari API Go + PostgreSQL. Domain dan shortlink baru tersimpan sebagai draft; refresh tidak menghapus data. Verifikasi DNS TXT, token instalasi, enrollment/recovery, snapshot signed, heartbeat dan revoke/audit sudah tersedia. Runtime agent Go tersedia untuk redirect dasar dan pengujian lokal. Mesin aturan bersama, deteksi visitor, simulator serta publish/rollback tersedia. Paket binary/image amd64/arm64 dan download private tersedia. Installer CLI B10 tersedia untuk pengujian lokal; acceptance VPS/TLS produksi masih terbuka. Worker B12 dan antrean klik persisten tersedia; [operasi event](docs/EVENTS.md).

## Menjalankan dashboard dan backend

Ikuti [docs/BACKEND.md](docs/BACKEND.md) untuk menjalankan database, migration, bootstrap owner dan API. Compose database: [deploy/compose.control.yml](deploy/compose.control.yml); contoh env: [deploy/.env.control.example](deploy/.env.control.example). Setelah backend siap, pada terminal lain dari root dengan Node >=22.12:

```sh
npm ci
npm run dev -- --port 5173 --strictPort
```

Buka `http://127.0.0.1:5173` dan masuk dengan akun owner hasil bootstrap. Origin backend harus `http://127.0.0.1:5173`; Vite meneruskan `/v1` dan `/health` ke API `http://127.0.0.1:8080`. Jika port dashboard berubah, sesuaikan CONTROL_ORIGIN. Target API alternatif memakai CONTROL_API_TARGET saat menjalankan Vite atau di `apps/dashboard/.env.local` (config lokal yang diabaikan Git). Lingkungan lokal ini memakai `CONTROL_API_TARGET=http://127.0.0.1:8086`; file tersebut dipakai lagi saat dashboard direstart.

Dashboard menampilkan loading, empty state, error/retry dan sesi kedaluwarsa. Simpan draft menunggu respons API; konflik revision meminta muat ulang lalu membuka kembali editor. Domain belum dinyatakan siap hanya karena didaftarkan. Detail domain menyediakan instruksi DNS, pemeriksaan kepemilikan, token sekali tampil, agent binding/revoke dan audit. Menu Aturan Traffic memakai API nyata; shortlink dapat dipublish setelah verifikasi domain dan enrollment agent lokal. Published target dapat diedit lewat draft, republish atau rollback tanpa mengganti URL shortlink.

## Preview fitur lanjutan

Prototipe B01 dipisahkan: jalankan `npm run dev:prototype`, lalu buka `http://127.0.0.1:5174/?demo=1`. Dashboard lokal biasa, staging, dan production mengabaikan `?demo=1` dan selalu memakai API nyata. Wizard browser: `http://127.0.0.1:5174/?demo=1#/agent/setup`, token contoh `DEMO-INSTALL`. Preview tidak memakai API/database, login hanya visual, dan refresh mengembalikan fixture. Jangan masukkan credential produksi ke preview.

Preview memperlihatkan aturan global/domain/shortlink, edit tujuan published melalui draft/publish, OS terpisah dari mobile/tablet/desktop, browser opsional, serta log merek/model atau Unknown. Parser uap-go nyata tersedia pada aplikasi utama; preview tetap fixture. [Screenshot aturan](docs/artifacts/rules-desktop.png), [edit tujuan](docs/artifacts/edit-link-desktop.png), [traffic OS](docs/artifacts/traffic-os-desktop.png), [dashboard desktop](docs/artifacts/dashboard-desktop.png), [mobile](docs/artifacts/dashboard-mobile.png), [setup desktop](docs/artifacts/setup-desktop.png), [setup mobile](docs/artifacts/setup-mobile.png) berasal dari preview.

Review dashboard API nyata dengan data database pengujian: [shortlink desktop](docs/artifacts/live-shortlink-desktop.png) dan [mobile](docs/artifacts/live-shortlink-mobile.png).

## Verifikasi

```sh
npm run build
npm run check:contracts
npm run test:e2e
./scripts/test-control.sh
./scripts/test-agent.sh
./scripts/test-geoip.sh
(cd packages/policy && go test -race ./...)
npm run test:live
```

`test:e2e` menguji preview (26 test desktop/mobile). `test-control.sh` memakai PostgreSQL sementara dan race detector, termasuk smoke CLI/HTTP. `test:live` menjalankan Go API + PostgreSQL sementara dan 24 browser test desktop/mobile terhadap API nyata; test empty/error UI memakai respons terkontrol. Dua script backend memerlukan PostgreSQL binaries di PATH dan user non-root; test live juga membutuhkan port 8085/5175 kosong. Keduanya membersihkan proses/database miliknya. Install Chromium jika diperlukan: `npx playwright install chromium`.

Contoh updater GeoLite terpisah: [deploy/compose.geoip.yml](deploy/compose.geoip.yml) dan [deploy/.env.geoip.example](deploy/.env.geoip.example). Publisher/API download dan aktivasi MMDB agent tersedia; panduan [docs/GEOIP.md](docs/GEOIP.md). Database GeoLite terbaru dari path GeoIP.conf Linux sudah disalin dan dipublish untuk uji lokal (build Oktober 2026). Updater produksi tetap perlu konfigurasi path/kredensial sendiri. Tidak ada deployment produksi pada tahap ini.

B05 tidak menandai enrollment sebagai Siap. Setelah DNS terverifikasi, token berlaku 30 menit; enrollment membawa domain ke Menyiapkan. Runtime redirect dasar tersedia di B06; Distribusi GeoLite tersedia B07; Probe readiness tersedia pada B11 dan menunggu acceptance HTTPS publik. Installer CLI tersedia sejak B10; ZIP PHP/wizard B16 tersedia untuk uji lokal; download signed/bootstrap/tutorial tersedia di dashboard B17.

## Agent lokal

Panduan: [docs/AGENT.md](docs/AGENT.md). Demo terisolasi bisa dijalankan tanpa DNS publik atau perubahan data dashboard:

```sh
cd services/agent
go run ./cmd/agent-demo --listen 127.0.0.1:8091
```

Lalu `curl -I -H 'Host: agent.example.test' http://127.0.0.1:8091/hello` untuk melihat redirect 302. Demo memakai fixture published; publish link dashboard tetap B08.

Halaman Sistem → GeoLite menampilkan rilis pusat dan versi yang dilaporkan agent. Sinkronisasi diuji dengan database sintetis terisolasi, tanpa mengubah data pengguna. Aturan, deteksi visitor dan publish sudah tersedia sejak B08.

Aturan operasional: negara, perangkat, OS, browser dan ASN opsional dengan kondisi AND. [Panduan aturan/publish](docs/POLICY.md), [UI aturan desktop](docs/artifacts/live-rules-desktop.png) dan [mobile](docs/artifacts/live-rules-mobile.png). Observasi/enforcement risiko B13 tersedia; pengujian kapasitas tetap B19. Readiness B11 telah diuji lokal; acceptance HTTPS publik tercatat pada agenda uji VPS.


Distribusi agent B09: [panduan rilis](docs/AGENT_RELEASES.md) dan [tutorial VPS](docs/INSTALL_AGENT.md). Panel Paket agent ada pada detail domain CLI, menampilkan rilis signed dan download private. Jalur default memakai image archive dari pusat, sehingga registry dan S3 tidak diperlukan. Rilis v0.10.0-b10 memuat installer CLI; B09 tetap immutable. Progress/probe readiness tersedia pada B11; antrean/worker tersedia pada B12.

## Installer CLI B10

Rilis v0.10.0-b10 tersedia di panel Paket agent. Binary yang sudah diverifikasi mendukung install, status, update dan rollback; token dimasukkan melalui prompt tersembunyi. Docker existing dipakai kembali, Caddy/digest atau external proxy dapat dipilih, MMDB tetap disinkronkan dari pusat. [Tutorial lengkap](docs/INSTALL_AGENT.md). Pengujian Docker lokal dan konfigurasi Caddy lulus; VPS bersih, TLS publik dan ARM native masih menjadi gate produksi pada [PHASES.md](docs/PHASES.md).

## Progress dan probe B11

Detail domain menampilkan progress instalasi dan diagnosis DNS, heartbeat, revision serta GeoLite. Periksa kesiapan publik memverifikasi HTTPS dan challenge agent terautentikasi; Siap membutuhkan bukti probe, bukan enrollment saja. Rilis terbaru v0.13.0-b13 tersedia pada Paket agent. [Agenda uji VPS ARM](docs/TEST_MATRIX.md) mencatat clean install, TLS publik dan native ARM yang akan dilakukan saat VPS owner tersedia. Implementasi lokal tetap berjalan di http://127.0.0.1:5173.

B12 menambahkan antrean SQLite, ingest batch durable/dedup, agregat menit/jam, counter klik serta retensi. Proteksi observasi/enforcement tersedia pada B13; [Analytics dan log B14](docs/ANALYTICS.md) tersedia.

B13: [proteksi traffic](docs/PROTECTION.md) tersedia di detail domain dan simulator, default observasi. Parser crawler, signed Tor/reputasi feed dan rate limit lokal tanpa request jaringan per klik. Enforcement membutuhkan agent B13/aturan explicit; challenge provider belum tersedia. Analytics dan log B14 tersedia; API keputusan B15 tersedia; paket PHP/wizard B16 tersedia melalui operator; download signed tersedia di pusat B17.

B14 menyediakan ringkasan traffic, pagination/filter server, rincian actual/candidate decision, export CSV terbatas dan kesehatan worker/antrean. [Panduan analytics](docs/ANALYTICS.md).

B15: [API keputusan pusat](docs/DECISIONS.md) memakai core rules/GeoLite/feed yang sama, domain-bound PHP credential, replay durable dan probe adapter tanpa klik. ZIP/wizard PHP B16 tersedia untuk uji lokal; integrasi download/publisher pusat dan diagnosis hosting tersedia sejak B17.

B16: [instalasi PHP tanpa Docker](docs/INSTALL_PHP.md), ZIP generic via `python3 scripts/build-php-release.py --output data/php-builds/v0.17.0-b17`, bootstrap public/private, wizard browser dengan preflight/recovery/lock dan public probe HMAC. Uji: `bash scripts/test-php-agent.sh` (memerlukan PHP 8.2+, cURL/OpenSSL, Python, Node dan Chromium Playwright) serta backend `bash scripts/test-control.sh` untuk integrasi PHP→Go/PostgreSQL. Hosting/HTTPS nyata dan throughput belum dianggap lulus.

B17: panel Paket PHP di detail domain API menyediakan ZIP signed v0.17.0-b17, bootstrap tanpa credential dan tutorial. Publisher `control publish-php` memakai PHP_ASSET_ROOT/PHP_RELEASE_INPUT terpisah, download khusus owner dan verifier `scripts/verify-php-release.py`. Progress mendukung probe/retry API dan idle tanpa heartbeat/MMDB lokal. Alur actual signed ZIP download→wizard PHP→Go redirect diuji dengan database terisolasi. Hosting nyata/public TLS/minimum PHP 8.2 tetap menunggu acceptance.


B18: halaman **Keamanan** menyediakan TOTP/recovery sekali pakai, reauthentication, rotasi credential pending/ACK dan dokumen migrasi origin signed. Tool operator `scripts/maintain-agent.py` memverifikasi identitas tujuan dengan pinned key sebelum mengirim bearer, mempertahankan binding/path Go/PHP dan menulis config private sebelum ACK. Hosting tanpa CLI memakai workflow manual; lihat [SECURITY.md](docs/SECURITY.md). Root signing tetap stabil; rotasi root trust belum diimplementasikan. Berikutnya **B19 — load/failure tests, backup/restore dan runbook**.


B19 sedang dikerjakan: [operasi/backup dan diagnostic kapasitas](docs/OPERATIONS.md). Tool restore menolak target berisi data. Diagnostic singkat menemukan 503 API pada beban 300/600 RPS; throughput production belum dinyatakan lulus.


B19 selesai untuk implementasi/uji lokal: [hasil kapasitas](docs/CAPACITY_B19.md), [runbook operasi](docs/OPERATIONS.md), backup/restore 90k events, SIGKILL/SQLITE_FULL/ENOSPC, outage singkat dan retensi. API full 300 RPS/15 menit: 270k tanpa error, p95 8,98 ms. Hasil tmpfs/loopback/shared host bukan jaminan VPS atau hosting. Berikutnya B20; VPS/HTTPS publik/ARM/hosting nyata tetap gate release. Image pusat lokal terbaru: `shortlink-control:b19-local`.

Pengujian publik B19 (11 Oktober 2026): pusat/agent native ARM, VPS Ubuntu 26.04 bersih, HTTPS dan PHP aaPanel/cPanel 8.2+ lulus fungsi. Go 300 RPS/15 menit tanpa error, backup/restore 388.790 events lulus. Kapasitas PHP/API nyata masih terbuka; lihat [hasil publik](docs/PUBLIC_TESTS_B19.md).

B20 dimulai untuk review private release: [alur review owner, upgrade/rollback dan gate production](docs/RELEASE_B20.md). Kapasitas PHP/API B19 tetap terbuka; deployment production belum dilakukan.
