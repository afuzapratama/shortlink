# UI/UX server pusat dan instalasi agent

Status: spesifikasi v1, 10 Oktober 2026. Acuan arsitektur: [PLAN.md](PLAN.md). Urutan implementasi: [PHASES.md](PHASES.md).

## 1. Arah pengalaman pengguna

Dashboard dipakai admin internal untuk memasang domain, membuat shortlink, mengatur traffic, dan mencari alasan keputusan. Bahasa awal Indonesia; nama produk sementara Shortlink Private. Desktop menjadi tampilan utama, tetapi operasi dasar dan wizard tetap dapat dipakai pada layar mobile.

Gunakan sidebar, tabel dengan pencarian/filter, detail per domain/link, dan formulir bertahap. Warna netral dengan aksen biru, hijau untuk siap, kuning untuk perlu perhatian, merah untuk gagal. Semua status mempunyai teks dan ikon; warna bukan satu-satunya penanda. Hindari informasi teknis pada langkah dasar; diagnosis lanjutan tersedia melalui panel detail.

Pisahkan tiga kondisi: perubahan tersimpan sebagai draft, perubahan sudah dipublikasikan pusat, dan perubahan sudah diterapkan agent. Tombol simpan tidak berarti aturan sudah berlaku. Semua waktu ditampilkan dalam zona waktu dashboard, default Asia/Jakarta; penyimpanan timestamp menggunakan UTC.

## 2. Navigasi dashboard pusat

| Menu | Isi utama | Aksi utama |
| --- | --- | --- |
| Ringkasan | Redirect, blokir, challenge, domain bermasalah, freshness data | Tambah domain / buat link |
| Domain & Agent | Domain, metode install, readiness, koneksi, versi terpasang | Tambah domain / buka detail |
| Shortlink | Domain, slug, tujuan, status aktif/draft, label | Buat link / salin URL |
| Aturan Traffic | Kebijakan global/domain/link, prioritas, versi | Edit draft / simulasikan / publish |
| Traffic & Log | Filter periode/domain/link/action/reason | Buka rincian keputusan / ekspor |
| Sistem | GeoLite, feed, rilis agent, kesehatan worker/antrean | Cek pembaruan / lihat kegagalan |
| Pengaturan | Alamat pusat, fallback, retensi, akun/2FA, audit, backup | Simpan pengaturan / migrasi pusat |

Download dan tutorial berada di detail domain agar paket dan token terkait domain yang benar. Tidak ada menu billing/paket pelanggan.

### Kerangka layar desktop

```text
Sidebar       Judul halaman                  Aksi utama
              Penjelasan singkat / status terakhir diperbarui
              Filter yang relevan
              Tabel atau formulir utama
              Pagination / rincian status / hasil operasi
```

Pada mobile, sidebar menjadi drawer; tabel memakai scroll horizontal atau kartu ringkas sesuai datanya. Aksi penting, label input, dan pesan validasi tetap terlihat tanpa hover.

## 3. Login dan penggunaan pertama pusat

1. Akun pemilik dibuat lewat bootstrap server sekali pakai; tidak ada pendaftaran publik.
2. Login, setup 2FA dan recovery codes; recovery codes hanya ditampilkan saat dibuat.
3. Ringkasan kosong menampilkan langkah: tambah domain, install agent, buat shortlink, uji redirect.
4. Jika GeoLite belum tersedia, tampilkan status dan instruksi admin pusat; jangan menjadikan agent VPS siap sebelum database wajib tersedia.

Session berakhir menampilkan login kembali dengan tujuan halaman tersimpan. Perubahan draft yang belum terkirim dipertahankan sementara tanpa menyimpan secret. Login gagal menggunakan pesan umum; token/secret tidak ditampilkan di log atau notifikasi.

## 4. Wizard tambah domain di pusat

| Langkah | Input/aksi pengguna | Hasil yang terlihat |
| --- | --- | --- |
| 1. Domain | Hostname, label opsional; validasi tanpa path/port | Domain dinormalisasi dan dicek duplikasi |
| 2. Metode | CLI VPS atau ZIP PHP | Penjelasan kemampuan lokal/API dan prasyarat |
| 3. Verifikasi | Ikuti petunjuk DNS TXT atau challenge file pada domain | Belum terverifikasi / terverifikasi / perlu diperbaiki |
| 4. Install | Buat token domain, salin perintah CLI atau unduh ZIP | Token tampil sekali, masa berlaku, tutorial sesuai metode |
| 5. Uji | Jalankan pemeriksaan koneksi dan URL probe domain | Hasil TLS, routing, enrollment, konfigurasi dan kesiapan |
| 6. Selesai | Buka detail domain atau buat shortlink pertama | Status domain dan langkah berikutnya |

