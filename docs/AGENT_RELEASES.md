# Rilis agent private — B09–B10

B09 menyiapkan distribusi dan trust sebelum installer B10: binary dan Docker image Linux amd64/arm64, Compose dan tutorial. Release B10 `v0.10.0-b10` tersedia pada panel Paket agent di detail domain CLI. Tidak perlu S3/registry untuk jalur default. PHP ZIP mengikuti B16.

## Build

Dari checkout source yang dipercaya, dengan Go sesuai go.mod, Python 3, Docker dan public CA bundle Linux:

```sh
python scripts/build-agent-release.py --version v0.9.0-b09 \
  --output /absolute/path/agent-builds/v0.9.0-b09
```

Output folder harus baru. Builder memakai go.mod/go.sum locked, trimpath/buildvcs=false dan CGO disabled, memasukkan release version ke binary. Dockerfile.agent menggunakan build context sementara yang hanya memuat binary, CA bundle dan Dockerfile, tidak seluruh project. Container UID 10001:10001. Kedua arch dibangun tanpa emulasi; native platform menjalankan version smoke. Binary ELF/platform dan image archive metadata diperiksa publisher. Native ARM execution memerlukan host ARM dan belum dilakukan di mesin amd64 lokal ini.

Registry opsional: tambahkan `--registry-repository registry.domainanda.com/private/shortlink-agent` hanya jika registry sudah dimiliki/dikonfigurasi. Docker login diprovision operator secara terpisah; builder push per-arch tag lalu mencatat immutable repo@digest. Tidak ada token registry yang dimasukkan manifest atau common package. Registry produksi belum diuji/dipublish; default private filesystem archive sudah diuji. Lihat [Docker multi-platform](https://docs.docker.com/build/building/multi-platform/) dan [Docker login](https://docs.docker.com/reference/cli/docker/login/).

## Publish dari pusat

Konfigurasi host absolute atau mount Docker:

```sh
AGENT_ASSET_ROOT=/absolute/path/agent-assets
AGENT_RELEASE_INPUT=/absolute/path/agent-builds/v0.9.0-b09
CONTROL_PUBLIC_URL=https://pusat.domainanda.com
# DATABASE_URL, CONTROL_MODE dan CONTROL_ORIGIN memakai konfigurasi control existing.
```

```sh
control publish-agent
# Public key dapat diperoleh dari console pusat/SSH yang dipercaya.
control agent-key > trusted-agent-key.txt
```

Perintah mengambil seed signing existing dari PostgreSQL; tidak mengubah key. File immutable di `releases/<version>/`, manifest signed di folder release dan pointer `current.json` atomik. Set ulang version dengan bytes/metadata yang berubah ditolak; buat version baru untuk perubahan paket. Publish rilis yang sama idempotent, tanpa mengganti PublishedAt/signature. Publisher memakai lock; kandidat gagal dibersihkan, current release sehat dipertahankan. API hanya butuh mount read-only, publisher memakai read/write di proses CLI terpisah. Rilis tidak dibersihkan otomatis agar download versi pinned tetap tersedia.

Metadata: version, platform, size, SHA256, relative download path, observed imageId, imageConfigDigest dan optional imageReference/imageDigest registry. Engine Docker dapat melaporkan OCI manifest ID atau config ID; publisher mengikat ID dengan archive config yang sesuai. Sesudah Docker load/pull, installer B10 wajib inspect dan mencocokkan identitas yang diizinkan manifest, lalu memakai ID/digest tetap dalam Compose. Jangan memilih tag mutable sebagai sumber kepercayaan.

Common package tidak memuat agent.json, token, installation proof, credential, private seed atau MMDB. Bootstrap domain dihasilkan API secara terpisah dengan domain ID/hostname/public key dan CONTROL_PUBLIC_URL saat ini. Mengganti domain pusat tidak memerlukan rebuild binary; memindahkan origin tetap membutuhkan penyuntingan config/provisioning terotorisasi.

## Download dan verifikasi

GET `/v1/assets/agent/manifest` serta `/v1/assets/agent/releases/{version}/{artifact}` memerlukan owner session atau CLI credential aktif. Unused enrollment token bukan credential download. Revoke menolak akses baru. Download hanya enam nama file, bukan path filesystem bebas; metadata signature/hash/size diperiksa sebelum streaming. GET/HEAD/ETag/Range, dua slot/proses, 30 unduhan/15 menit/actor. Artifact payload <=512 MiB, manifest <=64 KiB.

Owner dapat download dari browser melalui sesi HttpOnly. Untuk CLI terdaftar, simpan header Authorization di file private bila memakai curl; jangan menaruh token dalam argv/query/log atau memakai redirect yang meneruskan credential lintas origin. URL artifact relatif terhadap origin pusat terpercaya. Untuk first download, gunakan verifier dari checkout source yang dipercaya atau public key dari bootstrap/console terpercaya, bukan verifier baru yang belum terverifikasi.

```sh
python scripts/verify-agent-release.py --manifest manifest.json \
  --key PUBLIC_VERIFICATION_KEY --version v0.9.0-b09 \
  --artifact image-linux-amd64.tar --file image-linux-amd64.tar
```

Python verifier memerlukan OpenSSL Ed25519; Go `trusted-agent verify-release` menghasilkan pemeriksaan signature/version/checksum yang sama. [Tutorial runtime/manual](INSTALL_AGENT.md) menjelaskan config/mount. B09 memakai installerReady false; B10 true dan menyediakan install/status/update/rollback dalam binary terverifikasi. [Tutorial CLI](INSTALL_AGENT.md) menjelaskan Docker baseline, Caddy/external proxy dan token-only prompt. Runtime health bukan readiness produksi B11.

## Pengujian

```sh
(cd packages/artifacts && go test -race ./...)
./scripts/test-control.sh
./scripts/test-agent.sh
# Opsional: Linux Docker lokal, image rilis sudah dibuild.
CONTROL_TEST_AGENT_IMAGE=shortlink-agent:v0.9.0-b09-amd64 ./scripts/test-control.sh
# E2E default juga memeriksa status release belum dikonfigurasi.
# Input rilis opsional menambah alur download paket asli pada test yang sama.
AGENT_TEST_BUILD_INPUT=/absolute/path/agent-builds/v0.9.0-b09 npm run test:live
```

Semua database/container test terisolasi dan dibersihkan. API tests menggunakan artefak sintetis non-executable untuk boundary; integrasi Docker dan E2E release memakai binary/image asli. Signature/hash/path/platform/idempotent overwrite rejection, owner/agent/revoke auth, bootstrap origin dan mobile layout diuji. Python/OpenSSL dan Go verifier sepakat untuk enam artefak asli; invalid signature ditolak. Registry produksi, ARM native, VPS bersih dan HTTPS produksi belum diuji.

## Uji installer B10

Build B10 menggunakan build.json installerReady true. Publisher memasukkan capability ke payload signed; perubahan capability pada versi existing ditolak. Untuk membuat rilis B10, gunakan version v0.10.0-b10 dan output baru, lalu publish memakai konfigurasi pusat existing. B09 tetap immutable dan dapat didownload; contoh B09 di atas merupakan workflow distribusi yang sama.

```sh
# Pull official images secara terpisah, lalu pin digest untuk CI reproducible.
docker pull docker:cli
docker pull caddy:2
AGENT_TEST_CADDY_IMAGE=caddy:2 ./scripts/test-agent.sh
CONTROL_TEST_INSTALLER_RUNNER=docker:cli \
  AGENT_TEST_BUILD_INPUT=/absolute/path/agent-builds/v0.10.0-b10 \
  ./scripts/test-control.sh
```

Runner Docker CLI dipakai agar test non-root dapat menjalankan installer root dalam container terhadap engine lokal, dengan bind hanya root test/binary dan Docker socket. Cleanup menghentikan proyek test dan mengembalikan ownership root test; tidak menjalankan apt atau mengubah service pengguna. Integrasi ini menyiapkan credential dari enrollment terisolasi, lalu memeriksa install, rerun memakai CLI signed installed dan status, signed MMDB/config/heartbeat/policy dan outage. Token prompt/context diuji terpisah pada unit + API PostgreSQL; interaksi terminal manual dan VPS bersih bukan hasil test ini. Caddy validate membuktikan konfigurasi dapat dimuat, bukan penerbitan sertifikat publik.

## Rilis B11

Rilis B11 v0.11.1-b11 menambahkan protocol readiness terautentikasi. Builder/publisher/verifier sama, installerReady true, baseline Docker/manual tetap mengikuti [INSTALL_AGENT.md](INSTALL_AGENT.md). B09/B10 tidak ditimpa. Untuk test integrasi dengan image B11 gunakan CONTROL_TEST_INSTALLER_RUNNER dan AGENT_TEST_BUILD_INPUT seperti contoh B10, dengan folder rilis B11; runtime probe ikut diuji pada transport localhost yang terisolasi. Ini tidak mengklaim HTTPS publik.

## Rilis B12

Rilis B12 v0.12.0-b12 menambahkan antrean SQLite dan batch sender; binary/image Linux amd64 dan arm64 dibangun tanpa CGO, keenam artefak diverifikasi Go + Python/OpenSSL. Rilis B09–B11 tetap immutable. Installer Docker AMD64 lokal diuji dengan CONTROL_TEST_INSTALLER_RUNNER=docker:cli dan AGENT_TEST_BUILD_INPUT=/absolute/path/agent-builds/v0.12.0-b12, termasuk real event ingest/worker. ARM64 baru cross-build/archive, native tetap agenda VPS owner. [Operasi antrean](EVENTS.md).

## Rilis B13

Current release v0.13.0-b13 menyediakan parser crawler, signed feed/cache, limiter lokal dan mode observe/enforce. Binary/image linux amd64/arm64 dibangun tanpa CGO; enam file lulus trusted Go + Python/OpenSSL signature/hash verification. Installer Docker AMD64 + real risk/feed/policy/outage dan 18 live E2E lulus. Rilis lama tetap immutable. [Semantik proteksi dan downgrade](PROTECTION.md), [agenda VPS ARM](TEST_MATRIX.md).


B18 memakai tool operator `scripts/maintain-agent.py` dari checkout tepercaya untuk maintenance config Go/PHP. Tidak ada republish/mutasi paket immutable: runtime Go tetap v0.13.0-b13 dan ZIP PHP v0.17.0-b17. Tool membutuhkan Python 3/OpenSSL, runtime dihentikan, key pusat dipin dan untuk PHP config/bootstrap private keduanya diperbarui sebelum ACK. Distribusi tool embedded dalam installer/ZIP belum tersedia; [workflow keamanan](SECURITY.md) mencakup instalasi hosting tanpa CLI.

## B19 — pengujian publik 11 Oktober 2026

Ubuntu 26.04 ARM bersih didukung melalui rilis immutable v0.19.1-b19; v0.19.2-b19 dipakai menguji update/rollback. Pusat native ARM/HTTPS, Go agent dan kedua PHP aaPanel/cPanel lulus fungsi/readiness. Go 300 RPS/15 menit tanpa error; backup/restore dataset 388.790 event cocok pada seluruh 27 tabel. Kapasitas PHP/API VPS masih belum lulus; geoipupdate/jadwal production ditunda sesuai arahan. Hasil lengkap dan gate tersisa: [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md).
