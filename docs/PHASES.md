# Fase dan batch implementasi

Status: B01–B18 terimplementasi dan diuji lokal. B19 lokal selesai; VPS ARM bersih, HTTPS publik dan PHP aaPanel/cPanel lulus fungsi, Go publik 300 RPS/15 menit lulus. Gate kapasitas PHP/API nyata masih terbuka; B20 kembali ke revisi dev lokal setelah review owner: fallback/audience per link dan pemisahan UI aturan. Deployment production belum dilakukan. Hasil terbaru: [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md); baseline lokal: [CAPACITY_B19.md](CAPACITY_B19.md). Acuan desain: [PLAN.md](PLAN.md) dan [UI_UX.md](UI_UX.md).

## 1. Hasil review plan

Arsitektur hybrid cocok sebagai fondasi v1 private dengan asumsi traffic total 10–300 klik/detik. Kapasitas akhir menunggu benchmark. CLI Docker untuk VPS dan ZIP PHP mode API untuk hosting konsisten dengan pembagian ini.

Celah yang sudah diperjelas sebelum implementasi:

- Readiness dibedakan dari enrollment, koneksi, dan applied configuration.
- UI PHP mengikuti aktivitas/probe, bukan heartbeat daemon yang tidak tersedia di hosting.
- Setup browser mempunyai token gating, preflight, aktivasi atomik, pemulihan enrollment, dan penguncian setelah sukses.
- Akun admin dan kontrol akses dibuat sebelum API management dibuka; 2FA menjadi gate sebelum pemakaian private produksi.
- Installer penuh menunggu distribusi MMDB; smoke test awal memakai data fixture dan belum dianggap instalasi produksi.
- Rollback membuat revision baru; sinkronisasi tidak menurunkan nomor revision.
- Dependency eksternal, versi runtime, OS VPS yang didukung, registry dan kompatibilitas hosting dikunci saat fondasi/rilis diuji.

Bunny/S3, mesin aturan lokal PHP lengkap, billing, multi-tenant, dan binary native manual tidak masuk jalur wajib v1. Challenge layanan eksternal opsional; jika tidak dikonfigurasi, publish aturan challenge ditolak dengan pesan yang jelas.

## 2. Fase 0 — desain alur dan kontrak

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B01 | Wireframe dashboard, detail domain, tambah link, editor/simulator dan wizard ZIP; design tokens/components | Dokumen plan/UX | Happy path dan kondisi gagal dapat ditinjau, desktop/mobile dan status publish/readiness jelas |
| B02 | Struktur repo, kontrak API/OpenAPI, model state domain/setup/revision, schema event, runtime/OS baseline, fixture | B01 | Kontrak mencakup error/auth/capabilities; payload lokal/API berbagi semantik keputusan |

Mulai B01 dahulu. Wireframe boleh memakai fixture berlabel; UI belum mengklaim agent atau database benar-benar terhubung.

## 3. Fase 1 — fondasi pusat dan dashboard

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B03 | API Go, PostgreSQL/migrations, config/env, health, bootstrap owner, session/auth dan akses admin | B02 | Bootstrap sekali pakai, endpoint management menolak pengguna anonim, aplikasi dapat dijalankan lokal |
| B04 | Shell React/TypeScript, login, empty states, list/detail domain dan form link | B03 | UI memakai API nyata, validasi/error/authorization terlihat, operasi tidak bergantung fixture |
| B05 | Verifikasi domain, token install, enrollment idempotent/recovery, binding agent, snapshot revision dan audit | B03–B04 | Token salah/replay/domain mismatch ditolak; retry enrollment tidak membuat duplikasi; desired/applied dipisah |

Gate fase: domain dan link draft bisa dikelola; belum menandai domain Siap tanpa probe runtime.

## 4. Fase 2 — redirect lokal dan GeoLite

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B06 | Core agent Go, trusted proxy IP, host/slug, snapshot lokal, redirect dasar, health dan heartbeat | B05 | Redirect 302/no-store benar, host asing ditolak, revision dilaporkan; fixture jelas hanya untuk pengujian |
| B07 | Updater/publisher MMDB, manifest signature, API download, atomic activation, cleanup dan diagnosis UI Sistem | B05–B06 | Agent memperoleh GeoLite dari pusat tanpa key MaxMind; unduhan rusak tidak mengganti data sehat |
| B08 | Mesin aturan bersama Go untuk agent/API/simulator, device/OS/browser/geo/ASN, parser UA, unknown, draft/publish/rollback | B06–B07 | Parity iPhone/iPad/Android/desktop/unknown dan kondisi AND; fixture yang sama menghasilkan keputusan sama; rollback menghasilkan revision baru; invalid rule tidak dipublish |

Gate fase: dua agent uji menerima konfigurasi dan MMDB, lalu menjalankan routing yang dapat dijelaskan. Publish tertunda/offline terlihat pada UI.

## 5. Fase 3 — installer VPS dan operasi domain

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B09 | Build image amd64/arm64, registry/pull provisioning, manifest installer, download private dan tutorial CLI | B06–B08 | Versi/digest artefak tetap, bootstrap memakai URL pusat configurable, tidak ada secret dalam paket umum |
| B10 | Installer CLI Docker/Compose: preflight, install/reuse Docker, token prompt, MMDB, proxy/HTTPS, health, update/rollback | B09 | Berhasil pada VPS bersih dan VPS dengan Docker dalam baseline; rerun mempertahankan data; konflik service diberi penjelasan |
| B11 | Detail domain/installation UI, probe readiness, progress sync, retry, revoke dan panduan perbaikan | B07–B10 | Enrollment saja tidak memberi status Siap; CLI tutorial dan status runtime sesuai hasil pemeriksaan nyata |

Gate fase: alur domain pertama → perintah CLI → token → layanan siap dapat dilakukan mengikuti tutorial tanpa menyunting Compose pada kondisi default yang memenuhi prasyarat.

## 6. Fase 4 — log, proteksi dan analytics

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B12 | Antrean SQLite, batch ingest durable/dedup, worker agregasi, partisi dan retensi, watermark/metrik loss | B06–B08 | Retry tidak menggandakan event; outage/disk penuh mengikuti kebijakan yang tercatat; worker tidak menggandakan agregat |
| B13 | Parser/cache crawler, feed Tor/reputasi, rate limit, observasi/enforcement, challenge opsional dan capability validation | B08, B12 | Tidak ada lookup jaringan per klik lokal; reason code jelas; false positive dapat ditelusuri; challenge tanpa provider ditolak |
| B14 | Ringkasan/traffic/log, filters/pagination, rincian keputusan, export terbatas dan kesehatan sistem | B11–B13 | Angka memisahkan redirect/bot/challenge/error, freshness/lag terlihat; UI tidak streaming semua klik |

Gate fase: log dari dua agent dapat ditelusuri tanpa double count dan aturan risiko dapat dievaluasi dalam mode observasi sebelum aktif.

