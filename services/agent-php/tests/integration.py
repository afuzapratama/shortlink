#!/usr/bin/env python3
"""Disposable PHP HTTP tests; central stub exercises failures and protocol boundaries."""
import base64,fcntl,hashlib,hmac,http.cookiejar,http.server,json,os,pathlib,re,shutil,socket,subprocess,tempfile,threading,time,urllib.request,urllib.error,urllib.parse
ROOT=pathlib.Path(__file__).resolve().parents[3]
def tok():return base64.urlsafe_b64encode(os.urandom(32)).decode().rstrip('=')
def mac(cred,purpose,payload):return hmac.new(hashlib.sha256(cred.encode()).hexdigest().encode(),b'shortlink-probe-v1:'+purpose.encode()+b':'+payload,hashlib.sha256).hexdigest()
def freeport():
 with socket.socket() as s:s.bind(('127.0.0.1',0));return s.getsockname()[1]
state={'token':tok(),'installation':tok(),'agent':tok(),'credential':tok(),'contextCalls':0,'enrollCalls':0,'events':{},'proof':None,'loseEnrollment':False,'decisionMode':'redirect','ready':True,'probeCalls':0,'retryFirst':False}
class Central(http.server.BaseHTTPRequestHandler):
 def log_message(self,*a):pass
 def reply(self,status,body,headers={}):
  raw=json.dumps(body,separators=(',',':')).encode();self.send_response(status)
  for k,v in headers.items():self.send_header(k,v)
  self.send_header('Content-Type','application/json');self.end_headers()
  try:self.wfile.write(raw)
  except (BrokenPipeError,ConnectionResetError):pass
 def do_GET(self):self.reply(200 if state['ready'] else 503,{'ok':state['ready']})
 def do_POST(self):
  b=json.loads(self.rfile.read(int(self.headers.get('Content-Length','0'))))
  boot={'hostname':'localhost','method':'php_zip','domainId':'domain-test','controlPlaneUrl':central_url}
  if self.path=='/v1/agents/install-context':
   state['contextCalls']+=1
   if b['token']!=state['token']:return self.reply(410,{})
   return self.reply(200,{'installationId':state['installation'],'bootstrap':boot})
  if self.path=='/v1/agents/enroll':
   state['enrollCalls']+=1
   assert b['token']==state['token'] and b['installationId']==state['installation'] and b['hostname']=='localhost' and b['method']=='php_zip'
   if state['proof'] is not None:assert state['proof']==b['installationProof'],'Recovery changed proof'
   state['proof']=b['installationProof']
   if state['loseEnrollment']:state['loseEnrollment']=False;self.close_connection=True;return
   return self.reply(200,{'agentId':state['agent'],'credential':state['credential'],'bootstrap':boot})
  if self.headers.get('Authorization')!='Bearer '+state['credential']:return self.reply(401,{})
  if self.path=='/v1/agents/probe':
   state['probeCalls']+=1
   raw=json.dumps({'nonce':b['nonce'],'domainId':b['domainId'],'revision':1,'geoRelease':'test-release','ready':True,'version':b['version']},separators=(',',':')).encode()
   self.send_response(200);self.send_header('X-Shortlink-Proof',mac(state['credential'],'response',raw));self.end_headers();self.wfile.write(raw);return
  if self.path=='/v1/decisions':
   if state['decisionMode']=='down':return self.reply(503,{})
   if state['decisionMode']=='missing':return self.reply(404,{})
   id=b['eventId'];state['events'].setdefault(id,{'body':b,'calls':0});saved=state['events'][id];assert saved['body']==b,'Retry payload changed';saved['calls']+=1
   if state['retryFirst'] and saved['calls']==1:return self.reply(503,{})
   mode=state['decisionMode']
   if mode=='slow':time.sleep(3.2)
   destination=None if mode=='block' else ('javascript:alert(1)' if mode=='badtarget' else 'https://example.org/published')
   return self.reply(200,{'eventId':id if mode!='mismatch' else tok(),'configRevision':1,'action':'block' if mode=='block' else 'redirect','destination':destination})
  self.reply(404,{})
