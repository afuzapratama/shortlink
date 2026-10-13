# Revisi shortlink dan aturan

Tambahan: [ProxyCheck](PROXYCHECK.md) menyediakan key dashboard serta pilihan proxy/unknown fallback per link. Fitur proxy membutuhkan Go routingVersion 2; audience/fallback biasa tetap versi 1.

Arahan owner 11 Oktober 2026: kembali ke dev lokal sebelum release. Pengunjung yang memenuhi pengaturan menuju tujuan utama; selain itu, **termasuk bot/crawler**, menuju fallback milik shortlink.

## Alur pengguna

Tambah/edit link: pilih domain, isi slug, tujuan utama dan URL fallback. Bagian **Atur pengunjung tujuan utama** memuat negara, perangkat, OS dan browser; default semua. iPhone dapat dipilih melalui kombinasi mobile + iOS; iPad melalui tablet + iOS. Nama/model dari UA tetap informasi klasifikasi, bukan bukti hardware yang pasti.

Tujuan, fallback dan audience disimpan bersama sebagai draft. Link published tetap memakai versi aktif sampai publish. Publish/rollback memulihkan satu konfigurasi lengkap tanpa mengganti domain/slug/counter. Tombol **Buka** tersedia untuk link published dan membuka URL pendek di tab baru; membuka GET tersebut merupakan klik nyata.

Aturan global berada di halaman aturan global. Aturan domain berada di detail domain itu sendiri. Pilihan audience link langsung berada pada tambah/edit link; tidak perlu membuka halaman aturan terpisah. Editor global/domain menampilkan tindakan sederhana: izinkan atau arahkan ke fallback. Pengaturan lanjutan dilipat; prioritas tidak perlu diisi untuk membuat aturan baru.

## Keputusan

- Kondisi negara/device/OS/browser pada link memakai AND. Wildcard mengizinkan semua nilai, termasuk unknown; pilihan spesifik tidak mengizinkan unknown.
- Detected bot/crawler dan suspected automation menuju fallback walaupun audience cocok. Ini pengenalan sinyal, bukan jaminan mendeteksi bot yang menyamar sebagai browser normal.
- Pada tiap cakupan yang mempunyai aturan izin aktif, paling sedikit satu harus cocok (OR). Izin global, domain dan link serta audience link harus terpenuhi bersama (AND).
- Pengecualian yang cocok menang atas izin: tindakan block/fallback lama dipetakan ke fallback link yang dikonfigurasi. URL custom pada rule tidak mengganti tujuan/fallback link baru.
- Risk rules tambahan mengikuti mode proteksi domain: off/observe tidak menegakkan Tor/reputation/rate-limit; enforce menegakkan aturan risk yang dipublish. Bot/automation pada audience link selalu menuju fallback. Observasi risk tetap tercatat.
- Saat GeoLite wajib tidak tersedia, credential dicabut, snapshot kedaluwarsa atau pusat PHP gagal, runtime tetap fail-closed 503 tanpa Location. Fallback adalah hasil keputusan routing valid, bukan bypass kegagalan sistem. Slug tidak dikenal tetap 404.

## API dan penyimpanan

Request POST/PATCH link menerima `routing`:

```json
{
  "fallback": "https://destination.example/alternate",
  "country": "ID",
  "device": "mobile",
  "os": "ios",
  "browser": "safari"
}
```

Fallback wajib HTTP(S), tanpa kredensial/CRLF atau tujuan pada domain shortlink yang sama. Migration `011_link_routing.sql` menambah JSONB `routing`/`draft_routing`, history routing dan capability agent. Respons management memisahkan `routing` aktif dan `draftRouting`; signed snapshot tidak membawa draft. Omitted routing pada PATCH mempertahankan routing/draft sebelumnya. API lama tetap dapat membuat link tanpa routing; UI baru meminta fallback untuk link baru.

Link lama tanpa `routing` menggunakan policy lama sampai operator mengisinya secara eksplisit. Rollback ke history sebelum fitur ini memulihkan perilaku lama juga; jangan menganggap seluruh link telah berubah hanya karena backend diperbarui.

Simulator, API decisions PHP dan runtime Go memakai engine bersama. Event fallback memakai `rule_fallback`, termasuk ketika penyebabnya audience/bot dan `ruleId` null. Retry event yang sama tetap mendapatkan keputusan immutable sebelumnya; tidak dihitung ulang.

## Kompatibilitas dan status

Go baru mengiklankan `routingVersion=1` saat enrollment/heartbeat. Publish routing pada domain lokal ditolak jika agent aktif belum mendukungnya; API PHP menjalankan keputusan pada pusat sehingga tidak memerlukan perubahan ZIP. Enrollment/snapshot generation dan heartbeat downgrade juga memeriksa capability. Update runtime Go harus dilakukan sebelum mengaktifkan fitur. Jangan rollback ke binary lama selama signed snapshot dengan routing baru masih tersimpan: binary lama tidak memahami field tersebut. Pulihkan konfigurasi legacy, sinkronkan lalu rencanakan downgrade.

Revisi ini **lokal**, belum menjadi artifact immutable baru dan belum dipasang ke VPS. Hasil publik B19 bukan hasil kapasitas fitur ini. Build, policy, backend/API, Go runtime dan browser diuji sebelum review lokal; acceptance hosting/public serta kapasitas diulang pada kandidat release setelah revisi owner selesai.

## Hasil verifikasi lokal

30 tes UI demo dan 26 tes browser API/PostgreSQL lulus pada desktop/mobile. Policy race, backend race dan Go agent race lulus; tes tambahan memastikan draft tidak bocor, fallback/bot benar pada API dan Go, event retry tidak digandakan, history rollback memulihkan routing, URL fallback loop ditolak dan agent lama tidak dapat menerima publish fitur. Migration 011 sudah diterapkan pada database dev lokal, tanpa bootstrap ulang owner. Dashboard lokal menggunakan API loopback 8086 melalui proxy Vite 5173.