Draft domain bisa disimpan sebelum verifikasi. Enrollment hanya diizinkan setelah verifikasi kepemilikan atau pengecualian operator yang tercatat dalam audit. Probe jaringan pusat memvalidasi tujuan domain dan melarang akses alamat internal/reserved yang tidak diizinkan; jangan menyediakan fetch URL bebas.

Token enrollment terkait domain/metode, berlaku awal 30 menit, sekali pakai, dapat dibatalkan atau dibuat ulang. Token baru membatalkan token enrollment sebelumnya yang belum dipakai. Token install bukan password admin atau license key MaxMind. Setelah tertukar, dashboard menampilkan agent ID dan status, bukan secret agent.

Untuk CLI, UI memberi perintah download/verifikasi dan menjalankan installer dengan bootstrap alamat pusat; token dimasukkan pada prompt terminal. Untuk ZIP, UI memberi paket bootstrap tanpa token dan tutorial upload public/private path. Akses download mempunyai kontrol yang berbeda dari token enrollment.

## 5. Detail domain dan status agent

Tab: Ringkasan, Instalasi, Link, Aturan, Traffic, Pengaturan. Ringkasan menampilkan hostname, metode/mode, koneksi, TLS, konfigurasi desired/applied, GeoLite yang digunakan, dan waktu pemeriksaan terakhir.

| Status kesiapan | Makna | Aksi berikutnya |
| --- | --- | --- |
| Draft | Domain belum selesai dikonfigurasi | Lanjutkan setup |
| Menunggu verifikasi | Kepemilikan belum dibuktikan | Cek DNS/challenge |
| Menunggu instalasi | Terverifikasi, belum enrollment | Buka tutorial install |
| Menyiapkan | Enrollment berhasil, prasyarat layanan belum lengkap | Lihat langkah yang belum selesai |
| Siap | Routing/probe berhasil, konfigurasi aktif, prasyarat mode tersedia | Buat atau uji link |
| Perlu perhatian | Antrean menumpuk, data lama, versi tertinggal atau probe gagal | Buka diagnosis dan solusi |
| Dicabut | Kredensial agent tidak berlaku | Registrasi pengganti bila diperlukan |

Koneksi, sinkronisasi, dan kesiapan merupakan indikator terpisah. Status Siap memerlukan hasil uji yang masih relevan; tampilkan waktu uji dan jangan menyamakan enrollment dengan keberhasilan DNS/TLS.

Agent Go melaporkan heartbeat awal tiap 30 detik; tiga interval tanpa laporan menjadi koneksi tertunda, lebih dari 5 menit menjadi offline. Angka dapat dikonfigurasi. PHP tidak diasumsikan mempunyai daemon: tampilkan waktu request/diagnosis terakhir, probe terautentikasi dari pusat bila tersedia, dan status “belum ada aktivitas” saat idle. Tidak ada traffic bukan bukti PHP offline.

Pada PHP mode API, GeoLite diberi label “diproses di pusat”; download MMDB lokal ditampilkan “tidak diperlukan”. Antrean lokal Go dan applied snapshot tidak ditampilkan sebagai kemampuan PHP bila tidak tersedia. Aturan PHP memakai revision keputusan API pusat; fallback lokal mempunyai revision tersendiri.

Aksi perbaikan harus spesifik: ulangi pemeriksaan, buat token baru, unduh ulang paket, cek DNS, atau buka panduan migrasi. Pesan error mempunyai kode referensi tanpa membocorkan kredensial/path private lengkap.

## 6. Wizard instalasi ZIP PHP melalui browser

Browser dipakai setelah ZIP diupload ke hosting. Wizard tidak memasang runtime PHP/Docker, dan tidak mengubah DNS otomatis. Jalur setup mempunyai nama yang tidak bertabrakan dengan slug shortlink, misalnya `/__agent/setup`.