central=http.server.ThreadingHTTPServer(('127.0.0.1',0),Central);central_url=f'http://127.0.0.1:{central.server_port}';threading.Thread(target=central.serve_forever,daemon=True).start()
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*a):return None
jar=http.cookiejar.CookieJar();web=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar),NoRedirect())
port=freeport();origin=f'http://localhost:{port}'
def request(path,form=None,headers=None):
 h=headers or {}
 if form is not None:h={'Origin':origin,'Content-Type':'application/x-www-form-urlencoded',**h}
 r=urllib.request.Request(origin+path,data=urllib.parse.urlencode(form).encode() if form is not None else None,headers=h)
 try:response=web.open(r,timeout=10)
 except urllib.error.HTTPError as e:response=e
 return response.code,response.read().decode(),dict(response.headers)
def csrf(html):return re.search(r'name="csrf" value="([^"]+)"',html)[1]
def check_token(t=None):
 s,html,_=request('/__agent/setup');assert s==200
 return request('/__agent/setup',{'csrf':csrf(html),'action':'check','token':t or state['token']})
with tempfile.TemporaryDirectory(prefix='shortlink-php-test-') as temp:
 work=pathlib.Path(temp);private=work/'private';public=work/'public';shutil.copytree(ROOT/'services/agent-php/private',private);shutil.copytree(ROOT/'services/agent-php/public',public);private.chmod(0o700)
 (private/'bootstrap.json').write_text(json.dumps({'hostname':'localhost','publicUrl':origin,'controlPlaneUrl':central_url,'trustedProxies':[],'developmentLoopback':True}));(private/'bootstrap.json').chmod(0o600)
 env=os.environ.copy();env.update(SHORTLINK_PRIVATE_DIR=str(private),PHP_CLI_SERVER_WORKERS='4')
 log=open(work/'php.log','w');php=subprocess.Popen(['php','-S',f'127.0.0.1:{port}','-t',str(public),str(ROOT/'services/agent-php/tests/router.php')],env=env,stdout=log,stderr=log,start_new_session=True)
 try:
  for _ in range(80):
   try:
    s,html,headers=request('/__agent/setup');break
   except OSError:time.sleep(.05)
  assert s==200 and 'name="token"' in html and 'HttpOnly' in headers.get('Set-Cookie','') and 'SameSite=Strict' in headers.get('Set-Cookie','')
  for path in ['/private/bootstrap.json','/private/src/Adapter.php','/config.json','/pending.json']:
   s,html,_=request(path);assert s==503 and central_url not in html and state['token'] not in html
  s,html,_=request('/__agent/setup',{'csrf':'wrong','action':'check','token':state['token']});assert s==403
  s,html,_=request('/__agent/setup',{'csrf':csrf(html),'action':'check','token':state['token']},{'Origin':'https://evil.example'});assert s==403
  state['ready']=False;s,html,_=check_token();assert 'Pusat belum siap' in html and state['contextCalls']==0 and state['enrollCalls']==0;state['ready']=True
  b=json.loads((private/'bootstrap.json').read_text());b['publicUrl']=f'http://localhost:{freeport()}';(private/'bootstrap.json').write_text(json.dumps(b))
  s,html,_=request('/__agent/setup');s,html,_=request('/__agent/setup',{'csrf':csrf(html),'action':'check','token':state['token']},{'Origin':b['publicUrl']});assert 'Rewrite/routing publik gagal' in html and state['contextCalls']==0 and state['enrollCalls']==0
  b['publicUrl']=origin;(private/'bootstrap.json').write_text(json.dumps(b))
  private.chmod(0o777);s,html,_=request('/__agent/setup');assert s==503 and state['enrollCalls']==0;private.chmod(0o700)
  s,html,_=check_token(tok());assert 'Token salah' in html and state['enrollCalls']==0
  s,html,_=check_token();assert 'Siap untuk aktivasi' in html and state['token'] not in html and state['enrollCalls']==0
  with open(private/'setup.lock','w') as other_installer:
   os.chmod(private/'setup.lock',0o600);fcntl.flock(other_installer,fcntl.LOCK_EX)
   s,blocked,_=request('/__agent/setup',{'csrf':csrf(html),'action':'activate'});assert s==409 and state['enrollCalls']==0
  state['loseEnrollment']=True;s,html,_=request('/__agent/setup',{'csrf':csrf(html),'action':'activate'});assert 'Aktivasi belum selesai' in html and (private/'pending.json').exists() and not (private/'config.json').exists()
  proof=state['proof']
  subprocess.run(['node',str(ROOT/'services/agent-php/tests/browser.mjs')],input=json.dumps({'origin':origin,'token':state['token'],'artifactDir':str(ROOT/'docs/artifacts')}).encode(),check=True,cwd=ROOT)
  s,html,_=request('/__agent/setup');assert 'Agent sudah terpasang' in html and state['proof']==proof and state['enrollCalls']==2 and not (private/'pending.json').exists()
  assert (private/'config.json').stat().st_mode&0o777==0o600
  s,html,_=request('/__agent/setup',{'action':'activate'});assert s==423 and 'name="token"' not in html and state['enrollCalls']==2
  # Setup activation probe did not generate visitor events.
  assert len(state['events'])==0 and state['probeCalls']==1
  state['retryFirst']=True;s,_,h=request('/hello',headers={'X-Forwarded-For':'1.1.1.1','User-Agent':'PHP integration'});assert s==302 and h['Location']=='https://example.org/published'
  assert len(state['events'])==1;event=next(iter(state['events'].values()));assert event['calls']==2 and event['body']['signals']['ip']=='127.0.0.1'
  for mode,expected in [('block',403),('missing',404),('down',503),('mismatch',503),('badtarget',503)]:
   state['decisionMode']=mode;s,_,h=request('/hello');assert s==expected and 'Location' not in h,(mode,s)
  state['decisionMode']='slow';started=time.monotonic();s,_,h=request('/hello');assert s==503 and 'Location' not in h and time.monotonic()-started<4
  state['decisionMode']='redirect'
  s,_,h=request('/.well-known/shortlink-ready');assert s==404
  nonce=tok();s,raw,h=request('/.well-known/shortlink-ready',headers={'X-Shortlink-Nonce':nonce,'X-Shortlink-Proof':mac(state['credential'],'request',nonce.encode())});assert s==200 and json.loads(raw)['nonce']==nonce and h['X-Shortlink-Proof']==mac(state['credential'],'response',raw.encode())
  before=len(state['events']);s,_,_=request('/hello',headers={'Host':'foreign.example'});assert s==503 and len(state['events'])==before
  (private/'bootstrap.json').write_text(json.dumps({'hostname':'localhost','publicUrl':origin,'controlPlaneUrl':central_url,'trustedProxies':['127.0.0.1'],'developmentLoopback':True}))
  # Active config also pins the operator's proxy list; updating it requires explicit private config edit.
  c=json.loads((private/'config.json').read_text());c['trustedProxies']=['127.0.0.1'];(private/'config.json').write_text(json.dumps(c))
  s,_,_=request('/hello',headers={'X-Forwarded-For':'1.1.1.1, 127.0.0.1'});assert s==302
  assert list(state['events'].values())[-1]['body']['signals']['ip']=='1.1.1.1'
  s,_,h=request('/hello',headers={'X-Forwarded-For':'garbage'});assert s==503 and 'Location' not in h
  # Recovery or locked pages and failure responses must never contain private tokens.
  s,html,_=request('/__agent/setup');assert state['credential'] not in html and state['token'] not in html and state['proof'] not in html
  print('PHP HTTP integration passed: preflight, CSRF/origin, binding, recovery, lock, private paths, retry, redirect/block/503, trusted proxy and signed probe.')
 finally:
  import signal
  os.killpg(php.pid,signal.SIGTERM);php.wait(timeout=5);log.close();central.shutdown()
