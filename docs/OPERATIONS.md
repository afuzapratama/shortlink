# B19 — Operasi dan pengujian kapasitas

B19 lokal selesai sesuai [CAPACITY_B19.md](CAPACITY_B19.md). Pengujian nyata kini mencakup VPS ARM bersih, HTTPS, PHP aaPanel/cPanel, Go 300 RPS/15 menit, outage 10 detik dan backup/restore 388.790 events. **Gate kapasitas PHP/API belum lulus pada VPS pusat 2 vCPU**; pekerjaan kapasitas B19 tetap terbuka. Review B20 sudah dimulai, dengan status dan runbook di [RELEASE_B20.md](RELEASE_B20.md). Hasil dan batas pengujian: [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md).

## Backup dan restore pusat

Backup memuat database PostgreSQL, termasuk owner/password hash, encrypted MFA, recovery hash, sessions, signing seed, domain/link/snapshot, audit dan traffic. Perlakukan backup sebagai secret. Simpan pada filesystem yang menerapkan permission 0700/0600; mount DataDrive lokal tidak memenuhi syarat. Enkripsi dan transfer backup off-host dilakukan operator. Tool tidak membuat salinan MMDB atau memasukkan env/credential agent.

```sh
# DATABASE_URL sudah dimuat dari private env, tanpa ditulis pada command history.
bash scripts/backup-control.sh /private/backups/shortlink-20261010
# Buat database kosong khusus restore, tetapkan RESTORE_DATABASE_URL melalui private env.
bash scripts/restore-control.sh /private/backups/shortlink-20261010
```

Restore memverifikasi checksum/format/permission dan menolak target yang berisi tabel/view/sequence. pg_restore memakai satu transaksi dan berhenti saat error; tidak menghapus database atau tabel yang ada. Jangan memakai database production aktif sebagai target. Matikan pusat/worker lama sebelum cutover. Jalankan `control migrate` dengan private env target restore. Konfigurasi origin/database/asset paths harus disiapkan lagi; pastikan root key sama, gunakan GeoLite yang valid dari sumber berlisensi/publisher, pulihkan paket signed/feed yang relevan, lalu periksa readiness, login, probe agent dan publish revision. Sesi/recovery masih tersalin: bila restore untuk insiden credential, cabut sesi/agent dan rotasi melalui workflow keamanan. Backup yang memuat MMDB tidak dibuat; retensi GeoLite tetap dikelola publisher/agent.

Uji ulang tanpa menyentuh database owner:

```sh
bash scripts/test-backup-restore.sh
```

Fixture menguji password/MFA marker/session/audit/root/schema tetap identik, migration rerun, nonempty target dan corrupt backup. Ini bukan uji kehilangan seluruh VPS atau restore dataset produksi besar; waktu restore/RPO/RTO produksi belum diukur.

## Kapasitas

```sh
bash scripts/test-capacity.sh smoke /tmp/capacity-smoke.json
# Acceptance pusat: 50 RPS/30 menit, 300 RPS/15 menit, 600 RPS/5 menit.
bash scripts/test-capacity.sh full /private/results/capacity-full.json
# Profiling hanya pada fixture, tidak membuka endpoint debug production.
SHORTLINK_CPU_PROFILE=/tmp/shortlink-cpu.pprof bash scripts/test-capacity.sh smoke /tmp/profile-report.json
# Dari services/control:
go tool pprof -top /tmp/shortlink-cpu.pprof
```

