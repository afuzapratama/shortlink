# Instalasi agent PHP tanpa Docker

Revisi lokal B20 memakai ZIP **v0.20.1-b20**, menambah `private/src/Manage.php` untuk [URL pembuka dan manage](AGENT_MANAGE.md). Paket B17 di tutorial baseline tetap immutable; pilih manifest/version terbaru yang hendak dipasang dan pertahankan config/bootstrap saat update. Unknown/root kini opening redirect atau 503 bila kosong; bukan 404 publik biasa.

B17 menyediakan download ZIP PHP signed, bootstrap domain dan tutorial melalui panel Paket PHP pada detail domain mode API. Wizard operasional dibuat pada B16. Mode API memakai GeoLite, deteksi device/OS/browser dan aturan di pusat; tidak membutuhkan MMDB, database, cron, Composer atau Docker pada hosting. PHP bukan mesin aturan lokal.

## Prasyarat dan layout

PHP 8.2+ dengan cURL, OpenSSL, JSON, session, random_bytes, fsync, file locking dan filesystem lokal writable. HTTPS publik dan HTTPS outbound ke pusat harus valid, tanpa mematikan verifikasi sertifikat. Apache 2.4 dengan mod_rewrite/AllowOverride yang mendukung `.htaccess`, atau konfigurasi Nginx setara. Web server harus dapat melayani beberapa request bersamaan: preflight memanggil routing domain sendiri. Limit hosting tetap harus diuji.

```text
/home/account/shortlink/
  public/                 ← satu-satunya document root domain
    index.php
    setup.css
    .htaccess
  private/                ← di luar semua web root dan alias publik
    bootstrap.json        ← disiapkan operator; tanpa token
    src/                  ← kode adapter
    config.json           ← dibuat wizard; credential, 0600
    pending.json          ← hanya selama aktivasi/pemulihan, 0600
    sessions/             ← sesi wizard private
```

Jangan upload seluruh ZIP ke `public_html` lalu mengandalkan deny `.htaccess` untuk menyembunyikan secret. Jika hosting tidak mengizinkan private path di luar web root, instalasi belum memenuhi prasyarat. Symlink/alias vhost lain juga harus tidak mengekspos private. ZIP memuat layout awal, bukan secret aktif; path proyek VPS pusat tidak digunakan pada hosting.

## Build dan upload

Operator project menyiapkan paket lokal:

```sh
python3 scripts/build-php-release.py --output data/php-builds/v0.17.0-b17
```

Output `agent-php-v0.17.0-b17.zip` dan `manifest.json` mencatat checksum/ukuran. Manifest build lokal adalah input publisher, bukan manifest Ed25519 yang didownload owner. Pusat menjalankan `control publish-php` untuk menandatangani release immutable menggunakan signing key proyek. Paket tidak memuat token, credential, MMDB, database atau fixture test.

Sebelum upload, download `php-manifest.json` dan `agent-php.zip` dari panel Paket PHP. Gunakan verifier dari checkout project yang dipercaya, public verification key yang dikonfirmasi melalui dashboard tepercaya, dan expected version yang Anda pilih; jangan mengambil verifier/kunci dari ZIP yang belum diverifikasi.

```sh
python3 scripts/verify-php-release.py --manifest php-manifest.json \
  --key PUBLIC_VERIFICATION_KEY --version v0.17.0-b17 \
  --artifact agent-php.zip --file agent-php.zip
```

Publisher pusat:

```sh
# Set DATABASE_URL, CONTROL_*, PHP_ASSET_ROOT dan PHP_RELEASE_INPUT
# melalui environment private; jangan masukkan credential ke command.
control publish-php
```

`PHP_ASSET_ROOT` adalah direktori rilis pusat (misalnya `/var/lib/shortlink/php-assets`); `PHP_RELEASE_INPUT` adalah folder build terpisah. Pusat melayani `/v1/assets/php/manifest` dan `/v1/assets/php/releases/{version}/{artifact}` hanya melalui session owner. Download memakai checksum signed; token install/bearer agent tidak memberi akses. Rilis Go terpisah dan tidak berubah. Bootstrap domain di dashboard berisi hostname/publicUrl/alamat pusat, tanpa token/credential. Generic ZIP tetap tidak disisipkan secret.

