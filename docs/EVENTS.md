# B12 — antrean klik dan worker pusat

B12 mencatat keputusan GET untuk slug yang dikenal: redirect, fallback, atau block aturan. HEAD, probe/health, host asing, 404 dan konfigurasi unavailable tidak dihitung sebagai klik. B14 menampilkan challenge/error runtime sebagai belum dikumpulkan; perlu perluasan event pada batch terkait. Sejak revisi log 14 Oktober 2026, event baru menyimpan IP pengunjung, ASN dan organisasi ASN. Event tidak menyimpan UA mentah, query visitor atau header; hanya identitas domain/link, waktu UTC, revision, versi GeoLite dan klasifikasi negara/device/OS/browser beserta metadata deteksi opsional. Destination berasal dari keputusan routing yang sudah terverifikasi.

## Agent

`events.sqlite` berada pada `dataDir` volume persisten, mode 0600, bersama WAL/SHM dalam direktori 0700. Driver [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) tidak membutuhkan CGO. Writer tunggal menggunakan WAL, synchronous FULL, batch 100 event atau 50 ms. Redirect hanya memasukkan event ke buffer nonblocking 2.048 event; hot path tidak melakukan SQL atau request jaringan.

Default payload antrean 256 MiB, umur 47 jam untuk menyisakan margin dari batas ingest 48 jam. `queueMaxBytes` opsional (maksimum 1 GiB) dan `queueMaxAgeSeconds` opsional (maksimum 172.800 detik) ada pada config JSON agent. Batas payload berbeda dari ukuran file fisik SQLite; page limit sekitar dua kali batas payload + 16 MiB, WAL autocheckpoint 256 page, journal size limit 4 MiB. Tetap sediakan ruang bebas filesystem untuk WAL dan MMDB. Queue byte bookkeeping tidak menghitung ulang seluruh payload pada setiap batch. Expiry berjalan walaupun tidak ada klik baru. Data rusak/tidak dapat dibuka menghentikan startup untuk diagnosis, tanpa menghapus antrean.

Penuh buffer, batas byte, expiry atau gagal write: redirect tetap berjalan; event baru boleh hilang dan `droppedEvents` bertambah. `queueWriteErrors` membedakan gagal write dari overflow/expiry. Counter dipersist pada transaksi SQLite berikutnya yang berhasil; jika disk tetap gagal hingga crash, kenaikan counter terakhir bisa hilang. Buffer belum durable (termasuk batch writer) terlihat sebagai `bufferedEvents`; crash mendadak bisa kehilangan sampai 2.148 event (buffer 2.048 + batch maksimal 100). Pengukuran p95/p99 serta kehilangan nyata tetap B19. Graceful shutdown menguras buffer sebelum menutup database. Satu proses agent per dataDir; jangan jalankan dua container dengan volume queue sama.

Sender mengirim maksimal 100 event setiap satu detik; jika backlog >=100 sesudah ack, batch berikutnya setelah 10 ms. Request timeout 10 detik; retry 2/4/8/16/32 detik. ID random 32-byte canonical base64url dibuat sekali ketika klik diterima dan tidak berubah setelah restart/retry. Antrean hanya dihapus setelah HTTP 200, acknowledgement valid dan accepted + duplicates tepat sama dengan batch. Gagal jaringan, response hilang atau ack parsial menyimpan batch. 401/403 menghentikan sender dan mempertahankan data untuk diagnosis/reprovision. Setelah revoke, jangan memindahkan event ke identitas agent lain; bukti domain/agent lama tetap diperlukan.

## Ingest dan agregasi

`POST /v1/events/batch` membutuhkan bearer agent aktif CLI; cookie owner bukan credential agent. JSON dibatasi 1 MiB/500 event; setiap ID, enum, waktu dan binding diperiksa. Event maksimal 48 jam sebelumnya atau lima menit di masa depan; revision tidak boleh melebihi desired revision. B15 menghasilkan source PHP melalui transaksi keputusan pusat; adapter PHP tidak diperbolehkan ingest batch. Seluruh batch atomik: jika satu event salah/conflict, tidak ada event baru batch tersebut disimpan. Link harus ada pada domain agent; data lama tidak diasumsikan memiliki destination/revision saat ini karena agent bisa memakai snapshot leased sebelumnya.