Runner membuat PostgreSQL terisolasi lewat socket pada direktori sementara, schema/owner/domain API/GeoLite sintetis, real HTTP loopback, signed published link dan worker agregasi. Beban dijadwalkan berdasarkan waktu (bukan request berikutnya setelah respons), maksimum 128 in-flight pada generator, tanpa retry, koneksi keep-alive. Laporkan generator drops, error/503/rejected, p50/p95/p99 round-trip, allocations/heap/GC, event count dan pending worker. p95 mencakup body/transport dan bukan pengukuran server-only murni. Respons gagal ikut latency; p95 rendah dengan banyak 503 **tidak berarti gate lulus**. Full mode gagal bila error/rejected/generator drops >=0,1% atau p95 >=50 ms. Smoke mode hanya menghasilkan diagnosis, exit sukses bukan kelulusan gate. Load IPv4/IPv6 dan sebagian UA unik; belum mencakup seluruh kombinasi rule/cache/hostile pattern, PHP/provider, TLS publik atau network round-trip VPS.

Agent lokal diagnostic:

```sh
cd services/agent
SHORTLINK_LOAD_MODE=smoke SHORTLINK_LOCAL_LOAD_REPORT=/tmp/local-load.json \
  go test -count=1 -timeout 3m -run '^TestLocalCapacityHTTP$' -v ./internal/runtime
```

Tahap lokal 50/300/600 RPS masing-masing 10 detik, 32 slug, UA unik, snapshot signed, pusat 503, SQLite durable. GeoLite optional pada fixture ini; **belum** merupakan acceptance local GeoLite production atau outage 10 menit. Lease harus tetap berlaku; lease habis/revoke tidak dilonggarkan demi benchmark. Antrean tidak diupload saat diagnostic sehingga queue growth expected.

## Pemeriksaan kegagalan dan retensi

`test-agent.sh`, `test-control.sh` dan `test-geoip.sh` mencakup queue restart/retry/ACK, failure write melalui SQLite query_only, snapshot outage/revoke/expiry, API rollback/deadline dan GeoLite download corruption/restart/swap/expiry cleanup. Ini injeksi terkontrol; tidak diklaim sebagai ENOSPC filesystem sungguhan, process SIGKILL under load atau pusat putus 10 menit. Jangan memenuhi disk VPS owner untuk eksperimen; gunakan volume disposable bounded pada pengujian berikutnya.

Pusat menjalankan retensi raw/aggregate melalui worker; partisi dengan event pending tidak dihapus. Pantau worker lag/disk sebelum retensi backlog membengkak. GeoLite expired dibersihkan publisher/agent, dan cleanup tidak menyimpan backup MMDB. Backup database hanya menyimpan metadata/referensi versi; restore harus menyediakan database GeoLite yang masih valid. Jangan menyalin ulang rilis expired.

## Gate tersisa

Diagnosis bottleneck API dan tuning berbasis profiling; acceptance full 30/15/5 menit kedua jalur yang relevan; metrik CPU/RSS/database latency/loss end-to-end; outage singkat 10 detik/catch-up yang sekarang sudah diuji; pengujian prolonged outage terpisah; SIGKILL/ENOSPC disposable; restore dataset representatif; clean VPS/HTTPS publik/native ARM/shared hosting nyata. B19 belum selesai sampai hasil dan batas material dicatat.


## Hasil diagnostic lokal 10 Oktober 2026

| Jalur | RPS | Request | Error | p95 round-trip |
| --- | ---: | ---: | ---: | ---: |
| Go lokal, pusat 503 | 50 | 500 | 0 | 2,97 ms |
| Go lokal, pusat 503 | 300 | 3000 | 0 | 2,79 ms |
| Go lokal, pusat 503 | 600 | 6000 | 0 | 2,69 ms |
| API pusat | 50 | 500 | 0 | 9,39 ms |
| API pusat | 300 | 3000 | 137 (503) | 21,98 ms |
| API pusat | 600 | 6000 | 1780 (503) | 32,36 ms |

