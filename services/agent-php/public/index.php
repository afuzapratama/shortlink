<?php
declare(strict_types=1);
ini_set('display_errors','0');
header('Cache-Control: no-store');
header('X-Content-Type-Options: nosniff');
header('Referrer-Policy: same-origin');
header("Content-Security-Policy: default-src 'none'; style-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'");
$private=getenv('SHORTLINK_PRIVATE_DIR') ?: dirname(__DIR__).'/private';
if (!is_file($private.'/src/Adapter.php')) { http_response_code(503); exit('Agent belum dikonfigurasi.'); }
require $private.'/src/Adapter.php';
try {
    $private=\Shortlink\safePrivate($private);
    $bootstrap=\Shortlink\bootstrap($private);
    $dev=($bootstrap['developmentLoopback'] ?? false)===true;
    // HTTPS must be terminated locally, or the exact immediate proxy must be explicitly trusted.
    $tls=(!empty($_SERVER['HTTPS']) && $_SERVER['HTTPS']!=='off') || ((int)($_SERVER['SERVER_PORT'] ?? 0)===443);
    if(in_array($_SERVER['REMOTE_ADDR'] ?? '',$bootstrap['trustedProxies'] ?? [],true) && ($_SERVER['HTTP_X_FORWARDED_PROTO'] ?? '')==='https') $tls=true;
    if(!$tls && !($dev && in_array($_SERVER['REMOTE_ADDR'] ?? '',['127.0.0.1','::1'],true))) throw new \Shortlink\Failure('HTTPS wajib untuk domain agent.');
    $path=parse_url($_SERVER['REQUEST_URI'] ?? '/',PHP_URL_PATH);
    if(!is_string($path)) { http_response_code(400); exit('Request tidak valid.'); }
    if($path==='/__agent/preflight') {
        if($_SERVER['REQUEST_METHOD']!=='GET' || !is_file($private.'/routing.json')) { http_response_code(404); exit; }
        $challenge=\Shortlink\readJson($private.'/routing.json'); $nonce=$_GET['nonce'] ?? '';
        if(!\Shortlink\validToken($nonce) || ($challenge['expires'] ?? 0)<time() || !hash_equals($challenge['nonce'],$nonce)) { http_response_code(404); exit; }
        header('Content-Type: application/json'); echo json_encode(['proof'=>hash('sha256',$nonce)]); exit;
    }
    if($path==='/__agent/setup') { require $private.'/src/Setup.php'; exit; }
    if(!is_file($private.'/config.json')) { http_response_code(503); exit('Agent belum diaktifkan.'); }
    $config=\Shortlink\readJson($private.'/config.json');
    if(($config['hostname'] ?? '')!==$bootstrap['hostname'] || ($config['controlPlaneUrl'] ?? '')!==$bootstrap['controlPlaneUrl'] || !\Shortlink\validToken($config['credential'] ?? null)) throw new \Shortlink\Failure('Konfigurasi aktif tidak cocok.');
    $host=parse_url('http://'.($_SERVER['HTTP_HOST'] ?? ''),PHP_URL_HOST);if(!is_string($host)||strtolower(rtrim($host,'.'))!==$config['hostname']){http_response_code(421);exit;}
    $client=new \Shortlink\Client($config['controlPlaneUrl'],$dev);
    if($path==='/__agent/manage'){require $private.'/src/Manage.php';\Shortlink\manage($client,$config,$dev,$_SERVER);exit;}
    if($path==='/.well-known/shortlink-ready') {
        $nonce=$_SERVER['HTTP_X_SHORTLINK_NONCE'] ?? ''; $proof=$_SERVER['HTTP_X_SHORTLINK_PROOF'] ?? '';
        if($_SERVER['REQUEST_METHOD']!=='GET' || !\Shortlink\validToken($nonce) || !hash_equals(\Shortlink\mac($config['credential'],'request',$nonce),$proof)) { http_response_code(404); exit; }
        $r=\Shortlink\probe($client,$config,$nonce);
        header('Content-Type: application/json'); header('X-Shortlink-Proof: '.$r['headers']['x-shortlink-proof']); echo $r['raw']; exit;
    }
    if(!in_array($_SERVER['REQUEST_METHOD'],['GET','HEAD'],true)) { header('Allow: GET, HEAD'); http_response_code(405); exit; }
    if($path==='/__agent'||str_starts_with($path,'/__agent/')||str_starts_with($path,'/.well-known/')||str_starts_with($path,'/health/')){http_response_code(404);exit;}
    $valid=preg_match('#^/([A-Za-z0-9_-]{1,80})$#D',$path,$m)===1;
    if(!$valid){$result=\Shortlink\opening($client,$config);}
    elseif($_SERVER['REQUEST_METHOD']==='HEAD'){$result=\Shortlink\opening($client,$config,$m[1]);}
    else{$result=\Shortlink\decision($client,$config,$m[1],$_SERVER);if($result['status']===404)$result=\Shortlink\opening($client,$config);}
    http_response_code($result['status']);
    if(isset($result['location'])) header('Location: '.$result['location']);
    else echo match($result['status']) {403=>'Akses ditolak.',404=>'Link tidak ditemukan.',default=>'Layanan sementara tidak tersedia.'};
} catch (\Throwable $e) { http_response_code(503); echo 'Agent sementara tidak tersedia. Periksa konfigurasi melalui pusat atau operator hosting.'; }
