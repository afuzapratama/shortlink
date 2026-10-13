# URL pembuka dan manage agent

Revisi dev lokal 12 Oktober 2026. Root domain dan path publik yang bukan shortlink published diarahkan ke URL pembuka **per domain**. URL belum diisi menghasilkan **503**. Shortlink published tetap memakai audience, tujuan utama dan fallbacknya sendiri.

## Pengaturan dan routing

Dashboard: **Domain & Agent → pilih domain → URL pembuka & manage agent**. URL harus HTTP(S), tanpa credential/whitespace atau hostname agent sendiri. Simpan langsung membuat revision domain baru dan snapshot signed; ini pengaturan domain, bukan draft shortlink. Kosongkan untuk kembali ke 503.

| Request | Hasil |
|---|---|
| GET/HEAD `/` | 302 ke opening; kosong → 503 |
| Slug/path publik tidak ditemukan | 302 ke opening; kosong → 503 |
| Shortlink published | Keputusan shortlink normal |
| `/__agent/manage` | Manage terautentikasi |
| Setup/preflight/readiness/health | Tetap terpisah; route internal tidak dikenal tetap 404 |

Root/missing redirect tidak membuat event/counter dan tidak meneruskan query pengunjung. Go memakai URL snapshot selama lease valid, tanpa lookup GeoLite untuk root/missing. PHP mengambil pengaturan pusat; 404 internal dari keputusan missing diterjemahkan menjadi opening redirect/503. HEAD PHP mengecek keberadaan slug tanpa click. Kegagalan pusat/credential/snapshot tetap fail-closed; URL di database tidak menjamin respons bila konfigurasi tidak dapat diverifikasi.

## Manage agent

Owner membuat **akses manage sekali pakai** dari detail domain. Buka `https://domain-agent/__agent/manage` dan masukkan key lewat form, bukan URL. Key berlaku lima menit untuk satu agent/domain. Key baru menggantikan key yang belum dipakai; exchange menghabiskan key atomik dan membuat sesi 15 menit. Logout menghapus sesi pusat dan cookie.

Manage menyediakan edit URL pembuka, daftar shortlink domain itu (tujuan published/fallback/status/counter), tombol buka published, pagination lima link, muat ulang dan logout. Edit/publish shortlink serta aturan global tetap di dashboard pusat. URL opening Go baru berlaku setelah sinkronisasi.

Hash key/sesi disimpan **di pusat**, bukan password permanen atau key mentah di SQLite agent. Ini penyederhanaan penyimpanan lokal agar Go/PHP memakai validasi yang sama tanpa mewajibkan SQLite pada hosting. Agent menggunakan credential miliknya untuk menjembatani sesi opaque dari cookie host-only HttpOnly, SameSite=Strict, Secure pada production. Owner cookie/password tidak dikirim ke agent. Manage perlu pusat online; redirect lokal Go dengan snapshot valid berjalan terpisah.

POST form memeriksa Origin yang sama; update/logout memerlukan CSRF. Domain diturunkan dari agent dan sesi, bukan input browser. Login dibatasi 10 percobaan/agent/15 menit. HTML meng-escape data; key tidak dipantulkan setelah submit. Log tidak memuat key/session/credential/body. Dump database sekarang mencakup hash akses/sesi dan tetap disimpan privat.

## API dan kompatibilitas

- Owner PATCH `/v1/admin/domains/{id}/opening`: `openingUrl`, `expectedRevision`.
- Owner POST `/v1/admin/domains/{id}/manage-key`: key sekali tampil, expiry dan URL tanpa key.
- Agent POST `/v1/agents/opening`: opening dan keberadaan slug published.
- Agent POST `/v1/agents/manage/exchange`: key menjadi sesi.
- Agent POST `/v1/agents/manage/session`: `view`, `update`, `logout` dalam cakupan agent; update/logout dengan CSRF.

Migration 013 menambah URL domain, hash key/session dan routingVersion 3. Go v3 diperlukan sebelum opening nonempty dipasang pada domain yang mempunyai agent; enrollment/renewal memeriksa kompatibilitas. Jangan downgrade binary saat snapshot fitur baru masih tersimpan.

PHP **v0.20.1-b20** menambah `private/src/Manage.php`; verifier ZIP menerima file baru dan paket lama tetap immutable. Update seluruh source private sambil mempertahankan config/bootstrap. Default Go dev kini `b20-dev`. Paket/runtime baru belum dipasang ke VPS/hosting publik dan release signed Go production belum diterbitkan ulang.

Demo 8091 tetap fixture terpisah, opening contoh `https://example.org/?source=shortlink-opening-demo`; key pusat utama tidak berlaku di sana. Manage sungguhan memerlukan agent yang di-enroll ke pusat yang sama. Tests Go/PHP/PostgreSQL disposable menguji root/missing, no-click, key one-use/expiry, Origin/CSRF, cookie, scope domain, update/logout dan loop rejection.

Verifikasi final lokal: build dan 78 operasi kontrak lulus; 32 tes UI dan 30 tes browser API/PostgreSQL desktop/mobile lulus. Backend race, Go race, PHP lint/unit/HTTP/browser serta downloaded signed ZIP/wizard lulus. Paket PHP v0.20.1-b20 sudah dipublish dengan key pusat lokal; server API 8086 dan dashboard 5173 diperbarui. Owner tidak dibootstrap ulang dan domain utama tidak diisi fixture.