1. Ekstrak ZIP di folder account, lalu arahkan document root domain ke folder `public`.
2. Atur permission `private` ke `0700` atau `0750` dan pastikan user PHP dapat membaca/menulis. File konfigurasi harus `0600`. Jangan memakai `0777`.
3. Salin `private/bootstrap.example.json` menjadi `private/bootstrap.json`. Isi `hostname`, `publicUrl` dan `controlPlaneUrl` menggunakan alamat domain dan pusat aktual. Alamat pusat bisa berubah melalui tindakan operator pada file private; migrasi credential/alamat terpandu mengikuti B18. Tidak ada input URL arbitrary pada wizard publik.
4. Default path private adalah sibling folder `public`. Untuk lokasi lain, pindahkan **seluruh** folder private, termasuk `src`, lalu set `SHORTLINK_PRIVATE_DIR` pada lingkungan PHP/web server. Path harus absolut; jangan kirim lewat parameter URL.
5. Aktifkan routing dan HTTPS. Di detail domain mode API pusat, download bootstrap dan simpan di private. Verifikasi kepemilikan DNS, lalu buat token `php_zip` sekali pakai.
6. Buka `https://domain-agent/__agent/setup`. Masukkan token, jalankan pemeriksaan, lalu klik **Aktifkan agent**. Token tidak masuk URL, browser storage atau HTML respons. URL pusat berasal dari bootstrap operator, bukan token saja.
7. Setelah selesai, setup terkunci. Jalankan public readiness probe dari detail domain pusat, lalu buat/publish link dan uji redirect. Enrollment/probe API bukan bukti domain publik siap; DNS/TLS/proof/revision masih diperiksa oleh pusat.

Untuk Nginx, gunakan root folder public dan routing berikut dalam server HTTPS yang benar:

```nginx
client_max_body_size 8k;
root /home/account/shortlink/public;
index index.php;
location / { try_files $uri /index.php$is_args$args; }
location = /index.php {
    include fastcgi_params;
    fastcgi_param SCRIPT_FILENAME $document_root/index.php;
    fastcgi_pass unix:/run/php/php-fpm.sock; # sesuaikan socket runtime hosting
}
location ~ \.php$ { return 404; }
```

## Proxy dan request pengunjung

Default memakai IP socket, mengabaikan X-Forwarded-For. Bila ada reverse proxy/CDN, operator dapat menetapkan IP exact proxy yang benar dalam `trustedProxies` pada bootstrap sebelum aktivasi. Hanya immediate proxy terdaftar boleh memberi X-Forwarded-For; rantai dibaca dari kanan sampai IP pertama yang tidak tepercaya. X-Forwarded-Proto hanya dipercaya dari proxy tersebut. Provider dengan daftar CIDR dinamis membutuhkan konfigurasi tambahan, bukan menerima header publik begitu saja. Perubahan proxy setelah aktivasi juga harus diterapkan operator pada `config.json` private.

GET `/slug` membuat event random 32 byte dan meminta API keputusan. Timeout total client sekitar 3 detik, connect timeout maksimal 1 detik, paling banyak satu retry transport/5xx dengan event ID, waktu dan payload yang sama. Tidak mengikuti redirect API, tetap memverifikasi TLS, membatasi body response 32 KiB dan memvalidasi tujuan/action/event ID sebelum mengirim Location. Redirect/fallback memakai 302; block 403; slug published tidak ditemukan 404; kegagalan lain 503 tanpa Location. Fallback offline lokal belum tersedia. Query pengunjung tidak diteruskan ke tujuan. HEAD pada slug sintaktis valid mengembalikan 204 tanpa keputusan/counter; gunakan GET untuk uji redirect nyata.

`/.well-known/shortlink-ready` membutuhkan nonce dan HMAC purpose request dari pusat. Handler memanggil API probe dan meneruskan byte JSON/proof pusat tanpa re-encoding; tidak membuat klik. Pemanggil anonim mendapat 404. Agent tidak memiliki admin publik atau daemon heartbeat; hosting idle tetap dibedakan dari offline oleh pusat.

## Pemulihan dan penguncian

Preflight memeriksa private path, penyimpanan, pusat dan routing sebelum token ditukar. Installer memakai exclusive file lock; pending token, installation ID dan random proof disimpan atomik sebelum enrollment. Jika respons hilang, klik aktivasi lagi atau buka setup dan masukkan **token yang sama**; proof lama dipakai ulang. Pusat membatasi pemulihan 10 menit sejak enrollment pertama. Token pending tidak boleh diganti diam-diam.

Setelah response enrollment dan probe API valid, `config.json` atomik menjadi marker aktif sekaligus pengunci setup. Restart tidak membuka wizard kembali. Kegagalan penyimpanan/probe mempertahankan pending proof untuk retry. File lock memerlukan filesystem lokal yang benar; NFS/shared multi-host belum diterima. Atomic rename dan fsync file tidak dianggap sebagai bukti ketahanan semua konfigurasi hosting terhadap power loss.