Masing-masing tahap hanya 10 detik. Profil CPU terpisah 30,24 detik mencatat 19,52 detik CPU sample pada proses gabungan test/generator/server; pprof menunjukkan regex/UA sebagai kandidat investigasi, bukan atribusi server eksklusif karena setup dan load generator juga direkam. Contention SQL/advisory lock/worker memerlukan block/trace dan DB timing sebelum tuning. Profil tidak membuka debug endpoint pada pusat owner. Bukti di `data/control/b19-*.json/log/pprof`, tanpa credential fixture. Backend/agent/GeoLite race suites dan restore terisolasi lulus. Gate durasi penuh belum dijalankan karena diagnostic masih gagal pada API beban tinggi.

## Outage singkat dan diagnosis lanjutan

Sesuai permintaan pengguna, pengujian outage lokal dibatasi **10 detik**, menggantikan rencana outage 10 menit pada sesi ini. `TestShortOutageCatchupWithoutDuplicateIDs` mengirim redirect sekitar 50/detik dengan lease valid ke agent fixture dan pusat upload 503. Seluruh event harus tersimpan tanpa dropped/write error; setelah pusat pulih, respons upload pertama sengaja hilang setelah commit. Retry harus membawa ID yang sama, menghitung duplicates dan menguras queue, dengan jumlah unique sama dengan jumlah redirect. Receiver pada uji singkat merupakan stub dedup ID; PostgreSQL dedup/retry/agregasi atomik tetap diuji terpisah pada suite backend. Ini bukan simulasi reboot/network firewall VPS atau bukti outage 10 menit.

Runner kapasitas diperbaiki agar worker mengikuti proses pusat: 1000 event per satu detik, timeout kerja 10 detik. Hasil sebelumnya memakai worker eksperimen 5000 event per 250 ms; jangan memakai hasil tersebut sebagai representasi konfigurasi produksi. Baseline yang diperbaiki: 50 RPS 0 error p95 10,63 ms; 300 RPS 315/3000 error p95 10,17 ms; 600 RPS 1778/6000 error p95 31,07 ms (tetap 10 detik per tahap, shared host).

Parsing device/OS/browser/UA tidak bergantung pada mutable revision; parsing dipindahkan sebelum transaksi keputusan sehingga regex tidak menahan lock domain/event dan mutex GeoLite/feed. Lookup GeoLite/feed, evaluasi aturan terbaru, charge budget, replay dan event commit tetap mengikuti transaksi yang ada; retry tidak menambah charge. Pusat kini membatasi 16 evaluasi/probe aktif, dengan pool database tetap 10 koneksi dan deadline dua detik. Perubahan ini tidak melonggarkan keputusan/revoke agar benchmark lolos. Gate throughput tetap mengikuti hasil ukur sesudah perubahan, bukan klaim optimasi teoritis.


Hasil parsing di luar lock (10 detik per tahap, worker sesuai pusat): 50 RPS 0 error p95 10,95 ms; 300 RPS 86/3000 503 (2,87%) p95 17,38 ms; 600 RPS 1036/6000 503 (17,27%) p95 24,38 ms. Baseline yang worker-nya sama memiliki 315 dan 1778 error; ini perbandingan diagnostic shared host, bukan benchmark terkontrol berulang. Masih gagal gate error dan belum ada acceptance full duration. Pending kumulatif 4581 pada akhir stress; jangan menyimpulkan worker dapat sustain 600 RPS.

### Worker fairness

Worker sebelumnya memakai try-lock lalu melewati tick ketika ingest aktif; pada traffic kontinu ini dapat mempertahankan backlog. B19 mengubah worker menjadi menunggu advisory lock dengan `lock_timeout` lokal 250 ms. Bila batas terlampaui, transaksi rollback dan tick selanjutnya mencoba lagi; pending tidak dihapus. Event/rollup/dedup/retention tetap diserialkan seperti sebelumnya. Uji integrasi memeriksa worker benar-benar menunggu lock sibuk, melanjutkan setelah release dan timeout dalam batas. Batch pusat dipecah menjadi 100 event per 100 ms untuk memendekkan setiap transaksi; kapasitas nominal tetap 1000 event/detik bila setiap tick dapat menyelesaikan batch. Perubahan ini memberi giliran worker, tetapi durasi batch masih dapat menahan ingest: nilai latency/error benchmark tetap menentukan gate API.


