<?php
declare(strict_types=1);
require dirname(__DIR__).'/private/src/Adapter.php';
function check(bool $ok):void {if(!$ok)throw new RuntimeException('PHP invariant failed');}
function fails(callable $f):void {try{$f();}catch(\Shortlink\Failure $e){return;}throw new RuntimeException('Expected rejection');}
check(\Shortlink\validToken(\Shortlink\token()));check(!\Shortlink\validToken(str_repeat('a',43)));check(!\Shortlink\validToken('invalid'));
check(\Shortlink\visitorIP(['REMOTE_ADDR'=>'8.8.8.8','HTTP_X_FORWARDED_FOR'=>'1.1.1.1'],[])==='8.8.8.8');
check(\Shortlink\visitorIP(['REMOTE_ADDR'=>'127.0.0.1','HTTP_X_FORWARDED_FOR'=>'1.1.1.1, 8.8.8.8'],['127.0.0.1'])==='8.8.8.8');
check(\Shortlink\visitorIP(['REMOTE_ADDR'=>'127.0.0.1','HTTP_X_FORWARDED_FOR'=>'1.1.1.1, 8.8.8.8'],['127.0.0.1','8.8.8.8'])==='1.1.1.1');
check(\Shortlink\visitorIP(['REMOTE_ADDR'=>'::1','HTTP_X_FORWARDED_FOR'=>'2001:4860:4860::8888'],['::1'])==='2001:4860:4860::8888');
fails(fn()=>\Shortlink\visitorIP(['REMOTE_ADDR'=>'127.0.0.1','HTTP_X_FORWARDED_FOR'=>'bad'],['127.0.0.1']));
fails(fn()=>\Shortlink\url('http://example.org',true));fails(fn()=>\Shortlink\url('http://127.0.0.1',false));fails(fn()=>\Shortlink\url('https://user:pass@example.org',false));fails(fn()=>\Shortlink\url('https://example.org/path',false));
check(\Shortlink\url('https://example.org/',false)==='https://example.org');
check(\Shortlink\inside('/var/www/public/private','/var/www/public'));check(!\Shortlink\inside('/var/www/public-private','/var/www/public'));
$dir=sys_get_temp_dir().'/shortlink-php-unit-'.\Shortlink\token();mkdir($dir,0700);
try {
 \Shortlink\atomicJson($dir.'/state.json',['proof'=>'secret','version'=>1]);check(\Shortlink\readJson($dir.'/state.json')['version']===1);check((fileperms($dir.'/state.json')&0777)===0600);
 \Shortlink\atomicJson($dir.'/state.json',['version'=>2]);check(\Shortlink\readJson($dir.'/state.json')['version']===2);
 $_SERVER['DOCUMENT_ROOT']=$dir;fails(fn()=>\Shortlink\safePrivate($dir));
}finally{unlink($dir.'/state.json');rmdir($dir);}
echo "PHP unit checks passed: URL trust, IPv4/IPv6 proxy chain, canonical tokens, private path and atomic permissions.\n";
