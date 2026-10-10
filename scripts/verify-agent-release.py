#!/usr/bin/env python3
"""Use from a TRUSTED source checkout for first download verification. Requires OpenSSL Ed25519."""
import argparse,base64,hashlib,json,pathlib,re,subprocess,tempfile
p=argparse.ArgumentParser();p.add_argument('--manifest',required=True);p.add_argument('--key',required=True);p.add_argument('--version',required=True);p.add_argument('--artifact',required=True);p.add_argument('--file',required=True);a=p.parse_args()
def reject(message):p.exit(1,message+'\n')
def decode(s):
 if not isinstance(s,str) or not re.fullmatch(r'[A-Za-z0-9_-]+',s):reject('Invalid base64url')
 return base64.urlsafe_b64decode(s+'='*((-len(s))%4))
try:
 manifest_path=pathlib.Path(a.manifest)
 if manifest_path.stat().st_size>65536:reject('Manifest exceeds limit')
 outer=json.loads(manifest_path.read_bytes());raw=decode(outer['signedPayload']);sig=decode(outer['signature']);key=decode(a.key)
 if len(key)!=32 or len(sig)!=64:reject('Invalid Ed25519 key/signature length')
 with tempfile.TemporaryDirectory(prefix='verify-agent-') as temp:
  root=pathlib.Path(temp);(root/'key.der').write_bytes(bytes.fromhex('302a300506032b6570032100')+key);(root/'payload').write_bytes(raw);(root/'sig').write_bytes(sig)
  result=subprocess.run(['openssl','pkeyutl','-verify','-pubin','-keyform','DER','-inkey',str(root/'key.der'),'-rawin','-in',str(root/'payload'),'-sigfile',str(root/'sig')],capture_output=True)
  if result.returncode:reject('Manifest signature rejected')
 # Only decode the exact bytes authenticated above; ignore outer projection fields.
 m=json.loads(raw)
 if m['version']!=a.version or not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[a-z0-9.-]+)?',a.version) or type(m['installerReady']) is not bool:reject('Unexpected release version/capability')
 names={'agent-linux-amd64','agent-linux-arm64','image-linux-amd64.tar','image-linux-arm64.tar','compose.agent.yml','INSTALL.md'}
 if len(m['artifacts'])!=len(names) or {f['name'] for f in m['artifacts']}!=names:reject('Invalid artifact list')
 meta=next((f for f in m['artifacts'] if f['name']==a.artifact),None)
 if meta is None or meta['version']!=a.version or meta['downloadUrl']!='/v1/assets/agent/releases/'+a.version+'/'+a.artifact:reject('Invalid artifact binding/path')
 if type(meta['size']) is not int or not 0<meta['size']<=536870912 or not re.fullmatch(r'[a-f0-9]{64}',meta['sha256']):reject('Invalid artifact size/hash')
 file=pathlib.Path(a.file)
 if not file.is_file() or file.stat().st_size!=meta['size']:reject('Artifact size/type mismatch')
 digest=hashlib.sha256()
 with file.open('rb') as stream:
  for chunk in iter(lambda:stream.read(1024*1024),b''):digest.update(chunk)
 if digest.hexdigest()!=meta['sha256']:reject('Artifact checksum mismatch')
 print('Verified',a.version,a.artifact)
except (OSError,ValueError,KeyError,TypeError) as e:reject('Verification failed: '+type(e).__name__)