### Hasil worker batch kecil dan concurrency B19

Worker kini maksimal 100 event per 100 ms, lock wait lokal 250 ms; slot keputusan/probe dinaikkan secara terbatas menjadi 16, database pool tetap 10. Benchmark memakai worker yang sama. Di shared host (diagnostic 10 detik per tahap), hasil konfigurasi final:

| RPS | Request | Error 503 | p95 HTTP | Pending akhir tahap |
| ---: | ---: | ---: | ---: | ---: |
| 50 | 500 | 0 | 10,16 ms | 5 |
| 300 | 3000 | 0 | 16,67 ms | 25 |
| 600 | 6000 | 498 (8,30%) | 63,54 ms | 50 |

300 RPS memenuhi ambang error/latency pada diagnostic singkat ini; **belum** memenuhi acceptance 15 menit/TLS/hosting. 600 RPS gagal error dan latency; safeguards tetap menolak overload, jangan menyatakan kapasitas 600 RPS. Slot tambahan menambah request yang dapat menunggu koneksi/lock, bukan menambah koneksi database atau melewati validasi. Throughput maksimum nominal worker bukan jaminan karena setiap transaksi/tick memerlukan waktu. Bukti: `data/control/b19-slots16.json` dan final worker tests.


### Uji kestabilan per rate dan catch-up

Runner dapat memilih satu rate tanpa menyisipkan tahap lain:

```sh
SHORTLINK_LOAD_RATE=300 SHORTLINK_LOAD_SECONDS=120 \
  bash scripts/test-capacity.sh smoke /tmp/soak-300.json
# Acceptance tahap 300 saja tetap memakai 15 menit, bukan durasi smoke:
SHORTLINK_LOAD_RATE=300 bash scripts/test-capacity.sh full /private/results/full-300.json
```

`SHORTLINK_LOAD_RATE` hanya menerima 50/300/600. Smoke duration 1–120 detik; full duration tetap mengikuti plan. Runner sekarang menghentikan traffic di akhir dan memberi worker sampai 15 detik untuk drain. Jumlah event ID unik dan cumulative clicks harus sama dengan HTTP 200 yang berhasil, dan pending harus nol. Mismatch membuat test gagal sekalipun smoke; ambang latency/error smoke tetap diagnostic. Ini memverifikasi accounting/catch-up fixture tanpa mengirim traffic atau menulis fixture pada pusat owner.


Uji dua menit pertama pada 300 RPS: 36.000 request, 111 respons 503 (0,308%), p95 16,09 ms/p99 37,51 ms. 35.889 respons 200 sesuai dengan 35.889 event unik dan cumulative clicks; pending 30 saat beban selesai, nol saat pemeriksaan catch-up. Gate error <0,1% **gagal**, meskipun drain/accounting lulus. Tes regresi memakai database lain berjalan bersamaan pada sebagian periode dan menambah beban host; hasil ini tetap disimpan, lalu benchmark diulang tanpa menjalankan suite tambahan. Host masih shared/uncapped.


Pengulangan dua menit tanpa suite tambahan: 36.000 request, 149 respons 503 (0,414%), p95 13,14 ms/p99 41,28 ms. 35.851 HTTP 200 = 35.851 event unik = 35.851 cumulative clicks, pending nol; generator drops nol. Test diagnostic/accounting sukses tetapi `ApplicationGate=false`: smoke tidak membuat exit gagal hanya karena ambang error. **300 RPS masih belum stabil pada ambang error**, termasuk setelah host tidak menjalankan suite tambahan. Hasil 10 detik tanpa error tidak boleh digunakan sebagai kelulusan throughput. Tidak ada perubahan tuning baru pada runtime dalam tahap validasi ini; pusat owner tetap menggunakan konfigurasi sebelumnya.


