# Kontrak API — B02–B11

Kontrak mesin: [openapi.json](../packages/contracts/openapi.json). Tipe wire untuk UI: [index.ts](../packages/contracts/src/index.ts). Endpoint health/session/auth dan management domain/link draft serta verification/enrollment/snapshot/heartbeat/revoke/audit sudah diimplementasikan B03–B05; operasi lain masih spesifikasi batch berikutnya. Panduan runtime: [BACKEND.md](BACKEND.md).

## Struktur repo dan baseline

```text
apps/dashboard/             React + TypeScript, UI API nyata + preview B01
services/control/           API Go + migrasi/auth/domain/link PostgreSQL, B03–B04
packages/contracts/         OpenAPI dan tipe wire bersama
scripts/                    Pemeriksaan kontrak
tests/e2e/                  Uji alur browser desktop/mobile
deploy/                     Konfigurasi updater GeoLite
docs/                       Arsitektur, UX, fase, kontrak
```

Struktur berikut baru dibuat ketika batch terkait dimulai: `services/agent/` (agent Go) dan `agents/php/` (adapter/setup PHP). Worker pusat ditambahkan pada B12. Core Go packages/policy sudah dipakai oleh agent dan simulator API B08; simulator JavaScript B01 hanya fixture.

Baseline frontend Node >=22.12 dan npm lockfile; validasi lokal memakai Node 24.11.1. Versi React, Vite, TypeScript dan library dikunci di package-lock.json. Font dibundel lokal agar UI tidak memerlukan request font eksternal.

Runtime backend B03 dikunci pada Go 1.27.1 dengan pgx/v5 dan x/crypto Argon2id di go.mod/go.sum; PostgreSQL lokal tervalidasi versi 18.6. Target installer pertama Ubuntu 24.04 LTS + Docker Compose; arsitektur amd64/arm64. Dukungan OS/arsitektur baru hanya diklaim setelah diuji. Target adapter PHP >=8.3 dengan cURL, JSON, OpenSSL, HTTPS outbound dan file private writable; pemeriksaan versi/runtime dilakukan lagi saat B16. SQLite tidak menjadi syarat adapter API PHP.

## Autentikasi dan batas kepercayaan

- Admin: session cookie HttpOnly/Secure/SameSite dan CSRF token pada mutation; origin diperiksa juga pada login. Endpoint management tidak menerima kredensial agent.
- Implementasi B03: bootstrap owner hanya CLI dan singleton database, password Argon2id, session token 32 byte disimpan hash SHA-256; lifetime absolut default 12 jam. GET session mengembalikan csrfToken; login juga menyediakan header X-CSRF-Token. Origin persis CONTROL_ORIGIN wajib pada login/logout/mutation, termasuk caller non-browser. HTTP cookie hanya untuk development loopback. MFA B18 belum tersedia dan flag MFA-enabled gagal tertutup. Domain/link draft sudah dilayani B04; operasi management lain mengembalikan 501 setelah autentikasi/CSRF, bukan data fixture.
- Agent: bearer credential per agent, terikat domain/metode/capabilities dan dapat dicabut; admin session tidak otomatis memberi hak mengirim event sebagai agent.
- Enrollment: token domain sekali pakai, masa berlaku awal 30 menit, dengan installation ID dan client-generated proof yang disimpan private. Preflight terjadi sebelum token ditukar.
- Recovery enrollment: proof dan installation ID yang sama dapat memulihkan hasil dalam jendela awal 10 menit; proof berbeda tidak menerima credential. Penukaran dan hasil recovery harus durable/atomik serta rate limited. Proof diperlakukan sebagai rahasia dan tidak dicatat di log.
- Bootstrap/install download: admin session atau signed download URL yang scope-nya artifact/domain, TTL awal 10 menit. Signed URLs merupakan mekanisme delivery B09, bukan bearer agent yang dimasukkan ke query string.
- Manifest/aset setelah registrasi: admin/agent sesuai scope; edition/artifact allowlisted. Tidak meneruskan Authorization ke origin baru tanpa validasi origin dan identitas.
- Probe: permintaan administrator hanya ke hostname domain terdaftar. Validasi resolusi/redirect untuk mencegah akses ke jaringan internal/reserved; endpoint probe domain tidak memberikan konfigurasi/secret.

