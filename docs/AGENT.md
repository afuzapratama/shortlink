# Agent Go — B06–B09

Revisi B20 lokal: [URL pembuka dan manage agent](AGENT_MANAGE.md). Root/missing kini 302 ke opening atau 503 bila kosong; route internal tetap terpisah. Bagian B06–B09 berikut adalah baseline awal, bukan perilaku root terbaru.

Runtime lokal tersedia di `services/agent`. Redirect memakai lookup snapshot immutable di memori, tanpa API/SQL per klik. Agent polling pusat setiap 15–30 detik dengan jitter; timeout HTTP 10 detik, redirect HTTP dari pusat tidak diikuti. HTTPS wajib, kecuali development HTTP dengan IP loopback. GeoLite tersedia B07 (lihat [GEOIP.md](GEOIP.md)); aturan/publish B08 tersedia. Antrean/event B12 tersedia; installer CLI B10 dan readiness B11 tersedia lokal. Bot/feed/proteksi B13 tersedia; acceptance VPS/TLS/ARM belum diuji.

## Coba lokal tanpa DNS

Dari root project:

```sh
cd services/agent
go run ./cmd/agent-demo --listen 127.0.0.1:8091
```

Terminal lain:

```sh
curl -I -H 'Host: agent.example.test' http://127.0.0.1:8091/hello
curl -I http://127.0.0.1:8091/health/ready
curl -I -H 'Host: domain-lain.example.test' http://127.0.0.1:8091/hello
```

Hasil berturut-turut: `302` dengan Location `https://example.org/?source=shortlink-demo` dan Cache-Control no-store; `200` health runtime; `421` host asing. Incoming query tidak diteruskan. HEAD mengembalikan header redirect tanpa body. Jika ingin membuka melalui browser, gunakan DNS/hosts lokal untuk `agent.example.test`, lalu buka `http://agent.example.test:8091/hello`; akses dengan hostname localhost ditolak untuk routing karena binding domain.

**Demo adalah fixture terisolasi**, dengan pusat HTTP sementara, signature Ed25519 dan credential baru setiap proses. Tidak terhubung ke dashboard/PostgreSQL pengguna. Data private sementara dibersihkan saat Ctrl+C/SIGTERM. Demo tidak membuktikan DNS/HTTPS produksi, GeoLite, publish, analytics, atau kapasitas 100–300 klik/detik.

## Menjalankan agent terhubung ke pusat

Build dari `services/agent`:

```sh
mkdir -p ../../data/agent
go build -o ../../data/agent/agent ./cmd/agent
```

Salin [agent.example.json](../deploy/agent.example.json) ke lokasi private di luar web root. Sesuaikan listen, controlUrl, domainId, hostname, credential, verificationKey dan dataDir. File konfigurasi wajib private (0600); direktori data 0700.

Dari root project:

```sh
chmod 600 /absolute/private/path/agent.json
./data/agent/agent serve --config /absolute/private/path/agent.json
```

Provisioning B06 masih manual: verifikasi domain DNS TXT di dashboard, buat token metode CLI, ambil bootstrap publik melalui endpoint admin, lalu tukar token melalui `POST /v1/agents/enroll`. Payload membawa token, installationId, installationProof random 32 byte dalam base64url tanpa padding, hostname, method `cli`, dan version. Simpan proof private sebelum request agar retry memakai proof yang sama dalam jendela recovery B05. Ambil credential dari hasil enrollment, domainId/hostname dan manifestVerificationKey dari bootstrap yang dipercaya. Jangan menaruh credential/token di argumen shell, URL atau repo. Paket/distribusi B09 tersedia; installer otomatis mengikuti B10.

`controlUrl` adalah origin API yang bisa diakses agent. Pada lingkungan lokal saat ini, `http://127.0.0.1:5173` mem-proxy API pusat 8086; port bootstrap pusat mengikuti CONTROL_ORIGIN. Domain pusat bisa disesuaikan di file konfigurasi, tetapi migrasi otomatis/rotasi trust mengikuti B18. Jangan menerima public key baru hanya karena server mengirimkannya; key dipin melalui provisioning terpercaya.

Enrollment membuat snapshot awal **tanpa link draft**. Agent dapat menerima snapshot kosong dan melaporkan revision tanpa menghasilkan redirect link. Publish dashboard tersedia B08 setelah domain verified/enrolled. Integrasi menjalankan publish, perubahan tujuan dan rollback lewat store pusat di database test terisolasi, tanpa memodifikasi data pengguna. Runtime health ready hanya berarti snapshot valid dan lease aktif, bukan status Siap produksi pada dashboard.

## Snapshot, outage, dan proxy

Agent hanya menggunakan hasil decode `signedPayload` setelah verifikasi signature; duplikasi field envelope diabaikan. Domain/mode, revision, lease, slug unik, state published, URL tujuan dan direct loop divalidasi. Snapshot dengan rules/feed ditolak sampai capability tersebut diimplementasikan. GeoRelease harus valid dan, jika snapshot memintanya, sesuai rilis aktif agent. URL tujuan hostname yang sama ditolak tanpa memandang port/case/trailing dot. Loop lintas domain belum dideteksi pada B06.

Konfigurasi dipersist sebagai `snapshot.json` bertanda tangan: file temporary 0600, fsync, rename atomik, lalu fsync direktori, sebelum swap memori dan heartbeat. B06 memakai file atomik untuk snapshot, bukan SQLite; SQLite antrean event dan metrik loss tersedia pada [B12](EVENTS.md). Polling ulang/crash tidak menghapus cache valid. Cache corrupt/signature salah menghentikan startup untuk diagnosis. Cache expired mempertahankan revision anti-rollback tetapi tidak mengaktifkan redirect. Restore cache valid saat restart tetap memverifikasi signature dan binding.