### Diagnosis 503 dan admission bounded

Runner mencatat histogram `errorCauses` dengan code respons tanpa raw body/secret. Uji 300 RPS/60 detik sebelum admission queue mencatat 166/18.000 respons 503; seluruhnya `decision_busy`, bukan dependency/transaksi `control_unavailable`. Event/counter untuk 17.834 request berhasil cocok dan drain habis. Ini menunjukkan immediate rejection pada slot penuh dalam fixture, bukan bukti bahwa semua database production bebas bottleneck.

Candidate admission queue tetap maksimal 16 evaluasi/probe aktif dan 16 request menunggu, dengan wait paling lama 25 ms. Deadline dua detik dimulai sebelum admission; queue full/timeout/cancel menghasilkan 503 `decision_busy`. Pool database tetap 10. Berbeda dari eksperimen wait 100 ms sebelumnya, antrean ini diuji setelah batch worker diperkecil; hasil baru menentukan apakah diterapkan. Uji unit mencakup queue full, cancel, timeout bounded dan acquire setelah slot dilepas.


Konfigurasi yang dipertahankan: maksimal 16 request aktif + 16 penunggu selama 25 ms, deadline total dua detik mencakup admission. Hasil dua menit 25 ms: 63/36.000 503 (0,175%), seluruhnya `decision_busy`, p95 15,38 ms/p99 42,49 ms; 35.937 HTTP 200/event unik/klik cocok, pending nol. Percobaan 50 ms tidak dipertahankan: 138/36.000 503 (0,383%), p95 14,49 ms/p99 59,11 ms, accounting/drain juga cocok. Host shared menyebabkan variasi antarrun; angka ini tidak membuktikan optimalitas 25 ms atau throughput produksi. Kedua hasil gagal ambang error <0,1%. Seluruh 503 yang diamati berasal dari admission, sehingga diagnosis berikutnya perlu mengukur hold time per fase/lock/DB dan jeda host, bukan mengasumsikan API sudah stabil atau membuat antrean tanpa batas.


### Timing per fase pada fixture B19

Timing hanya aktif bila runner menyisipkan observer melalui context; tidak ada endpoint debug atau header timing publik dan tidak merekam ID/UA/IP/credential. Histogram tiap fase berisi count/p50/p95/p99/max waktu wall-clock, bukan CPU-exclusive. `pool_begin` mencakup acquire pool dan BEGIN, `event_lock` mencakup query/wait advisory lock, `domain_lock` mencakup SELECT/recheck/row lock, `transaction_total` mencakup seluruh MakeDecision termasuk pool. Percentile berbeda fase tidak boleh dijumlahkan.

Pada 300 RPS/60 detik sebelum batching: 68/18.000 503 semuanya decision_busy; event unik/clicks/HTTP200 17.932 dan drain nol. Event lock p95 7,93 ms/p99 19,30 ms/max 170,13 ms; pool+BEGIN p95 0,17 ms/p99 4,37 ms; UA p95 6,08 ms; transaksi p95 10,22 ms; commit p95 0,16 ms. Lock wait/tail transaksi lebih menonjol daripada eval/commit pada fixture ini; shared host tetap dapat mempengaruhi tail.

Candidate batching memakai pgx.Batch untuk lima statement tulis (dedup key, raw event, replay response, status domain, status agent) dalam **transaksi yang sama**. Validasi binding/revoke/replay/routing dan lock tetap. Setiap hasil statement diperiksa, error atau failure close membatalkan transaksi; commit tetap synchronous dan terukur. Ini mengurangi round trip, bukan menghapus durability atau lock untuk membuat benchmark lulus.