Dedup memakai `(agent_id,event_id)` dan hash canonical event pada tabel nonpartisi. Retry identik mendapat duplicates; ID sama dengan payload berubah mendapat 409 event_conflict. Transaksi mengunci agent aktif kembali agar revoke yang sudah committed tidak bisa dilewati. `synchronous_commit=on` dipaksakan untuk transaksi ingest; acknowledgement dikirim sesudah commit raw event dan dedup key. Retry setelah response hilang aman.

Raw `traffic_events` dipartisi harian UTC sesuai occurredAt. Pembuatan partisi, ingest, worker dan retensi diserialkan dengan advisory transaction lock B12 untuk menjamin konsistensi DDL/dedup. Ini keputusan v1 private; contention dan kapasitas 100–300/detik harus diukur B19 sebelum claim produksi.

Worker otomatis berjalan pada `control serve`, batch maksimal 1.000 setiap detik. Advisory try-lock mencegah dua worker memproses batch sama. Agregat per menit/jam memakai domain/link/country/device/OS/browser/action/reason. Raw processed marker, increment agregat dan `links.clicks` commit dalam satu transaksi. `links.clicks` menghitung redirect/fallback; block memiliki agregat tersendiri. Kegagalan/crash transaksi tidak meninggalkan increment parsial; retry tidak double count. Worker berhenti sebelum pool PostgreSQL ditutup.

Retensi default raw 7 hari (`EVENT_RAW_DAYS`: 3–30), agregat 90 hari (`EVENT_AGGREGATE_DAYS`: raw–365), maintenance setiap lima menit. Partisi tua yang masih punya event belum diproses dipertahankan sehingga kegagalan worker tidak diam-diam menghapus backlog. Ini dapat menambah kebutuhan disk; periksa kesehatan worker. Dedup key dipertahankan selama window retensi raw berdasarkan waktu receive, lebih panjang dari window retry 48 jam. Agregat lama dihapus sesuai bucket UTC. Existing link counters tetap cumulative.

`GET /v1/admin/system/events` khusus owner memberikan pendingEvents, oldestPendingReceivedAt, watermark, queueDepth, queueBytes, droppedEvents, bufferedEvents dan queueWriteErrors. Queue adalah laporan heartbeat terakhir agent aktif (bukan pengukuran real-time); counter revoked tidak dimasukkan. `watermark` adalah waktu commit batch agregasi terakhir, bukan jaminan semua event sebelum timestamp sudah diterima/diproses. Gunakan pending/oldest untuk lag. Dashboard analytics/filter/export tersedia pada [B14](ANALYTICS.md).

## Bukti dan batas uji

Unit/race mencakup restart SQLite, ID retry tetap, ack parsial, bounded buffer/byte, expiry tanpa traffic dan write failure/recovery. Integrasi PostgreSQL terisolasi mencakup cookie/anon ditolak, scope domain/link, dua agent dengan ID sama, atomic conflict rollback, worker failure rollback, worker paralel/replay, partition retention dan revoke. Real process agent mengirim block/redirect ke pusat dengan GeoLite/signed config, lalu redirect bertahan pada outage pusat.

Batas yang tetap menunggu: VPS bersih, HTTPS publik, ARM native ([agenda](TEST_MATRIX.md)) dan benchmark/load/crash process/disk benar-benar penuh B19. Test write failure SQLite query_only adalah injeksi kegagalan transaksi, bukan bukti filesystem ENOSPC produksi.

