# Analytics dan log traffic — B14

Dashboard Ringkasan menampilkan keputusan 24 jam terakhir yang sudah diagregasi. Traffic & Log menyediakan periode UTC (ditampilkan Asia/Jakarta), filter domain/link/action/negara/device/OS/browser, ringkasan per jam, pagination server dan rincian keputusan. Data kosong tetap kosong; kegagalan API tidak diganti angka preview. Muat ulang dilakukan manual, tanpa streaming semua klik. Grafik dibatasi 120 kelompok dan tabel jam pada 168 jam berisi data terbaru; ringkasan tetap seluruh periode aktif.

API owner:

- `GET /v1/admin/analytics`: agregat dalam retensi `EVENT_AGGREGATE_DAYS` (default 90).
- `GET /v1/admin/events`: log raw dalam `EVENT_RAW_DAYS` (default 7), limit 1–100/default 25.
- `POST /v1/admin/events/export`: CSV dari filter yang sama, wajib session, Origin dan CSRF. Maksimal 1.000 baris pertama dan 8 MiB, dua export bersamaan per proses; `X-Export-Truncated: true` dan pesan UI jika hasil terpotong. Persempit filter/periode untuk export lengkap. Tidak ada job/file publik.
- `GET /v1/admin/system/events`: pending pusat, laporan antrean/loss agent aktif, jumlah laporan agent terlambat dan waktu batch worker terakhir.

`from` dan `to` wajib RFC3339 dengan batas menit, periode `[from,to)`. UI mengisi 24 jam hingga awal menit terakhir: menit yang sedang berjalan tidak termasuk. UI log memerlukan periode dalam retensi raw; endpoint analytics dapat membaca periode lebih panjang tanpa log raw. Retensi raw memakai cutoff harian UTC; agregat memakai cutoff rolling dan batas bawah menit berikutnya agar tidak menawarkan bucket yang sudah dihapus sebagian. Filter kosong berarti semua; unknown tetap nilai explicit. Parameter tidak dikenal/berulang, enum gabungan dan cursor yang berubah filter ditolak.

Log diurutkan `received_at DESC, id DESC`; cursor opaque mengikat seluruh filter, batas ID ingest dan posisi waktu/ID. Klik baru tidak masuk halaman berikutnya dari sesi pagination yang sama, termasuk klik terlambat dengan waktu kejadian lebih lama. Muat ulang log memulai snapshot baru. Retensi dapat menghapus baris saat pagination; tidak ada snapshot transaksi panjang yang menahan retensi. Detail berasal dari baris log yang sudah dimuat, berisi agent/event ID, received/occurred time, action/reason/rule/target aktual, revision, GeoLite/feed, klasifikasi device dan kandidat risiko. IP penuh/raw UA/query visitor tidak disimpan atau diexport.

Ringkasan memakai hourly rollups untuk jam penuh dan minute rollups di kedua batas sehingga tidak menggandakan hitungan dan tidak perlu membaca seluruh raw. Keterlambatan ingest/worker tetap dapat menambah hitungan periode lama pada refresh. Dashboard menghitung redirect/fallback/block secara terpisah. Known bot merupakan subset event aktual yang memiliki sinyal `known_bot`, bukan tambahan klik atau bukti identitas crawler terverifikasi. Kolom `bot_events` mulai diisi worker B14, termasuk backlog yang baru diproses; agregat lama tidak di-backfill. UI menyatakan cakupan ini. Observasi dengan beberapa sinyal tetap dihitung satu kali sebagai bot.

Challenge dan error runtime belum dikumpulkan; ditampilkan sebagai belum tersedia, bukan angka nol yang menyiratkan tidak ada error. HEAD/404/host asing/kegagalan sebelum keputusan tidak dihitung. Mode observe menampilkan kandidat tersendiri dan tidak mengubah action aktual. Feed unavailable/unconfigured berarti risiko tidak diketahui, bukan visitor bebas proxy.

Watermark adalah waktu transaksi worker terakhir, bukan kelengkapan semua klik sebelum timestamp. Pending/oldest menunjukkan antrean pusat; queue/buffer/drop/write-error berasal dari heartbeat terakhir agent CLI yang belum revoked. Laporan >90 detik atau belum ada dihitung stale. Counter bersifat kumulatif sesuai runtime/queue dan bukan loss khusus periode filter; crash buffer dapat kehilangan klik yang tidak pernah mencapai pusat. Angka ringkasan dan log dapat berbeda selama worker tertinggal. Tidak ada klaim real-time atau benchmark 300 RPS.

Migrasi `008_analytics.sql` menambah indeks pagination/rollup dan kolom bot counter. Deploy pusat baru memerlukan migrasi; agent tetap rilis `v0.13.0-b13`, tidak perlu update artefak agent untuk B14. CSV mengutip sel dengan encoder standar dan mengawali nilai mirip formula dengan apostrof.

Pusat lokal B14 dimigrasikan dan dijalankan pada 8086, dashboard 5173. Fingerprint owner/domain/link/key sebelum/sesudah identik; smoke owner listing/summary/health/export dan logout denial lulus. Domain/log development tetap kosong jika belum ada agent nyata, tanpa seed fixture. Hasil lokal ignored: `data/control/b14-before.json`, `b14-after.json`, `b14-verification.json`.

B15 menambahkan sumber PHP sebagai keputusan pusat; respons/delivery redirect belum diakui adapter. Panel rincian menyatakan sumber ini, ringkasan menyatakan batas penghitungan; queue/stale health hanya CLI. [Kontrak API](DECISIONS.md).