Hasil batch transaksi 300 RPS/120 detik: 169/36.000 error (0,469%), seluruhnya decision_busy; p95 HTTP 9,96 ms/p99 83,54 ms. 35.831 respons 200 = event unik = counter klik, pending nol. Phase p95 event lock 1,86 ms, transaksi 3,80 ms, write batch 0,87 ms; max event lock masih 234,62 ms dan pool+BEGIN 133,45 ms. Perbandingan per-fase shared host menunjukkan round-trip/transaction p95 lebih kecil, tetapi **error gate tetap gagal dan tail belum stabil**. Baseline timing 60 detik dan batch 120 detik berbeda durasi; hasil bukan eksperimen hardware terkontrol. Jangan menyimpulkan batching menghilangkan 503.

Suite menambahkan kegagalan trigger pada UPDATE agents, statement terakhir batch, lalu memastikan dedup/raw event/replay/status domain semuanya rollback dan retry berikutnya menghasilkan satu respons replay. Ini melengkapi trigger kegagalan di tengah batch serta revoke/binding/replay/concurrency yang sudah ada. Root/owner config tetap; instrumentasi timing dinyalakan hanya oleh fixture context. Langkah berikutnya: mengukur hold time worker, SQL plan/DB waits dan jeda host secara terpisah sebelum merombak lock global; capacity gate dan full-duration tetap terbuka.


### Worker JIT dan hasil terbaru

Timing worker memperjelas tail: sebelum perubahan, pada diagnostic 30 detik/300 RPS, worker_lock_hold p95 94,20 ms/max 187,81 ms; worker_lock_wait p95 4,72 ms. API memperoleh 97/9000 decision_busy, HTTP p95 56,94 ms. Waktu worker mengerjakan batch saat sudah memegang lock jauh lebih panjang daripada menunggu lock.

Agregasi kini menjalankan `SET LOCAL jit=off` di dalam transaksi. Pengaturan database/session global tidak diubah; event ingest dan query lain tetap memakai konfigurasi semula. Tujuannya menghindari overhead kompilasi pada batch kecil yang sering berjalan. Bukti fixture menguatkan diagnosis ini, tetapi tidak diklaim membuktikan semua tail berasal dari JIT tanpa trace plan/hardware terkontrol. Lock global, batas antrean, synchronous commit, dedup dan atomic rollups tetap berlaku.

Uji 30 detik sesudah perubahan: 9000 request tanpa error, HTTP p95 8,61 ms, worker hold p95 5,85 ms. Pengulangan **120 detik/300 RPS**: seluruh 36.000 request HTTP200, error/rejected/generator drops nol, p95 HTTP 8,41 ms/p99 11,30 ms. Unique events dan cumulative clicks masing-masing 36.000; pending nol. Worker hold p95 5,80 ms/p99 8,71 ms/max 16,72 ms; event lock wait p95 1,08 ms. `ApplicationGate=true` pada diagnostic dua menit ini; **bukan acceptance 15 menit**.

Test memakai satu koneksi pool dengan JIT awal on dan trigger yang menolak worker apabila setting-nya bukan off; setelah commit, SHOW jit harus kembali on dan event sudah processed. Ini menguji override transaction-local dan pemulihan setting pooled connection. Observer timing worker dipasang hanya pada fixture; pengambilan histogram menunggu worker berhenti agar data akhir konsisten. Bukti: `data/control/b19-worker-nojit-soak.json/log`, regression `b19-nojit-control.log`.

Berikutnya acceptance 300 RPS/15 menit, baseline 50 RPS/30 menit dan ulang stress 600 RPS dengan konfigurasi terbaru; hasil stress lama belum membuktikan batas konfigurasi baru. Gate failure/real deployment B19/B20 tetap terbuka. Outage tetap 10 detik sesuai permintaan.


### Acceptance penuh B19 sedang berjalan

Runner kini mencetak progress setiap 30 detik tanpa payload/credential. Tahap 300 RPS/15 menit berjalan terisolasi dari database owner; suite tambahan tidak dijalankan bersamaan. `sample-capacity-process.py` membaca CPU/RSS dari /proc proses httpapi.test tanpa environ. Metrik mencakup generator/API/worker dalam satu proses, tidak mencakup PostgreSQL dan layanan host lain; interval sampling 10 detik. Peak observed RSS bukan upper bound absolut karena sampling.