### Sebelum wizard

Paket generik berisi aplikasi dan konfigurasi contoh. Paket bootstrap domain yang dibuat dashboard menambahkan hostname yang diharapkan, URL pusat, versi dan identitas verifikasi, tanpa token atau secret aktif. Halaman setup awal menampilkan informasi minimum. Diagnosis lengkap dan proses enrollment memerlukan token install yang benar.

### Langkah browser

| Langkah | Yang ditampilkan | Interaksi |
| --- | --- | --- |
| 1. Hubungkan | Nama domain, URL pusat dari bootstrap, input token tersembunyi | Masukkan token/key; URL tersedia di opsi lanjutan untuk pemulihan |
| 2. Periksa hosting | PHP/extensions, HTTPS outbound, private path writable/protected, routing | Status lulus/gagal per item dan cara memperbaiki; coba lagi |
| 3. Aktifkan | Ringkasan domain/mode API/fallback dan langkah penyimpanan | Aktifkan agent; progres enrollment, simpan konfigurasi, uji koneksi |
| 4. Selesai | Status terhubung dan hasil uji routing, tautan pusat | Buka dashboard pusat; setup otomatis dikunci |

Pemeriksaan yang tidak membutuhkan autentikasi dapat dilakukan internal lebih awal, tetapi jangan membuka diagnosis rahasia ke publik. Token dikirim POST melalui HTTPS dengan perlindungan CSRF/origin dan rate limit; tidak berada di URL, browser storage, telemetry, atau HTML setelah aktivasi. URL pusat dari bootstrap dianggap tepercaya; override memakai konfigurasi lokal yang dilindungi atau bukti provisioning baru, bukan fetch arbitrary URL oleh pengunjung anonim.

Lakukan preflight sebelum menukar token sekali pakai. Simpan instalasi secara atomik dan kunci proses agar dua tab tidak melakukan aktivasi bersamaan. Jika jaringan putus setelah enrollment, retry memakai installation ID yang sama agar pusat bisa memulihkan hasil secara aman; secret hasil enrollment hanya dikembalikan melalui proof instalasi yang sama dan jendela waktu terbatas. Jika pemulihan tidak mungkin, admin mencabut registrasi yang belum selesai dan membuat token baru.

Setelah berhasil, tidak ada panel admin publik di agent. Setup menolak akses konfigurasi/enrollment baru. Membuka setup kembali memerlukan tindakan eksplisit operator pada file private dan token enrollment baru dari pusat; bukan tombol publik “reset”. Endpoint diagnosis status pusat terautentikasi dan tidak mengembalikan secret.

### Kondisi gagal yang wajib didesain

- Token salah/kedaluwarsa/terpakai: jelaskan bahwa token baru dibuat di detail domain pusat.
- Domain tidak cocok: tampilkan hostname yang diharapkan tanpa mengganti binding secara diam-diam.
- Private path tidak aman atau tidak writable: aktivasi berhenti sebelum token dikonsumsi.
- Pusat tidak dapat dihubungi/TLS gagal: tampilkan coba lagi dan pemeriksaan alamat; jangan menonaktifkan validasi TLS.
- Routing gagal: koneksi bisa berhasil tetapi readiness belum Siap; tampilkan petunjuk rewrite.
- Respons enrollment hilang: tampilkan proses pemulihan/retry tanpa membuat agent duplikat.
- Sudah terpasang: arahkan pengelolaan ke pusat tanpa membuka ulang wizard.

## 7. Membuat link dan mengatur traffic

Form dasar: domain yang siap, slug (opsional auto-generate), tujuan HTTP/HTTPS, label, fallback, dan masa aktif. Slug diperiksa per domain; validasi URL dan potensi loop muncul dekat input. Draft dapat dibuat untuk domain belum siap, tetapi publish memerlukan domain dengan kemampuan yang sesuai.

Link published mempunyai aksi **Edit tujuan**. Domain, slug, ID link, dan histori traffic tetap sama; perubahan URL tujuan disimpan sebagai draft terpisah dari tujuan published. Tabel menampilkan keduanya selama ada perubahan. Publish mempromosikan tujuan baru menjadi revision baru; sampai acknowledgement, agent lokal dapat masih memakai tujuan sebelumnya. Simpan dengan URL yang sama dengan tujuan published membatalkan draft tujuan yang ada. Tujuan published adalah konfigurasi pusat, bukan jaminan semua agent sudah menerapkannya.

