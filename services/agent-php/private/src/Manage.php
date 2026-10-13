<?php
declare(strict_types=1);
namespace Shortlink;

function manageEscape(string $value): string { return htmlspecialchars($value,ENT_QUOTES|ENT_SUBSTITUTE,'UTF-8'); }
function manageCookie(string $value,bool $development,int $expires): void {
    setcookie('sl_agent_manage',$value,['expires'=>$expires,'path'=>'/__agent/manage','secure'=>!$development,'httponly'=>true,'samesite'=>'Strict']);
}
function manage(Client $client,array $config,bool $development,array $server): void {
    header('Content-Type: text/html; charset=utf-8');header('Referrer-Policy: no-referrer');
    $method=$server['REQUEST_METHOD'] ?? '';if(!in_array($method,['GET','POST'],true)){header('Allow: GET, POST');http_response_code(405);exit;}
    $session=$_COOKIE['sl_agent_manage'] ?? '';if(!is_string($session)||!validToken($session))$session='';
    $message='';$view=null;
    $call=static fn(string $path,array $body):array=>$client->call($path,$body,$config['credential'],3000,[],131072);
    if($method==='POST'){
        $expectedOrigin=($development?'http://':'https://').($server['HTTP_HOST'] ?? '');
        if(($server['HTTP_ORIGIN'] ?? '')!==$expectedOrigin){http_response_code(403);exit('Origin ditolak.');}
        if((int)($server['CONTENT_LENGTH'] ?? 0)>16384){http_response_code(413);exit;}
        foreach(['action','key','csrf','openingUrl','expectedRevision'] as $field)if(isset($_POST[$field])&&!is_string($_POST[$field])){http_response_code(400);exit;}
        $action=$_POST['action'] ?? '';
        if($action==='login'){
            $r=$call('/v1/agents/manage/exchange',['key'=>$_POST['key'] ?? '']);$grant=$r['body'];
            $expires=is_array($grant)?strtotime($grant['expiresAt'] ?? ''):false;
            if($r['status']===200&&validToken($grant['session'] ?? null)&&$expires!==false&&$expires>time()){
                manageCookie($grant['session'],$development,min($expires,time()+900));header('Location: /__agent/manage',true,303);exit;
            }
            http_response_code($r['status']>=500||$r['status']===0?503:401);$message='Key tidak valid, kedaluwarsa, atau pusat belum tersedia.';
        }else{
            $body=['session'=>$session,'csrf'=>$_POST['csrf'] ?? '','action'=>$action];
            if($action==='update'){
                $revision=filter_var($_POST['expectedRevision'] ?? '',FILTER_VALIDATE_INT);
                if($revision===false||$revision<0){http_response_code(400);exit('Revision tidak valid.');}
                $body['expectedRevision']=$revision;$body['openingUrl']=$_POST['openingUrl'] ?? '';
            }
            $r=$call('/v1/agents/manage/session',$body);
            if($r['status']===200){if($action==='logout')manageCookie('',$development,1);header('Location: /__agent/manage',true,303);exit;}
            if($r['status']===403){http_response_code(403);exit('Akses ditolak.');}
            $message='Perubahan belum tersimpan. Muat ulang jika revision berubah; periksa URL dan koneksi pusat.';
            if($r['status']>=500||$r['status']===0)http_response_code(503);
        }
    }
    if($session!==''){
        $after=$_GET['after'] ?? '';if(!is_string($after)||strlen($after)>128){http_response_code(400);exit;}
        $r=$call('/v1/agents/manage/session',['action'=>'view','session'=>$session,'after'=>$after]);$data=$r['body'];
        if($r['status']===200&&is_array($data)&&($data['domain']['hostname'] ?? '')===$config['hostname']&&is_array($data['links'] ?? null)&&validToken($data['csrf'] ?? null))$view=$data;
        elseif($r['status']===401){manageCookie('',$development,1);$message='Sesi berakhir. Buat key baru dari pusat.';}
        else{http_response_code(503);$message='Pusat belum tersedia atau data tidak valid.';}
    }
    $base=($development?'http://':'https://').($server['HTTP_HOST'] ?? $config['hostname']);
    ?>
<!doctype html><html lang="id"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Manage agent</title><link rel="stylesheet" href="/setup.css"></head><body><main><h1>Manage agent</h1>
<?php if($message!==''):?><p role="alert"><?=manageEscape($message)?></p><?php endif ?>
<?php if($view===null):?><p>Buat key sekali pakai dari detail domain di pusat. Key berlaku lima menit.</p><form method="post" action="/__agent/manage"><input type="hidden" name="action" value="login"><label>Key akses manage<input type="password" name="key" required maxlength="43" autocomplete="off"></label><button>Masuk</button></form>
<?php else:$domain=$view['domain'];?><p>Domain: <?=manageEscape($domain['hostname'])?> · revision <?=(int)$domain['desiredRevision']?></p>
<form method="post" action="/__agent/manage"><input type="hidden" name="action" value="update"><input type="hidden" name="csrf" value="<?=manageEscape($view['csrf'])?>"><input type="hidden" name="expectedRevision" value="<?=(int)$domain['desiredRevision']?>"><label>URL pembuka domain<input type="url" name="openingUrl" maxlength="4096" value="<?=manageEscape($domain['openingUrl'] ?? '')?>"></label><p>Root dan slug tidak ditemukan diarahkan ke sini. Kosong berarti 503.</p><button>Simpan URL pembuka</button></form>
<h2>Shortlink domain ini</h2><?php foreach($view['links'] as $link):?><article><h3><?php if($link['state']==='published'):?><a href="<?=manageEscape($base.'/'.$link['slug'])?>" target="_blank" rel="noopener noreferrer"><?=manageEscape($link['slug'])?></a><?php else:?><?=manageEscape($link['slug'])?><?php endif?></h3><p><?=manageEscape($link['destination'])?></p><p>Fallback: <?=manageEscape($link['routing']['fallback'] ?? 'Belum diatur')?></p><p><?=manageEscape($link['state'])?> · <?=(int)$link['clicks']?> redirect</p></article><?php endforeach ?>
<?php if(($view['nextCursor'] ?? '')!==''):?><a href="/__agent/manage?after=<?=rawurlencode($view['nextCursor'])?>">Halaman berikutnya</a><?php endif?><p><a href="/__agent/manage">Muat ulang</a></p><form method="post" action="/__agent/manage"><input type="hidden" name="action" value="logout"><input type="hidden" name="csrf" value="<?=manageEscape($view['csrf'])?>"><button>Keluar</button></form>
<?php endif?></main></body></html>
<?php }
