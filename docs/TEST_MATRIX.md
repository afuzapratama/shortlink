# Data dan agenda pengujian

## Gate publik ARM/hosting — 11 Oktober 2026

Lingkungan owner tersedia dan pengujian berlangsung. Bukti terbaru di [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md); sejarah batch lokal di bawah tidak menggantikan status lingkungan nyata ini.

| ID | Skenario | Lingkungan/bukti | Status |
|---|---|---|---|
| VPS-01 | Installer pada VPS bersih | Ubuntu 26.04.1 ARM64; Docker 29.9.0/Compose 5.6.0 dipasang oleh CLI; signed config/MMDB siap | Lulus pada Ubuntu 26.04; 22.04/24.04/Debian belum native |
| VPS-02 | Reuse Docker dan rerun | Identitas/data tetap; rerun tanpa token baru; restart ready kembali | Lulus ARM |
| TLS-01 | HTTPS publik | Pusat Nginx/Certbot dan agent ARM Caddy; kedua PHP HTTPS valid; authenticated public probes | Lulus ketiga agent |
| ARM-01 | Eksekusi native ARM64 | Pusat/agent berjalan native; iPhone/iOS, GeoLite, heartbeat, update/rollback | Lulus eksekusi/fungsi dan soak Go300/15 menit |
| OPS-01 | Recovery | Update 19.1→19.2/rollback 19.1; outage pusat 10 detik; lokal crash/ENOSPC | Lulus kasus yang dicatat; konflik port/failed download live belum diulang |
| PHP-01 | aaPanel | Nginx/private path/PHP-FPM 8.4, browser install lock, readiness/routing/edited destination/device | Lulus fungsi; kapasitas API belum lulus |
| PHP-02 | cPanel/LiteSpeed | PHP web 8.2.34, private di luar webroot, browser install lock, readiness/routing/device | Lulus fungsi/versi minimum dan diagnostic 10 RPS sesudah Go load berhenti |
| CAP-01 | Go publik 300 RPS/15 menit | Caddy/native ARM/GeoLite/SQLite, upload ke PostgreSQL nyata | Lulus 270.000 redirect, error/drop 0; accounting/queue lulus |
| CAP-02 | PHP/API VPS | 50 RPS diagnostic lulus; 300 RPS gagal target; 100 RPS bersama Go 300 juga gagal target | Gate throughput tinggi masih terbuka |

Gunakan domain dan token test khusus, database/data yang terisolasi dari trafik produksi. Catat distro/CPU, versi release, image digest, waktu, hasil dan log yang telah dibersihkan dari credential. Kapasitas 100–300 klik/detik tetap gate B19, bukan hasil dari pengujian installer.

## Bukti lokal yang sudah tersedia

B10: race/unit backend dan agent, PostgreSQL terisolasi, Docker AMD64 install/rerun/status, signed release/MMDB, shared policy, heartbeat/outage, update recovery/manual rollback, Caddy configuration validation, serta 16 browser tests desktop/mobile. Ini tidak menggantikan gate VPS/TLS/ARM di atas.

B11: unit/race protocol probe, wrong nonce/proof/domain, expiry, CSRF, credential non-disclosure, private/reserved IP rejection, revision berubah saat network, revoked agent dan real runtime challenge/GeoLite pada Docker AMD64 lokal. UI progress/prasyarat/retry diperiksa desktop/mobile. Resolver/transport test dikontrol; HTTPS/sertifikat publik tetap belum diuji.

B12: unit/race antrean SQLite restart, ack parsial/stable ID, buffer/byte bound, expiry tanpa traffic dan write failure/recovery; PostgreSQL atomic ingest/conflict, dua agent terisolasi, worker rollback/paralel/replay, retensi dan revoke; real agent process serta installer Docker AMD64 mengirim event block/redirect. OpenAPI 50 operasi dan build dashboard lulus. Rilis v0.12.0-b12 signed, binary/image ARM64 cross-build saja. Filesystem ENOSPC nyata, process-kill loss dan benchmark 50/300/600 RPS tetap B19; VPS/TLS/ARM tetap menunggu.

