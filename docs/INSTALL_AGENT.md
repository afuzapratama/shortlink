# Instalasi agent VPS — B10–B11

Rilis B10 memuat installer dalam binary Linux amd64/arm64, image archive, template Compose manual dan panduan ini. Gunakan manifest dengan installerReady true. Rilis B09 tetap immutable dan hanya mendukung runtime/manual setup. ZIP PHP/wizard B16 tersedia melalui [panduan PHP](INSTALL_PHP.md); tidak memerlukan Docker. Download dashboard mengikuti B17.

## Download dan kepercayaan

1. Masuk ke dashboard pusat, buka domain metode CLI, lalu panel Paket agent. Download manifest bertanda tangan, binary arsitektur VPS, image archive, Compose dan panduan. Tidak perlu membuat token enrollment hanya untuk download owner.
2. Ambil public verification key dan alamat pusat dari bootstrap domain yang diakses melalui HTTPS terpercaya. Simpan bootstrap private; tidak berisi kredensial agent. Perintah memakai URL pusat dari konfigurasi, sehingga domain pusat bisa diganti tanpa rebuild paket.
3. Sebelum menjalankan binary pertama, verifikasi manifest Ed25519 dengan alat tepercaya di komputer owner atau build verifier dari source yang dipercaya. Binary yang baru diunduh tidak dapat membuktikan keasliannya sendiri. Dari checkout source yang dipercaya, gunakan `python scripts/verify-agent-release.py --manifest manifest.json --key PUBLIC_VERIFICATION_KEY --version v0.13.0-b13 --artifact image-linux-amd64.tar --file image-linux-amd64.tar` (Python 3 dan OpenSSL Ed25519). `agent verify-release` dapat dipakai bila binary verifier sudah dipercaya sebelumnya; pin expected version dari dashboard.

```sh
# Public key bukan token. Pinned key diperoleh dari bootstrap pusat tepercaya.
./trusted-agent verify-release --manifest manifest.json --key PUBLIC_VERIFICATION_KEY \
  --version v0.13.0-b13 --artifact image-linux-amd64.tar --file image-linux-amd64.tar
```

Binary hasil browser download perlu `chmod 0700` setelah verifikasi.

Bentuk manifest mengikuti signedPayload + signature. Verifikasi byte signedPayload, bukan proyeksi metadata luar, lalu periksa ukuran/checksum setiap file. URL download relatif, hanya resolve terhadap origin pusat yang dipercaya. Download setelah enrollment memakai credential CLI aktif melalui header Authorization dari file private; jangan membawa cookie/token ke query, riwayat shell atau argumen proses. Download owner dari browser menggunakan sesi HttpOnly. Redirect ke origin storage lain tidak diperlukan pada jalur default.

## Docker tanpa registry tambahan

Image archive diperoleh dari API pusat yang private. Manifest membedakan imageId yang dilaporkan Docker, imageConfigDigest dari config archive, dan imageDigest registry opsional. Sesudah load, inspect image lalu cocokkan ID dengan imageId atau imageConfigDigest signed (engine Docker dapat memakai OCI manifest ID atau config ID). Gunakan ID yang cocok itu di Compose. Setelah verifikasi checksum/signature:

```sh
docker load --input image-linux-amd64.tar
# AGENT_IMAGE menggunakan loaded image ID yang cocok dengan manifest verified, bukan tag mutable.
# Isi path absolute dan UID/GID yang dapat membaca file/data private.
docker compose --env-file .env.agent -f compose.agent.yml config
docker compose --env-file .env.agent -f compose.agent.yml up -d
```

`.env.agent` menyimpan AGENT_IMAGE, AGENT_CONFIG_PATH, AGENT_DATA_PATH, AGENT_BIND_IP, AGENT_PORT dan UID/GID; tidak menyimpan token. Default host listener 127.0.0.1:8091, container 0.0.0.0:8080. Config private agent.json tetap 0600; direktori data persistent 0700 dan dimiliki UID agent, default 10001. Config menunjuk dataDir `/var/lib/shortlink-agent`, geoipRequired true, origin pusat HTTPS dan trusted proxies yang sesuai reverse proxy milik Anda. Mount config read-only, data read/write; image non-root, filesystem read-only, tanpa capability.

Untuk pemasangan manual, enrollment mengikuti [AGENT.md](AGENT.md). Jangan membagikan token/proof/credential atau agent.json; MMDB diunduh terpisah setelah enrollment. Port 8091 dan Docker Compose hanya runtime, belum menyiapkan sertifikat/route publik. Health/snapshot/GeoLite belum menggantikan production probe B11.

## Registry opsional