## 7. Fase 5 — ZIP PHP dan instalasi browser

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B15 | API keputusan pusat memakai core rules, timeout/fallback contract, event idempotency dan probe adapter | B05, B08, B12–B13 | Sinyal visitor/adapter dipisah, token dibatasi ke domain, request retry tidak menggandakan log |
| B16 | Paket ZIP PHP, bootstrap, public/private layout, entrypoint/routing, wizard browser dan setup lock | B01, B09, B15 | Preflight sebelum token ditukar, aktivasi/recovery aman, wizard terkunci setelah sukses, secret tidak bisa diakses publik |
| B17 | Download/tutorial PHP di pusat, status mode API, diagnosis, retry, hasil probe dan uji hosting | B11, B16 | Alur upload → token → setup → redirect berjalan tanpa Docker; hosting idle tidak dinyatakan offline hanya karena tanpa traffic |

Gate fase: adapter berjalan pada baseline hosting PHP yang diuji; koneksi API, kegagalan TLS/routing/private path dan pusat outage ditangani sesuai UX. Jika server hosting nyata belum tersedia, catat validasi lokal sebagai sementara, bukan mengklaim lulus di semua hosting.

## 8. Fase 6 — pematangan dan private release

| Batch | Output | Dependensi | Kriteria selesai |
| --- | --- | --- | --- |
| B18 | 2FA/recovery, migrasi alamat pusat, rotasi/revoke credential, audit dan permission review | B05, B10–B11, B16–B17 | Login 2FA/recovery diuji, URL baru diterapkan tanpa token bocor, agent offline tersisa terlihat |
| B19 | Load/failure tests, profiling, tuning, backup/restore, MMDB cleanup dan retensi, deployment runbook | B12–B18 | Hasil 50/300/600 request per detik dicatat sesuai PLAN; restore dan outage diuji; batas kapasitas dinyatakan berdasarkan hasil |
| B20 | End-to-end private release, accessibility/responsive review, upgrade/rollback Go/PHP dan catatan operasi | B19 | Dua jalur install lulus, semua gate produksi dipenuhi, known issues/material limits didokumentasikan |

Gate produksi: auth/2FA, GeoLite valid, dua jalur install, rule parity, event dedup, backup/restore dan hasil kapasitas tersedia. Fitur yang belum diimplementasikan tidak ditampilkan sebagai aktif.

## 9. Cara menjalankan batch

Setiap batch dimulai dari acceptance di atas, diakhiri dengan perubahan yang dapat ditinjau, verifikasi yang sesuai risiko, dan status pada dokumen ini. Tidak perlu menunggu seluruh UI selesai sebelum integrasi; batch menghubungkan alur pengguna dengan API secara bertahap.

Catat status, hasil verifikasi, file utama dan hambatan pada tabel tracking berikut. Tidak ada estimasi tanggal sebelum ukuran batch dan lingkungan uji terukur.

| Batch | Status | Bukti/hasil | Hambatan |
| --- | --- | --- | --- |
| B01 | Selesai | Prototipe React/TypeScript di apps/dashboard; 26 E2E desktop/mobile lulus; build berhasil; screenshot desktop/mobile di docs/artifacts | Preview memakai fixture; hosting/agent nyata belum tersedia |
| B02 | Selesai | OpenAPI core diperluas menjadi 40 operations termasuk readiness dan kontrak CSRF session; tipe wire/fixtures dan kontrak state/auth/error di [CONTRACTS.md](CONTRACTS.md) | Kontrak analytics/settings/2FA provisioning diperluas pada batch terkait |
| B03 | Selesai | Go API/health, migration PostgreSQL, owner singleton CLI, session durable, guard admin, CSRF/origin, login rate limit; test PostgreSQL terisolasi dengan race detector dan smoke CLI/HTTP lulus; panduan [BACKEND.md](BACKEND.md) | CRUD domain/link dan integrasi dashboard pada B04; 2FA B18 |
| B04 | Selesai | Dashboard login/session/logout, domain list/detail/create dan link draft create/edit dari API PostgreSQL; 10 E2E API nyata desktop/mobile, 26 regression preview, test backend/race dan build/kontrak lulus | Publish/verifikasi/enrollment belum tersedia; resource draft tidak melayani redirect |
| B05 | Selesai | DNS TXT challenge/verify, token hashed/domain/method, enrollment atomic + bounded recovery, unique active binding, signed snapshot/ETag/lease, heartbeat ack, revoke dan audit; PostgreSQL race tests serta 12 E2E desktop/mobile lulus | Runtime redirect B06, GeoLite B07, publish/rules B08 dan readiness probe B11; DNS positif diuji dengan resolver terkontrol |
| B06 | Selesai | Agent Go: snapshot signed/persist atomik, cache/memory routing, host/slug, trusted proxy IP, 302/no-store, health, heartbeat/polling, revoke marker; race tests dan integrasi binary dengan API/PostgreSQL lulus | Demo fixture terisolasi; SQLite queue B12, publish/rules B08, GeoLite B07 dan readiness produksi B11 |
| B07 | Selesai | Publisher set tiga MMDB, manifest signed/ETag, API authenticated streaming/rate limit, agent checksum/reader swap/restart/expiry/cleanup, Sistem GeoLite; race/integrasi dan 14 E2E lulus; Docker image pusat build | Pengujian memakai MMDB sintetis; download MaxMind asli dan DNS/HTTPS produksi belum diuji; rules/publish B08 |
| B08 | Selesai | Core Go shared control/agent, uap-go cache, negara/device/OS/browser/ASN, mandatory/AND/scope, simulator published/preview, publish/history/rollback atomic dan snapshot signed; race/integrasi serta 16 live E2E lulus | Bot/proxy B13, PHP API B15, production readiness B11, kapasitas B19 |
| B09 | Selesai | Binary/image Linux amd64/arm64, manifest Ed25519, immutable publisher, private API downloads, image archive default/registry digest opsional, bootstrap origin configurable, panel paket dan tutorial; Docker amd64 real integration, race dan 16 live E2E lulus | Runtime ARM native dan registry produksi belum diuji; installer otomatis B10, PHP B16 |
| B10 | Implementasi dan verifikasi lokal selesai | CLI install/status/update/rollback, token-only enrollment context, Docker reuse/apt baseline, signed downloads, MMDB, Caddy atau external proxy, rerun/data retention; integrasi Docker lokal dan validasi konfigurasi Caddy lulus | Gate VPS bersih Ubuntu/Debian, DNS/sertifikat HTTPS publik dan ARM native belum diuji |
| B11 | Implementasi dan verifikasi lokal selesai | Progress instalasi, probe HTTPS terautentikasi, TTL readiness, revision/race guards, retry/revoke, audit dan panduan perbaikan; race/integrasi dan browser desktop/mobile | Probe HTTPS publik nyata dan ARM native menunggu VPS owner pada TEST_MATRIX.md |
| B12 | Terimplementasi, diuji lokal | SQLite queue, durable ingest/dedup, worker/retensi dan loss metrics; [events](EVENTS.md) | Outage/disk kebijakan terdokumentasi; kapasitas tetap B19 |
| B13 | Terimplementasi, diuji lokal | Crawler cache, signed Tor/reputasi, limiter, observe/enforce dan capability gate; [proteksi](PROTECTION.md) | Challenge/proxy provider belum tersedia; false-positive/load menunggu |
| B14 | Terimplementasi, diuji lokal | Ringkasan/grafik/log, filter/pagination/rincian, CSV terbatas, health; 55 kontrak dan 20 browser tests; [analytics](ANALYTICS.md) | Bot aggregate lama tidak di-backfill; challenge/error belum dikumpulkan; kapasitas/VPS/TLS/ARM belum diuji |
| B15 | Terimplementasi, diuji lokal | Keputusan API core Go, classified PHP event + immutable replay atomik, probe HMAC tanpa klik, publish/proteksi API dan idle semantics; backend race, 56 kontrak dan 22 browser tests; [API keputusan](DECISIONS.md) | ZIP/public handler PHP B16; hosting/HTTPS nyata dan throughput belum diuji |
| B16 | Terimplementasi, diuji lokal | ZIP PHP v0.16.0-b16, bootstrap private, routing, wizard CSRF/preflight, enrollment/recovery dan setup lock; PHP HTTP/browser desktop/mobile serta integrasi PHP→Go/PostgreSQL lulus | Download/publisher dashboard B17; hosting dan HTTPS publik belum diuji |
| B17 | Terimplementasi, diuji lokal | PHP v0.17.0-b17 signed/immutable, owner-only download ZIP/tutorial, bootstrap domain, diagnosis API/idle dan probe/retry; actual downloaded ZIP→wizard→Go/PostgreSQL redirect serta 24 dashboard browser tests lulus | Hosting/provider PHP nyata dan HTTPS publik tetap gate; runtime minimum PHP 8.2 belum diuji |
| B18 | Terimplementasi, diuji lokal | TOTP/recovery, sesi MFA verified, reauth/CSRF, rotasi credential pending/ACK, proof identitas pusat, migrasi signed dan tool private config Go/PHP; 26 browser tests dan backend race lulus | Root signing tetap dipin; hosting manual perlu verifikasi runtime setelah upload; HTTPS publik/ARM tetap gate |
| B19 | Terimplementasi, diuji lokal | Full API 50/300/600 durasi 30/15/5 menit, full Go lokal 300/600, batch ingest 300, accounting/drain, restore 90k events, SIGKILL/SQLITE_FULL/actual ENOSPC, GeoLite cleanup/retensi dan Docker build lulus; [hasil kapasitas](CAPACITY_B19.md) | Shared host/tmpfs/synthetic/loopback; VPS/SSD/HTTPS publik/ARM/hosting nyata tetap gate; wrapper error full300 tercatat |
| B20 | Berjalan | Review rilis, keyboard/responsive dan runbook upgrade/rollback; lihat [RELEASE_B20.md](RELEASE_B20.md) | Kapasitas PHP/API B19 dan acceptance production masih terbuka |

