# GeoLite pusat dan agent — B07

GeoLite Country, City dan ASN diunduh oleh geoipupdate hanya di VPS pusat. Publisher memvalidasi ketiga MMDB, lalu membuat rilis immutable dan manifest Ed25519 memakai seed pusat B05. Agent mengunduh melalui API dengan credential miliknya; tidak menerima license key MaxMind. Bunny/S3 belum diperlukan.

Implementasi: [packages/geoip](../packages/geoip), [API assets](../services/control/internal/httpapi/geoip.go), dan [halaman Sistem](../apps/dashboard/src/GeoIPSystem.tsx). Reader pinned `github.com/oschwald/maxminddb-golang/v2 v2.7.0`; [dokumentasi reader](https://pkg.go.dev/github.com/oschwald/maxminddb-golang/v2@v2.7.0). MMDB sintetis dibuat dengan mmdbwriter untuk test, bukan database lokasi produksi.

## Path dan update di VPS pusat

Struktur mengikuti PLAN: `<GEOIP_ROOT>/staging`, `releases/<sha256-content-id>`, dan `current.json`. Host memakai path absolut, misalnya `/opt/shortlink/data/geoip`; Docker memakai `/var/lib/shortlink/geoip`. Mount direktori, bukan satu file MMDB. Folder ini di luar web root, private, dan tidak masuk image/Git.

Salin `.env.geoip.example` menjadi `.env.geoip`, isi account/license key MaxMind sendiri di pusat, dan sesuaikan path. Perintah dari root:

```sh
mkdir -p data/geoip/staging
chmod 700 data/geoip data/geoip/staging
cp deploy/.env.geoip.example deploy/.env.geoip
# Isi credentials dan pin image updater yang sudah diverifikasi.
docker compose --env-file deploy/.env.geoip -f deploy/compose.geoip.yml up -d geoipupdate
```

Set `GEOIP_ROOT` di env control ke path host absolut. Publisher dapat berjalan bersama API host memakai `GEOIP_PUBLISH_ENABLED=true`: memeriksa staging saat startup dan setiap lima menit. Updater default memeriksa download setiap 24 jam; database belum lengkap tidak dipublish.

Untuk memisahkan publisher dari API: set `GEOIP_PUBLISH_ENABLED=false` pada API, dan jalankan binary control dengan env/database pusat yang sama:

```sh
# Dari root, setelah env control diekspor dan binary dibuild:
./data/control/control publish-geoip
# Atau proses berkelanjutan dengan SIGTERM/Ctrl+C untuk berhenti:
./data/control/control watch-geoip
```

CLI memakai database pusat untuk membaca seed yang sama; tidak membuat key signing lain. Database harus sudah migrate/bootstrap. Jangan mengganti seed secara manual. Publisher hanya mengganti pointer setelah salinan stabil, size/hash, tipe edition, build timestamp, reader.Verify serta smoke lookup IPv4/IPv6 valid. Kandidat gagal tidak mengganti rilis sehat. Build lebih lama daripada rilis sebelumnya atau lebih dari 30 hari ditolak. Konten yang sama tidak menghasilkan rilis/publishedAt/expiry baru.

## Docker pusat

`deploy/Dockerfile.control` dibuild dari root karena module GeoLite dipakai bersama API dan agent. Build context mengecualikan data, credentials dan MMDB. Compose GeoLite menyediakan profile opsional Linux:

```sh
docker compose --env-file deploy/.env.geoip -f deploy/compose.geoip.yml --profile publisher build geoip-publisher
docker compose --env-file deploy/.env.geoip -f deploy/compose.geoip.yml --profile publisher up -d geoip-publisher
```

`geoip-publisher` menjalankan watch-geoip, dengan root directory baca/tulis. `geoip-api` pada profile `central-api` menjalankan API dengan directory yang sama read-only dan publishing disabled. Keduanya memakai `deploy/.env.control` dan host networking Linux agar DATABASE_URL dapat mencapai PostgreSQL host. Set CONTROL_UID/GID sesuai pemilik folder private; default 1000:1000. `GEOIP_HOST_STAGING_DIR` harus `<GEOIP_HOST_ROOT>/staging`.

Jika API host sudah berjalan, gunakan publisher saja; profile central-api adalah cara menggantikan API tersebut pada port CONTROL_LISTEN. Compose tidak memasang/migrasi/bootstrap database atau HTTPS secara otomatis. Installer deployment lengkap tetap batch terkait. Image pusat dibuild lokal amd64 pada B07; rollout produksi/arm64 dan image signing belum divalidasi.

## Manifest, API dan agent

- `GET /v1/assets/geoip/manifest`: owner session atau agent credential; ETag content release ID; 304 tanpa body.
- `GET /v1/assets/geoip/releases/{release}/{edition}`: autentikasi sama, release/edition whitelist, streaming/MMDB ETag, dua download concurrent per proses dan 30 request per actor per 15 menit.
- `GET /v1/admin/system/geoip`: owner session; status pusat dan geoRelease/lastSeenAt agent CLI aktif. Halaman Sistem polling 30 detik dan tidak menganggap heartbeat lama sebagai versi terkonfirmasi.

Manifest memuat releaseId, publishedAt/expiresAt, edition/buildEpoch/size/SHA256, attribution/license URL, signature dan signedPayload. Agent memverifikasi **byte signedPayload**, lalu hanya memakai payload tersebut; outer fields bukan sumber kebenaran. Download URL adalah relative path pada origin pusat yang dikonfigurasi, bukan arbitrary URL. Dengan demikian domain pusat dapat diganti melalui konfigurasi tanpa menanam domain lama pada manifest atau mengirim bearer ke origin lain. Rotasi/migrasi otomatis tetap B18.

Agent B07 memeriksa manifest saat startup dan setiap enam jam dengan jitter; kegagalan retry dengan backoff capped sekitar delapan menit ditambah jitter. Ukuran MMDB maksimal 512 MiB per edition, manifest 64 KiB; download HTTP dibatasi lima menit per file. Redirect HTTP pusat tidak diikuti. Partial/corrupt/size/hash/signature/edition/build mismatch tidak mengaktifkan kandidat. Setelah reader dibuka/diverifikasi, pointer file dipersist atomik, lalu reader diganti dengan lock yang menunggu lookup lama selesai sebelum close. Restart memverifikasi cache/hash/readers kembali.

Set `geoipRequired: true` pada konfigurasi agent produksi. Tanpa database yang valid, health/redirect agent menghasilkan 503. Default false menjaga demo B06 tanpa GeoLite tetap bisa dijalankan; contoh konfigurasi sekarang true. Heartbeat melaporkan release yang benar-benar aktif; capability/readiness produksi pusat tetap memerlukan probe B11. Lookup negara/kota/ASN tersedia untuk mesin aturan B08; B07 belum menambahkan aturan routing atau analytics visitor.

## Expiry dan cleanup

Rilis memiliki expiry konservatif 29 hari sejak build database paling lama dalam set; konten yang tidak berubah tidak dapat memperpanjangnya. Lookup/health langsung menolak rilis expired. Agent cleanup hourly (juga sebelum update), publisher cleanup setiap lima menit saat proses berjalan. Rilis expired dibuang, termasuk file rilis aktif yang expired; pointer tetap menyimpan metadata signed untuk anti-rollback/diagnosis. Kandidat sementara sisa crash dihapus setelah 24 jam. Rilis sehat bertahan saat outage/unduhan gagal sampai expiry. Response asset 401/403 memblokir routing agent dan membuat marker revoke persisten sebagaimana B06.

Penghapusan dilakukan lebih awal daripada batas 30 hari setelah pembaruan yang disebut [GeoLite EULA](https://www.maxmind.com/en/geolite/eula). Jangan backup MMDB: backup database/key/konfigurasi dan referensi rilis, kemudian download ulang. Mesin/proses yang berhenti tidak bisa menjalankan cleanup; saat restart, cleanup dijalankan sebelum pemeriksaan update. Folder staging updater di pusat tetap memakai file terbaru, tidak membuat arsip versi lama.

## Status pengujian

```sh
./scripts/test-geoip.sh
./scripts/test-agent.sh
./scripts/test-control.sh
npm run test:live
npm run build
npm run check:contracts
```

Test GeoLite menggunakan MMDB sintetis IPv4/IPv6: publisher/no-change/corruption/wrong edition/rollback/orphan recovery, signature/path tamper, size/checksum download, reader activation/restart/outage, 304/expiry, cleanup dan race lookups. API test memakai PostgreSQL terisolasi dengan enrollment credential, stream tiga MMDB, aktivasi Manager, heartbeat status dan revoke. Test binary agent terhadap pusat/PostgreSQL juga memakai geoipRequired true. Browser berjumlah 14 desktop/mobile, termasuk status kosong/error dan agent stale; rilis tampilan positive adalah fixture browser yang ditandai.

Saat B07 diselesaikan, account/license key dan database GeoLite asli belum tersedia di workspace. Pengunduhan MaxMind nyata serta ketepatan lookup pada database produksi belum diuji. Halaman lokal pusat menampilkan Menunggu database; tidak ada database sintetis dipublish ke data pengguna.

## Import database Linux lokal — 10 Oktober 2026

Ketiga file dari `/usr/share/GeoIP` sudah disalin ke `data/geoip/staging` untuk diagnostik lokal; sumber sistem tetap utuh. Salinan tidak masuk Git/image. Set lain ditemukan di `/var/lib/GeoIP`. `/etc/GeoIP.conf` tersedia tetapi hanya root yang dapat membaca; DatabaseDirectory aktif belum dikonfirmasi karena sudo memerlukan password. Kredensial sistem tidak disalin.

Build Country/City adalah 28 November 2025 dan ASN 1 Desember 2025. Reader.Verify serta lookup IPv4 8.8.8.8 dan IPv6 2001:4860:4860::8888 diuji pada salinan. Database sudah melewati batas freshness; publisher menolaknya dan tidak mengubah rilis aktif. Pembaruan database diperlukan untuk menguji distribusi/routing dengan GeoLite required, sedangkan inspector dapat dipakai untuk memeriksa format/hasil lookup file lokal lama secara eksplisit:

```sh
cd packages/geoip
go run ./cmd/inspect --dir ../../data/geoip/staging --ip 8.8.8.8
```

Inspector tidak mempublish database atau melewati kebijakan expiry runtime. Untuk produksi, sesuaikan GEOIP_ROOT dan mount root/staging mengikuti deployment; tidak ada path `/usr/share/GeoIP` yang ditanam pada agent/API.

## Pembaruan sumber lokal — 10 Oktober 2026

Pengguna mengonfirmasi DatabaseDirectory di GeoIP.conf adalah `/mnt/DataDrive/project-beon/BEON-IPQuality/data/mmdb`. Ketiga database terbaru telah disalin ke staging project, menggantikan salinan 2025: Country/City build 9 Oktober 2026, ASN build 10 Oktober 2026. Validasi reader/lookup IPv4 dan publisher berhasil; rilis signed aktif tersedia pada halaman Sistem. File sumber tetap utuh; reputation.mmdb tidak diimpor karena bukan bagian set GeoLite B07. GEOIP_ROOT tetap folder project untuk lokal, lalu disesuaikan dengan path/mount produksi. Catatan salinan lama di atas adalah histori, bukan status aktif.