Release builder menerima --registry-repository untuk registry yang sudah dikonfigurasi operator. Docker login dilakukan terpisah; token enrollment pusat tidak dianggap credential registry. Publisher menyimpan repo@sha256 manifest digest di manifest signed, berbeda dari imageId (image identifier). Untuk private registry, owner menyediakan akun pull-only terpisah pada VPS menggunakan credential helper atau --password-stdin, tanpa menaruhnya di paket/manifest. Setelah pull by digest, template Compose memakai imageId/reference immutable dengan pull_policy never, sehingga startup tidak meminta credential lagi. Installer B10 dapat memakai Docker credential store yang telah diprovision.

[Dokumentasi multi-platform Docker](https://docs.docker.com/build/building/multi-platform/) dan [docker login](https://docs.docker.com/reference/cli/docker/login/) menjelaskan platform build serta password-stdin. Build image B09 memakai binary Go cross-compile dan scratch runtime, sehingga tidak memerlukan emulasi ARM untuk menjalankan build steps.

## Installer CLI

Setelah binary diverifikasi dari komputer/source terpercaya, salin bootstrap domain dari dashboard melalui HTTPS tepercaya. Pastikan domain metode CLI sudah diverifikasi melalui TXT dan token masih berlaku. Bootstrap berisi origin pusat, public key, domain dan versi; token hanya diperlukan ketika prompt muncul. Jangan memasukkan token ke argv, URL atau environment.

```sh
chmod 0700 ./agent-linux-amd64
# VPS dengan Docker aktif: --install-docker juga aman; engine dipakai kembali.
sudo ./agent-linux-amd64 install --bootstrap bootstrap.json --install-docker
# Untuk VPS ARM gunakan agent-linux-arm64.
```

Default root /opt/shortlink-agent harus direktori khusus, kosong pada instalasi pertama, mode 0700 dan tanpa symlink. Pemasangan Docker otomatis memakai repository resmi pada Ubuntu 22.04/24.04/26.04 dan Debian 12/13. Distro lain bisa memakai Docker Engine + Compose plugin yang dipasang operator. Docker context harus engine lokal lewat Unix socket; engine remote, Docker daemon mati dan paket yang konflik diberi penjelasan tanpa menghentikan service lain. Minimal ruang kosong 512 MiB; download juga memeriksa ruang sebesar artefak + cadangan 32 MiB. Rerun dengan root yang sama memakai identitas, credential dan data sebelumnya.

Installer memakai endpoint install-context untuk memperoleh Installation ID yang terikat token, lalu enrollment dengan proof acak yang telah disimpan private. Retry setelah respons enrollment hilang memakai proof yang sama dalam window recovery pusat 10 menit. Token tidak disimpan; credential/proof hanya berada dalam installation.json dan config/agent.json mode 0600. Setelah credential diterima, rerun tidak meminta token lagi. Kehilangan proof sekaligus credential memerlukan revoke/re-enroll dari pusat.

Image agent diunduh dengan credential CLI aktif, signature Ed25519 dan SHA256/size diverifikasi, lalu Docker image ID/platform dicocokkan. Binary installer untuk operasi berikutnya disimpan di /opt/shortlink-agent/agent setelah runtime lulus. Direktori data owned UID 10001, agent non-root/read-only/cap_drop ALL, listener hanya loopback. Config/MMDB ditangani runtime existing: signed snapshot dan GeoLite harus tersedia agar health ready 200. Compose project unik berdasarkan root, sehingga domain lain tidak ikut diubah.

## Proxy dan HTTPS

Default --proxy caddy menggunakan official caddy:2, resolve RepoDigest sekali dan simpan immutable caddy@sha256 untuk rerun/update. Operator bisa pin terlebih dahulu lewat --proxy-image caddy@sha256:DIGEST. Image Caddy berasal dari Docker Hub, berbeda dari release agent yang signed oleh pusat. Kedua layanan memakai host networking Linux; agent tetap bind 127.0.0.1:8091 dan Caddy membuka 80/443. Data sertifikat dan konfigurasi Caddy dipertahankan. DNS A/AAAA harus menunjuk VPS dan inbound TCP 80/443 harus terbuka. Installer tidak mengubah firewall atau menghentikan nginx/Apache/proxy existing. Jika port sudah digunakan, operasi berhenti sebelum enrollment.

```sh
# Reverse proxy sudah dikelola sendiri, atau beberapa domain berbagi nginx/Caddy existing:
sudo ./agent-linux-amd64 install --bootstrap bootstrap.json --proxy external --port 8092
```

External proxy diarahkan ke 127.0.0.1:8092, pertahankan Host dan **overwrite** X-Forwarded-For dengan IP peer yang dipercaya. Loopback IPv4/IPv6 merupakan trusted proxies; jangan meneruskan header spoofing pengunjung tanpa overwrite. Mode external hanya memeriksa runtime lokal; TLS/probe publik diverifikasi B11. Mode Caddy menunggu health lokal dari container proyek yang benar serta health publik HTTPS dengan validasi sertifikat biasa. Timeout default 5 menit, dapat diatur --timeout 10m. Gagal readiness tidak menghapus data atau credential; perbaiki DNS/snapshot/MMDB lalu rerun. Health installer tidak otomatis memberi status Siap produksi.

## Operasi dan update

```sh
sudo /opt/shortlink-agent/agent status
sudo /opt/shortlink-agent/agent update
sudo /opt/shortlink-agent/agent rollback
# Root custom harus selalu dibawa:
sudo /srv/my-agent/agent status --root /srv/my-agent
```

Update mengambil current manifest signed dari origin/key yang dipin, mempertahankan versi sebelumnya, credential, queue dan MMDB. Bila start atau readiness gagal, image dan metadata sebelumnya dipulihkan; tidak ada compose down -v atau penghapusan bind data. Rollback menukar image/version current dan previous; tidak mengembalikan aturan/snapshot GeoLite. Release dan image previous harus masih tersimpan. Binary CLI installed mengikuti runtime release yang berhasil. Installer tidak merotasi credential atau memigrasikan origin/key pusat otomatis (B18). Proxy image tetap dipin; upgrade proxy merupakan operasi terpisah.

## Uji lokal dan batas validasi

Untuk center loopback HTTP gunakan --development --proxy external dan root/port khusus; ini tidak mengaktifkan HTTPS dan tidak boleh dipakai produksi. Baseline Ubuntu/Debian bersih dan penerbitan sertifikat publik perlu VPS/DNS nyata; hasil lokal tidak membuktikan acceptance tersebut. ARM64 dapat dibuild, tetapi execution ARM native perlu host ARM.

Rujukan: [instalasi Docker Ubuntu](https://docs.docker.com/engine/install/ubuntu/), [Docker Debian](https://docs.docker.com/engine/install/debian/), [Compose plugin](https://docs.docker.com/compose/install/linux/), [Caddy automatic HTTPS](https://caddyserver.com/docs/automatic-https), [Caddy reverse_proxy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy).

## Readiness B11

Rilis B11 menambahkan endpoint challenge terautentikasi /.well-known/shortlink-ready. Pusat memverifikasi DNS/IP publik yang dipin, HTTPS/sertifikat tanpa redirect, nonce/MAC, domain, revision dan GeoLite. Dashboard Progress instalasi menampilkan prasyarat dan hasil probe; Periksa kesiapan publik memerlukan owner + CSRF. Heartbeat/enrollment saja tidak memberi status Siap. Hasil Siap kedaluwarsa setelah 5 menit, heartbeat lebih dari 90 detik atau revision berbeda juga memberi Perlu perhatian. Rilis B10 tetap immutable; upgrade ke B11 untuk probe. VPS ARM, clean install dan sertifikat publik menunggu lingkungan owner sesuai [TEST_MATRIX.md](TEST_MATRIX.md).

## B12 — penyimpanan klik

Rilis v0.13.0-b13 menyimpan events.sqlite pada dataDir persisten dan mengirim batch dengan ID tetap. Jangan hapus volume saat restart/update atau menjalankan dua agent pada volume sama. [Queue/retensi/metrik](EVENTS.md); analytics UI B14.

## B13 — proteksi lokal

Rilis v0.13.0-b13 melaporkan protectionVersion=1, mengunduh signed risk bundle dan menerapkan mode domain. Default observe tidak memblokir risiko; aktifkan enforce hanya setelah meninjau hasil observasi dan publish aturan yang diinginkan. [Feed/capability/rate limit](PROTECTION.md). Sebelum downgrade ke B12, hapus/rollback published risk rules B13 dan mode domain; binary lama menolak snapshot yang memerlukan risiko baru. Tidak menghapus snapshot/queue/MMDB/feed sebagai cara melewati validasi.


B18: rotasi credential dan migrasi origin dijalankan dengan private config pada runtime yang dihentikan. Tool operator, signature, ACK/retry dan rollback credential dijelaskan pada [SECURITY.md](SECURITY.md). Binary/runtime B13 dapat memakai config hasil maintenance; installer tidak perlu enrollment ulang kecuali agent dicabut.

## B19 — Ubuntu 26.04

Rilis immutable v0.19.1-b19 menambahkan instalasi Docker resmi untuk Ubuntu 26.04 (Resolute), termasuk ARM64. Rilis B13 tetap tersedia dan tidak diubah. Status pengujian publik dan konfigurasi aaPanel dicatat pada [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md).
