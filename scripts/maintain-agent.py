#!/usr/bin/env python3
"""Private operator maintenance; supports Go and PHP agent config JSON."""
import argparse,base64,datetime,json,secrets,os,pathlib,stat,subprocess,tempfile,urllib.parse,urllib.request

def decode(v):
 return base64.urlsafe_b64decode(v+'='*((-len(v))%4))
def verify(doc,key):
 raw=decode(doc['payload']);sig=decode(doc['signature']);public=decode(key)
 if len(raw)>16384 or len(public)!=32 or len(sig)!=64:raise ValueError('invalid signature input')
 with tempfile.TemporaryDirectory() as directory:
  root=pathlib.Path(directory)
  (root/'key').write_bytes(bytes.fromhex('302a300506032b6570032100')+public);(root/'payload').write_bytes(raw);(root/'signature').write_bytes(sig)
  result=subprocess.run(['openssl','pkeyutl','-verify','-pubin','-keyform','DER','-inkey',str(root/'key'),'-rawin','-in',str(root/'payload'),'-sigfile',str(root/'signature')],capture_output=True)
  if result.returncode:raise ValueError('signature rejected')
 return json.loads(raw)
def migration(path,key):
 document=pathlib.Path(path)
 if document.stat().st_size>16384:raise ValueError('migration too large')
 m=verify(json.loads(document.read_bytes()),key)
 if m['purpose']!='shortlink-control-migration-v1' or datetime.datetime.fromisoformat(m['expiresAt'].replace('Z','+00:00'))<=datetime.datetime.now(datetime.timezone.utc):raise ValueError('invalid/expired migration')
 return m
class NoRedirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,*args,**kwargs):return None

def origin(value,development):
 p=urllib.parse.urlsplit(value)
 if not p.hostname or p.username or p.password or p.path not in ('','/') or p.query or p.fragment:raise ValueError('invalid control origin')
 if p.scheme!='https' and not (development and p.scheme=='http' and p.hostname in ('127.0.0.1','::1')):raise ValueError('HTTPS required')
 return value.rstrip('/')
def token(value):
 return isinstance(value,str) and len(value)==43 and len(decode(value))==32 and base64.urlsafe_b64encode(decode(value)).decode().rstrip('=')==value

def call(url,credential,path,body=None):
 data=None if body is None else json.dumps(body).encode()
 req=urllib.request.Request(url+path,data=data,headers={**({'Authorization':'Bearer '+credential} if credential else {}),'Content-Type':'application/json'},method='GET' if data is None else 'POST')
 with urllib.request.build_opener(NoRedirect()).open(req,timeout=15) as response:
  raw=response.read(65537)
  if len(raw)>65536:raise ValueError('response too large')
  return json.loads(raw)
def private_json(path):
 if path.is_symlink():raise ValueError('config symlink rejected')
 info=path.stat()
 if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode)&0o077 or info.st_size>16384:raise ValueError('config must be private mode 0600')
 return json.loads(path.read_bytes()),info

def persist(path,config,info):
 descriptor,temporary=tempfile.mkstemp(prefix='.config-maintain-',dir=path.parent)
 try:
  os.fchmod(descriptor,0o600)
  if os.geteuid()==0:os.fchown(descriptor,info.st_uid,info.st_gid)
  with os.fdopen(descriptor,'w') as stream:json.dump(config,stream,indent=2);stream.write('\n');stream.flush();os.fsync(stream.fileno())
  os.replace(temporary,path)
  descriptor=os.open(path.parent,os.O_RDONLY|os.O_DIRECTORY)
  try:os.fsync(descriptor)
  finally:os.close(descriptor)
 finally:
  if os.path.exists(temporary):os.unlink(temporary)

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--config',required=True);parser.add_argument('--migration');parser.add_argument('--key-file',help='Previously pinned PUBLIC verification key (required for PHP maintenance)');parser.add_argument('--runtime-stopped',action='store_true',help='Confirm Go container stopped / PHP site in maintenance mode')
 a=parser.parse_args()
 if not a.runtime_stopped:raise ValueError('Stop the runtime before maintenance; pass --runtime-stopped')
 path=pathlib.Path(a.config)
 config,info=private_json(path)
 field='controlUrl' if 'controlUrl' in config else 'controlPlaneUrl';bootstrap=None;bootstrap_path=path.parent/'bootstrap.json';bootstrap_info=None
 if field=='controlPlaneUrl':
  bootstrap,bootstrap_info=private_json(bootstrap_path)
  if bootstrap['hostname']!=config['hostname']:raise ValueError('PHP bootstrap binding rejected')
 development=config.get('development',False) if bootstrap is None else bootstrap.get('developmentLoopback',False)
 url=origin(config[field],development);migration_id=''
 if not token(config['credential']):raise ValueError('invalid private config')
 key=config.get('verificationKey') or (pathlib.Path(a.key_file).read_text().strip() if a.key_file else '')
 if not key:raise ValueError('previously pinned public key required')
 if a.migration:
  m=migration(a.migration,key);url=origin(m['origin'],development);migration_id=m['id']
 nonce=secrets.token_urlsafe(32)
 identity=verify(call(url,None,'/v1/control/identity',{'nonce':nonce}),key)
 if identity.get('purpose')!='shortlink-control-identity-v1' or identity.get('nonce')!=nonce or identity.get('origin')!=url:raise ValueError('control identity rejected')
 state=call(url,config['credential'],'/v1/agents/control')
 if state['domainId']!=config['domainId'] or state['hostname']!=config['hostname'] or ('agentId' in config and state['agentId']!=config['agentId']):raise ValueError('agent binding rejected')
 config[field]=url
 if state.get('credential'):
  if not token(state['credential']):raise ValueError('invalid replacement credential')
  config['credential']=state['credential']
 persist(path,config,info)
 if bootstrap is not None:
  bootstrap['controlPlaneUrl']=url
  persist(bootstrap_path,bootstrap,bootstrap_info)
 call(url,config['credential'],'/v1/agents/control/ack',{'migrationId':migration_id})
 print('Private config persisted; central acknowledgment complete. Restart the runtime.')
if __name__=='__main__':
 try:main()
 except Exception:
  # Never include URLs, bearer credentials, response bodies or config in errors.
  print('Maintenance failed. Keep runtime stopped; fix connectivity/config/signature and retry. If config was saved, retry uses the saved credential.',file=__import__('sys').stderr)
  raise SystemExit(1)