Setelah simpan draft, tampilkan preview URL dan tombol Uji/Publish. Publish menampilkan revision dan daftar agent penerima; UI menunjukkan Menunggu diterapkan hingga acknowledgement diterima. Shortlink yang sudah pernah aktif tetap melayani revision terakhir selama perubahan draft belum dipublikasikan.

Editor aturan memakai baris kondisi + tindakan + prioritas, dengan pilihan semua/sebagian kondisi yang eksplisit. Editor dasar mengutamakan negara, jenis perangkat, OS, browser opsional dan fallback; opsi jaringan/bot tersedia dalam bagian lanjutan. Setiap aturan dapat diberi nama dan deskripsi. OS terpisah dari mobile/tablet/desktop: pilihan iOS/iPadOS mencakup iPhone dan iPad yang dikenali. Semua kondisi digabung dengan AND; pilihan Semua OS/browser berbeda dengan Unknown. Merek/model ditampilkan sebagai metadata, belum menjadi filter aturan v1.

Menu Aturan Traffic mempunyai pemilih cakupan: **Global**, **Domain**, atau **Shortlink**, dengan target spesifik untuk dua cakupan terakhir. Tombol Aturan pada baris shortlink membuka cakupan link tersebut. Aturan hanya diedit pada cakupannya; hasil efektif untuk sebuah link menggabungkan aturan global, domain tempat link berada, dan aturan khusus link. Aturan domain/link lain tidak ikut dievaluasi. Simulator wajib memilih shortlink, bukan hanya negara/perangkat.

Aturan redirect/fallback memilih sumber tujuan: **ikuti tujuan shortlink** atau **URL khusus aturan**. Pilihan pertama mengikuti perubahan tujuan dasar setelah publish; pilihan kedua tetap memakai URL khusus hingga aturan tersebut diedit/publish. UI menyebutkan perbedaan ini saat mengedit tujuan link agar admin memahami routing yang sebenarnya. Blokir wajib global dievaluasi sebelum routing. Prioritas unik dalam satu cakupan; tie-break lintas cakupan memakai link, domain, kemudian global, dengan ID sebagai penentu terakhir.

Simulator menerima domain/link, negara/device/OS/browser/IP/ASN serta sinyal risiko; tampilkan hasil, aturan cocok, alasan, dan revision yang diuji. Input sintetis diberi label; hasil simulator bukan pengukuran lokasi/IP pengunjung sebenarnya. Publish dan rollback menampilkan dampak domain/link, lalu menghasilkan revision baru yang dapat disinkronkan. Rollback tidak menurunkan nomor revision.

Perubahan yang membatalkan akses (hapus link/domain, revoke agent) menjelaskan akibatnya melalui dialog konfirmasi ringkas. Simpan/edit rutin tidak memerlukan dialog tambahan. Pencabutan pusat tidak diklaim langsung berlaku pada agent lokal offline; tampilkan batas lease dan acknowledgement.

## 8. Traffic, sistem, dan pengaturan

Traffic memakai filter periode/domain/link/action/reason/device/OS/browser/negara dengan pagination server-side. Detail event memperlihatkan keputusan, alasan, versi aturan/data dan sumber Go/PHP, serta OS/browser dan versi jika tersedia, merek/model perangkat jika dikenali. Nilai yang tidak tersedia ditampilkan Unknown, tanpa menebak hardware dari browser saja. Tampilkan redirect terbit, challenge terbit/lulus, blokir, dan error secara terpisah. Estimasi unik diberi label estimasi; jangan menyebut request sebagai manusia terverifikasi.

Ringkasan awal memakai polling 10–15 detik dengan label waktu pembaruan. Agregat mempunyai waktu watermark/lag; rentang historis tidak perlu refresh terus. Jangan mengirim setiap event ke dashboard pada 300 request/detik.

Halaman Sistem membedakan: GeoLite diunduh, divalidasi, dipublikasikan, dan terpasang pada agent. Feed mempunyai sumber, freshness, hasil import dan mode observasi/enforcement. Tombol retry tidak mengaktifkan file yang gagal validasi.