Batch implementasi berikutnya: **B20 — review E2E private release, accessibility/responsive, upgrade/rollback dan gate deployment**. Acceptance B10 pada VPS bersih/HTTPS publik tetap menjadi gate sebelum produksi. Prototipe B01 tidak dianggap sebagai aplikasi operasional.

### Catatan hasil fase 0

UI mencakup ringkasan, domain/list/detail, draft link/publish, aturan/simulator, traffic log, sistem, pengaturan, preview login, dan wizard browser. Form memvalidasi hostname, URL tujuan, slug duplikat dan prioritas duplikat. Wizard mendemonstrasikan token kedaluwarsa, private path/routing/koneksi gagal, aktivasi dan penguncian state sesi. Desktop dan mobile diperiksa secara visual melalui screenshot.

Perbaikan hasil review pengguna: aturan terikat global/domain/shortlink, simulator memilih link dan menyaring scope; link published dapat mengubah tujuan lewat draft/publish tanpa mengganti URL shortlink atau statistik. Rule redirect memakai sumber tujuan link atau URL khusus. Ini masih prototipe; revision/applied snapshot nyata tetap pekerjaan B05/B08.

Tambahan klasifikasi: editor dan simulator memisahkan OS dari jenis perangkat, dengan filter browser opsional; iOS dapat mencakup mobile iPhone dan tablet iPad. Traffic menyediakan filter OS/browser/device dan detail merek/model dengan unknown eksplisit. Kontrak API mengikuti field tersebut; parser uap-go nyata dan cache menunggu B08, data UI saat ini sintetis. Uji mencakup routing Android/iOS/iPad/unknown, penyimpanan edit OS/browser, kondisi AND, dan filter/detail log pada desktop/mobile.

Pengujian responsive membandingkan document scroll width dengan visual viewport; overflow dari label tersembunyi pada tabel sudah diperbaiki. Uji browser belum mencakup semua kombinasi assistive technology atau audit kontras menyeluruh; review accessibility akhir tetap pada B20.

OpenAPI merupakan kontrak bertahap; endpoint health/session/auth dan management domain/link draft sudah diimplementasikan B03–B04. Simulator JS prototype belum core rules Go. Download installer/ZIP diberi status belum tersedia sesuai B09/B16. Tidak ada deployment atau koneksi MaxMind dari prototipe.

### Catatan hasil B03

Migrasi menggunakan transaksi/advisory lock dan checksum. Owner singleton tidak dapat ditimpa oleh bootstrap ulang. Cookie session HttpOnly/SameSite=Strict/Secure pada production, token database di-hash, lifetime absolut dan logout/rotation durable. Guard management menolak anonim; resource yang belum diimplementasikan menghasilkan 501 setelah guard. Rate limit login bertahan setelah handler restart dan tidak mempercayai forwarded IP. B03 menyediakan fondasi akses, belum CRUD/worker/agent atau 2FA produksi.

Verifikasi dilakukan pada PostgreSQL 18.6 lokal sementara; script test membersihkan instance dan schema miliknya. Smoke membangun binary, migrate/bootstrap/serve, lalu login/session melalui HTTP dan graceful shutdown. Go vet lulus; Compose PostgreSQL 18.6 juga diuji hingga healthy dan query SELECT 1 berhasil pada project sementara, lalu container/network/volume uji dibersihkan. Panduan lokal menggunakan Compose untuk PostgreSQL dan Go API pada host.

### Catatan hasil B04

Dashboard utama memakai API nyata; fixture hanya lewat mode preview ?demo=1. Akun owner, loading/empty/error/retry, session expiry, modal submit async, duplicate hostname/slug dan expectedRevision terhubung ke backend. Domain baru draft/unknown/capabilities belum dikonfirmasi. Link published hanya dapat mengubah draft tujuan; identitas/tujuan published/klik tetap, diuji lewat data published yang disiapkan khusus dalam integration test. Endpoint publish masih belum tersedia.

Migrasi 002 menambahkan domain/link dan uniqueness slug per domain. Link revision adalah concurrency version resource, terpisah dari domain snapshot revision. API mempunyai GET link dan cursor listing/filter domain. Input fallback/expiry/query allowlist belum didukung dan ditolak, menunggu batch fitur terkait. Frontend management mengambil seluruh halaman; traffic server pagination tetap B14.