Jika jendela pemulihan habis: revoke agent di pusat, minta token baru, lalu operator dapat menghapus pending state/sesi lama di folder private. Membuka ulang instalasi aktif juga memerlukan revoke dari pusat dan tindakan operator pada file private; tidak ada tombol reset publik. Jangan menghapus konfigurasi aktif untuk sekadar update source paket.

## Update dan rollback paket PHP

Update dilakukan operator, bukan dengan membuka ulang wizard. Verifikasi signed manifest, expected version dan checksum ZIP memakai verifier dari checkout tepercaya. Periksa kompatibilitas API dan format config kandidat; jika berubah tanpa dukungan rollback, hentikan update sampai ada prosedur migrasi.

1. Ekstrak kandidat di direktori staging **di luar semua web root**. Simpan salinan paket sebelumnya serta backup config/bootstrap privat, dengan akses owner saja. Jangan memasukkan backup ke public atau Git.
2. Aktifkan maintenance pada **domain agent tersebut** sebelum mengganti file, agar satu request tidak mencampur source dua versi. Pertahankan private config, pending state, sesi dan identitas; jangan menyalin bootstrap example ke konfigurasi aktif. Maintenance dapat mengembalikan 503 tanpa redirect.
3. Ganti `private/src` dan file public paket sebagai satu perubahan terkoordinasi saat maintenance. Pertahankan custom `SHORTLINK_PRIVATE_DIR`, konfigurasi vhost, permission dan `.htaccess` hosting; gabungkan aturan paket yang diperlukan. Public root hanya berisi file public.
4. Invalidate OPcache melalui mekanisme panel/runtime hosting yang tersedia; jangan membuat endpoint reset OPcache publik. Periksa syntax memakai versi PHP yang sama dengan web runtime, lalu buka kembali domain.
5. Periksa setup tetap terkunci, probe publik pusat, redirect published, unknown slug, block/fallback dan pencatatan event. Pantau HTTP503 dan latency. Catat versi/checksum yang dipasang; wizard terkunci saja tidak membuktikan upgrade berhasil.
6. Jika gagal, aktifkan maintenance lagi, kembalikan source/file public dan konfigurasi routing sebelumnya, invalidate OPcache, lalu jalankan pemeriksaan yang sama. Pertahankan credential/config aktif yang masih kompatibel; rollback source bukan restore database atau rotasi key.

B19 menguji instalasi dua hosting; update/rollback source PHP pada hosting nyata masih menjadi gate B20. Backup credential harus disimpan privat, karena kebocoran backup memberi akses agent meskipun paket ZIP tidak membawa secret.

## Uji lokal

```sh
bash scripts/test-php-agent.sh
bash scripts/test-control.sh
```

Suite PHP memakai pusat stub untuk respons hilang, retry, batas keamanan dan wizard HTTP. Test `TestPHPAdapterRealCentral` memakai PHP cURL asli terhadap server Go, PostgreSQL terisolasi dan GeoLite synthetic untuk enrollment, proof/probe dan klasifikasi redirect/event. Tidak memakai database owner development.

Development HTTP hanya diperbolehkan untuk origin loopback jika operator menyetel `developmentLoopback: true`; hostname/publicUrl harus tetap sama dan request datang dari loopback. Built-in server perlu `PHP_CLI_SERVER_WORKERS=4` pada Linux agar self-preflight tidak deadlock. File router test tidak ada di ZIP. Production wajib `developmentLoopback: false` dan HTTPS. Hosting nyata, public TLS dan target 100–300 RPS belum dinyatakan lulus pada B16.

Referensi implementasi: [PHP session cookies](https://www.php.net/manual/en/function.session-set-cookie-params.php), [PHP fsync](https://www.php.net/manual/en/function.fsync.php) dan [PHP flock](https://www.php.net/manual/en/function.flock.php). Kontrak pusat: [DECISIONS.md](DECISIONS.md).


B18: maintenance `controlPlaneUrl`/credential pada private config tersedia melalui tool operator dari checkout terpercaya; bukan handler publik PHP. Hosting tanpa CLI memakai workstation/SFTP dengan maintenance mode dan verifikasi runtime setelah upload. ACK workstation bukan bukti config hosting sudah diterapkan. Lihat [SECURITY.md](SECURITY.md). ZIP signed v0.17.0-b17 tidak diubah/ditimpa untuk menambah tool operator.