Pada lingkungan lokal ini /tmp berupa **tmpfs**: PostgreSQL fixture/WAL tidak mengukur SSD fisik. Host shared dan tidak dicap 4 vCPU/8 GB; GeoLite fixture sintetis dan transport loopback. Hasil full duration tetap penting untuk accounting/long-run behavior, tetapi bukan sizing atau acceptance VPS/TLS/provider. B19 lokal dan gate deployment nyata harus dilaporkan terpisah.

Tes tambahan disiapkan: SIGKILL helper proses queue setelah sepuluh event sudah durable, restart harus mempertahankan ID dan ACK; SQLITE_FULL page limit lalu recovery; actual ENOSPC dengan Docker tmpfs 1 MiB tanpa jaringan/volume proyek. Test ENOSPC memakai static Go test binary dan existing image amd64 hanya sebagai runtime. Seluruhnya dijalankan setelah benchmark, bukan dianggap lulus sebelum hasil tersedia.

Opt-in `SHORTLINK_LOAD_RESTORE=1` membuat database restore kosong pada cluster benchmark lalu backup/restore dataset setelah load/worker selesai. Report mengukur backup/restore seconds/bytes dan membandingkan fingerprint owner/root/link, event/dedup/replay counts serta aggregates. Ini fixture subset integrity, bukan klaim semua data produksi atau backup MMDB.


### Hasil acceptance 300 RPS penuh dan gate berikutnya

API pusat **900 detik/300 RPS**: 270.000 HTTP200, error/rejected/generator drops nol, p95 8,98 ms/p99 13,91 ms; 270.000 unique events = successful requests = clicks, pending nol. Full-mode Go test lulus, duration 902,45 detik termasuk catch-up/fixture cleanup. Observed RSS proses gabungan generator/API/worker sekitar 148,58 MiB (88 samples, tidak mencakup PostgreSQL). Report/log: `data/control/b19-full300.json/log`, process samples `b19-full300-process.json`.

Wrapper bash sempat error setelah Go test lulus karena file wrapper diubah saat proses masih membacanya. Error dicatat pada log, bukan dihapus atau disebut wrapper sukses. Rerun smoke runner baru dengan restore 250-event lulus; tahap berikutnya memakai **snapshot wrapper immutable** dengan root project fixed. Hasil Go full300 tetap tersedia dan accounting tidak terpengaruh, tetapi exit wrapper pertama tidak bersih.

Baseline API **50 RPS/1800 detik** menggunakan wrapper immutable dan restore dataset setelah load. Go lokal **300 RPS/900 detik** berjalan paralel pada host yang sama, healthy receiver stub/upload SQLite, 32 slug dan optional GeoLite. Ini workload gabungan, bukan single-service isolation; receiver lokal stub bukan PostgreSQL. Mode full lokal tidak mematikan pusat selama 15 menit, sehingga batas outage 10 detik tetap dipatuhi. Setelah itu stress 600 RPS/300 detik pada API dijalankan terpisah. Status tahap baru masih berjalan sampai report akhir tersedia.

Tambahan failure gates lulus: actual SIGKILL mempertahankan 10 event durable dengan ID sama setelah restart dan ACK; real SQLITE_FULL page limit/recovery; actual ENOSPC Docker tmpfs 1 MiB/read-only container/no-network lalu space release dan recovery. Ini crash queue process, bukan power loss host. Queue unflushed buffer tidak diklaim durable. ENOSPC tidak mengisi filesystem proyek; bukti `b19-queue-enospc.log` dan agent race suite. Smoke restore dataset 250 event memverifikasi source/restore fingerprint dan memerlukan target DB kosong, checksum/private modes tetap berlaku.


