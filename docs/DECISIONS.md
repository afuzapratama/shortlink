# API keputusan pusat — B15

Mode `local` tetap menjalankan routing pada agent Go. Mode `api` memakai credential agent `php_zip` yang terikat pada satu domain: adapter mengirim visitor IP/UA dan slug; pusat menghitung GeoLite, device/OS/browser/ASN, crawler, feed risiko dan budget IP dengan mesin Go yang sama. ZIP/wizard/public handler PHP tersedia untuk pengujian lokal sejak B16; download/publisher dashboard tersedia sejak B17.

## Request dan binding

`POST /v1/decisions`, bearer agent aktif PHP, Content-Type application/json, maksimal 16 KiB:

```json
{
  "eventId": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
  "occurredAt": "2026-10-10T12:00:00Z",
  "domainId": "id-domain-dari-bootstrap",
  "slug": "hello",
  "signals": {
    "ip": "1.1.1.1",
    "userAgent": "Mozilla/5.0 ..."
  }
}
```

Event ID/waktu pada contoh adalah placeholder. Request nyata memakai 32 byte random kriptografis dalam base64url tanpa padding dan waktu kejadian UTC. UA wajib hadir, dapat kosong (suspected automation), maksimal 4096 byte. Slug 1–80 huruf/angka/underscore/hyphen. Waktu dalam 48 jam sebelumnya hingga lima menit ke depan. IP canonical IPv4/IPv6 tanpa port/zone, unicast; IPv4-mapped IPv6 dinormalisasi. Private IP tidak memberi lokasi publik dan dapat menghasilkan country unknown. Country/device/OS/browser/ASN/bot/proxy/clientHints dari adapter ditolak sebagai field override. Client Hints belum digunakan.

IP socket pusat adalah koneksi hosting, bukan visitor IP. Adapter tepercaya wajib mengambil IP dari REMOTE_ADDR atau proxy chain dengan allowlist CIDR; jangan percaya X-Forwarded-For pengunjung secara bebas. Pusat tidak dapat membuktikan kebenaran IP yang dikirim adapter yang credential-nya telah bocor. Ekstraksi host/proxy dan proteksi credential pada PHP diuji B16. Cookie owner, install token unused, credential Go/CLI dan domain lain tidak dapat memakai keputusan API.

## Keputusan dan retry

Respons `Decision` memakai action/reason/rule/destination aktual, event ID yang sama, revision konfigurasi published dan versi GeoLite/feed aktif. Mode observe mengembalikan kandidat risiko terpisah tanpa menerapkannya. Link draft/slug tidak dikenal menghasilkan 404 tanpa klik. Edit tujuan draft tidak memengaruhi keputusan sampai publish; publish/rollback API memperbarui snapshot/revision pusat, tidak menunggu polling daemon PHP.

Transaksi mengambil lock domain lalu agent, mengecek revoke kembali, membaca revision dan memakai compiled snapshot immutable. Cache routing dibatasi 128 domain dengan invalidasi revision. Keputusan, classified event source `php` dan replay record disimpan atomik dengan synchronous commit sebelum 200. Worker B12 mengagregasi event ini sekali; adapter tidak mengirim ulang event keputusan ke `/v1/events/batch` (endpoint itu tetap CLI-only).

Retry wajib mempertahankan event ID, occurredAt, slug dan seluruh sinyal. `X-Decision-Replayed: true` mengembalikan keputusan pertama, termasuk jika tujuan/aturan/feed sudah berubah; tidak menambah event, counter klik atau charge budget IP. Payload berbeda pada agent/event ID yang sama menghasilkan 409 event_conflict. Domain/method salah menghasilkan 422; revoke menghasilkan 401 bahkan untuk retry yang sudah tersimpan. Fingerprint canonical request memakai HMAC purpose-separated dari key pusat; IP pengunjung dan metadata ASN disimpan pada event keputusan baru. UA mentah tidak disimpan pada event, dan respons replay/log aplikasi tetap tidak memuat IP/UA. Parser/limiter tetap dapat menyimpan sinyal transient dalam cache memori.

Replay tersedia dalam window validasi occurredAt 48 jam, bukan 48 jam baru sejak retry. Record/hash dibersihkan bersama dedup retention raw (default tujuh hari, minimum tiga); reboot mempertahankan replay dalam PostgreSQL. Watermark worker dan log pusat tetap mengikuti B14. Untuk PHP, jumlah mencatat keputusan yang diproduksi pusat, bukan bukti browser sudah menerima/melakukan redirect; response dapat hilang setelah commit.

## Timeout, aset dan risiko

Endpoint mempunyai context budget dua detik dan maksimal 16 evaluasi/probe in-flight per proses sejak tuning B19 (sebelumnya delapan). Admission B19 menampung maksimal 16 penunggu selama 25 ms sebelum menolak dengan 503 decision_busy; budget dua detik dimulai sebelum admission. Context membatasi SQL/commit; pembacaan request/file lokal tidak merupakan jaminan SLA wall-clock. Adapter default: connect timeout satu detik, total tiga detik, TLS verification wajib, API redirect tidak diikuti. Retry paling banyak sekali dengan request identik hanya jika sisa budget cukup; jika tidak, tampilkan 503 tanpa Location. B15 belum menyediakan fallback offline yang dapat melewati pemeriksaan pusat; fallback lokal hanya boleh ditambahkan dengan konfigurasi admin explicit dan diuji pada batch adapter. 401/403 menghentikan pemakaian credential, 404 tetap not found, 409 tidak diperbaiki dengan mengganti ID otomatis.