Pengaturan mencakup retensi, alamat pusat/fallback, secrets berstatus “tersimpan” tanpa menampilkan nilai lama, 2FA/recovery, dan audit. Wizard migrasi pusat: alamat baru, validasi identitas/koneksi, publish endpoint revision, progres acknowledgement, kemudian panduan cutover. Agen yang offline tetap terlihat dalam daftar pekerjaan tersisa.

## 9. Komponen dan kriteria UX

Komponen reusable: sidebar, breadcrumb, status badge, tabel filter/pagination, form field/error, stepper, progress checklist, secret input, copy button dengan feedback, dialog dampak, panel diagnosis dan timeline revision.

Setiap halaman mempunyai loading, kosong, gagal/retry, success, unauthorized dan data stale bila relevan. Keyboard navigation, label input, focus yang terlihat, focus management dialog, serta pesan status yang dapat dibaca screen reader termasuk acceptance. Jangan membuat interaksi utama hanya tersedia pada hover atau drag; prioritas aturan juga dapat diubah memakai tombol.

Alur uji UX utama: domain pertama → install CLI atau ZIP → readiness → link pertama → publish → uji; kemudian kasus token kedaluwarsa, konfigurasi tertunda, PHP idle, pusat putus, dan aturan memblokir traffic yang perlu ditelusuri.

Dashboard utama B04 sudah memakai autentikasi/session serta management domain/link draft dari API nyata. Preview B01 tersedia melalui ?demo=1 dengan fixture dan screenshot review di [README](../README.md). Preview tidak otomatis dipakai saat API gagal. B05 menambahkan instruksi/verifikasi DNS, token ephemeral, enrollment/recovery, signed snapshot, ack/revoke dan audit; publish, runtime agent, persistent setup lock dan instalasi hosting belum tersedia. Resource draft dan kemampuan agent yang belum dikonfirmasi tidak ditampilkan sebagai siap.

## Status implementasi B07 — Sistem GeoLite

Halaman Sistem pada dashboard utama memakai GET /v1/admin/system/geoip, polling 30 detik. Menampilkan status belum dikonfigurasi/menunggu database/rilis tersedia/kedaluwarsa/tidak valid, tanggal build/expiry dan ukuran Country/City/ASN, atribusi/lisensi, serta versi agent dan heartbeat terakhir. Heartbeat >90 detik tidak ditampilkan sebagai versi terkonfirmasi. Error/retry tersedia, tanpa fallback fixture. Screenshot test terisolasi: [desktop](artifacts/geoip-system-desktop.png), [mobile](artifacts/geoip-system-mobile.png). Panduan [GEOIP.md](GEOIP.md); wizard/probe readiness produksi tetap B11/B16.

## Hasil B08 — UI live

Navigasi Aturan Traffic operasional: pilih cakupan global/domain/shortlink, buat draft set, edit aturan, priority/mandatory/ASN, enable/hapus, publish dengan konfirmasi dan history/rollback. Daftar negara dari API, label lokal Intl.DisplayNames. Deteksi UA mengisi perangkat/OS/browser dan model optional; simulator published/preview memakai API Go. Preview mencakup draft yang tersimpan, bukan perubahan editor yang belum disimpan. Conflict mempertahankan editor dan meminta reload.

Shortlink memiliki publish tujuan dengan konfirmasi dan riwayat tujuan. Publish menampilkan pending sinkronisasi, sementara applied tetap ada di detail domain. Belum verified/enrolled mematikan tombol publish; aturan bot/challenge belum dapat diterapkan. Refresh setelah publish mempertahankan cakupan halaman aturan. Screenshot live desktop/mobile berasal dari PostgreSQL pengujian, terpisah dari workspace pengguna.

## Hasil B09 — panel download private

Detail domain menambahkan Paket agent: current release, architecture/file sizes/checksums/image IDs, manifest signed, bootstrap domain dan tutorial download. Halaman bekerja sebelum verification untuk owner, tanpa otomatis membuat token. Credential dan MMDB tidak masuk paket; instalasi otomatis diberi label Menunggu B10, ZIP PHP Menunggu B16. Error/no-release dan muat ulang memakai API nyata. Bootstrap URL pusat dan public verification key terlihat untuk verifikasi paket, bukan input credential umum.