B13: shared policy/risk dan agent/backend vet/race lulus; Tor/reputation index, signed feed tamper/rollback/restart/expiry/stale-source, crawler category/human boundary, observe/enforce/off, token bucket/IPv6/bounds, fresh capability gate/CSRF/revision/revoke dan risk aggregate once. Real native agent serta installer Docker AMD64 menerima signed feed/config/MMDB, observe→enforce lalu outage pusat. 18 live browser desktop/mobile, build dashboard dan OpenAPI 53 operasi lulus. Rilis v0.13.0-b13 terverifikasi enam file; Tor resmi diambil pusat lokal 10 Oktober 2026 (1.206 entry saat verifikasi). Reputasi belum diimpor, provider proxy/challenge belum dikonfigurasi, akurasi/false-positive dan 100–300 RPS belum dibenchmark. VPS/TLS/ARM native tetap menunggu owner.

B14: backend PostgreSQL/race dan Go vet, filter/cursor/retensi, late-ingest stable pagination, minute/hour boundary sums, bot subset tanpa multiple-signal double count, auth/CSRF, CSV formula escape/batas 1.000/truncation; build dan OpenAPI 55 operasi. 20 live browser tests desktop/mobile mencakup real empty data, actual/candidate detail, pagination, aggregate-only, export/error dan responsive layout. Dashboard memakai data API; browser fixture keputusan hanya respons terkontrol, tidak seed database development. VPS bersih/HTTPS publik/ARM native/load belum diuji.

B15: PostgreSQL terisolasi/race + Go vet, API domain/method binding/auth/revoke, concurrent immutable replay, event/replay commit atomic + rollback injected failure, parity iPhone/GeoLite/ASN/engine simulator, published destination change/new ID vs old replay, canonical mapped IPv4 retry, override rejection/privacy, SQL lock timeout/no unsafe target, GeoLite fail-closed, feed observe/enforce dan retry visitor budget. Probe HMAC/no-click, controlled public callback/revision guard dan PHP idle/probe TTL. Build + OpenAPI 56 operasi dan 22 browser tests desktop/mobile lulus, termasuk proteksi API persist tanpa fake readiness. Migrasi 009 lokal, fingerprint owner/domain/link/key tetap identik. ZIP PHP/client/public handler pada B16; real hosting/TLS dan API throughput 100–300 RPS belum diuji.

B16: PHP lint/unit dan HTTP real local server mencakup preflight gagal pusat/routing/private permission sebelum enrollment, CSRF/origin, bootstrap/hostname binding, token tidak di HTML, respons enrollment hilang/recovery proof sama, atomic config 0600/setup lock, default ignore spoofed headers, trusted IPv4/IPv6 proxy chain, retry request identik, block/missing/outage/malformed target dan public probe HMAC/no-click. Browser Chromium desktop/mobile memeriksa form/password, pemeriksaan, recovered activation, lock/reload dan overflow; screenshot `php-setup-*`. PHP cURL asli→Go/PostgreSQL/GeoLite synthetic membuktikan enrollment, signed probe dan redirect source PHP dengan klasifikasi negara/iOS/mobile. Backend race dan Go vet lulus; database owner tidak diisi fixture. ZIP v0.16.0-b16 dibangun dari allowlist source tanpa secret/test/MMDB. Hosting Apache/Nginx nyata, HTTPS/sertifikat publik dan 100–300 RPS belum diuji; distribusi signed dashboard mengikuti B17.

Runtime PHP yang diuji pada B16 adalah 8.5.10 Linux dengan Chromium lokal. Minimum bahasa 8.2 ditetapkan paket, tetapi acceptance runtime PHP 8.2/provider hosting belum dijalankan.

