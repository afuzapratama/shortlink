# Catatan integrasi ProxyCheck (historis)

**Integrasi ini dihentikan pada revisi owner 13 Oktober 2026.** Dashboard dan lookup kini hanya BEON-IPQuality, tanpa kuota harian, dengan delapan pilihan flag per shortlink. Gunakan [IP_QUALITY.md](IP_QUALITY.md) untuk perilaku saat ini. Penjelasan di bawah adalah riwayat implementasi sebelumnya.

# ProxyCheck pada pusat

Update 13 Oktober 2026: panel sekarang bernama **Pemeriksaan IP · Proxy/VPN/Botnet**, dengan pilihan ProxyCheck atau API BEON-IPQuality privat. Lihat [IP_QUALITY.md](IP_QUALITY.md) untuk pemilihan provider, URL/key, botnet dan batas implementasi. Penjelasan v3 di bawah berlaku ketika provider ProxyCheck dipilih.

Implementasi dev lokal 11 Oktober 2026 menggunakan HTTPS API **v3**, dipin melalui `ver=24-June-2026`. Acuan: [dokumentasi resmi](https://proxycheck.io/api/). Tidak mengubah GeoLite lokasi/ASN atau mengganti deteksi bot dari UA.

## Pengaturan owner

Pada **Sistem → Pemeriksaan IP · Proxy/VPN/Botnet**, masukkan private API key, tentukan batas lookup harian lokal lalu aktifkan dan simpan. Input kosong mempertahankan key; input baru menggantinya. Key tidak pernah dikirim balik oleh GET, tidak disisipkan ke artifact/snapshot/agent dan input dibersihkan setelah submit. Hapus key menonaktifkan integrasi. Uji koneksi menerima IP publik; cache miss mengonsumsi kuota akun.

Key disimpan AES-GCM dalam PostgreSQL, diturunkan dari seed pusat dengan purpose khusus. API hanya menerima sesi owner dan CSRF untuk perubahan/test; revision mencegah overwrite dari tab lama. Audit mencatat perubahan tanpa key atau IP. Backup database memuat encrypted key dan seed; tetap perlakukan dump sebagai secret.

Pada form shortlink pilih **Arahkan proxy/VPN/botnet ke fallback**. Pilihan berikutnya menentukan apakah hasil **belum diketahui** juga menuju fallback. Kedua pilihan disimpan sebagai draft, dipublish dan dirollback bersama routing lain. Provider dinonaktifkan/key dihapus juga menghasilkan unknown; jika unknown-fallback aktif, link itu tetap menuju fallback. Mengaktifkan provider saja tidak otomatis melindungi semua link lama.

## Jalur keputusan

PHP/API: lookup pusat dilakukan sebelum transaksi keputusan/lock domain. Request retry tetap mendapat keputusan event sebelumnya. Go: runtime mengirim IP yang telah dinormalisasi ke endpoint pusat berautentikasi; key provider tidak keluar dari pusat. Agent mempunyai cache/coalescing sendiri. Callback GeoLite tidak melakukan lookup provider di dalam SQL transaction.

Parser memakai `detections.anonymous` boolean pada entry IP yang diminta. Hosting/ASN/risk-score saja tidak dianggap proxy. `ok`/`warning` dengan entry valid dapat dipakai; warning juga ditampilkan sebagai status provider. Denied, timeout, malformed/missing boolean dan response terlalu besar tidak diubah menjadi clean. Event menyimpan status/detection ProxyCheck tanpa IP atau raw User-Agent; dapat dilihat di rincian keputusan. Simulator menerima sinyal proxy sintetis dan tidak menggunakan kuota.

## Cache, batas dan kegagalan

- Pusat: maksimal 10.000 cache entries; clean 30 menit, anonymous 5 menit, hasil gagal 30 detik. Coalescing satu IP, maksimal delapan pekerjaan upstream dan sepuluh cache miss baru/detik.
- Request provider: timeout 650 ms, endpoint HTTPS tetap, tidak mengikuti redirect, response maksimum 128 KiB. Pesan error provider/URL berkey tidak ditulis ke log atau ditampilkan mentah.
- Counter harian UTC tersimpan di database dan direservasi atomik sebelum lookup. Default lokal 900, dapat disesuaikan dengan akun. Counter konservatif mencakup request gagal, tetapi tidak mencakup penggunaan key di aplikasi lain. Provider tetap menjadi penentu kuota sebenarnya.
- Denied/budget exhausted mengaktifkan cooldown satu menit; key/settings berubah membersihkan cache dan cooldown pusat. Config pusat dibaca ulang maksimal setiap lima detik; update melalui dashboard menginvalidasi segera.
- Go: maksimal delapan lookup aktif, cache paling lama satu menit untuk known dan sepuluh detik untuk unknown, timeout pusat 750 ms. Pergantian key/disable pada pusat dapat baru terlihat setelah cache agent berakhir. Saat pusat terputus, hasil mengikuti pilihan unknown link; link tanpa ProxyCheck tetap menggunakan routing lokal biasa.

Provider menyebut kuota akun gratis terdaftar **1.000 pemeriksaan per hari**; batas itu tidak sama dengan 1.000 klik karena cache hit tidak menggunakan lookup baru. [Service limits dan caching guidance](https://proxycheck.io/api/). Trafik banyak IP unik dapat mencapai batas lokal/kuota; busy/kuota habis adalah unknown, bukan bukti bersih. Tidak ada klaim 100–300 RPS untuk fitur aktif sampai workload nyata diuji kembali.

## Kompatibilitas dan verifikasi

Migration `012_proxycheck.sql` menambah pengaturan terenkripsi, counter serta dukungan routingVersion 2. Go baru mengiklankan versi 2; publish routing proxy ke runtime v1 ditolak. Routing biasa tetap menerima versi >=1. Jangan downgrade runtime Go selama snapshot proxy masih tersimpan. ZIP PHP tidak memerlukan key atau perubahan protocol keputusan.

Endpoint: owner GET/PATCH `/v1/admin/system/proxycheck`, owner POST `/v1/admin/system/proxycheck/test`, CLI POST `/v1/agents/proxycheck`. Semua response pengaturan redacted; POST test/agent hanya mengembalikan status, detection dan expiry. IP private/loopback ditolak. Lookup dari pengunjung memang mengirim IP ke layanan ProxyCheck; antarmuka menjelaskan hal ini.

Tests memakai provider stub: encryption/CSRF/revision/replace/remove, cache/coalescing/key rotation, parser strict/429/ukuran, budget dan API fallback, Go cache/offline serta simulator. API key nyata belum diberikan; koneksi akun nyata dan acceptance VPS/kapasitas masih perlu dilakukan setelah key diisi melalui dashboard. Tidak ada request live menggunakan key contoh test.

Verifikasi final dev: build dan 73 operasi kontrak lulus; 32 tes UI serta 28 tes browser dengan API/PostgreSQL lulus. Backend, policy, provider client dan runtime Go race tests lulus. Panel test menggunakan key dummy hanya pada database disposable; pengaturan owner dev tidak diisi/diganti oleh tes.

Revisi UX key: sesudah simpan/reload, input menampilkan mask key tersimpan. Klik input dan ketik key baru untuk mengganti, lalu Simpan. Mask tidak dikirim sebagai key ke API; simpan tanpa perubahan mempertahankan key lama. Pesan berhasil membedakan perubahan key/pengaturan, dan input draft tetap tersedia jika penyimpanan gagal. Key asli tidak dikirim balik oleh GET.