Scope benchmark API: satu domain API dan satu published slug, country/device/OS/core rules synthetic, UA unik sebagian dan IPv4/IPv6, replay durable dan worker yang sesuai pusat. Bukan whole `control serve` dengan semua publisher/maintenance/DNS probe aktif atau simulasi banyak domain/rule kompleks. Local Go full memakai 32 slug, UA unik dan optional GeoLite, healthy receiver **stub** dengan unique ID accounting; tidak menggantikan uji GeoLite production/hosting atau ingest PostgreSQL end-to-end agent pada suite terpisah.


## Penutupan B19 lokal

[CAPACITY_B19.md](CAPACITY_B19.md) menjadi ringkasan final; bagian progress sebelumnya adalah riwayat diagnosis, bukan status terbaru. Backend/agent race, GeoLite cleanup/retensi, vet/build, dashboard build dan 69 kontrak API lulus. Dockerfile pusat diperbaiki untuk menyalin go.mod/source modul risk; image `shortlink-control:b19-local` berhasil dibangun dan executable smoke lulus tanpa jaringan. Image lama/Go v0.13.0-b13/PHP v0.17.0-b17 tidak di-republish.

Perintah pengulangan:

```sh
SHORTLINK_LOAD_RATE=300 bash scripts/test-capacity.sh full /private/results/full300.json
SHORTLINK_LOAD_RATE=50 SHORTLINK_LOAD_RESTORE=1 bash scripts/test-capacity.sh full /private/results/full50.json
SHORTLINK_LOAD_RATE=600 bash scripts/test-capacity.sh full /private/results/full600.json
SHORTLINK_LOAD_PATH=events SHORTLINK_LOAD_RATE=300 bash scripts/test-capacity.sh full /private/results/batch300.json
bash scripts/test-queue-enospc.sh
```

Untuk Go lokal, dari services/agent: `SHORTLINK_LOAD_MODE=full SHORTLINK_LOAD_RATE=300 SHORTLINK_LOCAL_LOAD_REPORT=/private/results/local300.json go test -count=1 -timeout 20m -run '^TestLocalCapacityHTTP$' -v ./internal/runtime` (600 memakai durasi lima menit). Default full lokal menggunakan healthy stub/upload queue; outage 10 detik diuji terpisah pada suite agent. Test ENOSPC membutuhkan existing image amd64 `shortlink-agent:v0.13.0-b13-amd64`; tidak menarik image baru atau mengisi disk production.

Simpan snapshot runner sebelum long test dan jangan mengubah script aktif. B20 berikutnya: review E2E/responsive/a11y, upgrade/rollback dan acceptance deployment yang masih terbuka. Jangan menandai release production siap sampai gate lingkungan nyata lulus.

## B19 — lingkungan publik mulai 11 Oktober 2026

Deployment pusat native ARM/aaPanel dan HTTPS sudah berjalan, browser login desktop/mobile lulus. Pengujian VPS ARM bersih dan PHP aaPanel/cPanel sedang berjalan; hasil serta gate yang belum selesai mengikuti [PUBLIC_TESTS_B19.md](PUBLIC_TESTS_B19.md). Ini melengkapi gate B19 nyata sebelum penutupan deployment/B20.

### Revisi status shortlink

Migrasi 016 menambah disabled/deleted_at/published_revision. Backup database,
binary pusat dan index dashboard disimpan di direktori private
`/var/backups/shortlink-production/link-lifecycle-*`. Clone migrasi telah diuji
sebelum production. Jangan rollback ke backend yang belum memahami disabled
atau deleted_at setelah fitur dipakai: backend lama dapat mengaktifkan kembali
link yang telah dimatikan/dihapus. Rollback dashboard dapat memakai index lama
beserta asetnya. Data tombol hapus link dipertahankan sebagai tombstone untuk
binding event yang terlambat; penghapusan domain tetap menghapus seluruh link
beserta tombstone dalam transaksi domain.