Verifikasi: Go vet, test PostgreSQL dengan race detector, build frontend, kontrak 36 operations, 26 regression preview dan 10 browser tests live (desktop/mobile). Test live memakai API/database sementara; empty/error states diuji dengan respons terkontrol. Tidak ada database owner development yang diubah atau deployment.

### Catatan hasil B05

Ownership melalui TXT _shortlink.<hostname>, challenge berlaku 24 jam. Token install 30 menit, ditampilkan sekali dan disimpan hash, bound domain/method/installation ID. Token baru mencabut token unused sebelumnya. Enrollment transaksi dengan lock domain/token dan constraint satu agent aktif; proof acak client 32 byte base64url disimpan hash. Recovery proof/installation/domain/method sama mengembalikan agent/credential identik dalam 10 menit; proof lain, expired/revoked/replay ditolak. Domain menjadi preparing, bukan ready.

Seed private persisted dalam PostgreSQL, dipakai untuk snapshot Ed25519 dan derivasi credential dengan domain separation. Credential agent disimpan hash; bootstrap hanya membawa public verification key. Snapshot memakai signedPayload berisi byte JSON asli; agent wajib memverifikasi/decode bytes tersebut dan hanya memakai hasil decode. Envelope luar bukan sumber kebenaran independen. Lease 24 jam diperbarui bila tersisa kurang dari 12 jam, menghasilkan payload/signature/ETag baru; 304 tidak memperpanjang lease. Snapshot awal hanya link published, tanpa draft; aturan/GeoLite belum aktif.

Heartbeat CLI mencatat appliedRevision/metadata queue/version/data, menolak revision masa depan atau menurun dan tidak membuat readiness ready. PHP tidak memakai heartbeat daemon. Koneksi CLI yang tidak melapor lebih dari 90 detik dibaca offline. Revoke menolak credential/recovery lama dan membuka enrollment baru; pemanggilan revoke lama ulang tidak mengganggu agent pengganti. Audit atomic bersama mutation: domain/link draft, verification, token, enrollment, config ack dan revoke, tanpa token/proof/credential.

UI live menyediakan instruksi/verifikasi DNS, token ephemeral, agent summary/revoke dan audit 50 event terakhir. Test backend memverifikasi enrollment concurrent, recovery, batas waktu, signature/tampering, lease/ETag, ack dan revoke; DNS positif/negatif memakai resolver terkontrol. Browser menguji alur instruksi serta kegagalan DNS tanpa mengklaim domain nyata terverifikasi. Local backend dimigrasikan/restart pada 8086 dan dashboard tetap 5173; owner/data sebelumnya dipertahankan. Tidak ada agent produksi atau installer dirilis.

### Catatan hasil B06

Runtime agent Go tersedia di services/agent, panduan [AGENT.md](AGENT.md). Redirect memakai snapshot immutable di memori, tanpa API pusat per klik. Snapshot bertanda tangan dipersist dengan fsync/rename atomik sebelum aktivasi; B06 menggunakan file snapshot private, SQLite event queue mengikuti B12. Pusat outage mempertahankan cache sampai lease habis; 304 tidak renew. Revision rollback dan perubahan routing pada same-revision renewal ditolak. 401/403 pusat membuat marker blok persisten. Host asing, slug invalid, draft dan loop tujuan langsung ditolak; query masuk tidak diteruskan.

Proxy CIDR default kosong, hanya peer terpercaya dapat membawa X-Forwarded-For; IPv4/IPv6 dan spoofing diuji. Demo lokal memiliki pusat/key/snapshot fixture terisolasi, tanpa perubahan data pengguna. Integrasi binary agent terhadap API dan PostgreSQL test asli membuktikan redirect, heartbeat revision dan outage. GeoLite/rules/event/installer, DNS/HTTPS produksi dan capacity gate belum diuji/tersedia. Runtime health bukan readiness produksi dashboard.

### Catatan hasil B07

Panduan [GEOIP.md](GEOIP.md). Publisher host/CLI/watch atau Docker share GEOIP_ROOT dengan updater; API bisa read-only saat publisher terpisah. Manifest signedPayload dengan URL relatif pada origin pusat; rilis immutable content hash, validasi metadata/hash/size/reader IPv4/IPv6. Agent startup/enam jam, retry backoff; pointer dan reader aktif atomik, heartbeat melaporkan versi aktif. Expiry konservatif 29 hari, cleanup hourly agent/lima menit publisher, tanpa backup MMDB.

geoipRequired true membuat agent 503 tanpa GeoLite valid. Aturan dan publish tetap B08, production readiness tetap B11. Test memakai database sintetis terisolasi; GeoLite asli belum tersedia di workspace. Halaman Sistem menampilkan status menunggu database pada lingkungan lokal. Docker pusat amd64 dibuild dan Compose profiles divalidasi; installer/arm64/produksi tetap gate masing-masing.

### Catatan hasil B08

Panduan [POLICY.md](POLICY.md). Rule set immutable binding global/domain/link; draft memakai revision optimistic. Publish/rollback menyimpan history, audit, desired revision dan signed snapshot dalam satu transaksi. Rollback menghasilkan revision baru, bukan menurunkan revision agent. Global publish memberi mapping revision per domain; pending agent dibedakan dari applied heartbeat. Link publish memerlukan domain verified dan agent CLI aktif; probe readiness produksi tetap B11.

Mesin shared Go: mandatory global block, priority kecil dahulu, tie link/domain/global, AND country/device/OS/browser/ASN dan explicit unknown. Agent lookup GeoLite serta uap-go cache lokal, tanpa API/SQL per klik; rules per link dievaluasi dari dua daftar terurut dengan batas 300 kondisi efektif. uap-go dipin, parser diinisialisasi sekali dan sorting internal dimatikan untuk concurrency. Model perangkat tidak ditebak bila parser tidak mengenali. Simulator published tidak memakai draft; preview saved rule set membutuhkan expectedRevision.

Backend race tests memakai PostgreSQL sementara, mencakup concurrent publish, fanout global, atomic rollback saat ID efektif duplikat, invalid rule, CSRF, perubahan tujuan dan binary agent asli. Enam belas live E2E desktop/mobile dan 26 preview E2E lulus. GeoLite build Oktober 2026 lokal tetap tersedia; integrasi aturan memakai MMDB sintetis. Kapasitas 100–300 klik/detik belum dibenchmark. Bot/proxy/challenge, antrean log, PHP dan installer tetap batch terkait.

### Catatan hasil B09

[AGENT_RELEASES.md](AGENT_RELEASES.md), [INSTALL_AGENT.md](INSTALL_AGENT.md). Rilis v0.9.0-b09: enam artefak (binary + image archive masing-masing dua arsitektur, Compose dan tutorial). Build cross-compile CGO disabled, scratch image non-root, public CA bundle; tanpa config/token/proof/credential/MMDB. Build script smoke-test versi container pada native arch. ARM build dan ELF/archive metadata diverifikasi, eksekusi ARM native belum diuji di host amd64.

