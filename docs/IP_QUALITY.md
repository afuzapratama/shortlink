# BEON-IPQuality — pemeriksaan IP per shortlink

Revisi dev lokal 13 Oktober 2026. BEON-IPQuality adalah satu-satunya provider aktif; integrasi eksternal ProxyCheck sudah dihapus dari dashboard dan jalur lookup. Referensi kontrak: [API BEON](https://github.com/afuzapratama/BEON-IPQuality/blob/main/docs/API.md).

## Pengaturan dan keputusan

Di **Sistem → BEON-IPQuality · Pemeriksaan IP**, atur URL origin HTTPS server dan API key client. Key terenkripsi hanya di pusat; agent tidak menerima key provider. Key tersimpan ditampilkan sebagai mask di input; klik input untuk mengetik pengganti, lalu Simpan. Simpan gagal mempertahankan draft key. URL baru meminta key baru agar credential lama tidak terkirim ke tujuan baru. BEON tidak mempunyai batas lookup harian di aplikasi ini; jumlah lookup tetap dicatat.

Di form tambah/edit shortlink, aktifkan **Periksa IP dengan BEON-IPQuality**. Centang flag yang ditolak: `proxy`, `vpn`, `tor`, `datacenter`, `botnet`, `spam`, `malware`, `attacker`. Satu flag terpilih bernilai true cukup menuju **fallback link**. Flag yang tidak dicentang tidak menolak akses, tetapi negara/device/OS/browser, aturan domain/global, serta deteksi bot/crawler tetap berlaku. Contoh: datacenter=true dan Tolak datacenter aktif berarti fallback; jika tidak dicentang, datacenter saja tidak menghalangi tujuan utama. Daftar kosong tidak menolak flag IP apa pun.

`score`, `risk_score`, `risk_level`, dan `threat_types` tidak dipakai sebagai boolean filter. Botnet adalah sinyal reputasi IP, bukan pembuktian sebuah request adalah crawler. Provider check tidak berjalan pada link yang tidak mengaktifkannya. Pilihan disimpan/publish/rollback bersama tujuan dan routing lainnya. Link lama tanpa daftar flag eksplisit mempertahankan pilihan proxy/VPN/Tor/botnet sebelumnya; mengedit link dengan pemeriksaan aktif menyimpan daftar eksplisit.

Simulator aturan menyediakan delapan flag sintetis untuk menguji pilihan tanpa request ke API atau menambah counter lookup.

## Agent dan koneksi

Go agent meminta `POST /v1/agents/proxycheck` ke pusat dengan credential agent. Pusat memanggil `GET {origin}/api/v1/check/{ip}` memakai `X-API-Key`. Parser memverifikasi IP yang sama, delapan boolean lengkap dan data_version. Respons pusat memuat flag mentah sehingga cache yang sama dapat dievaluasi dengan pilihan berbeda per link. PHP memakai keputusan API pusat, dengan evaluasi flag yang sama.

Timeout, key ditolak, HTTP 503/dataset stale, respons tidak lengkap atau gagal koneksi adalah unknown. Pilihan **Arahkan hasil IP yang belum diketahui ke fallback** menentukan aksi tersebut. Known tanpa delapan flag tidak dianggap bersih oleh Go baru. Tidak ada request endpoint admin BEON.

Cache pusat/Go maksimal 10.000 IP, singleflight per IP; pusat membatasi delapan lookup aktif dan sepuluh request baru/detik, timeout 650 ms. Cache BEON known satu menit, unknown pusat 30 detik; cache unknown Go maksimal 10 detik. Cache tetap bisa menggunakan hasil sebelumnya hingga TTL berakhir setelah perubahan feed/key/origin. HTTPS wajib, redirect tidak diikuti, body terbatas, alamat tujuan yang didial wajib publik. Redis tetap ditunda.

## Kompatibilitas dan pengujian

Migration 015 mempertahankan konfigurasi BEON, menonaktifkan/menghapus credential provider lama, dan menambah kemampuan Go routingVersion 4. Publish daftar flag eksplisit ke Go versi <4 ditolak; heartbeat downgrade juga ditolak. Validasi snapshot memeriksa kebutuhan setiap link. PHP tetap menggunakan API pusat tanpa perubahan ZIP untuk fitur ini. Endpoint dan kolom bernama proxycheck dipertahankan untuk kompatibilitas internal, tanpa lookup ke layanan ProxyCheck. Kolom daily_budget hanya data historis dan tidak digunakan sebagai kuota.

Urutan rollout test: backup pusat, migrasi/build pusat, update Go ke runtime routingVersion 4, cek heartbeat/applied revision, baru publish pilihan flag. Revisi ini masih dev lokal, belum dipasang ke VPS/hosting publik.

Pemeriksaan publik **13 Oktober 2026 sekitar 19.22 WIB**: ARM online/ready revision 6/6, PHP aaPanel online/ready 6/6, PHP cPanel online/ready 9/9 setelah request link dan probe. Kedua PHP menghasilkan HTTP 302. PHP tidak memiliki daemon heartbeat sehingga status dapat kembali idle ketika tidak diakses; probe/aktivitas menunjukkan koneksi, bukan kapasitas. Uji tersebut memakai rilis publik sebelumnya dan tidak membuktikan flag baru telah diterapkan publik.

Benchmark BEON aktif bersama shortlink 100–300 klik/detik tetap belum dilakukan. Banyak IP unik bisa terkena pembatas request sesaat meskipun tidak ada kuota harian.

Verifikasi akhir: backend dengan race detector, agent Go dengan race/vet, shared policy, build dashboard dan 78 operasi kontrak lulus; 30 tes browser desktop/mobile lulus. Lookup nyata BEON terbaru melalui pusat lokal dan langsung ke upstream mendapat HTTP 503 `reputation_unavailable`, dengan /health upstream unhealthy; hasil ditangani sebagai unknown, bukan IP bersih. Domain `gopane.com` di database dev lokal masih draft/connection unknown dan belum terpasang agent. Ini terpisah dari tiga agent publik yang sudah diuji online/ready.
