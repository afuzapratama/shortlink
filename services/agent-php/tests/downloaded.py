#!/usr/bin/env python3
"""Downloaded signed ZIP -> PHP HTTP wizard -> actual Go decisions. Isolated Go fixture only."""
import base64,hashlib,http.cookiejar,json,os,pathlib,re,signal,socket,subprocess,sys,tempfile,time,urllib.request,urllib.error,urllib.parse,zipfile
config=json.load(sys.stdin);root=pathlib.Path(__file__).resolve().parents[3]
def central(path):
 with urllib.request.urlopen(urllib.request.Request(config['origin']+path,headers={'Cookie':config['cookie']}),timeout=8) as r:return r.read()
manifest=central('/v1/assets/php/manifest');outer=json.loads(manifest)
with socket.socket() as s:s.bind(('127.0.0.1',0));port=s.getsockname()[1]
origin=f'http://localhost:{port}';jar=http.cookiejar.CookieJar()
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a):return None
client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar),NoRedirect())
def request(path,form=None,headers=None):
 h=headers or {}
 if form is not None:h={'Origin':origin,**h}
 r=urllib.request.Request(origin+path,data=None if form is None else urllib.parse.urlencode(form).encode(),headers=h)
 try:x=client.open(r,timeout=10)
 except urllib.error.HTTPError as e:x=e
 return x.code,x.read().decode(),dict(x.headers)
with tempfile.TemporaryDirectory(prefix='php-downloaded-') as temp:
 work=pathlib.Path(temp);(work/'manifest.json').write_bytes(manifest)
 # Trust the public key passed by the Go fixture, not the downloaded ZIP.
 payload=json.loads(base64.urlsafe_b64decode(outer['signedPayload']+'='*((-len(outer['signedPayload']))%4)));version=payload['version']
 zipMeta=next(f for f in payload['artifacts'] if f['name']=='agent-php.zip');raw=central(zipMeta['downloadUrl']);archive=work/'agent-php.zip';archive.write_bytes(raw)
 subprocess.run(['python3',str(root/'scripts/verify-php-release.py'),'--manifest',str(work/'manifest.json'),'--key',config['key'],'--version',version,'--artifact','agent-php.zip','--file',str(archive)],check=True,stdout=subprocess.DEVNULL)
 with zipfile.ZipFile(archive) as z:z.extractall(work)
 private=work/'private';private.chmod(0o700)
 (private/'bootstrap.json').write_text(json.dumps({'hostname':'localhost','publicUrl':origin,'controlPlaneUrl':config['origin'],'trustedProxies':['127.0.0.1'],'developmentLoopback':True}));(private/'bootstrap.json').chmod(0o600)
 router=work/'router.php';router.write_text("<?php if(parse_url($_SERVER['REQUEST_URI'],PHP_URL_PATH)==='/setup.css')return false;require __DIR__.'/public/index.php';")
 env=os.environ.copy();env.update(SHORTLINK_PRIVATE_DIR=str(private),PHP_CLI_SERVER_WORKERS='4');log=open(work/'php.log','w')
 php=subprocess.Popen(['php','-S',f'127.0.0.1:{port}','-t',str(work/'public'),str(router)],env=env,stdout=log,stderr=log,start_new_session=True)
 try:
  for _ in range(80):
   try:s,html,_=request('/__agent/setup');break
   except OSError:time.sleep(.05)
  assert s==200
  csrf=re.search(r'name="csrf" value="([^"]+)"',html)[1]
  s,html,_=request('/__agent/setup',{'action':'check','csrf':csrf,'token':config['token']});assert s==200 and 'Siap untuk aktivasi' in html
  csrf=re.search(r'name="csrf" value="([^"]+)"',html)[1]
  s,html,_=request('/__agent/setup',{'action':'activate','csrf':csrf});assert s==200 and 'Agent sudah terpasang' in html
  s,html,_=request('/__agent/setup');assert 'name="token"' not in html and 'Agent sudah terpasang' in html
  s,_,headers=request('/hello',headers={'X-Forwarded-For':'1.1.1.1','User-Agent':config['ua']});assert s==302 and headers['Location']=='https://example.org/first'
  for path in ['/private/config.json','/private/pending.json']:
   s,html,_=request(path);assert s==404 and 'credential' not in html
  print(json.dumps({'downloadVerified':True,'wizardInstalled':True,'setupLocked':True,'redirect':302,'domainId':config['domainId']}))
 finally:os.killpg(php.pid,signal.SIGTERM);php.wait(timeout=5);log.close()