Keputusan private: distribusi image archive terotorisasi dari filesystem pusat menjadi default, tanpa registry/S3. Registry opsional memakai repository yang disediakan operator, digest tetap dan kredensial pull terpisah pada Docker credential store. Token enrollment bukan password registry. Push/pull registry produksi tidak dilakukan karena belum ada registry akun/deployment yang dikonfigurasi.

Publisher memverifikasi ELF/platform, archive config/OCI identity, signature/hash/size, lalu menyalin ke immutable release dan mengganti current pointer atomik. Manifest memisahkan observed imageId, imageConfigDigest dan optional registry manifest digest untuk engine Docker berbeda. Download owner session atau active CLI credential; unused token, revoked credential, file di luar whitelist dan corrupt file ditolak. Bootstrap memakai CONTROL_PUBLIC_URL yang configurable, default CONTROL_ORIGIN.

Race/unit/integrasi lulus, termasuk image Docker amd64 terhadap API/PostgreSQL test terisolasi: signed config/MMDB, heartbeat, policy dan outage. Enam belas live browser test dengan rilis binary/image asli lulus, termasuk download tutorial/bootstrap dan layout mobile; Python/OpenSSL verifier dan Go verifier sepakat atas enam file, signature invalid ditolak. Rilis private lokal dipublish memakai signing key existing tanpa mengubah owner/domain/link/GeoLite. InstallerReady false; installer VPS bersih/proxy/HTTPS tetap B10 dan readiness B11.

## Hasil B10 — installer CLI lokal

Command install/status/update/rollback tersedia dalam rilis v0.10.0-b10 (installerReady true); B09 immutable tetap false. Bootstrap pin origin/key/domain/version, install-context menyelesaikan token-bound ID tanpa credential; proof private disimpan sebelum enrollment untuk recovery. Docker lokal/Compose direuse, clean apt installer dibatasi Ubuntu 22.04/24.04 dan Debian 12/13, konflik paket/port diberi error. Caddy image official di-resolve ke digest sekali atau operator menyediakan digest, external proxy tersedia untuk service existing. Host networking Linux, agent loopback/non-root/read-only, shared data owned UID 10001, credential mode 0600.

Runtime mengambil signed snapshot/GeoLite dari pusat dan readiness lokal membutuhkan keduanya. Update gagal mencoba mengembalikan image/metadata sebelumnya; rollback memverifikasi manifest/checksum CLI cache, dan rerun tidak menghapus data. File proxy/sertifikat dan data persistent tetap. Test lokal mencakup TLS origin/pin/tamper, token binding terhadap PostgreSQL, Docker install/rerun/status via CLI terverifikasi, shared policy/GeoLite/heartbeat/outage, update recovery/manual rollback dan Caddy configuration validation; 16 live browser tests desktop/mobile lulus. Ini bukan acceptance install apt pada VPS bersih, issuance sertifikat publik, ARM native atau capacity test. [Tutorial](INSTALL_AGENT.md).

## Agenda VPS ARM

Owner akan menyediakan VPS ARM kemudian. Gate VPS bersih, HTTPS publik dan eksekusi ARM native tetap berstatus menunggu, dengan skenario/prasyarat/bukti di [TEST_MATRIX.md](TEST_MATRIX.md). Pengembangan B11 dilanjutkan lokal tanpa menganggap gate tersebut lulus.

## Hasil B11 — progress dan readiness

Migration 005_readiness menambahkan timestamp/result probe tanpa mengubah credential/key atau URL shortlink. GET installation menampilkan prasyarat DNS, enrollment, heartbeat, applied/desired dan GeoLite; dashboard polling 10 detik saat terlihat. POST probe memerlukan sesi owner/CSRF, rate limit 10 per domain dan dua slot/proses. Pusat resolve hostname registered, menolak IP private/reserved/mapped/internal, pin hasil DNS pada dial TLS port 443, tidak mengikuti redirect dan memverifikasi challenge HMAC nonce/domain/revision/GeoLite. Agent endpoint hanya mengungkap state setelah proof request valid; API bearer credential tidak dikirim ke domain publik.

Hasil probe disimpan/audit atomic dengan guard revision dan active agent. Enrollment/heartbeat tidak memberi Siap. Hasil Siap turun menjadi Perlu perhatian saat probe >5 menit, heartbeat >90 detik atau revision berbeda. Revoke menghapus hasil lama dan retry memeriksa ulang; tidak merestart service VPS dari browser. Runtime B11 dirilis v0.11.1-b11; B10/B09 immutable tetap tersedia. Tes lokal memakai resolver/transport terkontrol, database sintetis dan Docker AMD64; production HTTPS/DNS/ARM tetap gate pada TEST_MATRIX.md.

## Hasil B12 — antrean dan agregasi

SQLite WAL/FULL, buffer terbatas, retry/ack stable ID, durable scoped ingest, daily partitions, worker transactional minute/hour, retensi dan queue/loss/lag tersedia. [Kontrak operasional](EVENTS.md) menjelaskan batas crash buffer dan kebijakan fail-open. Migration 006_events wajib. Acceptance VPS/TLS/ARM tetap terbuka; kapasitas B19 belum diukur. Batch berikutnya B13 proteksi crawler/proxy dalam mode observasi.

Validasi B12 lulus: go vet/race agent dan backend, PostgreSQL terisolasi, real agent process, installer Docker AMD64 install/rerun/status + event ingest/aggregate/outage, OpenAPI 50 operasi dan build dashboard. Rilis signed v0.12.0-b12 tersedia; migration 006_events diterapkan pada pusat lokal. Fingerprint akun/domain/link/kunci sebelum/sesudah sama. Tidak ada klaim benchmark throughput atau native ARM.

## Hasil B13 — proteksi traffic

Parser crawlerdetect v0.2.32/cache bounded, kategori search/preview/automation, signed Tor/reputation bundle dan local IP index, token bucket lokal, mode off/observe/enforce, candidate risk event/agregat dan UI domain/simulator tersedia. Migration 007_protection wajib. Aturan risiko/enforce membutuhkan heartbeat protectionVersion=1 yang aktif; challenge tanpa provider tetap ditolak. [Operasi dan batas deteksi](PROTECTION.md). Analytics/log/filter/export tersedia B14; batch berikutnya B15 API keputusan.

B13 terverifikasi: vet/race shared policy/risk/agent/backend, PostgreSQL terisolasi dan installer Docker AMD64, real signed feed/config/MMDB dengan observe→enforce/outage, serta 18 live browser tests desktop/mobile. Build dashboard/OpenAPI 53 operasi lulus. Rilis signed v0.13.0-b13 dipublish memakai key existing; migration 007_protection dan publisher Tor aktif di pusat lokal. Fingerprint akun/domain/link/kunci sebelum/sesudah sama (kolom default protection baru dikecualikan). Source reputasi belum diimpor; challenge tetap unsupported; gates VPS/TLS/ARM dan benchmark/false-positive tetap terbuka.

## Hasil B14 — analytics/log lokal

Ringkasan traffic aktual, filter domain/link/action/country/device/OS/browser, grafik dan tabel jam, cursor server stabil waktu ingest + ID, rincian actual/candidate decision, export CSV maksimum 1.000 baris dengan truncation explicit, serta kesehatan worker/antrean/stale heartbeat tersedia. Mode agregat saja dapat membaca retensi panjang tanpa mengambil log raw. [Panduan analytics](ANALYTICS.md).