B17: artifacts race/signature/immutable republish/tamper/ZIP traversal dan secret-file rejection; backend PostgreSQL race untuk owner-only PHP manifest/download, agent/install bearer rejection, size/hash/HEAD, bootstrap domain tanpa secret dan file tamper 503. Real signed PHP ZIP didownload dari API Go, diverifikasi OpenSSL dengan key fixture tepercaya, diekstrak di temp public/private, lalu wizard HTTP melakukan preflight/enrollment/probe dan GET menghasilkan satu event source PHP/iOS/ID melalui Go GeoLite. Tidak ada Docker pada adapter, fixture tidak dimasukkan DB owner. Domain loopback di DB test tidak mengubah domain validation/public pinned probe production.

24 dashboard browser desktop/mobile lulus: real ZIP/bootstrap downloads, PHP tutorial, state prerequisite sebelum enrollment, controlled idle/probe/retry/error UI dan responsive layout. PHP HTTP/browser suite B16 kembali lulus pada source v0.17.0-b17. Build, Go vet dan OpenAPI 58 operasi lulus. Deployment lokal mempertahankan owner/domain/link/signing fingerprint dan Go v0.13.0-b13. PHP runtime aktual tetap 8.5.10 Linux; provider hosting/minimum 8.2/public TLS/VPS ARM/RPS gates belum diuji.


B18: backend PostgreSQL/race untuk MFA session boundary, replay TOTP/recovery/challenge, limit lima percobaan/expiry, reauth/CSRF, rotasi pending/ACK dan revoke. RFC 6238 enam digit dan AEAD purpose/tamper unit lulus. Python/OpenSSL operator→Go API menguji origin lama tidak tersedia→origin baru, Go config serta PHP config/bootstrap konsisten mode 0600, metadata/path tetap, ACK retry, signature tamper dan target tanpa pinned key ditolak sebelum bearer. Browser Chromium desktop/mobile 26 tests lulus, termasuk aktivasi/recovery login/disable/reload. Build dashboard, Go vet dan 69 kontrak OpenAPI lulus. Evidence lokal private: `data/control/b18-*-final.log` dan `b18-verification.json`. Actual shared-hosting SFTP upload/restart, dua VPS dengan DNS/TLS publik, root trust overlap dan load 50/300/600 RPS belum diuji; konfirmasi deployment/runtime tetap diperlukan sebelum mematikan origin lama.


Progress B19: backup/restore PostgreSQL disposable mempertahankan owner/password/MFA marker/session/audit/root/schema, migration rerun dan menolak checksum tamper/nonempty target. Diagnostic real HTTP API/local 50/300/600 RPS masing-masing 10 detik tersedia; API tinggi masih menghasilkan 503, jadi gate gagal. Uji singkat local optional GeoLite bukan kapasitas production. Full duration, outage 10 menit, cold/warm complete scenarios, profiling/tuning, actual SIGKILL/ENOSPC dan restore dataset representatif masih pending.


Lanjutan B19: outage real-time dipendekkan ke 10 detik atas permintaan pengguna; agent queue persistence/no-drop, pusat 503, catch-up/lost ACK/stable ID/unique receiver dan queue drain lulus dengan race detector. Receiver stub dan dedup PostgreSQL diuji terpisah; tidak disebut uji real network 10 menit. Parsing UA kini di luar transaksi/mutex resource; runner memakai worker pusat asli 1000/detik. Diagnostic 300/600 RPS masih 503 (86/3000,1036/6000), gate tetap belum lulus.


B19 worker fairness: uji PostgreSQL memastikan worker menunggu lock ingest, berjalan sesudah release dan timeout bounded ketika lock tetap ditahan. Worker final 100 event/100 ms, slot API/probe 16 dengan pool DB 10. Smoke 300 RPS 3000 request tanpa error p95 16,67 ms; stress 600 RPS masih 8,30% error/p95 63,54 ms. Diagnostic ini tidak menggantikan acceptance durasi penuh.