Signal IP visitor pada mode PHP dikirim agent terotorisasi; socket IP adapter dicatat terpisah. Pusat menjalankan lookup negara/bot sendiri. Input negara/device sintetis hanya untuk endpoint simulator. Header proxy dipercaya berdasarkan konfigurasi; tidak langsung mempercayai `X-Forwarded-For` dari internet.

OS/browser sintetis juga hanya untuk simulator. `Signals` produksi tetap membawa IP, UA dan Client Hints; pusat melakukan normalisasi pada mode API. `OperatingSystem` dan `BrowserFamily` adalah enum terpisah dari `Device`; `ios` mencakup iOS/iPadOS yang dikenali. Rule mempunyai `os`/`browser` dengan wildcard `*`, semua kondisi AND; kebijakan lama tanpa field tersebut dinormalisasi menjadi `*` saat backend dibangun. `SimulationRequest` meminta OS/browser eksplisit, termasuk unknown. `TrafficEvent` menyimpan OS/browser ternormalisasi dan metadata opsional nullable `osVersion`, `browserVersion`, `deviceBrand`, `deviceModel`; merek/model bukan kondisi routing v1. Endpoint daftar events menyediakan query OS/browser/device. Nilai unknown tidak otomatis berubah menjadi Android/iOS berdasarkan jenis perangkat.

## State dan revision

Domain: draft → verification_pending → installation_pending → preparing → ready. Readiness dapat menjadi attention jika prasyarat/probe/data tidak valid; revoke menghasilkan revoked. Transition readiness dijalankan backend berdasarkan bukti, bukan field yang dapat diubah bebas oleh client.

Go memakai heartbeat/applied snapshot. PHP memakai activity/probe; mode idle tidak dinyatakan offline karena tidak ada pengunjung. API decision revision adalah revision pusat; appliedRevision PHP bernilai null.

Link/rule set: save draft → validate/simulate → publish revision baru → acknowledgement agent. Optimistic concurrency memakai expectedRevision; mismatch mengembalikan 409. Update draft link memakai expectedRevision tambahan pada body; publish tidak boleh mengabaikan konflik.

`Shortlink.revision` adalah versi optimistic concurrency resource, bertambah saat PATCH draft; bukan nomor snapshot domain. Domain desired/applied revision tetap terpisah dan tidak berubah oleh simpan draft. B05 membuat revision konfigurasi awal saat enrollment; publish perubahan link/aturan mengikuti B08.

`Shortlink.destination` adalah tujuan published (atau tujuan draft awal jika link belum pernah publish). Field opsional `draftDestination` menyimpan perubahan pending untuk link published. PATCH tujuan tidak menimpa destination published, ID, domain/slug atau analytics. Domain/slug link yang pernah published tidak dapat diubah melalui edit tujuan. Publish mempromosikan draftDestination dan menghapus pending field; agent lokal memakai snapshot terakhir sampai menerima revision baru. Menyimpan kembali tujuan published membatalkan pending draft.

`RuleSet` mengikat scope/scopeId; `ScopedRule` TypeScript adalah projection UI dari binding parent, bukan schema rule terpisah di API. Pemilihan link ID pada simulator menyusun global + domain link itu + link itu sendiri. Preview draft rule set opsional menggunakan ruleSetId dan expectedRevision; scope lain tetap published. Prototipe B01 memakai aturan draft di memori untuk memperlihatkan alur; belum membuat snapshot published rule set nyata.

`Rule.destinationSource=link` mengikuti tujuan published link yang dievaluasi. `custom` wajib mempunyai URL tujuan dan hanya berubah melalui edit aturan. `mandatory=true` hanya sah untuk block global. Matching mandatory block didahulukan, lalu priority ascending; tie-break lintas scope link/domain/global lalu rule ID. Validasi duplicate priority berlaku per rule set, bukan terhadap semua scope sekaligus.