GeoLite signed yang belum tersedia/kedaluwarsa atau kegagalan DB mengembalikan 503 tanpa tujuan. IP yang tidak tercakup database valid menghasilkan unknown, bukan kegagalan lookup. Pusat membuka reader GeoLite saat startup dan menyimpan last-good reader; pointer GeoLite/feed diperiksa paling cepat sekali per menit, tanpa request jaringan per klik. Signature/reader rusak tidak mengganti last-good cache; expiry cache tetap diberlakukan. Feed Tor/reputasi optional yang unavailable/stale tetap unknown sesuai aturan published, bukan bukti bebas proxy.

Risk mode API berjalan pusat sehingga tidak memerlukan heartbeat capability CLI. Default observe. Enforcement membutuhkan aturan risiko explicit; challenge belum tersedia. Limiter pusat dibatasi 128 domain, masing-masing 16.384 IP buckets, IPv6 /64, per proses/domain. Restart/eviction atau perubahan rate/burst dapat mereset bucket; retry sukses tidak mengonsumsi bucket baru. Percobaan yang gagal commit setelah evaluasi dapat sudah mengonsumsi budget transient. Beberapa replica memiliki budget terpisah; shared limiter/load tuning merupakan B19. Advisory lock event serializes transaksi ingest/decision/retensi; throughput API 100–300 RPS belum dibenchmark.

## Probe adapter dan idle

`POST /v1/agents/probe` memakai bearer PHP, body `{domainId,hostname,nonce,version}` dengan nonce random 32 byte base64url. Hostname/domain terikat credential. Pusat mengecek GeoLite/revision, mencatat aktivitas API dan mengembalikan `ProbeState` dengan header `X-Shortlink-Proof` HMAC purpose `response` atas byte JSON asli. Probe tidak menulis event/counter klik dan tidak sendiri menandai domain Siap.

Public handler PHP B16 di `/.well-known/shortlink-ready` harus memverifikasi nonce dan HMAC purpose `request` sebelum memanggil pusat. Kembalikan byte JSON dan response proof tanpa re-encoding; tidak mengirim API bearer ke pemanggil publik. Endpoint ini bukan proxy credential untuk request arbitrary pengunjung. Owner probe tetap memeriksa public DNS yang dipin, TLS, tanpa redirect, HMAC, nonce, domain, revision dan GeoLite, lalu mengecek mutable prerequisites lagi sebelum commit readiness.

PHP tidak mengirim heartbeat CLI. Applied revision berarti revision terakhir dilayani oleh API/probe. Aktivitas yang lebih lama dari 90 detik dibaca `idle`, bukan offline; antrean dan stale heartbeat pada system events hanya menghitung CLI. Readiness probe memiliki TTL lima menit dan menjadi attention jika kedaluwarsa/revision berubah, tanpa menyimpulkan hosting idle telah mati. UI/download/diagnosis hosting lengkap mengikuti B17.

## Verifikasi lokal

Migrasi `009_decisions.sql` wajib sebelum binary B15. Test PostgreSQL terisolasi/race mencakup concurrent retry, binding/auth, iPhone + GeoLite/ASN parity simulator, edit/publish dan immutable replay, request override, privacy payload, rollback event/replay saat error, DB lock timeout, GeoLite fail-closed, feed observe/enforce, retry budget, probe proof/no-click, public probe orchestration terkontrol dan PHP idle/revoke. Database fixture bukan database owner development; HTTPS publik/hosting nyata belum dianggap lulus.

Agent Go tetap rilis `v0.13.0-b13`. B16 menambahkan ZIP PHP, layout public/private, client timeout/retry, wizard/recovery dan setup lock; hosting nyata dan target kapasitas belum dinyatakan lulus.

Verifikasi ulang setelah restart lokal 10 Oktober 2026: backend PostgreSQL terisolasi dengan race detector lulus, build dashboard lulus, dan 22 pengujian browser desktop/mobile lulus. Pusat lokal `http://127.0.0.1:8086` dan dashboard `http://127.0.0.1:5173` kembali aktif. Smoke login/logout owner, GeoLite, manifest agent, analytics dan penolakan cookie owner pada endpoint agent lulus; fingerprint data owner/domain/link/key tetap sama. Bukti lokal yang diabaikan Git berada di `data/control/b15-control-tests-final.log`, `b15-browser-tests-final.log`, dan `b15-verification.json` tanpa menyimpan credential login.

B16 kini mengimplementasikan client PHP, public probe handler dan wizard di [INSTALL_PHP.md](INSTALL_PHP.md); download/publisher dashboard tersedia sejak B17. Default client 503/no Location, batas timeout/retry identik dan pemisahan decision/receipt tetap berlaku.


B18: perubahan credential memakai protokol pending/persist/ACK, menjaga agent ID/domain dan queue. Operator menghentikan runtime saat private config diganti; PHP memerlukan config dan bootstrap origin yang konsisten. Migrasi signed tidak cukup untuk membuktikan target hidup: tool meminta nonce proof dengan pinned root sebelum mengirim bearer. Root tetap stabil agar trust manifest, enrollment recovery dan enkripsi TOTP tidak terputus; tidak diklaim mendukung rotasi root overlap. [Keamanan dan workflow hosting manual](SECURITY.md) menjelaskan batas ACK workstation versus runtime hosting.
