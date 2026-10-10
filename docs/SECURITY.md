# Keamanan owner dan maintenance agent — B18

B18 memakai TOTP SHA-1 enam digit, periode 30 detik, toleransi satu langkah waktu. Langkah yang sudah diterima tidak dapat dipakai lagi, termasuk untuk perubahan keamanan. Sinkronkan waktu pusat/authenticator. Acuan algoritme: [RFC 6238](https://datatracker.ietf.org/doc/html/rfc6238); perubahan faktor memerlukan reauthentication sesuai [OWASP MFA](https://cheatsheetseries.owasp.org/cheatsheets/Multifactor_Authentication_Cheat_Sheet.html).

## Aktivasi dan recovery

Buka **Keamanan**, masukkan password, pilih **Siapkan 2FA**. Masukkan secret ke aplikasi authenticator, lalu masukkan password kembali dan kode enam digit untuk **Aktifkan 2FA**. Setup berlaku 10 menit. Delapan recovery code random ditampilkan sekali setelah aktivasi; simpan offline/private lalu sembunyikan. Recovery code juga dapat menggantikan TOTP untuk login, disable atau regenerasi recovery. Regenerasi membatalkan seluruh kode lama. Aktivasi, disable dan regenerasi membatalkan sesi owner lain.

Password benar pada akun 2FA hanya menghasilkan challenge random lima menit, bukan sesi admin. Challenge dibatasi lima percobaan faktor; limiter IP durable juga berlaku. Keberhasilan menghapus challenge dan membuat sesi MFA terverifikasi secara atomik. TOTP/recovery dipakai sekali dengan lock owner/transaksi agar request concurrent tidak dapat memakai faktor yang sama. Session/CSRF tidak disimpan di localStorage; cookie HttpOnly, SameSite Strict dan Secure pada produksi. Semua mutation admin memerlukan Origin dan CSRF, operasi keamanan juga password serta faktor jika 2FA aktif.

Secret TOTP dan credential rotasi pending dienkripsi AES-256-GCM dengan purpose/owner binding; key diturunkan dari seed pusat yang stabil. Recovery/session/challenge disimpan sebagai hash. Enkripsi ini menghindari plaintext pada kolom secret, **bukan perlindungan terhadap kompromi seluruh database**, sebab seed juga berada di database. Backup database/seed harus private. Secret, password, bearer dan recovery tidak masuk audit/request log. Akun owner lokal tidak otomatis diaktifkan 2FA; enrollment dilakukan owner melalui UI.

## Rotasi credential

Di panel **Migrasi pusat & credential agent**, masukkan password/faktor dan pilih **Siapkan rotasi** pada agent. Browser hanya memperoleh status. Agent mengambil credential baru dari endpoint bearer terikat identitasnya. Satu rotasi pending berlaku 24 jam; credential lama tetap aktif sampai acknowledgment. Jika jendela habis sebelum ACK, rotasi pending kedaluwarsa dan credential lama tetap aktif; owner dapat membuat rotasi baru. Untuk credential yang dicurigai bocor gunakan **Revoke** pada detail domain, bukan menunggu rotasi: pencabutan menolak credential lama maupun pengganti dan memerlukan enrollment ulang.

Hentikan runtime Go/container atau pasang maintenance mode PHP sebelum mengubah config. Buat backup private config di lokasi private mode 0600. Tool memerlukan Python 3 dan OpenSSL dengan Ed25519, dijalankan dari checkout terpercaya, bukan endpoint web:

```sh
python3 scripts/maintain-agent.py --config /opt/shortlink-agent/config.json --runtime-stopped
# PHP: gunakan private config dan public key pusat yang dipin sebelumnya
python3 scripts/maintain-agent.py --config /private/shortlink/config.json --key-file /private/control-public-key.txt --runtime-stopped
```

Sesuaikan path dengan install directory sebenarnya. PHP memakai private directory `config.json` dan `bootstrap.json` di folder yang sama, keduanya mode 0600 di luar document root. Tool mempertahankan publicUrl/hostname/trustedProxies dan menyamakan origin pada kedua file sebelum ACK. Penulisan dua file bukan transaksi filesystem tunggal; runtime wajib dihentikan hingga keduanya selesai, dan retry menyelesaikan penulisan yang terputus. Tool mengenali `controlUrl` Go atau `controlPlaneUrl` PHP, mempertahankan field/path lainnya, menulis atomik mode 0600 dan fsync sebelum ACK. Setelah berhasil, restart runtime. Credential lama langsung ditolak setelah ACK. Jika koneksi putus setelah config disimpan, ulangi command: credential baru diterima selama pending dan ACK idempotent. Jangan mengembalikan backup credential lama setelah ACK. Jika pending sudah kedaluwarsa sementara config baru belum ACK, pulihkan backup lama sebelum menyiapkan rotasi baru; bila backup tidak tersedia, revoke/enroll ulang.

## Migrasi alamat pusat

Siapkan origin HTTPS baru, database yang sama/hasil restore lengkap (termasuk `control_keys`), assets GeoLite/agent/PHP/feed dan konfigurasi. Tetapkan `CONTROL_PUBLIC_URL` ke origin baru dan `CONTROL_ORIGIN` ke dashboard baru. Jangan membuat seed signing baru atau menjalankan bootstrap owner untuk mengganti identitas. Jangan menjalankan worker/publisher dua pusat yang menulis database berbeda. DNS/TLS/deployment dikerjakan operator; UI tidak mengubah DNS atau env otomatis.

Pada pusat lama, masukkan origin baru dan download `control-migration.json`. Dokumen bertanda tangan Ed25519 berisi purpose khusus, migration ID, origin dan expiry tujuh hari. Simpan dokumen jika agent offline; bila kedaluwarsa, buat dokumen baru. Pertahankan origin lama selama masa transisi.

Go menggunakan verification key yang sudah dipin di config:

```sh
python3 scripts/maintain-agent.py --config /opt/shortlink-agent/config.json --migration control-migration.json --runtime-stopped
```

Untuk PHP, gunakan public key pusat yang **sudah disimpan saat verifikasi paket sebelumnya**, bukan key yang baru didownload dari origin tujuan:

```sh
python3 scripts/maintain-agent.py --config /private/shortlink/config.json --migration control-migration.json --key-file /private/control-public-key.txt --runtime-stopped
```

Shared hosting tanpa Python/OpenSSL dapat menggunakan tool dari workstation terpercaya terhadap salinan private config yang dapat mengakses HTTPS pusat secara langsung. Selama proses, hosting harus maintenance: download private config **beserta bootstrap.json** melalui SFTP, jalankan tool, lalu upload kedua hasil lewat SFTP secara atomik sebelum mengaktifkan hosting. ACK workstation membuktikan config hasil telah disimpan di workstation, **belum membuktikan file hosting telah terpasang**; verifikasi probe/aktivitas hosting setelah upload sebelum mematikan origin lama. Jangan menjalankan tool di public directory atau mengekspos config lewat browser. Mode hosting tanpa CLI/SSH tidak diklaim mempunyai migrasi otomatis.

Tool memverifikasi signature, purpose, expiry dan origin dokumen migrasi, lalu meminta `/v1/control/identity` tanpa bearer memakai nonce random baru. Proof identitas pusat harus ditandatangani root yang sudah dipin dan terikat nonce/origin tujuan sebelum credential dikirim. Pemeriksaan berikutnya memverifikasi binding domain/hostname/agent. PHP memerlukan `--key-file` yang dipin sebelumnya untuk rotasi maupun migrasi. HTTPS wajib; HTTP hanya IP loopback development. API tidak mengikuti redirect. Endpoint ACK memeriksa origin tujuan terhadap `CONTROL_PUBLIC_URL`; domain/agent ID, spool dan GeoLite path tetap sama. Tabel dashboard menampilkan aktivitas terakhir serta agent yang masih menunggu ACK migration terbaru; aktivitas/probe diperlukan untuk menilai runtime sesudah restart.

## Audit dan permission review

Admin cookie tidak dapat mengambil private credential agent; endpoint control agent memerlukan bearer aktif. Agent tidak dapat memakai endpoint admin. Rotasi pending encrypted hanya dikembalikan kepada agent sendiri. Revoke dan commit ACK mengunci domain/agent; recovery enrollment tidak mengembalikan credential lama sesudah rotasi. Audit mencatat setup/enable/disable/regenerate, MFA login, stage/ACK rotasi dan migrasi dengan ID saja. Daftar status tidak mengandung credential, hashes, TOTP atau recovery.

Signing root tetap stabil dan dipin selama B18. Rotasi root signing memerlukan protokol trust overlap dan recovery terpisah; jangan mengganti seed secara manual karena memutus signature, enrollment recovery dan dekripsi MFA. B18 mengimplementasikan rotasi **credential agent**, bukan rotasi root trust. HTTPS publik/VPS bersih/native ARM dan deployment hosting nyata masih merupakan gate terpisah sebelum produksi.


## Catatan filesystem lokal

Mount `/mnt/DataDrive` bertipe fuseblk pada lingkungan pengujian ini melaporkan file 0755 walaupun `chmod 600` dijalankan. B18 memindahkan `deploy/.env.control`, `secrets/local-owner.json`, backup database sebelum migrasi dan verification private ke `/home/natama/.local/state/shortlinkSimple/` (direktori 0700, file 0600), dengan symlink pada path proyek agar workflow tetap berjalan. Runtime pusat/database dan key signing tidak diganti. Config agent untuk production harus berada pada filesystem yang menerapkan Unix permission; tool maintenance menolak config dengan mode yang terlalu terbuka. Jangan menganggap `chmod` sukses berarti permission berubah: periksa mode hasilnya.