Rilis lokal terverifikasi: v0.12.0-b12. Pusat lokal tetap http://127.0.0.1:8086 dan dashboard http://127.0.0.1:5173. Data uji/hasil lokal berada di data/control/b12-before.json, b12-after.json dan b12-verification.json (ignored); tidak berisi password/token. B13 kini memakai explicit protectionVersion heartbeat untuk gate publish risiko/enforce; status queue operasional saat ini dibaca melalui heartbeat/health B12.

B13 menambah optional risk evidence pada decision/event, candidate risk hourly aggregate serta feedVersion aktif. B12 sender/dedup/durability tetap dipakai; nil risk dari agent B12 tetap diterima. [Proteksi](PROTECTION.md).

## Keputusan API B15

[API keputusan](DECISIONS.md) menulis event `source=php` bersama respons replay dalam satu transaksi durable. Worker/retensi B12 dan log B14 dipakai kembali. Retry sama tidak menambah event atau charge visitor budget; payload berubah ditolak, revoke diperiksa lagi. IP pengunjung dan metadata ASN dicatat pada event baru; UA mentah tidak disimpan; keyed request fingerprint menjaga parity retry tanpa menyimpan sinyal mentah. Counter PHP menghitung keputusan pusat, belum membuktikan browser melakukan redirect.

`GET /v1/admin/system/events` menghitung queue/active/stale hanya dari agent CLI; PHP idle tidak memiliki heartbeat/queue lokal. Pending/watermark tetap mencakup seluruh source. Probe adapter tidak menghasilkan klik. Retensi membersihkan api_decisions berdasarkan waktu ingest bersama event_keys; window retry validasi tetap maksimal 48 jam dari occurredAt.

B17 menyediakan distribusi ZIP PHP signed dan wizard yang benar-benar memanggil API keputusan. Public/activation probe tidak menulis klik; GET shortlink mengirim satu event ID random dengan paling banyak satu retry identik. Counter tetap menghitung keputusan pusat, bukan receipt browser. Upload/download/probe memakai jalur terpisah dari ingest event, tanpa fixture pada database owner. [Panduan PHP](INSTALL_PHP.md).

B19: worker agregasi menunggu advisory lock ingest maksimal 250 ms, menggantikan try-lock yang langsung melewati siklus ketika traffic kontinu. Lock timeout membatalkan transaksi dan dilaporkan sebagai worker failure untuk dicoba kembali pada tick berikutnya; tidak menghapus event pending. Ini mengurangi starvation, bukan menjamin throughput API. Batch kini maksimal 100 event per 100 ms (maksimal nominal 1000/detik), seluruh rollup/counter/processed/watermark commit atomik. Uji lock busy→release→resume dan timeout terbatas melengkapi dedup/concurrency/rollback suite.


B19 terbaru: worker AggregateEvents memakai SET LOCAL jit=off khusus transaksi batch kecil; pool/global setting tidak diubah. Timing sebelum/sesudah menunjukkan hold p95 94,20→5,80 ms. Soak 300 RPS/120 detik lulus diagnostic: 36.000 request tanpa error, p95 HTTP 8,41 ms, event/counter tepat dan pending nol. Trigger test memastikan worker off dan setting koneksi kembali on sesudah commit. Observer hanya di fixture; full-duration/deployment gate tetap terbuka, outage tetap 10 detik.


Penutupan B19 lokal: [CAPACITY_B19.md](CAPACITY_B19.md) dan [OPERATIONS.md](OPERATIONS.md) berisi hasil full-duration, scope, failure/restore dan batas yang masih terbuka. API 50/30m,300/15m,600/5m memenuhi gate lokal; Go lokal 300/15m,600/5m tanpa error/loss; real batch ingest 300 events/s menerima 270k dalam 15m dengan accounting cocok. Restore dataset 90k, crash queue SIGKILL, SQLITE_FULL/recovery dan actual ENOSPC Docker tmpfs lulus. Backend/agent race, GeoLite cleanup/retensi, dashboard/contract dan Docker image build lulus. Source Dockerfile pusat sekarang menyertakan risk module. Riwayat status B19 berjalan sebelumnya bukan status final; B20/gate lingkungan nyata belum selesai.