Rollback memilih konten revision lama dan mempublikasikannya sebagai nomor revision baru. Prioritas rule harus unik dalam rule set; susunan lintas scope dikompilasi deterministik pada B08. Rule redirect/fallback dengan sumber custom wajib mempunyai URL tujuan valid; rule challenge ditolak jika provider/capability belum dikonfigurasi.

Snapshot mempunyai signature dan lease expiry. Respons 304 berarti snapshot yang sama, bukan perpanjangan lease; renew lease menghasilkan payload/signature/ETag baru. Agent tidak menerima revision lebih rendah, kecuali proses pemulihan eksplisit operator yang terotorisasi.

## Error, idempotency, dan batas request

Error selalu `{error: {code, message, requestId, fields?}}`. UI menggunakan code untuk tindakan pemulihan; message tidak membocorkan secret atau file private. Status dasar: 400 payload invalid, 401 belum terautentikasi, 403 scope ditolak, 404 resource tidak tersedia, 409 konflik/replay, 410 token/rilis tidak berlaku, 422 prasyarat/capability gagal, 429 rate limit, 503 dependency tidak tersedia.

Kode awal: `validation_failed`, `unauthorized`, `forbidden`, `not_found`, `revision_conflict`, `slug_conflict`, `domain_mismatch`, `enrollment_expired`, `enrollment_used`, `preflight_failed`, `capability_missing`, `rate_limited`, `geo_unavailable`, `control_unavailable`.

API keputusan dan batch event memakai `(authenticated agent ID, eventId)` sebagai kunci idempotency. Event ID sama dengan payload berbeda mengembalikan conflict, bukan menimpa keputusan. Acknowledgement batch hanya setelah durable write; jumlah accepted/duplicates tersedia. Worker mengagregasi idempotent; batch retry tidak menggandakan statistik.

Batas awal: JSON request 1 MiB, batch maksimum 500 event, UA maksimum 2048 karakter, page limit 1–100. Request oversized menghasilkan 413; invalid batch ditolak atomik (tidak menerima sebagian tanpa hasil terstruktur). Timestamp menggunakan RFC3339 UTC, UI memakai Asia/Jakarta. Client timestamp yang tidak masuk akal ditolak atau ditandai; ingestion timestamp server disimpan terpisah.

Login B03 memakai batas khusus 8 KiB, password 12–256 byte, dan request JSON field tak dikenal/trailing value ditolak. Rate limit durable per socket IP/email (30/10 request per 15 menit), hashing maksimal dua bersamaan per proses. Error tambahan runtime: method_not_allowed (405), not_implemented (501); Content-Type salah menghasilkan 415 validation_failed. GET /health/ready hanya memeriksa database/migrasi/owner dan tidak membuka diagnosis private.

Cursor opaque, urutan event stabil berdasarkan waktu ingest + ID, filter periode dengan batas `from` inklusif dan `to` eksklusif. Grafik/agregat B14 memakai watermark waktu transaksi worker terakhir dan pending/oldest untuk lag, bukan kelengkapan semua event. Core kontrak saat ini meliputi login, domain/link, enrollment, snapshot, aturan/simulator, events, decisions dan assets; secrets/settings, backup/2FA provisioning dan migrasi diperluas pada batch implementasi terkait sebelum UI tersebut dihubungkan.

## Fixtures dan batas prototipe

Fixture B01 berada di [fixtures.ts](../apps/dashboard/src/fixtures.ts), mencakup agent Go online/offline, PHP idle, desired/applied berbeda, link draft/published, bot block dan geo/device redirect. Semua hostname `.example` dan data traffic bersifat sintetis.

Wizard prototipe menerima hanya `DEMO-INSTALL`; tidak menukar credential atau mengubah hosting. Setup lock bersifat state sesi React, bukan security boundary produksi. Kontrak recovery/persistent lock baru diimplementasikan B05/B16. Refresh prototipe mengembalikan state contoh.