Migration 008 menambah indeks dan bot counter; agregat bot sebelum B14 tidak di-backfill dan cakupan ini terlihat di UI. Known bot adalah subset keputusan, bukan klik tambahan. Challenge/error runtime belum dikumpulkan dan dinyatakan belum tersedia; watermark adalah transaksi worker terakhir, bukan jaminan kelengkapan event. Grafik tidak streaming klik. Agent tetap v0.13.0-b13.

Verifikasi: backend PostgreSQL terisolasi/race dan Go vet, pagination dengan late ingest/tie-break, filter/range/cursor, batas minute/hour, agregasi sekali, auth/CSRF, escape formula CSV/truncation, build dashboard, OpenAPI 55 operasi dan 20 browser tests desktop/mobile. Fixture browser traffic diberi identitas khusus dan tidak dimasukkan database owner lokal. Counter login hanya di-reset dalam database uji disposable antar kelompok, tanpa mengubah limiter aplikasi. VPS bersih/HTTPS publik/ARM native dan benchmark traffic tetap agenda uji berikutnya.

## Hasil B15 — keputusan API pusat

POST /v1/decisions memakai bearer PHP/API domain, visitor IP/UA terpisah dari socket hosting, detector/GeoLite/feed/engine Go dan snapshot compiled cache. Transaksi menyimpan classified PHP event + immutable replay response; retry sama tidak menambah klik/charge budget, request berubah 409 dan revoke tetap 401. Publish API dan proteksi memakai revision pusat tanpa daemon heartbeat. GeoLite/DB gagal mengembalikan 503 tanpa tujuan; fallback offline belum tersedia. [Kontrak timeout/fallback/idempotency](DECISIONS.md).

POST /v1/agents/probe mengembalikan nonce/domain/revision/GeoLite dan HMAC byte response, tanpa event klik. Public owner probe tetap public DNS/TLS/pinned transport dan prerequisite/revision guard; public PHP handler akan dibuat B16. Applied API revision berarti terakhir dilayani pusat; hosting tanpa aktivitas dibaca idle, bukan offline. Queue/stale metrics hanya CLI; response generation PHP belum membuktikan browser redirect.

Go vet/backend PostgreSQL race dan build lulus; mencakup concurrent retry, parity iPhone/GeoLite/ASN, changed destination/publish, override/auth/binding/revoke, privacy, rollback failure, SQL deadline, feed observe/enforce, no retry budget charge, HMAC/no-click/probe guard dan idle. OpenAPI 56 operasi; 22 browser tests desktop/mobile termasuk API protection persist tanpa klaim instalasi ready. Pusat lokal dimigrasikan 009 dan dijalankan kembali setelah restart, owner/domain/link/key dipertahankan. Agent Go tetap v0.13.0-b13. HTTPS publik/hosting PHP nyata/native ARM dan kapasitas B19 tetap terbuka.

## Hasil B16 — ZIP PHP dan wizard browser

Adapter PHP 8.2+ tanpa Composer/Docker/database lokal memakai layout public/private. Bootstrap operator menetapkan hostname, public URL, alamat pusat dan trusted proxy exact; tidak ada fetch URL arbitrary dari wizard anonim. ZIP generic tanpa token, credential, MMDB atau fixture disiapkan dengan build deterministik dan checksum lokal; publisher/signature serta download/bootstrap domain dari dashboard mengikuti B17. [Panduan instalasi PHP](INSTALL_PHP.md).

Wizard `/__agent/setup` memakai private session, CSRF/origin, rate limit dan exclusive installation lock. Preflight private path/permission/atomic storage, HTTPS pusat dan self-routing berjalan sebelum enrollment. Pending installation ID/token/proof disimpan sebelum token ditukar; respons hilang dipulihkan menggunakan proof sama dan jendela pusat 10 menit. Config aktif atomik menjadi pengunci setup, termasuk setelah reload/restart; recovery/reset hanya lewat tindakan operator dan revoke pusat. Public readiness handler mengautentikasi nonce/proof sebelum meneruskan response probe pusat tanpa re-encoding atau klik.

GET slug memakai IP socket atau rantai proxy explicit, timeout client sekitar tiga detik dan paling banyak satu retry identik. Action/event/tujuan divalidasi; block 403, published missing 404, outage/malformed response 503 tanpa Location. Tidak ada fallback offline atau klaim receipt browser. HEAD tidak membuat event.

Verifikasi lokal: lint PHP, unit URL/IP/permission, HTTP failure/preflight/routing/CSRF/origin/credential privacy/recovery/retry/probe, browser Chromium desktop/mobile hingga setup terkunci, serta PHP cURL terhadap backend Go + PostgreSQL/GeoLite fixture terisolasi. Backend race dan Go vet lulus. Screenshot: [wizard mobile](artifacts/php-setup-mobile.png), [aktivasi desktop](artifacts/php-setup-activation-desktop.png), [selesai mobile](artifacts/php-setup-complete-mobile.png). Hosting nyata/public TLS, filesystem nonlokal dan kapasitas API tetap gate B17/B19; agent Go tetap v0.13.0-b13.

## Hasil B17 — distribusi PHP dan diagnosis hosting

Pusat mempublikasikan PHP v0.17.0-b17 melalui `control publish-php` menggunakan signing key proyek. PHP_ASSET_ROOT/PHP_RELEASE_INPUT terpisah dari rilis Go. Manifest Ed25519 dan ZIP/tutorial immutable; publisher membatasi layout ZIP, ukuran expanded, duplicate/traversal/symlink dan menolak config/pending/fixture aktif. Download GET/HEAD/Range melalui `/v1/assets/php/*` khusus sesi owner, size/SHA256 diperiksa sebelum streaming; bearer/token install tidak memberikan akses. [Instalasi dan verifikasi](INSTALL_PHP.md).

Panel Paket PHP pada detail domain API memuat ZIP, manifest signed, tutorial dan bootstrap domain tanpa token/credential. Bootstrap download berisi hostname/publicUrl/controlPlaneUrl serta defaults proxy/development yang aman; alamat pusat HTTP lokal diberi penjelasan sebelum dipakai hosting publik. Kunci/expected version dikonfirmasi dari pusat tepercaya, verifier berasal dari checkout tepercaya sebelum ZIP diekstrak.

Progress instalasi mendukung probe API setelah enrollment, read-only retry saat status tidak tersedia, timestamp aktivitas API dan idle yang tidak dianggap offline. Config/GeoLite diagnosis tidak meminta daemon heartbeat atau MMDB hosting. Public probe tetap menolak alamat internal/redirect dan mengecek TLS, HMAC, nonce, domain/revision/GeoLite serta revoke/revision guard; hasil belum lulus tidak otomatis Siap. Status domain/revoke juga menjelaskan kemampuan mode API secara khusus.

