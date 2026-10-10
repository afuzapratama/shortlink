#!/usr/bin/env python3
"""Run from a trusted checkout before extracting a downloaded PHP ZIP. Requires OpenSSL."""
import argparse,base64,hashlib,json,pathlib,re,subprocess,tempfile
p=argparse.ArgumentParser();p.add_argument('--manifest',required=True);p.add_argument('--key',required=True);p.add_argument('--version',required=True);p.add_argument('--artifact',choices=['agent-php.zip','INSTALL.md'],required=True);p.add_argument('--file',required=True);a=p.parse_args()
def reject(message):p.exit(1,message+'\n')
def decode(value):
 if not isinstance(value,str) or not re.fullmatch(r'[A-Za-z0-9_-]+',value):reject('Invalid base64url')
 raw=base64.urlsafe_b64decode(value+'='*((-len(value))%4))
 if base64.urlsafe_b64encode(raw).decode().rstrip('=')!=value:reject('Noncanonical base64url')
 return raw
try:
 path=pathlib.Path(a.manifest)
 if path.stat().st_size>65536:reject('Manifest exceeds limit')
 envelope=json.loads(path.read_bytes());payload=decode(envelope['signedPayload']);signature=decode(envelope['signature']);key=decode(a.key)
 if len(key)!=32 or len(signature)!=64:reject('Invalid key/signature length')
 with tempfile.TemporaryDirectory(prefix='verify-php-') as temp:
  work=pathlib.Path(temp);(work/'key.der').write_bytes(bytes.fromhex('302a300506032b6570032100')+key);(work/'payload').write_bytes(payload);(work/'signature').write_bytes(signature)
  r=subprocess.run(['openssl','pkeyutl','-verify','-pubin','-keyform','DER','-inkey',str(work/'key.der'),'-rawin','-in',str(work/'payload'),'-sigfile',str(work/'signature')],capture_output=True)
  if r.returncode:reject('PHP manifest signature rejected')
 m=json.loads(payload)
 if m['version']!=a.version or not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[a-z0-9.-]+)?',a.version) or m['method']!='php_zip' or m['minimumPhp']!='8.2':reject('Unexpected PHP release/method')
 if len(m['artifacts'])!=2 or {f['name'] for f in m['artifacts']}!={'agent-php.zip','INSTALL.md'}:reject('Invalid PHP artifact list')
 for f in m['artifacts']:
  if f['version']!=a.version or f['platform']!='php' or f['downloadUrl']!='/v1/assets/php/releases/'+a.version+'/'+f['name'] or type(f['size'])is not int or not 0<f['size']<=4*1024*1024 or not re.fullmatch(r'[a-f0-9]{64}',f['sha256']):reject('Invalid PHP artifact binding')
 meta=next(f for f in m['artifacts'] if f['name']==a.artifact);file=pathlib.Path(a.file)
 if not file.is_file() or file.stat().st_size!=meta['size'] or hashlib.sha256(file.read_bytes()).hexdigest()!=meta['sha256']:reject('PHP artifact checksum/size rejected')
 print('Verified',a.version,a.artifact)
except (OSError,ValueError,KeyError,TypeError):reject('PHP verification failed')