Login B01 adalah preview visual tanpa autentikasi. Paket CLI/ZIP belum dibangun. Jangan mengekspos prototipe sebagai dashboard produksi.

Validasi: `npm run check:contracts` memeriksa OpenAPI, unique operation IDs, serta deklarasi auth/error setiap operation. `npm run build` memeriksa TypeScript termasuk tipe fixture bersama; `npm run test:e2e` menguji preview desktop/mobile. Endpoint yang sudah tersedia diuji lewat PostgreSQL integration tests dan `npm run test:live`; parity penuh diperluas ketika endpoint batch berikutnya diimplementasikan.

## Management B04 yang tersedia

Domain POST menerima hostname/mode; readiness draft, connection unknown, appliedRevision/lastSeenAt null, capabilities false. Nama dinormalisasi dan disimpan unik; duplikasi menghasilkan 409 domain_conflict. Verifikasi/probe belum dilakukan. Link POST menerima domainId/destination dan slug opsional; fallbackDestination/expiresAt/queryAllowlist belum didukung dan dikeluarkan dari wire schema awal sampai fitur tersebut tersedia. Unknown fields ditolak.

GET domain/link detail menghasilkan 404 bila tidak ada. PATCH link meminta domainId/destination/expectedRevision dan slug opsional; race edit hanya satu berhasil, lainnya revision_conflict. Published domain/slug immutable. List GET memakai limit/cursor, link dapat difilter domainId; cursor terikat jenis listing dan filter domain. GET link ditambahkan sehingga kontrak menjadi 36 operations. Origin dan CSRF wajib pada mutation admin.

Browser UI utama memakai API/session nyata, mode preview terpisah ?demo=1. API failure tidak mengembalikan fixture. `npm run test:live` menjalankan API/PostgreSQL sementara dan Playwright; `./scripts/test-control.sh` menguji persistence/auth/resource/race. B05 menyediakan snapshot signed dan enrollment agent; field advanced, publish, rules/traffic/settings dan runtime agent diperluas pada batch terkait.

## B05 — binding, trust dan recovery

Kontrak diperluas menjadi 39 operations: POST domain verification challenge, GET agent summary dan GET audit. Verify ownership memakai DNS TXT dan mengembalikan ProbeResult hanya untuk DNS check; endpoint probe runtime masih belum tersedia. Installation ID/method/hostname harus sama dengan token provisioning; proof client canonical base64url 32 byte (43 karakter), TTL token 30 menit dan recovery 10 menit. Recovery expired/proof mismatch/revoked menghasilkan enrollment_used; unused expired enrollment_expired. Active binding satu per domain, credential hanya hash di database; derived recovery memakai private seed persisted.

Snapshot signedPayload wajib: base64url tanpa padding dari JSON payload asli yang ditandatangani Ed25519. Verifier harus memverifikasi bytes dengan pinned manifestVerificationKey lalu hanya memakai decoded payload. Field envelope tidak independen terautentikasi. Initial revision naik saat enrollment; desired/applied dipisah, draft links tidak masuk snapshot. Renew lease menghasilkan payload/ETag/signature baru pada revision sama; agent B06 harus mendukung same-revision lease renewal tanpa menerima revision lebih rendah. 304 tidak mengubah lease.

Heartbeat CLI menolak appliedRevision lebih rendah/lebih tinggi dari desired, mencatat queue counters/version/geo/feed, tetapi tidak menandai domain ready/capability confirmed. PHP heartbeat ditolak domain_mismatch. Revoke berlaku untuk akses pusat/recovery, bukan menjamin menghentikan redirect agent offline sebelum lease expiry. Audit resource/enrollment/ack/revoke atomic bersama mutation; schema domain audit dibatasi 50 terakhir. Key version/rotation, probe readiness, local MMDB dan rules/publish tetap batch terkait.

## B06 — konsumen snapshot lokal