Verifikasi: artifacts race (signature/immutable/tamper/unsafe ZIP), backend PostgreSQL race + PHP cURL asli, download auth/bootstrap/checksum/HEAD/tamper, serta signed ZIP hasil download yang diverifikasi/extract lalu dipasang wizard HTTP dan menghasilkan redirect/event Go dengan GeoLite/iOS. Hostname loopback pada test DB disiapkan eksplisit hanya untuk fixture; validasi domain/probe production tidak dilonggarkan. 24 uji dashboard desktop/mobile, build, Go vet dan OpenAPI 58 operasi lulus. Screenshot [desktop](artifacts/php-packages-desktop.png), [mobile](artifacts/php-packages-mobile.png). Pusat lokal memakai rilis PHP signed v0.17.0-b17; Go tetap v0.13.0-b13, fingerprint owner/domain/link/signing tetap sama.

Pengujian memakai PHP 8.5.10 Linux, filesystem lokal dan HTTP loopback developer. Hosting Apache/Nginx nyata, public TLS, minimum runtime PHP 8.2, VPS bersih/ARM native dan kapasitas B19 belum dianggap lulus. Gate hosting fase 5 tetap terbuka sampai lingkungan nyata tersedia.


## Hasil B18 — keamanan owner dan maintenance agent

TOTP RFC 6238/replay dan encryption binding/tamper diuji. PostgreSQL/race membuktikan aktivasi membatalkan sesi lain, password tidak membuat sesi untuk owner MFA, recovery/challenge hanya sekali pakai, lima percobaan/expiry, disable dan CSRF. Alur operator real Python/OpenSSL ke API Go membuktikan migration signature/identity proof sebelum bearer, origin berubah dari alamat lama tak tersedia, config Go/PHP dan bootstrap PHP private, path/binding tetap, ACK idempotent, credential lama ditolak dan revoke. Tujuan bertanda alamat valid tetapi tidak memiliki root key ditolak tanpa bearer. Dashboard desktop/mobile: 26 tests lulus termasuk aktivasi, recovery login, reload dan disable MFA. OpenAPI: 69 operasi; build dan vet lulus.

Panduan: [SECURITY.md](SECURITY.md). Paket Go v0.13.0-b13 dan PHP v0.17.0-b17 immutable tetap dipakai; maintenance tool berasal dari checkout tepercaya, bukan endpoint web/package yang ditimpa. Root signing tidak dirotasi; kebutuhan protokol overlap dicatat terpisah. VPS bersih, TLS publik, native ARM, shared hosting sebenarnya dan kapasitas masih gate produksi.


Pusat lokal telah dimigrasikan ke schema `010_security.sql` dan direstart pada 8086, dashboard 5173. Backup private dibuat sebelum perubahan. Smoke owner login/logout, status keamanan, manifest tetap, proof pinned root dan readiness lulus. Fingerprint field owner lama/domain/link/root signing sama sebelum/sesudah; flag MFA owner tetap nonaktif hingga owner melakukan enrollment UI.


## Progress B19 — diagnostic load dan backup/restore

Backup database-only (tanpa MMDB) memverifikasi private filesystem, format/checksum dan restore ke target kosong satu transaksi. Fixture terisolasi membuktikan owner/password/MFA marker/session/audit/signing/schema tetap, backup tamper/nonempty target ditolak dan migration rerun. Runtime owner tidak direset. Runner opt-in mengukur real HTTP/PostgreSQL/GeoLite fixture serta worker; fixture agent lokal mempunyai 32 slug dan snapshot signed saat pusat 503.

Diagnostic 10 detik per tahap: local 50/300/600 RPS tanpa error, p95 2,97/2,79/2,69 ms pada shared host dan optional GeoLite. API 50 RPS tanpa error p95 9,39 ms; 300 RPS 137/3000 respons 503 (4,57%) p95 21,98 ms; 600 RPS 1780/6000 respons 503 (29,67%) p95 32,36 ms, pending worker kumulatif 4676. P95 yang mencakup 503 tidak membuktikan capacity gate lulus. Tidak ada claim 300 RPS mode API production; kapasitas dan queue loss masih harus ditindaklanjuti. Full 30/15/5 menit, profiling/tuning, outage 10 menit, actual ENOSPC/crash dan restore representatif tetap terbuka. [OPERATIONS.md](OPERATIONS.md) menyediakan perintah dan batas pengujian.


Lanjutan B19: outage dipendekkan menjadi 10 detik sesuai permintaan pengguna. Redirect berlanjut, seluruh event tersimpan tanpa dropped/write errors, recovery upload yang kehilangan ACK diretry dengan ID sama dan queue habis; receiver dedup stub, PostgreSQL dedup diuji suite backend terpisah. Target outage 10 menit tidak dianggap diuji. Benchmark worker kini sama dengan pusat (1000 event/detik), dan parsing UA dipindah sebelum transaksi keputusan untuk memendekkan lock. Diagnostic sesudah perubahan: 50 RPS 0/500 error p95 10,95 ms; 300 RPS 86/3000 (2,87%) error p95 17,38 ms; 600 RPS 1036/6000 (17,27%) error p95 24,38 ms. Error tetap di atas gate 0,1%, backlog worker tetap perlu diagnosis; B19 belum selesai.


Tuning worker B19 berikutnya: try-lock yang melewati tick diganti wait maksimal 250 ms untuk mengurangi starvation; batch 100/100 ms, bounded keputusan/probe concurrency 16, DB pool tetap 10. Smoke final 50/300 RPS tanpa error; 300 RPS p95 16,67 ms dan pending 25. Stress 600 RPS tetap gagal (498/6000 error, p95 63,54 ms), pending 50. Full duration/public TLS/PHP hosting belum lulus; status B19 tetap berjalan. Outage tetap singkat 10 detik sesuai permintaan.


Validasi B19 dua menit pada 300 RPS diulang: 36.000 request, 149 respons 503 (0,414%), p95 13,14 ms. Seluruh 35.851 respons 200 cocok dengan event unik dan cumulative clicks, pending nol. Accounting/catch-up lulus, capacity error gate gagal. Pengulangan tidak menjalankan suite tambahan; host tetap shared. Runner mendukung rate terpilih dan pemeriksaan drain 15 detik. Backend race, vet dan backup/restore ulang lulus. B19 tetap berjalan; belum lanjut B20 atau menyatakan acceptance 15 menit lulus. Outage tetap 10 detik sesuai preferensi pengguna.


Lanjutan B19 admission: histogram error membedakan `decision_busy` dari dependency. Seluruh error pada diagnostic 300 RPS berasal dari batas admission. Antrean dibatasi 16 penunggu/25 ms dengan 16 aktif dan deadline total dua detik; 50 ms tidak dipertahankan. Dua menit 25 ms: 63/36.000 503 (0,175%), p95 15,38 ms, accounting/drain cocok. Gate error masih gagal; B19 belum selesai. Variasi shared host tidak dianggap bukti optimasi final; uji bounded queue/cancel/timeout/release ditambahkan.


