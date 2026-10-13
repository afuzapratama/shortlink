# B13 — observasi bot/crawler, Tor, reputasi dan rate limit

Revisi dev 11 Oktober 2026 menambah integrasi [ProxyCheck](PROXYCHECK.md), key terenkripsi yang dikelola owner dari dashboard dan pilihan proxy/unknown fallback per shortlink. Bagian B13 di bawah merupakan baseline sebelumnya; provider ini memakai lookup pusat + cache, bukan daftar feed Tor/reputasi. Dukungan provider tidak berarti kuota, akurasi dan kapasitas publik sudah lulus.

Default domain **observe**. Agent mengenali sinyal lokal dan mencatat kandidat keputusan aturan risiko, tetapi routing nyata tetap menjalankan aturan biasa dan tujuan published shortlink. Enforce hanya menerapkan aturan risiko yang sudah dipublish; mengubah mode tanpa membuat aturan bukan blokir otomatis. Off mengabaikan aturan risiko dan tidak mencatat risk observation. Aturan geo/device/OS/browser/ASN tetap berlaku pada ketiga mode.

## Sumber dan batas deteksi

Parser [x-way/crawlerdetect](https://github.com/x-way/crawlerdetect) v0.2.32 (MIT) memakai pola UA dan dipin pada Go modules. Regex dikompilasi sekali; cache LRU thread-safe dibatasi 4.096 UA, UA maksimal 4.096 byte. Device/OS/browser tetap uap-go. Pola bot dapat dipalsukan atau dilewati memakai UA browser; tidak menjamin semua bot terdeteksi. UA kosong/terlalu panjang menjadi suspected_automation, bukan otomatis known_bot. Visitor IP, UA mentah dan query tidak ditambahkan ke event.

Kategori UA: search (Googlebot/Bingbot/DuckDuckBot/Yandex/Baidu), preview (Facebook/Twitter/Slack/Discord/Telegram/WhatsApp bila dikenali crawlerdetect), automation lainnya, dan unknown. Kategori bukan identitas crawler resmi; tidak ada auto-allowlist berdasarkan UA atau reverse DNS per klik. Rule known_bot dapat memakai botCategory wildcard/search/preview/automation/unknown untuk membedakan preview dari crawler pencarian. Membuat pengecualian preview melalui redirect berprioritas lebih tinggi adalah keputusan operator; UA mudah ditiru.

Tor memakai [exit list resmi](https://check.torproject.org/torbulkexitlist), diunduh pusat pada background/command, bukan per klik. IPv4/IPv6 dan CIDR lokal dibaca ke exact-set/prefix index memori. Source Tor terkini mayoritas IPv4; tidak ada klaim cakupan IPv6 atau semua anonymizer. Feed reputasi menerima file IP/CIDR operator. [stamparm/ipsum](https://github.com/stamparm/ipsum) adalah kandidat reputasi malicious IP, **bukan database proxy/VPN**; format hit-score upstream tidak langsung diperlakukan sebagai CIDR. B13 tidak mengunduh atau memasukkan seluruh daftar tersebut secara otomatis. Tidak ditemukan dalam feed berarti unknown; GeoLite ASN bukan bukti proxy, VPN atau residential proxy. Provider proxy lengkap belum dikonfigurasi dan tidak dijanjikan.

## Konfigurasi pusat dan feed

```
RISK_FEED_ROOT=/absolute/path/project/data/risk
RISK_TOR_ENABLED=true
```

Docker pusat kelak memasang direktori host tersebut pada `/var/lib/shortlink/risk`, lalu RISK_FEED_ROOT memakai path di container. Agent menggunakan `dataDir/risk/current.json` di volume persisten existing; tidak menambah bucket/registry atau key eksternal.

Command memakai env pusat yang sudah ada, termasuk DATABASE_URL/signing key existing:

```
control refresh-feeds   # fixed HTTPS Tor URL bila enabled; lalu publish signed bundle
control publish-feeds  # publish staging yang ada tanpa download jaringan
```

Letakkan satu IP atau CIDR masked per baris pada `staging/reputation.txt` di RISK_FEED_ROOT; komentar `#` dan baris kosong diizinkan. Prefix terlalu luas (IPv4 < /16, IPv6 < /32), private/loopback, URL dan format salah ditolak. Maksimum 4 MiB/100.000 entry per sumber, 8 MiB envelope. Pilih sumber dengan izin pemakaian yang sesuai dan cek false positive sebelum enforcement. Waktu mtime file menyatakan kapan data sumber diperoleh; jangan menyentuh ulang file lama hanya agar terlihat fresh.

Tor refresh menggunakan fixed official HTTPS URL, timeout 15 detik, tanpa redirect. Data invalid/empty atau perubahan >4x (bila baseline >100 entry) ditolak dan source terakhir dipertahankan. Refresh source gagal dicatat pada log, publisher boleh mendistribusikan cache lama; ini tidak mengubah timestamp sumber menjadi baru. control serve mempublish staging pada startup dan maintenance lima menit; refresh Tor jika file belum ada/lebih dari enam jam. Source yang tidak dikonfigurasi dilaporkan unconfigured.

GET /v1/assets/feeds/manifest mengirim satu **bundle lengkap signed**, bukan URL download arbitrary. Payload schema 1: publishedAt, expiresAt, torFetchedAt/reputationFetchedAt dan dua daftar; version adalah SHA-256 payload, Ed25519 memakai key pusat yang sudah pinned. Envelope expiry maksimal 48 jam; freshness sumber maksimum 48 jam. Source stale tidak digunakan untuk matching meskipun envelope fresh. Feed tidak mengubah routing revision; event/heartbeat mencatat versi feed yang benar-benar aktif, snapshot feedVersion tetap nullable untuk kompatibilitas. Endpoint `/feeds/releases/{version}` pada kontrak lama belum diimplementasikan; runtime B13 hanya membutuhkan bundle current.

Agent mengunduh pada startup dan setiap 15 menit, ETag/304, timeout 20 detik, bounded retry 30/60/120/240/480 detik. HTTPS wajib melalui konfigurasi origin agent, HTTP hanya development loopback. Signature/version/schema/size/source timestamp diperiksa sebelum file 0600 fsync/rename dan swap atomic memori. Cached publishedAt mencegah rollback selama file cache dipertahankan. Restart mempertahankan feed; tamper tidak mengganti cache valid. Saat manifest/source expired, lookup menjadi unknown/stale, bukan memakai daftar basi untuk blokir. Known-bot/rate signals tetap tersedia. Readiness routing B11 tidak menganggap feed opsional gagal sebagai kegagalan seluruh domain; panel/feed evidence menunjukkan diagnosisnya.

## Aturan, mode dan capability

Rule risk: `*`, known_bot, suspected_automation, tor_exit, reputation_flag, rate_limited. Semua kondisi AND dan ordering scope/prioritas existing tetap berlaku. Ordinary mandatory global blocks tetap pertama pada semua mode. Mandatory risk blocks ditolak karena tidak boleh memberi makna enforcement yang bertentangan dengan mode observasi/off; gunakan aturan risiko global biasa.

GET/PATCH `/v1/admin/domains/{id}/protection` untuk owner (PATCH membutuhkan CSRF) memakai expectedRevision dan settings `{mode,ratePerSecond,burst}`. Default rate 20/detik, burst 40; konfigurasi 1–1.000/detik dan burst 1–5.000. Publish atomik meningkatkan desired revision, menyimpan signed snapshot dan audit protection_published. Revision conflict 409; agen offline masih memakai mode lama sampai sync/lease selesai. Mode off/observe dapat disiapkan sebelum enrollment, tidak memberi status Siap.

Heartbeat B13 menyatakan protectionVersion=1; pusat memeriksa capability CLI aktif dan heartbeat <=90 detik sebelum publish enabled risk rules atau mode enforce. Agent lama tidak diam-diam diberi konfigurasi yang tidak bisa ditegakkan. Reported supported pada UI bukan readiness atau proof hasil probe. Capability generic Domain yang belum terkonfirmasi tidak diubah; gate B13 memakai heartbeat eksplisit. API/PHP masih B15–B17.

Challenge opsional tetap tidak tersedia tanpa provider/cookie verifier. Draft/publish challenge ditolak dengan 422, tidak dianggap sukses dan tidak diubah menjadi allow. Tidak ada key/account Turnstile yang dikonfigurasi, sehingga B13 tidak memasang challenge eksternal.

## Rate limit lokal dan event

Token bucket per agent/domain, keyed pada ClientIP yang sudah dinormalisasi melalui trusted proxy rules. IPv4 per alamat, IPv6 per /64. Maksimum 16.384 bucket LRU; IP berputar, eviction, restart atau perubahan budget dapat mereset kuota. Rule-only sync mempertahankan bucket; perubahan budget/mode mereset state. Ini sinyal lokal, bukan kuota global lintas agent atau perlindungan volumetric DDoS. IP NAT bersama perlu burst dan evaluasi false positive.

GET/HEAD untuk slug valid mengonsumsi bucket; hanya GET yang menghasilkan event klik. Health/probe/404/host salah tidak dihitung. Melampaui budget menambahkan rate_limited; **butuh rule rate_limited yang dipublish untuk block/fallback pada enforce**. Default observe tidak mengubah response. Tidak ada SQL/API per klik, termasuk lookup Tor/reputasi.

Decision/event optional risk memuat mode, daftar sinyal, botCategory, feed/source status, candidateAction/candidateRuleId/candidateReasonCode. Action/reason utama adalah keputusan nyata: misalnya observe crawler → redirect link_destination dengan candidate block rule_block. Feed version null ketika tidak ada feed fresh. Unknown proxy tetap unknown; risk signal tidak diberi nama “proxy terdeteksi” tanpa bukti.

Worker B12 menyimpan agregat risk per jam/domain/link/mode/sinyal/kandidat secara transaksi yang sama dengan raw processed marker dan counter. Retry/worker rollback tidak menggandakan risk observation. Summary domain berisi bucket sekitar 24 jam terakhir, per rule/reason agar false positive dapat ditelusuri. UI detail domain menyediakan mode/budget, source freshness, summary serta refresh; simulator menerima sinyal sintetis dan mode domain. Daftar raw traffic/filter lengkap mengikuti B14. Retensi risk aggregate mengikuti EVENT_AGGREGATE_DAYS (90 hari default).

## Validasi dan gate tersisa

Unit/race: cache/crawler categories, human/spoof boundary, strict enum, observe/enforce/off, category-specific rule, mandatory normal block, signed feed tamper/rollback/restart/expiry/stale-source, IP/CIDR, burst/refill/IPv6 dan cache bound. Runtime memeriksa event observasi/actual block, budget tidak reset pada rule-only sync, Tor fixture dan off. PostgreSQL terisolasi memeriksa capability/CSRF/revision/offline/revoke, feed bearer/ETag, candidate risk agregat/dedup. Real native/Docker agent memakai signed config/MMDB/feed, observe → enforce dan outage pusat.

Batas: akurasi bot/proxy dunia nyata, NAT false positive, throughput 100–300 klik/detik, hostile UA dan cardinality IP perlu B19. VPS bersih, HTTPS publik, ARM native tetap [agenda owner](TEST_MATRIX.md). Rilis B13 dibangun untuk amd64/arm64; cross-build tidak membuktikan native ARM.

Rilis lokal v0.13.0-b13 telah dipublish dan pusat tetap di http://127.0.0.1:8086, dashboard http://127.0.0.1:5173. Unit/race, integrasi installer Docker AMD64 dan 18 live browser tests lulus. Keenam artefak lulus Go/Python/OpenSSL verifier. Private data uji b13-before.json/b13-after.json/b13-verification.json berada pada data/control (ignored), tanpa password/token. Owner smoke check tidak menambahkan fixture/domain ke database pengguna. B14 berikutnya untuk analytics/log browsing.
