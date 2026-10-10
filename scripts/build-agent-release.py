#!/usr/bin/env python3
"""Build two Linux binaries/images. No credentials, MMDB or domain config enter artifacts."""
import argparse,hashlib,tarfile,json,os,pathlib,re,shutil,subprocess,tempfile
ROOT=pathlib.Path(__file__).resolve().parent.parent
def run(*args,**kwargs):return subprocess.run(args,check=True,**kwargs)
def output(*args):return subprocess.check_output(args,text=True).strip()
p=argparse.ArgumentParser();p.add_argument('--version',required=True);p.add_argument('--output',required=True);p.add_argument('--registry-repository',help='Optional existing registry/repository. Authenticate Docker separately.');a=p.parse_args()
if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[a-z0-9.-]+)?',a.version) or len(a.version)>64:p.error('Invalid version')
if a.registry_repository and (not re.fullmatch(r'[a-z0-9.:-]+/[a-z0-9/_-]+',a.registry_repository) or '..' in a.registry_repository):p.error('Invalid registry repository')
out=pathlib.Path(a.output).resolve();out.parent.mkdir(parents=True,exist_ok=True)
if out.exists():p.error('Output directory exists; use a new directory to preserve immutable builds')
with tempfile.TemporaryDirectory(prefix='.agent-release-',dir=out.parent) as temp:
 staging=pathlib.Path(temp);build={'version':a.version,'installerReady':True,'images':{}}
 for arch in ('amd64','arm64'):
  env=os.environ.copy();env.update(CGO_ENABLED='0',GOOS='linux',GOARCH=arch)
  binary=staging/f'agent-linux-{arch}'
  run('go','build','-mod=readonly','-trimpath','-buildvcs=false','-ldflags',f'-s -w -X shortlink.local/agent/internal/runtime.Version={a.version}','-o',str(binary),'./cmd/agent',cwd=ROOT/'services/agent',env=env)
  with tempfile.TemporaryDirectory(prefix='agent-image-') as image_temp:
   context=pathlib.Path(image_temp);shutil.copyfile(binary,context/'agent');(context/'agent').chmod(0o755);shutil.copyfile('/etc/ssl/certs/ca-certificates.crt',context/'ca-certificates.crt');(context/'ca-certificates.crt').chmod(0o644);shutil.copyfile(ROOT/'deploy/Dockerfile.agent',context/'Dockerfile')
   tag=f'shortlink-agent:{a.version}-{arch}'
   run('docker','build','--platform',f'linux/{arch}','-t',tag,str(context))
  info=json.loads(output('docker','image','inspect',tag))[0]
  if info['Architecture']!=arch or info['Os']!='linux':raise RuntimeError('Wrong image platform')
  if arch==output('docker','version','--format','{{.Server.Arch}}'):
   if output('docker','run','--rm',info['Id'],'version')!=a.version:raise RuntimeError('Image executable/version smoke test failed')
  image={'id':info['Id'],'reference':None,'digest':None}
  if a.registry_repository:
   registry_tag=f'{a.registry_repository}:{a.version}-{arch}';run('docker','tag',tag,registry_tag);run('docker','push',registry_tag)
   digests=json.loads(output('docker','image','inspect',registry_tag))[0]['RepoDigests'];ref=next(x for x in digests if x.startswith(a.registry_repository+'@'));image.update(reference=ref,digest=ref.split('@',1)[1])
  build['images'][arch]=image
  run('docker','save','--output',str(staging/f'image-linux-{arch}.tar'),tag)
  with tarfile.open(staging/f'image-linux-{arch}.tar') as archive:
   meta=json.load(archive.extractfile('manifest.json'))[0];config=archive.extractfile(meta['Config']).read();image['configDigest']='sha256:'+hashlib.sha256(config).hexdigest()
 shutil.copyfile(ROOT/'deploy/compose.agent.yml',staging/'compose.agent.yml');shutil.copyfile(ROOT/'docs/INSTALL_AGENT.md',staging/'INSTALL.md')
 (staging/'build.json').write_text(json.dumps(build,indent=2)+'\n');os.rename(staging,out)
print('Agent release build prepared at',out)