Agent Go memverifikasi signedPayload dan hanya membaca payload terverifikasi. Binding domain/mode dan revision monotonic wajib. Same-revision renewal menerima lease lebih panjang dengan routing yang sama; 304 tidak memperpanjang lease. Snapshot yang memerlukan rules/feed belum didukung dan ditolak; B07 menerima geoRelease valid dan mewajibkan rilis tersebut aktif untuk routing. Snapshot dipersist sebelum aktivasi dan ack; expired/no snapshot menghasilkan 503. Pusat 401/403 membuat blok persisten, outage lain mengikuti lease terakhir. Redirect dasar GET/HEAD memakai 302/no-store, Host terikat domain dan slug 1–80 karakter alfanumerik/underscore/hyphen. Query masuk tidak diteruskan. Panduan runtime/test: [AGENT.md](AGENT.md).

## B07 — GeoLite trust dan status

GeoManifest menambahkan signedPayload dan expiresAt. Verifikasi Ed25519 pada bytes payload asli; hanya hasil decode dipakai. GeoFile.downloadUrl adalah relative uri-reference pada origin pusat, tidak boleh dialihkan ke origin lain. Rilis content hash/edition/hash/size/build divalidasi, manifest max64KiB, MMDB max512MiB. 304 tidak renew expiry. Retention expiry konservatif maksimal 29 hari dari build database tertua tanpa republish konten sama, database build rollback ditolak.

Assets menerima owner session atau agent bearer; /v1/admin/system/geoip hanya owner dan melaporkan status not_configured/awaiting_database/ready/expired/invalid, manifest nullable dan active CLI agents dengan geoRelease/lastSeenAt nullable. Heartbeat geoRelease adalah rilis benar-benar aktif, bukan sekadar berhasil diunduh. Capability confirmed tetap mengikuti probe B11. Kontrak menjadi 40 operations; implementasi/test [GEOIP.md](GEOIP.md).

## B08 — kontrak policy

47 operations. Tambahan create/detail/history rule set, link history/rollback, countries dan visitor detection. Rule set draft PATCH dibatasi 128 KiB/100 rules; binding tidak berubah. POST publish menerima expectedRevision; rollback menambahkan targetRevision history published, menghasilkan resource revision baru. PublishResult.revision adalah revision resource; domainRevisions memetakan desired snapshot revision per domain. PendingAgentIds adalah agent aktif yang perlu polling, bukan bukti applied.

Snapshot.rules memakai ScopedRule dengan scope/scopeId dari rule set. Signature atas signedPayload tetap sumber tunggal. Snapshot payload dibatasi 3 MiB agar envelope termasuk proyeksi JSON/base64 tetap di bawah batas 8 MiB agent. Draft tidak ikut snapshot. Agent menolak binding/rule invalid, URL loop, duplikat ID, feed yang belum didukung dan known_bot aktif sebelum B13.

Simulator memilih linkId dan sinyal normalisasi, ASN opsional null untuk unknown. Default menggabungkan published rule sets; ruleSetId+expectedRevision mengganti hanya satu cakupan dengan draft tersimpan. Ini preview konfigurasi; tidak menghasilkan event analytics. Detector menerima userAgent <=4096 byte, mengembalikan device/OS/browser, versi, brand/model nullable. Country/ASN untuk request redirect berasal dari GeoLite dan trusted peer IP, bukan field browser.

Dokumentasi lengkap [POLICY.md](POLICY.md). Mutation endpoints tetap owner session + Origin + CSRF; asset/agent auth tidak berubah. Bot/challenge dan API-mode publish ditolak capability_missing; link tanpa verified domain/enrolled local agent ditolak domain_not_ready. Conflict revision tetap 409 dan invalid policy 422.

## B09 — agent release assets

GET /v1/assets/agent/manifest dan releases/{version}/{artifact} sudah aktif; jumlah kontrak tetap 47 karena kedua route sudah disediakan sebelumnya. Owner session atau credential CLI aktif; token enrollment dan credential PHP ditolak. Download GET/HEAD, whitelist nama file, version SemVer terbatas, SHA256/size verified, ETag/Range dan 30 download/15 menit per actor dengan dua slot stream/proses. Manifest punya signedPayload dan signature, installerReady false pada B09; semua keputusan memakai payload verified, proyeksi luar diabaikan.

