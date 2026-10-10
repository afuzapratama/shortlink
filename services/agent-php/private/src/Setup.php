<?php
declare(strict_types=1);
// Included after bootstrap, HTTPS, Host and private-path checks.
header('Content-Type: text/html; charset=utf-8');
function escape(string $s): string { return htmlspecialchars($s,ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8'); }
$installed=is_file($private.'/config.json');
$step='connect'; $message=''; $csrf='';
if(!$installed) {
    $sessions=$private.'/sessions';
    if(!is_dir($sessions) && !@mkdir($sessions,0700)) throw new \Shortlink\Failure('Session private tidak tersedia.');
    if(is_link($sessions) || (fileperms($sessions)&0007)!==0 || !is_writable($sessions)) throw new \Shortlink\Failure('Session private tidak aman.');
    session_save_path($sessions); session_name('shortlink_setup');
    ini_set('session.use_strict_mode','1'); ini_set('session.use_only_cookies','1'); ini_set('session.use_trans_sid','0');
    ini_set('session.gc_maxlifetime','1800');
    session_set_cookie_params(['lifetime'=>0,'path'=>'/__agent','secure'=>!$dev,'httponly'=>true,'samesite'=>'Strict']);
    if(!session_start()) throw new \Shortlink\Failure('Session tidak tersedia.');
    $_SESSION['csrf'] ??= \Shortlink\token(); $csrf=$_SESSION['csrf'];
    if(($_SESSION['validatedUntil'] ?? 0)>time()) $step='activate'; else unset($_SESSION['installToken']);
    if($_SERVER['REQUEST_METHOD']==='POST') {
        if(($_SERVER['HTTP_ORIGIN'] ?? '')!==$bootstrap['publicUrl'] || !hash_equals($csrf,(string)($_POST['csrf'] ?? ''))) { http_response_code(403); $message='Sesi atau origin tidak valid. Muat ulang halaman setup.'; }
        elseif((int)($_SERVER['CONTENT_LENGTH'] ?? 0)>8192) { http_response_code(413); $message='Request terlalu besar.'; }
        else {
            $lock=@fopen($private.'/setup.lock','c');
            if(!$lock || !chmod($private.'/setup.lock',0600) || !flock($lock,LOCK_EX|LOCK_NB)) { http_response_code(409); $message='Instalasi sedang berjalan di tab lain. Coba lagi.'; }
            else try {
                if(is_file($private.'/config.json')) { $installed=true; }
                else {
                    $limitFile=$private.'/setup-rate.json'; $now=time();
                    $limits=is_file($limitFile)?\Shortlink\readJson($limitFile):['start'=>$now,'count'=>0,'ips'=>[]];
                    if($now-(int)$limits['start']>=60) $limits=['start'=>$now,'count'=>0,'ips'=>[]];
                    $ip=hash('sha256',(string)($_SERVER['REMOTE_ADDR'] ?? ''));
                    if($limits['count']>=60 || ($limits['ips'][$ip] ?? 0)>=10) { http_response_code(429); throw new \Shortlink\Failure('Terlalu banyak percobaan. Tunggu satu menit lalu coba lagi.'); }
                    $limits['count']++; $limits['ips'][$ip]=($limits['ips'][$ip] ?? 0)+1; \Shortlink\atomicJson($limitFile,$limits);
                    $action=$_POST['action'] ?? '';
                    if($action==='check') {
                        $t=(string)($_POST['token'] ?? ''); if(!\Shortlink\validToken($t)) throw new \Shortlink\Failure('Token instalasi tidak valid.');
                        \Shortlink\preflight($private,$bootstrap);
                        $client=new \Shortlink\Client($bootstrap['controlPlaneUrl'],$dev);
                        $r=$client->call('/v1/agents/install-context',['token'=>$t]); $boot=$r['body']['bootstrap'] ?? [];
                        if($r['status']!==200 || ($boot['hostname'] ?? '')!==$bootstrap['hostname'] || ($boot['method'] ?? '')!=='php_zip' || ($boot['controlPlaneUrl'] ?? '')!==$bootstrap['controlPlaneUrl']) throw new \Shortlink\Failure('Token salah, kedaluwarsa atau binding domain/mode/pusat tidak cocok. Periksa token PHP melalui pusat.');
                        session_regenerate_id(true); $_SESSION['installToken']=$t; $_SESSION['validatedUntil']=time()+600; $_SESSION['csrf']=\Shortlink\token(); $csrf=$_SESSION['csrf']; $step='activate';
                        $message='PHP, private path, koneksi pusat dan routing lulus. Token belum ditukar menjadi credential.';
                    } elseif($action==='activate' && $step==='activate' && isset($_SESSION['installToken'])) {
                        // Repeat preflight immediately before consumption; pending proof is reused after interruptions.
                        \Shortlink\preflight($private,$bootstrap);
                        \Shortlink\activate($private,$bootstrap,$_SESSION['installToken']);
                        $installed=true; $_SESSION=[]; session_destroy();
                        setcookie('shortlink_setup','',['expires'=>time()-3600,'path'=>'/__agent','secure'=>!$dev,'httponly'=>true,'samesite'=>'Strict']);
                    } else throw new \Shortlink\Failure('Sesi aktivasi habis. Masukkan token yang sama untuk pemulihan.');
                }
            } catch(\Shortlink\Failure $e) { $message=$e->getMessage(); }
            finally { flock($lock,LOCK_UN); fclose($lock); }
        }
    } elseif($_SERVER['REQUEST_METHOD']!=='GET') { header('Allow: GET, POST'); http_response_code(405); exit; }
    if(session_status()===PHP_SESSION_ACTIVE) session_write_close();
} else {
    // No session or token form remains once the atomic config marker exists.
    if($_SERVER['REQUEST_METHOD']!=='GET') http_response_code(423);
}
?>
<!doctype html><html lang="id"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Instalasi agent shortlink</title><link rel="stylesheet" href="/setup.css"></head><body><main>
<div class="brand">SHORTLINK · AGENT PHP</div><h1><?= $installed?'Agent sudah terpasang':'Hubungkan domain ke pusat' ?></h1>
<p class="intro"><?= $installed?'Setup terkunci. Kelola link dan aturan melalui dashboard pusat.':'Pasang adapter tanpa Docker. GeoLite dan aturan diproses oleh server pusat.' ?></p>
<div class="domain"><?= escape($bootstrap['hostname']) ?><span>Mode API · <?= escape(\Shortlink\VERSION) ?></span></div>
<?php if($message): ?><div class="notice" role="status"><?= escape($message) ?></div><?php endif ?>
<?php if($installed): ?><section><h2>Terhubung ke pusat</h2><p>Konfigurasi tersimpan di direktori private. Periksa kesiapan publik di detail domain pusat sebelum menerbitkan link.</p><a class="button" href="<?= escape($bootstrap['controlPlaneUrl']) ?>">Buka pusat</a></section>
<?php else: ?><ol class="steps"><li class="active">1 · Hubungkan</li><li class="<?= $step==='activate'?'active':'' ?>">2 · Aktifkan</li><li>3 · Selesai</li></ol>
<form method="post" action="/__agent/setup"><input type="hidden" name="csrf" value="<?= escape($csrf) ?>">
<?php if($step==='connect'): ?><label for="token">Token instalasi PHP</label><input id="token" name="token" type="password" autocomplete="off" required minlength="43" maxlength="43" spellcheck="false"><p class="help">Buat token sekali pakai dari domain mode API yang sudah diverifikasi di pusat.</p><button name="action" value="check">Periksa hosting dan koneksi</button>
<?php else: ?><h2>Siap untuk aktivasi</h2><p>Aktivasi menukar token, menyimpan credential secara atomik, menguji API pusat dan mengunci setup.</p><button name="action" value="activate">Aktifkan agent</button><?php endif ?></form>
<details><summary>Konfigurasi dan pemulihan</summary><p>Pusat: <?= escape($bootstrap['controlPlaneUrl']) ?></p><p>Alamat pusat dan public/private path diatur operator pada file bootstrap di luar web root. Untuk pemulihan respons terputus, gunakan token yang sama; pusat menyediakan jendela pemulihan 10 menit. Jika habis, revoke agent melalui pusat dan minta token baru.</p></details>
<?php endif ?><footer>Data GeoLite berada di pusat · Tidak membutuhkan database lokal</footer>
</main></body></html>