Revision lebih rendah ditolak. Same-revision renewal boleh memperpanjang lease, tetapi tidak mengubah routing. `304` tidak memperpanjang lease. Outage/5xx atau snapshot rusak mempertahankan konfigurasi terakhir sampai lease habis; tanpa snapshot/lease valid respons `503`. Tidak ada fallback otomatis pada B06. Response pusat `401/403` memblokir routing segera dan menyimpan marker `revoked`; blok bertahan saat restart. Reprovision agent pengganti dengan direktori data private baru. Agent offline belum bisa mengetahui pencabutan pusat sebelum polling/lease berakhir.

Default `trustedProxies: []`: IP dari socket, X-Forwarded-For diabaikan. Bila peer socket termasuk CIDR tepercaya, agent memvalidasi X-Forwarded-For lalu berjalan dari kanan ke kiri sampai hop pertama yang tidak dipercaya; mendukung IPv4/IPv6. Header Forwarded/X-Real-IP/X-Forwarded-Host tidak digunakan. Proxy harus mempertahankan Host domain dan mengatur rantai X-Forwarded-For; jangan memasukkan semua jaringan ke trustedProxies. IP baru dinormalisasi pada B06; B07 menyediakan lookup GeoLite terpisah untuk integrasi aturan B08; analytics mengikuti B12.

## Verifikasi

Dari root:

```sh
./scripts/test-agent.sh
./scripts/test-control.sh
```

Test agent menggunakan race detector: binding host, redirect/query/HEAD, trusted proxy IPv4/IPv6/spoofing, signature/tamper, revision rollback, same-revision lease renewal, expiry/304, gagal persist, cache/restart, revoke marker, dan redirect concurrent saat sync. Test control menjalankan binary agent terhadap API pusat/PostgreSQL asli di schema sementara: snapshot signed, redirect, applied heartbeat dan outage. Test tidak membutuhkan DNS publik, Docker agent, atau perubahan database pengguna.

## B08 — keputusan lokal

Agent memverifikasi binding, IDs/slug dan rule snapshot, lalu mengompilasi core shared sebelum persist/activate. GeoLite country/ASN berasal dari socket IP atau chain trusted proxies; User-Agent diparse/cache lokal. Engine menggabungkan mandatory global block, global/domain/link, priority/tie dan AND country/device/OS/browser/ASN. Block menghasilkan 403 tanpa Location; redirect/fallback 302 no-store. Unknown tidak disimpulkan menjadi perangkat/model tertentu.

Risk rules/feed B13 tersedia melalui mode domain dan capability heartbeat; challenge tetap tidak didukung dan ditolak. Aturan per link dipisah saat compile sehingga request tidak memindai rule link lain. Snapshot <=8 MiB envelope, payload pusat <=3 MiB, effective rules/domain <=10.000 dan <=100 per set. Routing mencatat keputusan GET melalui antrean B12; HEAD dan request tanpa link valid tidak dihitung. Parser UA bisa dipalsukan dan iPad desktop mode bisa tampak macOS/desktop; akurasi semua model tidak dijamin.

Publish/rollback menghasilkan desired revision baru; polling/persist/heartbeat menerapkan revision. Cache lama tetap melayani selama lease valid bila pusat unavailable. [POLICY.md](POLICY.md) menjelaskan pengujian parity dengan binary agent, simulator, GeoLite sintetis dan PostgreSQL sementara. Demo dasar tetap fixture terisolasi; tidak ada benchmark kapasitas atau kesiapan DNS/HTTPS produksi.

## B09 — paket dan Docker runtime

Binary/image Linux amd64/arm64 tersedia dengan version command dan verifier release. Docker image scratch dengan CA bundle dan UID 10001, config read-only/data persistent private; Compose template tersedia. Signature, checksum, size dan image identity diverifikasi sebelum pemakaian. Origin/key/domain berada pada bootstrap pusat, bukan binary. [AGENT_RELEASES.md](AGENT_RELEASES.md) dan [INSTALL_AGENT.md](INSTALL_AGENT.md).

Docker amd64 menjalankan integrasi API/PostgreSQL/GeoLite/rules nyata di database sementara. ARM hanya cross-build/ELF/archive metadata, belum native execution. Tidak ada reverse proxy/HTTPS/install Docker otomatis sampai B10, atau production readiness sampai B11. Demo host B06 yang sudah berjalan tetap terisolasi.

## CLI installer B10

Binary rilis B10 menyediakan install/status/update/rollback. Installer menyiapkan root/config/Compose, enrollment token-only, signed image, Caddy atau external proxy; runtime snapshot/MMDB existing tetap dipakai. [INSTALL_AGENT.md](INSTALL_AGENT.md) memuat langkah, baseline dan batas uji. Rerun mempertahankan identitas/data; cache CLI diverifikasi lagi saat rollback. Readiness produksi tetap B11.

## Runtime probe B11

Endpoint /.well-known/shortlink-ready hanya menerima nonce canonical 32-byte dan MAC request yang valid, mengembalikan domain ID/revision/GeoLite/ready/version dan MAC response. Ready memerlukan snapshot signed masih dalam lease dan GeoReady. Response no-store, anonymous/invalid proof mendapat 404. Proxy harus meneruskan headers X-Shortlink-Nonce/X-Shortlink-Proof dan mempertahankan response proof/body. Pusat tidak mempercayai 200 saja; verifikasi raw response, domain dan revision tetap wajib. Readiness pusat dan expiry dijelaskan di BACKEND.md.

B13 memakai signed feed cache dataDir/risk, local index dan token bucket bounded. Heartbeat protectionVersion=1 + active feedVersion; event optional risk membedakan keputusan actual/candidate. [Kontrak proteksi](PROTECTION.md).