16 E2E live dengan binary/image rilis asli menguji download tutorial/bootstrap serta layout desktop/mobile. Hostname panjang sekarang membungkus pada mobile. Screenshot [desktop](artifacts/agent-packages-desktop.png) dan [mobile](artifacts/agent-packages-mobile.png) memakai database test terisolasi.

## Panel installer B10

Paket agent membaca installerReady dari release pusat. Rilis B10 menampilkan command install dengan bootstrap, petunjuk binary ARM/AMD, prompt token tersembunyi dan opsi proxy external. Rilis B09 tetap menampilkan manual setup. Token tidak masuk command/contoh, bootstrap download tidak berisi credential. Installer melakukan token context lookup sendiri agar pengguna tidak perlu mengetik Installation ID. Status/progress/probe/retry UI operasional dilanjutkan B11; availability installer tidak berarti domain Siap produksi.

## Progress instalasi B11

Detail domain memuat panel Progress instalasi: prasyarat DNS/enrollment/heartbeat/config/GeoLite dengan diagnosis, timestamp status, applied/desired dan last public probe. Polling read-only setiap 10 detik saat tab terlihat. Periksa kesiapan publik/retry membutuhkan CSRF dan memberi hasil per check; tidak mengirim perintah restart ke VPS. Domain tidak otomatis Siap dari enrollment/heartbeat. Hasil Siap memiliki TTL lima menit, stale/offline/revision mismatch tampil Perlu perhatian. Probe result lama dihapus saat revoke; audit menjelaskan pemeriksaan tanpa menampilkan proof/credential. PHP adapter tetap gate B15–B17.

## B13 detail proteksi

Detail domain menyediakan mode observe/enforce/off, budget request/burst, revision desired/applied, capability agent, freshness feed dan summary sinyal/kandidat. Default observe; enforcement disabled sebelum capability dilaporkan, backend tetap memeriksa heartbeat terbaru. Label reputasi tidak menyebut semua IP sebagai proxy. Rule editor memilih risk + category untuk crawler/preview, simulator menampilkan mode/candidate terpisah dari hasil actual. Refresh tidak menghapus draft lokal tanpa klik operator. Analytics/log/rincian/export tersedia B14; periode UTC dengan waktu display Asia/Jakarta, mode agregat saja, explicit freshness/stale heartbeat dan error tanpa fixture fallback. [Batas analytics](ANALYTICS.md).

B15: editor proteksi mode API mengelola konfigurasi pusat, menampilkan revision API terakhir dan pesan publish yang berlaku pada keputusan baru. Tidak memerlukan capability heartbeat CLI; ZIP/public handler belum terpasang tidak memberi Siap. Traffic detail menjelaskan source PHP sebagai keputusan pusat, bukan receipt browser redirect. Queue/stale hanya CLI; status hosting yang tidak aktif menjadi idle, public readiness tetap memiliki TTL/probe. UI/download wizard hosting lengkap B16–B17.

B16: wizard PHP operasional terdapat pada agent `/__agent/setup`, terpisah dari prototype dashboard. Dua langkah check/activate menggabungkan preflight sebelum token ditukar; mode installed menutup form/token dan mengarahkan pengelolaan ke pusat. Private bootstrap adalah sumber alamat tepercaya; override URL publik bukan input wizard. Chromium desktop/mobile memverifikasi aktivasi yang pulih, reload/lock dan layout tanpa overflow. [Desktop](artifacts/php-setup-desktop.png), [mobile](artifacts/php-setup-mobile.png), [selesai](artifacts/php-setup-complete-mobile.png). Download/diagnosis mode API di pusat mengikuti B17.

B17: Paket PHP operasional di domain mode API menampilkan current signed release, checksum/download ZIP/tutorial, bootstrap domain tanpa token dan trusted public key. Hosting publik memerlukan HTTPS pusat; bootstrap HTTP development diberi penjelasan. Tutorial meliputi upload/root/private permission, token/preflight, recovery dan readiness. Progress mendukung API probe setelah enrollment, status retry read-only, aktivitas terakhir/idle dan diagnosis GeoLite pusat. Detail domain memakai revision API/capability/revoke yang sesuai, tanpa menyebut daemon atau leased snapshot PHP. [Desktop](artifacts/php-packages-desktop.png), [mobile](artifacts/php-packages-mobile.png). Uji provider/TLS publik belum tersedia.