Artifact membawa size, hash, relative downloadUrl, platform dan image metadata nullable. ImageId adalah observed Docker ID, imageConfigDigest mengikat config archive, imageDigest adalah optional registry manifest digest. ImageReference optional repository@digest; tidak mengandung registry credential. Common release memiliki enam file tanpa secrets/MMDB/bootstrap spesifik domain.

Bootstrap/install token/configUrl memakai CONTROL_PUBLIC_URL, bukan Origin request/Host dari client. artifactVersion menunjukkan current verified release untuk CLI atau unreleased saat belum ada paket; PHP tetap unreleased. Signed release tidak ditimpa ketika bytes atau metadata versi yang sama berubah. B10 menambahkan installer/provisioning lengkap. [AGENT_RELEASES.md](AGENT_RELEASES.md).

## B10 — token-only install context

48 operations. POST /v1/agents/install-context memerlukan possession token CLI di JSON body; owner cookie atau agent bearer tidak menggantikannya. Response installationId + Bootstrap tidak membawa credential/proof dan tidak mengonsumsi token. Expired/revoked/non-CLI ditolak; token used hanya dapat menyelesaikan konteks dalam recovery window 10 menit, credential tetap membutuhkan proof melalui enroll. Rate limiting memakai IP bucket yang terpisah. Manifest installerReady merupakan signed capability yang ditentukan builder dan immutable pada versi yang sama: B09 false, B10 true. Verifier mendukung kedua capability.

## B11 — installation/readiness

49 operations. GET /v1/admin/domains/{domainId}/installation merupakan read-only owner operation dengan schema InstallationStatus. Existing POST /probe kini diimplementasikan dengan ProbeResult dan owner/CSRF; response check status pass/fail/not_applicable, bukan restart runtime. Retry tidak mengganti token/credential. Readiness derived attention ketika probe kedaluwarsa, heartbeat offline atau applied != desired. Endpoint runtime /.well-known/shortlink-ready bukan API pusat/OpenAPI: request X-Shortlink-Nonce + X-Shortlink-Proof, response JSON raw + X-Shortlink-Proof, MAC purpose-separated. Tidak ada bearer token di URL/header probe domain publik.

## B12 event ingest

POST /v1/events/batch sudah tersedia untuk CLI Go dengan window 48 jam, 1 MiB/500 event, durable atomic commit dan dedup agent/event ID; konflik payload menghasilkan 409 event_conflict, scope mismatch 422 binding_mismatch. Heartbeat menerima optional bufferedEvents/queueWriteErrors. GET /v1/admin/system/events owner-only menggunakan EventHealth; total OpenAPI 50 operasi. [Semantik queue/worker/watermark](EVENTS.md). Admin event browsing tersedia B14; PHP/API decision source tersedia B15; ZIP/wizard mengikuti B16–B17.

## B13 protection contract

53 operasi. Rule risk diperluas dan botCategory opsional; Snapshot protection, Heartbeat protectionVersion opsional, Decision/TrafficEvent risk opsional, ProtectionSettings/Status/View/Update dan SignedRiskFeed/RiskFeedStatus/RiskSummary ditambahkan. Assets feed current adalah signed bundle lengkap, <=8 MiB; endpoint release feed individual tetap reserved. Simulasi memakai mode domain dan sinyal sintetis; optional candidate berbeda dari action actual. [Semantik enforcement/freshness](PROTECTION.md).

## Analytics B14

OpenAPI kini 55 operasi. `EventPage.items` berisi `TrafficRecord` (id ingest, receivedAt, agentId, processed, event) sehingga eventId yang sama dari agent berbeda tetap dapat ditelusuri. Cursor opaque mengikat seluruh filter dan batas ingest ID, urut receivedAt/id descending. `from/to` wajib RFC3339 minute-aligned `[from,to)` dalam retensi. `TrafficAnalytics` memisahkan action/bot subset, health, filter, retensi dan jenis event yang belum dikumpulkan. Export POST owner + Origin/CSRF berisi paling banyak 1.000 baris, header `X-Export-Truncated` dan formula escape. [Detail batas/coverage](ANALYTICS.md).