Validasi B19 dua menit pada 300 RPS diulang: 36.000 request, 149 respons 503 (0,414%), p95 13,14 ms. Seluruh 35.851 respons 200 cocok dengan event unik dan cumulative clicks, pending nol. Accounting/catch-up lulus, capacity error gate gagal. Pengulangan tidak menjalankan suite tambahan; host tetap shared. Runner mendukung rate terpilih dan pemeriksaan drain 15 detik. Backend race, vet dan backup/restore ulang lulus. B19 tetap berjalan; belum lanjut B20 atau menyatakan acceptance 15 menit lulus. Outage tetap 10 detik sesuai preferensi pengguna.


Lanjutan B19 admission: histogram error membedakan `decision_busy` dari dependency. Seluruh error pada diagnostic 300 RPS berasal dari batas admission. Antrean dibatasi 16 penunggu/25 ms dengan 16 aktif dan deadline total dua detik; 50 ms tidak dipertahankan. Dua menit 25 ms: 63/36.000 503 (0,175%), p95 15,38 ms, accounting/drain cocok. Gate error masih gagal; B19 belum selesai. Variasi shared host tidak dianggap bukti optimasi final; uji bounded queue/cancel/timeout/release ditambahkan.


B19 pengukuran fase dan batch transaksi: observer context fixture mengukur admission/UA/pool/event lock/domain lock/evaluate/write batch/commit/total tanpa debug endpoint publik. Lima statement tulis dikirim sebagai pgx.Batch dalam transaksi synchronous yang sama; rollback perintah terakhir dan retry/replay diuji. Soak 300 RPS/120 detik: p95 HTTP 9,96 ms, transaksi 3,80 ms, event lock 1,86 ms; 169/36.000 decision_busy (0,469%) sehingga gate error tetap gagal. Event unik/clicks cocok dengan 35.831 respons 200, pending nol. Tail lock masih >200 ms pada shared host; butuh diagnosis worker/DB wait/host. B19 tetap berjalan, deployment nyata/full duration belum diklaim lulus.


B19 terbaru: worker AggregateEvents memakai SET LOCAL jit=off khusus transaksi batch kecil; pool/global setting tidak diubah. Timing sebelum/sesudah menunjukkan hold p95 94,20→5,80 ms. Soak 300 RPS/120 detik lulus diagnostic: 36.000 request tanpa error, p95 HTTP 8,41 ms, event/counter tepat dan pending nol. Trigger test memastikan worker off dan setting koneksi kembali on sesudah commit. Observer hanya di fixture; full-duration/deployment gate tetap terbuka, outage tetap 10 detik.


Penutupan B19 lokal: [CAPACITY_B19.md](CAPACITY_B19.md) dan [OPERATIONS.md](OPERATIONS.md) berisi hasil full-duration, scope, failure/restore dan batas yang masih terbuka. API 50/30m,300/15m,600/5m memenuhi gate lokal; Go lokal 300/15m,600/5m tanpa error/loss; real batch ingest 300 events/s menerima 270k dalam 15m dengan accounting cocok. Restore dataset 90k, crash queue SIGKILL, SQLITE_FULL/recovery dan actual ENOSPC Docker tmpfs lulus. Backend/agent race, GeoLite cleanup/retensi, dashboard/contract dan Docker image build lulus. Source Dockerfile pusat sekarang menyertakan risk module. Riwayat status B19 berjalan sebelumnya bukan status final; B20/gate lingkungan nyata belum selesai.

Hasil akhir publik: backup/restore dataset 388.790 events dengan fingerprint seluruh 27 tabel cocok; native ARM SQLITE_FULL/SIGKILL/ENOSPC lulus; kedua PHP fail closed saat outage 10 detik. API/PHP throughput tinggi tetap belum memenuhi gate. Detail scope/durasi/generator: [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md).
