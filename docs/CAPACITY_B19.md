# Hasil kapasitas B19 — pengujian lokal

Tanggal review: 11 Oktober 2026 (Asia/Jakarta). Data berikut berasal dari fixture terisolasi, bukan database owner.

## API keputusan pusat

| Beban | Durasi | Request | Error | p95 HTTP | p99 HTTP | Gate lokal |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| 50 RPS | 30 menit | 90.000 | 0 | 9,61 ms | 12,42 ms | Lulus |
| 300 RPS | 15 menit | 270.000 | 0 | 8,98 ms | 13,91 ms | Lulus |
| 600 RPS | 5 menit | 180.000 | 37 (0,0206%) | 9,66 ms | 35,59 ms | Lulus ambang stress |

Gate: error/rejected/generator drops <0,1% dan p95 <50 ms. Semua 37 error pada stress merupakan `decision_busy`, tidak ditutupi retry. Request yang berhasil cocok dengan event unik dan cumulative clicks; pending setelah catch-up nol pada semua tahap. Latency adalah round-trip HTTP loopback termasuk body, bukan server-only murni.

Tahap 300: Go test lulus tetapi wrapper bash mengalami error setelah tes selesai karena file wrapper diubah ketika masih berjalan. Log error dipertahankan. Smoke wrapper baru/restore lulus; 50 dan 600 memakai snapshot wrapper immutable dan selesai bersih. Bukti utama: `data/control/b19-full{50,300,600}.json/log`.

## Go lokal dan upload antrean

| Beban | Durasi | Redirect | Error | p95 | p99 | Delivered unique | Dropped/write error |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| 300 RPS | 15 menit | 270.000 | 0 | 2,48 ms | 4,06 ms | 270.000 | 0/0 |
| 600 RPS | 5 menit | 180.000 | 0 | 2,28 ms | 5,20 ms | 180.000 | 0/0 |

Routing memakai signed snapshot, 32 slug, UA unik, optional GeoLite, SQLite WAL/FULL dan receiver HTTP stub sehat. Antrean habis setelah load. Ini tidak membuktikan GeoLite produksi atau receiver PostgreSQL end-to-end dalam satu pipeline. Kedua tahap Go berjalan bersama sebagian baseline API 50 RPS pada host yang sama. Bukti: `b19-local-full{300,600}.json/log`.

## Batch ingest PostgreSQL nyata

CLI enroll/verify, link dipublish dan synthetic classified events dikirim melalui bearer-bound `/v1/events/batch` memakai 100 event/request. Selama 15 menit pada 300 event/detik: 270.000 event diterima melalui 2.700 HTTP request, tanpa error; tercapai 300,10 event/detik. P95 batch 53,74 ms/p99 73,15 ms; counter/unique masing-masing 270.000 dan pending nol. Latency satu batch 100 event tidak dibandingkan dengan target latency satu klik. Routing agent tidak termasuk pada tahap ini. Bukti: `b19-batch-ingest-full300.json/log`.

## Backup, recovery dan retensi

Dataset baseline 90.000 event dibackup (22.746.639 byte) dalam 1,56 detik dan direstore dalam 1,51 detik. Fingerprint owner/signing/link, event/dedup/replay counts dan aggregates cocok. Database restore kosong; checksum dan private modes diverifikasi. Ini waktu database fixture di tmpfs, bukan RTO seluruh VPS atau jaminan seluruh row produksi. Backup tidak mengandung MMDB/env; assets dan GeoLite valid harus disiapkan lagi.

Uji tambahan: SIGKILL process queue setelah 10 event durable lalu restart mempertahankan ID/ACK; SQLITE_FULL page limit/recovery; actual ENOSPC pada Docker tmpfs 1 MiB lalu space release/recovery; outage 10 detik, kehilangan ACK, stable ID/dedup dan drain; rollback tengah/akhir pgx batch; worker lock wait bounded dan JIT transaction-local; event retention mempertahankan pending partition; GeoLite corruption/expiry/cleanup. Buffer yang belum durable tidak diklaim bebas kehilangan setelah crash. Bukti di `b19-queue-enospc.log`, `b19-final-agent-all.log`, `b19-final-control.log`, `b19-final-geoip.log` dan `b19-full50.json`.

## Lingkungan dan batas

PostgreSQL/WAL fixture berada di **tmpfs**, bukan SSD fisik. Host shared, tidak dibatasi 4 vCPU/8 GB. API satu domain/satu slug, fixture GeoLite sintetis, sebagian UA unik dan IPv4/IPv6; challenge provider tidak diuji. Benchmark bukan seluruh `control serve` dengan publisher/maintenance/DNS probe aktif. Batch ingest berjalan bersama akhir baseline dan sebagian stress API; workload gabungan ini dicatat, bukan disebut single-service isolation.

Sampling CPU/RSS hanya proses gabungan generator/API/worker, tidak termasuk PostgreSQL atau services lain. Pada full300, observed peak RSS sekitar 148,58 MiB (sampling 10 detik), bukan upper bound absolut atau sizing produksi. Environment: `data/control/b19-environment.json`; samples `b19-full{50,300}-process.json`.

Hasil memenuhi gate aplikasi **lokal** yang dijalankan dan menunjukkan margin untuk target private 100–300 klik/detik pada fixture. VPS bersih, SSD/network round-trip, HTTPS publik, ARM native, hosting/provider PHP nyata, minimum PHP 8.2, berbagai rule/domain dan deployment/upgrade tetap harus diuji sebelum produksi. Tidak ada klaim kapasitas 600 RPS pada VPS atau shared hosting dari hasil ini.
