# Aturan dan publish — B08

Dashboard lokal: `http://127.0.0.1:5173/#/rules`. Masuk memakai akun owner yang sudah dibuat. Menu live menyimpan semua perubahan ke API + PostgreSQL; preview `?demo=1` tetap fixture terpisah.

1. Tambahkan domain, verifikasi DNS TXT, lalu enroll agent lokal. Installer CLI/Docker mengikuti B09–B10; untuk pengujian runtime manual lihat [AGENT.md](AGENT.md).
2. Buat shortlink dengan tujuan HTTP/HTTPS. Setelah verified/enrolled, pilih Publish dan periksa tujuan dalam dialog. Agent menerapkan pada polling berikutnya, dengan applied revision dari heartbeat.
3. Buka Aturan dari baris shortlink atau menu Aturan Traffic. Pilih Global, Domain atau Shortlink dan buat draft cakupan. Satu rule set per binding.
4. Tambahkan aturan: negara, perangkat, OS, browser, ASN opsional, priority dan tindakan block/redirect/fallback. Redirect/fallback memakai tujuan link atau URL khusus. Kondisi dalam satu rule memakai AND.
5. Simpan dan jalankan simulator. Default memakai published; centang Preview draft cakupan untuk memasukkan draft tersimpan dengan expectedRevision. User-Agent opsional dapat diparse untuk mengisi device/OS/browser; negara/ASN simulasi tetap input eksplisit.
6. Publish aturan, periksa cakupan dan konfirmasi. History menyimpan versi published; rollback mengembalikan versi itu sebagai revision baru dan mengganti draft saat ini.

Edit tujuan shortlink published menyimpan draftDestination. Tujuan aktif tetap berlaku sampai republish/sync. ID, hostname/slug dan statistik tidak berubah. Riwayat tujuan dapat dipulihkan lewat rollback; history hanya tersedia untuk publish B08 dan sesudahnya. Tidak ada history retroaktif untuk data yang sebelumnya dipublish langsung lewat SQL pengujian.

## Urutan keputusan

Enabled mandatory global block diperiksa dahulu. Berikutnya priority kecil lebih dahulu; priority sama lintas cakupan memilih shortlink, domain, global. Priority harus unik dalam satu set. Aturan pertama yang semua kondisinya cocok menghasilkan keputusan. Tanpa kecocokan, redirect ke tujuan published link. Country/device/OS/browser unknown eksplisit; ASN unavailable null tidak cocok dengan rule ASN spesifik. Wildcard mencakup unknown.

Rule IDs harus unik dalam konfigurasi efektif suatu domain. Tujuan HTTP/HTTPS tanpa credential, invalid port atau loop ke domain agent ditolak. Binding immutable; link dengan rule set tidak dapat dipindah domain. B13 menyediakan aturan risiko observe/enforce dengan capability heartbeat; challenge tanpa provider tetap ditolak. [Proteksi domain](PROTECTION.md). GeoLite ASN hanya operator/jaringan, bukan identifikasi proxy.

## Deteksi dan operasi

Shared module [packages/policy](../packages/policy) digunakan central simulator dan agent Go. Library [uap-go](https://pkg.go.dev/github.com/ua-parser/uap-go/uaparser) dipin ke commit, parser sekali startup, cache 4096, sorting internal dimatikan untuk penggunaan concurrent. Library [x/text/language](https://pkg.go.dev/golang.org/x/text/language) memvalidasi country/territory codes dan memberi daftar negara. UI memakai Intl.DisplayNames untuk nama Indonesia.

iPhone iOS/mobile, iPad iOS/tablet, Android phone/tablet, Windows/macOS/Linux/ChromeOS desktop diklasifikasi terpisah OS/device. UA kosong/tidak dikenali menghasilkan unknown, versi/brand/model yang tidak tersedia null. UA dapat dipalsukan; iPad desktop mode tanpa identitas iPad dapat terlihat macOS/desktop. Tidak menjanjikan deteksi semua model.

Agent lookup GeoLite lokal dan evaluasi policy tanpa request jaringan/SQL per klik. Country/ASN berasal dari IP dengan trusted proxies yang dikonfigurasi, bukan header bebas. Engine menggabungkan daftar global/domain dengan daftar link terpilih tanpa membangun daftar baru per request; paling banyak 300 rules efektif. Target 100–300 klik/detik tetap perlu benchmark B19.

Publish memakai optimistic expectedRevision dan transaksi PostgreSQL. Audit, published rules/target, history, desired revisions dan signed snapshots tersimpan bersama; kegagalan salah satu membatalkan semuanya. Concurrent global publish/enrollment/edit binding diserialisasi. Resource revision berbeda dari config revision: hasil publish memberi resource revision dan mapping desired revision tiap domain. Pending agent bukan applied, heartbeat bukan kesiapan DNS/HTTPS produksi (B11).

Batas: 100 rules/set, priority 1–1000, 10.000 rules/domain snapshot, 128 KiB PATCH rule draft, 3 MiB signed payload pusat dan 8 MiB envelope agent. History endpoint menampilkan 100 publish terbaru; database tetap menyimpan history lama untuk rollback API by revision. Belum ada event analytics sampai B12; simulator tidak menambah klik. PHP/API keputusan mengikuti B15.

## Pengujian

`(cd packages/policy && go test -race ./...)`, `./scripts/test-control.sh`, `./scripts/test-agent.sh`, `npm run check:contracts`, `npm run build`, `npm run test:live`, `npm run test:e2e`.

Backend memakai PostgreSQL sementara. Test mencakup AND/order/mandatory, iPhone/iPad/Android/desktop/unknown, fallback/loop/duplicate validation, CSRF/conflict, concurrent publish dan fanout/atomic rollback, serta binary agent asli dengan GeoLite sintetis. Browser live 16 desktop/mobile mencakup persistence, deteksi UA, preview, publish dan rollback. Test tidak mengisi domain/rule palsu dalam database lokal pengguna.

Risiko B13: known_bot (optional botCategory search/preview/automation/unknown/*), suspected_automation, tor_exit, reputation_flag, rate_limited; AND/prioritas tetap sama. Mode domain observe mengabaikan rule risiko pada actual routing dan menyimpan candidate; enforce memakai rules risiko; off mengabaikannya. Mandatory risk tidak didukung agar tidak meniadakan mode observasi; ordinary mandatory global blocks tetap berlaku.
