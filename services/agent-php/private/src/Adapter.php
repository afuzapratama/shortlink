<?php
declare(strict_types=1);
namespace Shortlink;

const VERSION = 'v0.17.0-b17';
final class Failure extends \RuntimeException {}
function token(): string { return rtrim(strtr(base64_encode(random_bytes(32)), '+/', '-_'), '='); }
function validToken(mixed $value): bool {
    if (!is_string($value) || !preg_match('/^[A-Za-z0-9_-]{43}$/D', $value)) return false;
    return rtrim(strtr(base64_encode(base64_decode(strtr($value, '-_', '+/'), true)), '+/', '-_'), '=') === $value;
}
function mac(string $credential, string $purpose, string $payload): string {
    return hash_hmac('sha256', 'shortlink-probe-v1:'.$purpose.':'.$payload, hash('sha256', $credential));
}
function readJson(string $file): array {
    $raw = @file_get_contents($file);
    if ($raw === false || strlen($raw) > 65536) throw new Failure('Konfigurasi private belum tersedia atau tidak valid.');
    try { $data = json_decode($raw, true, 32, JSON_THROW_ON_ERROR); } catch (\Throwable $e) { throw new Failure('Konfigurasi private tidak valid.'); }
    if (!is_array($data)) throw new Failure('Konfigurasi private tidak valid.');
    return $data;
}
function atomicJson(string $file, array $data): void {
    $tmp = dirname($file).'/.write-'.token();
    $handle = @fopen($tmp, 'xb');
    if ($handle === false) throw new Failure('Direktori private tidak dapat ditulis.');
    try {
        if (!chmod($tmp, 0600)) throw new Failure('Permission private gagal.');
        $raw = json_encode($data, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES)."\n";
        if (fwrite($handle, $raw) !== strlen($raw) || !fflush($handle) || !fsync($handle)) throw new Failure('Penyimpanan private gagal.');
        fclose($handle); $handle = null;
        if (!rename($tmp, $file)) throw new Failure('Aktivasi file private gagal.');
    } finally { if (is_resource($handle)) fclose($handle); if (is_file($tmp)) @unlink($tmp); }
}
function inside(string $path, string $root): bool { return $path === $root || str_starts_with($path, rtrim($root, DIRECTORY_SEPARATOR).DIRECTORY_SEPARATOR); }
function safePrivate(string $dir): string {
    $private = realpath($dir); $web = realpath($_SERVER['DOCUMENT_ROOT'] ?? '');
    if (!$private || !$web || inside($private, $web) || inside((string)realpath(__DIR__), $web) || !is_writable($private)) throw new Failure('Arahkan document root ke public dan simpan private di luar web root.');
    // The operator must keep all private paths outside every vhost/alias served by this account.
    if ((fileperms($private) & 0007) !== 0) throw new Failure('Permission direktori private harus 0700 atau 0750.');
    return $private;
}
function url(string $value, bool $development): string {
    $p = parse_url($value);
    if (!$p || !isset($p['scheme'],$p['host']) || isset($p['user'],$p['pass']) || isset($p['query']) || isset($p['fragment']) || !in_array($p['path'] ?? '', ['', '/'], true)) throw new Failure('URL harus berupa origin pusat/domain tanpa path atau credential.');
    $local = in_array(strtolower($p['host']), ['127.0.0.1','localhost','[::1]'], true);
    if ($p['scheme'] !== 'https' && !($development && $local && $p['scheme'] === 'http')) throw new Failure('HTTPS wajib; HTTP hanya untuk loopback development yang disetel operator.');
    return rtrim($value, '/');
}
function bootstrap(string $private): array {
    $b = readJson($private.'/bootstrap.json');
    $dev = ($b['developmentLoopback'] ?? false) === true;
    $b['controlPlaneUrl'] = url((string)($b['controlPlaneUrl'] ?? ''), $dev);
    $b['publicUrl'] = url((string)($b['publicUrl'] ?? ''), $dev);
    $host = strtolower((string)($b['hostname'] ?? ''));
    if (!preg_match('/^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$/D',$host) || str_contains($host,'..')) throw new Failure('Hostname bootstrap tidak valid.');
    if (strtolower(parse_url($b['publicUrl'],PHP_URL_HOST)) !== $host) throw new Failure('Hostname dan URL publik harus sama.');
    $b['hostname'] = $host;
    $incoming = strtolower((string)parse_url('http://'.($_SERVER['HTTP_HOST'] ?? ''), PHP_URL_HOST));
    if ($incoming !== $host) throw new Failure('Hostname request tidak sesuai bootstrap.');
    if (!is_array($b['trustedProxies'] ?? [])) throw new Failure('Trusted proxy tidak valid.');
    foreach ($b['trustedProxies'] ?? [] as $ip) if (!is_string($ip) || !filter_var($ip,FILTER_VALIDATE_IP)) throw new Failure('Trusted proxy harus berupa IP exact.');
    return $b;
}
final class Client {
    public function __construct(private string $origin, private bool $development = false) { $this->origin = url($origin, $development); }
    public function call(string $path, ?array $body = null, ?string $credential = null, int $timeout = 3000, array $extraHeaders = []): array {
        if (!str_starts_with($path,'/') || str_starts_with($path,'//') || preg_match('/[\r\n]/',$path)) throw new Failure('Endpoint tidak valid.');
        $raw = ''; $headers = []; $overflow = false;
        $c = curl_init($this->origin.$path);
        $requestHeaders = ['Accept: application/json', 'Content-Type: application/json', ...$extraHeaders];
        if ($credential !== null) $requestHeaders[] = 'Authorization: Bearer '.$credential;
        curl_setopt_array($c, [CURLOPT_FOLLOWLOCATION=>false, CURLOPT_MAXREDIRS=>0, CURLOPT_CONNECTTIMEOUT_MS=>min(1000,$timeout), CURLOPT_TIMEOUT_MS=>$timeout, CURLOPT_SSL_VERIFYPEER=>true, CURLOPT_SSL_VERIFYHOST=>2, CURLOPT_PROTOCOLS=>CURLPROTO_HTTPS | ($this->development ? CURLPROTO_HTTP : 0), CURLOPT_PROXY=>'', CURLOPT_HTTPHEADER=>$requestHeaders, CURLOPT_WRITEFUNCTION=>static function($c,$chunk) use (&$raw,&$overflow) { if (strlen($raw)+strlen($chunk)>32768) { $overflow=true; return 0; } $raw.=$chunk; return strlen($chunk); }, CURLOPT_HEADERFUNCTION=>static function($c,$line) use (&$headers) { $pair=explode(':',$line,2); if(count($pair)===2) $headers[strtolower(trim($pair[0]))]=trim($pair[1]); return strlen($line); }]);
        if ($body !== null) { curl_setopt($c,CURLOPT_POST,true); curl_setopt($c,CURLOPT_POSTFIELDS,json_encode($body,JSON_THROW_ON_ERROR|JSON_UNESCAPED_SLASHES)); }
        $ok=curl_exec($c); $status=(int)curl_getinfo($c,CURLINFO_RESPONSE_CODE);
        if ($ok === false || $overflow) return ['status'=>0,'body'=>null,'raw'=>'','headers'=>[]];
        try { $data=json_decode($raw,true,32,JSON_THROW_ON_ERROR); } catch (\Throwable $e) { $data=null; }
        return ['status'=>$status,'body'=>is_array($data)?$data:null,'raw'=>$raw,'headers'=>$headers];
    }
}
function visitorIP(array $server, array $trusted): string {
    $remote = $server['REMOTE_ADDR'] ?? '';
    if (!filter_var($remote,FILTER_VALIDATE_IP)) throw new Failure('IP pengunjung tidak valid.');
    // Only explicitly listed immediate proxies may supply X-Forwarded-For. Walk right to left.
    if (!in_array($remote,$trusted,true)) return $remote;
    $raw=$server['HTTP_X_FORWARDED_FOR'] ?? '';
    if ($raw === '') return $remote;
    if (strlen($raw)>1024) throw new Failure('Forwarded IP tidak valid.');
    $chain=array_map('trim',explode(',',$raw)); if(count($chain)>16) throw new Failure('Forwarded IP tidak valid.');
    foreach($chain as $ip) if(!filter_var($ip,FILTER_VALIDATE_IP)) throw new Failure('Forwarded IP tidak valid.');
    for($i=count($chain)-1;$i>=0;$i--) { if(!in_array($remote,$trusted,true)) break; $remote=$chain[$i]; }
    return $remote;
}
function decision(Client $client, array $config, string $slug, array $server): array {
    $ua=$server['HTTP_USER_AGENT'] ?? ''; if(strlen($ua)>4096) throw new Failure('User-Agent terlalu panjang.');
    $id=token(); $body=['eventId'=>$id,'occurredAt'=>gmdate('Y-m-d\TH:i:s\Z'),'domainId'=>$config['domainId'],'slug'=>$slug,'signals'=>['ip'=>visitorIP($server,$config['trustedProxies'] ?? []),'userAgent'=>$ua]];
    $end=microtime(true)+3;
    $response=$client->call('/v1/decisions',$body,$config['credential'],1800);
    if (($response['status']===0 || $response['status']>=500) && ($left=(int)(($end-microtime(true))*1000))>=150) $response=$client->call('/v1/decisions',$body,$config['credential'],$left);
    if($response['status']===404) return ['status'=>404];
    $d=$response['body'];
    if($response['status']!==200 || !is_array($d) || ($d['eventId'] ?? '')!==$id || !is_int($d['configRevision'] ?? null) || $d['configRevision']<1) return ['status'=>503];
    if(($d['action'] ?? '')==='block' && ($d['destination'] ?? null)===null) return ['status'=>403];
    if(!in_array($d['action'] ?? '',['redirect','fallback'],true) || !is_string($d['destination'] ?? null)) return ['status'=>503];
    $target=$d['destination']; $p=parse_url($target);
    if(!$p || !isset($p['host']) || !in_array($p['scheme'] ?? '',['http','https'],true) || isset($p['user']) || isset($p['pass']) || preg_match('/[\x00-\x20\x7f]/',$target) || strlen($target)>8192) return ['status'=>503];
    return ['status'=>302,'location'=>$target];
}
function probe(Client $client, array $config, string $nonce): array {
    $r=$client->call('/v1/agents/probe',['domainId'=>$config['domainId'],'hostname'=>$config['hostname'],'nonce'=>$nonce,'version'=>VERSION],$config['credential']);
    $b=$r['body']; $proof=$r['headers']['x-shortlink-proof'] ?? '';
    if($r['status']!==200 || !is_array($b) || ($b['nonce'] ?? '')!==$nonce || ($b['domainId'] ?? '')!==$config['domainId'] || ($b['ready'] ?? false)!==true || !is_int($b['revision'] ?? null) || $b['revision']<1 || !is_string($b['geoRelease'] ?? null) || !hash_equals(mac($config['credential'],'response',$r['raw']),$proof)) throw new Failure('Probe pusat belum siap. Periksa GeoLite, konfigurasi dan credential di pusat.');
    return $r;
}
function preflight(string $private, array $b): void {
    if(PHP_VERSION_ID<80200 || !extension_loaded('curl') || !extension_loaded('openssl') || !function_exists('fsync')) throw new Failure('Memerlukan PHP 8.2+, cURL, OpenSSL, session dan fsync.');
    $test=$private.'/.preflight-'.token(); atomicJson($test,['ok'=>true]); if(!unlink($test)) throw new Failure('Private path gagal.');
    $client=new Client($b['controlPlaneUrl'],($b['developmentLoopback'] ?? false)===true);
    $health=$client->call('/health/ready'); if($health['status']!==200 || ($health['body']['ok'] ?? false)!==true) throw new Failure('Pusat belum siap atau koneksi HTTPS gagal.');
    $challenge=token(); atomicJson($private.'/routing.json',['nonce'=>$challenge,'expires'=>time()+30]);
    try {
        $self=new Client($b['publicUrl'],($b['developmentLoopback'] ?? false)===true);
        $r=$self->call('/__agent/preflight?nonce='.$challenge);
        if($r['status']!==200 || ($r['body']['proof'] ?? '')!==hash('sha256',$challenge)) throw new Failure('Rewrite/routing publik gagal. Periksa document root, HTTPS dan aturan web server.');
    } finally { @unlink($private.'/routing.json'); }
}
function activate(string $private, array $b, string $installToken): array {
    if(!validToken($installToken)) throw new Failure('Token instalasi tidak valid.');
    $client=new Client($b['controlPlaneUrl'],($b['developmentLoopback'] ?? false)===true);
    $pendingFile=$private.'/pending.json';
    if(is_file($pendingFile)) {
        $pending=readJson($pendingFile);
        if(!hash_equals(hash('sha256',(string)$pending['token']),hash('sha256',$installToken)) || ($pending['controlPlaneUrl'] ?? '')!==$b['controlPlaneUrl'] || ($pending['hostname'] ?? '')!==$b['hostname']) throw new Failure('Instalasi tertunda terikat token/alamat sebelumnya. Pulihkan token yang sama atau revoke melalui pusat.');
    } else {
        $r=$client->call('/v1/agents/install-context',['token'=>$installToken]); $context=$r['body'];
        if($r['status']!==200 || !validToken($context['installationId'] ?? null)) throw new Failure('Token salah, kedaluwarsa, terpakai atau pusat tidak tersedia. Buat token PHP di pusat lalu coba lagi.');
        $boot=$context['bootstrap'] ?? [];
        if(($boot['hostname'] ?? '')!==$b['hostname'] || ($boot['method'] ?? '')!=='php_zip' || ($boot['controlPlaneUrl'] ?? '')!==$b['controlPlaneUrl'] || !is_string($boot['domainId'] ?? null)) throw new Failure('Binding domain, mode atau alamat pusat tidak cocok.');
        $pending=['token'=>$installToken,'installationId'=>$context['installationId'],'installationProof'=>token(),'hostname'=>$b['hostname'],'controlPlaneUrl'=>$b['controlPlaneUrl'],'domainId'=>$boot['domainId']];
        // Write the same recovery proof before the one-time token can be consumed.
        atomicJson($pendingFile,$pending);
    }
    $r=$client->call('/v1/agents/enroll',['token'=>$pending['token'],'installationId'=>$pending['installationId'],'installationProof'=>$pending['installationProof'],'hostname'=>$b['hostname'],'method'=>'php_zip','version'=>VERSION]);
    $enrolled=$r['body'];
    if($r['status']!==200 || !validToken($enrolled['credential'] ?? null) || !validToken($enrolled['agentId'] ?? null) || ($enrolled['bootstrap']['domainId'] ?? '')!==$pending['domainId'] || ($enrolled['bootstrap']['hostname'] ?? '')!==$b['hostname'] || ($enrolled['bootstrap']['method'] ?? '')!=='php_zip' || ($enrolled['bootstrap']['controlPlaneUrl'] ?? '')!==$b['controlPlaneUrl']) throw new Failure('Aktivasi belum selesai. Coba lagi dengan token yang sama; bila jendela pemulihan habis, revoke dan buat token baru di pusat.');
    $config=['domainId'=>$pending['domainId'],'agentId'=>$enrolled['agentId'],'credential'=>$enrolled['credential'],'hostname'=>$b['hostname'],'controlPlaneUrl'=>$b['controlPlaneUrl'],'trustedProxies'=>$b['trustedProxies'] ?? [],'version'=>VERSION];
    // Config is the single activation/lock marker. Once present no enrollment is allowed again.
    probe($client,$config,token());
    atomicJson($private.'/config.json',$config);
    @unlink($pendingFile);
    return $config;
}