## Kontrak keputusan B15

OpenAPI 56 operasi. DecisionRequest hanya menerima eventId/occurredAt/domainId/slug dan visitor IP/UA; client klasifikasi/hints ditolak. Domain/method berasal dari agent aktif. HMAC fingerprint canonical request mengikat immutable replay; keputusan/event/replay commit atomik. X-Decision-Replayed membedakan replay tanpa klik baru. Source php adalah keputusan pusat, belum delivery acknowledgement. AdapterProbeRequest/State memakai nonce/domain/hostname/version dan response HMAC bytes asli, tanpa click event. [Deadline, fallback, expiry, cache dan status idle](DECISIONS.md).

## B17 — paket PHP dan bootstrap

OpenAPI 58 operasi. GET `/v1/assets/php/manifest` dan `/v1/assets/php/releases/{version}/{artifact}` memakai sesi owner saja; Authorization bearer (termasuk CLI/PHP/install token) ditolak, GET/HEAD download didukung runtime. Manifest PHP method php_zip/minimumPhp 8.2 berisi dua file allowlist agent-php.zip/INSTALL.md, platform php, maksimal 4 MiB masing-masing. signedPayload Ed25519 authoritative, metadata mirror tidak dipercaya; file size/hash diverifikasi sebelum download/Range. Tidak ada token query atau signed URL publik. Batas 30 downloads/15 menit per owner dan dua slot download bersama paket Go per proses.

Bootstrap API menambahkan publicUrl, trustedProxies default kosong dan developmentLoopback false; artifactVersion adalah current verified PHP release atau unreleased jika belum tersedia. ZIP/bootstrap tidak membawa token/credential. PHP installer memakai token install-context/enroll yang sudah terikat domain/method; public probe tetap protocol B11/B15. Publisher/asset roots PHP terpisah dari Go; CLI release lama tidak berubah. [Tutorial dan verifier](INSTALL_PHP.md).


B18 menambahkan `GET /v1/admin/security`, POST `/security/mfa/{setup,confirm,disable,recovery}` (prefix `/v1/admin`), GET `/v1/admin/control/agents`, POST `/v1/admin/control/{rotations,migrations}` dan bearer-only GET `/v1/agents/control`, POST `/v1/agents/control/ack`. Mutation admin memerlukan session, Origin/CSRF serta password; operasi rotasi/migrasi/disable/regenerate membutuhkan faktor sekali pakai jika MFA aktif. Setup secret dan recovery code hanya dikembalikan saat provisioning terkait. Status agent tidak mempunyai credential. Endpoint agent control dapat mengembalikan credential pending kepada agent terikat; jangan log responsnya. ACK memakai credential baru, migrationId dapat kosong untuk rotasi saja. `rotation_pending` berarti 409; `factor_invalid` berarti 401 dan tidak otomatis membatalkan sesi owner yang melakukan reauthentication. `verifyMfa` memakai Origin, challengeId dan code (TOTP/recovery); sukses menghasilkan LoginResult authenticated serta cookie/CSRF, password login mfa_required tidak menghasilkan sesi. Public POST `/v1/control/identity` menerima nonce random dan menghasilkan proof signed purpose/nonce/origin tanpa bearer; limiter durable 300 request per IP per 15 menit. Tool memverifikasi proof sebelum mengirim credential. OpenAPI kini 69 operasi.


B19 membedakan HTTP 503 `decision_busy` ketika slot evaluasi/probe penuh dari `control_unavailable` untuk dependency/transaksi. Respons hanya mengandung code/message/requestId tanpa error SQL/secret. Adapter tetap mengikuti kebijakan 503/timeout dan retry identik yang sudah ada; tidak mengganti event ID untuk mengatasi overload. Runner kapasitas menyimpan jumlah per error code, bukan body atau bearer.