B19 pengukuran fase dan batch transaksi: observer context fixture mengukur admission/UA/pool/event lock/domain lock/evaluate/write batch/commit/total tanpa debug endpoint publik. Lima statement tulis dikirim sebagai pgx.Batch dalam transaksi synchronous yang sama; rollback perintah terakhir dan retry/replay diuji. Soak 300 RPS/120 detik: p95 HTTP 9,96 ms, transaksi 3,80 ms, event lock 1,86 ms; 169/36.000 decision_busy (0,469%) sehingga gate error tetap gagal. Event unik/clicks cocok dengan 35.831 respons 200, pending nol. Tail lock masih >200 ms pada shared host; butuh diagnosis worker/DB wait/host. B19 tetap berjalan, deployment nyata/full duration belum diklaim lulus.


Tuning B19 terbaru: worker hold p95 94,20 ms sebelum override JIT, turun menjadi 5,80 ms sesudah SET LOCAL jit=off pada transaksi agregasi. Soak 300 RPS/120 detik: 36.000 HTTP200 tanpa error, p95 8,41 ms, unique/clicks cocok dan pending nol. Trigger regresi memverifikasi JIT worker off dan pooled connection kembali on setelah transaksi. Diagnosis/benchmark tidak mengubah root, owner atau pola lock global. Diagnostic lulus; acceptance 15 menit, baseline 30 menit, stress terbaru dan deployment nyata masih pending.


## Penutupan B19 lokal

Hasil final [CAPACITY_B19.md](CAPACITY_B19.md) menggantikan status progress sebelumnya. API 50/30 menit dan 300/15 menit tanpa error; stress 600/5 menit 37/180k decision_busy (0,0206%) memenuhi ambang <0,1%, p95 9,66 ms. Go lokal 300/600 tanpa error/loss; ingest PostgreSQL 270k/15 menit tanpa error dan accounting cocok. Dataset 90k dibackup/restore, failure/restart/outage 10 detik/retensi/GeoLite/contract/regression/image build lulus. Scope local fixture/shared host/tmpfs/optional GeoLite/stub/network tetap dicatat; private production release belum dinyatakan siap.

## B19 — lingkungan publik mulai 11 Oktober 2026

Deployment pusat native ARM/aaPanel dan HTTPS sudah berjalan, browser login desktop/mobile lulus. Pengujian VPS ARM bersih dan PHP aaPanel/cPanel sedang berjalan; hasil serta gate yang belum selesai mengikuti [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md). Ini melengkapi gate B19 nyata sebelum penutupan deployment/B20.

## Revisi B20 setelah review owner

Owner meminta kembali ke dev lokal: fallback per shortlink, tombol buka URL pendek, aturan global di halaman global dan aturan domain di detail domain. Form tambah/edit link menyimpan tujuan, fallback dan audience sebagai draft; bot serta pengunjung yang tidak cocok menuju fallback. Implementasi dan batas kompatibilitas: [LINK_ROUTING.md](LINK_ROUTING.md). Rilis/public acceptance B20 tetap berjalan; revisi ini belum dipasang ke VPS.

Revisi dev ProxyCheck: private API key dikelola dari Sistem, disimpan terenkripsi di pusat dan dapat diganti/dihapus. Link dapat memilih fallback untuk anonymous/proxy serta hasil unknown. Cache, kuota, failure semantics dan gate akun nyata mengikuti [PROXYCHECK.md](PROXYCHECK.md). Go membutuhkan routingVersion 2 untuk fitur proxy; hasil kapasitas B19 belum dianggap acceptance provider aktif.

Revisi lokal 12 Oktober 2026: root dan slug tidak ditemukan memakai URL pembuka domain (302); kosong menghasilkan 503. Detail domain menyediakan editor opening, daftar link domain dan key manage sekali pakai. Go/PHP manage scoped-domain memakai hash sesi di pusat, cookie aman, Origin/CSRF; [AGENT_MANAGE.md](AGENT_MANAGE.md). PHP kandidat v0.20.1-b20, Go dev routingVersion 3. Belum dipasang ke VPS/hosting publik.

Revisi dev 13 Oktober 2026: provider IP dapat dipilih antara ProxyCheck dan BEON-IPQuality milik owner. URL HTTPS dan key client dapat diganti dari Sistem; key terenkripsi hanya di pusat. Flag proxy/VPN/Tor/botnet mengarahkan link yang mengaktifkan fitur ke fallback, crawler tetap memakai deteksi UA. Unknown mengikuti pilihan link. Konfigurasi dan smoke API nyata hanya dilakukan di dev lokal; gate kapasitas dan production tetap terbuka. [Detail integrasi BEON](IP_QUALITY.md).

Koreksi owner: BEON-IPQuality tanpa kuota lookup harian. Field/batas harian hanya digunakan ProxyCheck; cache, singleflight dan pembatas request sesaat tetap berlaku untuk BEON.

Revisi UX input API key: key tersimpan ditampilkan sebagai mask dalam input setelah simpan/reload. Klik input untuk mengganti langsung dan simpan; pesan berhasil jelas, draft key dipertahankan jika request gagal. Mask hanya penanda dan tidak dikirim sebagai credential.

Revisi owner: fokus BEON-IPQuality saja, provider eksternal ProxyCheck dihapus. Delapan flag boolean dapat dipilih per shortlink; flag terpilih true menuju fallback. Datacenter yang tidak dipilih tidak menolak akses. Simulator dan runtime Go/API memakai pilihan yang sama; Go memerlukan routingVersion 4 untuk daftar eksplisit. Pemeriksaan koneksi publik ARM/PHP aaPanel/cPanel online/ready dan applied revision cocok pada 13 Oktober 2026; kode flag baru tetap hanya dev lokal. [Detail BEON](IP_QUALITY.md).

Persiapan staging/production: prototype UI dipisahkan ke mode prototype/port 5174 dan build dist-prototype. Dashboard nyata lokal/staging/production mengabaikan demo=1; bundle real tidak membawa fixture atau link preview. Build staging dist-staging dan production dist terpisah, contoh environment database/path/hostname terpisah tersedia. Pusat aaPanel test saat ini dikelola systemd (binary Go + dashboard statis Nginx), bukan Node/Go Project panel. Cutover publik menunggu penetapan hostname dan penyelesaian gate rilis; [ENVIRONMENTS.md](ENVIRONMENTS.md).

Staging publik stg-dashort.beon-api.site sudah terpasang terpisah pada aaPanel ARM: HTTPS, DB/role/seed/owner tersendiri, Go v0.20.2-staging dan PHP v0.20.1-b20. Health/login/logout/manifests lulus; BEON upstream masih unavailable. Pusat lama dashort tetap normal, cutover production menunggu keputusan mempertahankan data/agent atau memulai kosong; backup pre-production/off-host tersedia.

Cutover 13 Oktober 2026 disetujui owner dengan mempertahankan data/agent: production dashort telah memakai build tanpa preview dan backend baru; staging terpisah tetap aktif. Restore/migrasi 28 tabel dan fingerprint identitas lulus. Go ARM v0.20.4 serta PHP aaPanel/cPanel v0.20.1-b20 terpasang tanpa mengganti credential; redirect/probe ketiganya lulus. Lookup BEON final known. Gate kapasitas/operasi tetap terbuka; [catatan cutover dan rollback](ENVIRONMENTS.md).
