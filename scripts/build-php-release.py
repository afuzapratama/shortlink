#!/usr/bin/env python3
"""Deterministic source ZIP, without live config/credentials. Central signing is B17."""
import argparse,hashlib,json,os,pathlib,re,subprocess,tempfile,zipfile
ROOT=pathlib.Path(__file__).resolve().parent.parent
p=argparse.ArgumentParser();p.add_argument('--output',required=True);a=p.parse_args()
source=ROOT/'services/agent-php'
code=(source/'private/src/Adapter.php').read_text();version=re.search(r"const VERSION = '(v[0-9.a-z-]+)';",code)[1]
out=pathlib.Path(a.output).resolve();out.parent.mkdir(parents=True,exist_ok=True)
if out.exists():p.error('Output already exists; preserve immutable builds and choose a new directory')
files=['public/index.php','public/setup.css','public/.htaccess','private/src/Adapter.php','private/src/Setup.php','private/src/Manage.php','private/bootstrap.example.json']
with tempfile.TemporaryDirectory(prefix='.php-release-',dir=out.parent) as temp:
 staging=pathlib.Path(temp);name=f'agent-php-{version}.zip';archive=staging/name
 with zipfile.ZipFile(archive,'w',compression=zipfile.ZIP_DEFLATED,compresslevel=9) as z:
  for directory in ['public/','private/','private/src/']:
   info=zipfile.ZipInfo(directory,(2026,1,1,0,0,0));info.create_system=3;info.external_attr=((0o40700 if directory.startswith('private') else 0o40755)<<16)|0x10;z.writestr(info,b'')
  for relative in files+['INSTALL.md']:
   path=ROOT/'docs/INSTALL_PHP.md' if relative=='INSTALL.md' else source/relative
   if path.suffix=='.php':subprocess.run(['php','-l',str(path)],check=True,stdout=subprocess.DEVNULL)
   info=zipfile.ZipInfo(relative,(2026,1,1,0,0,0));info.create_system=3;info.compress_type=zipfile.ZIP_DEFLATED;info.external_attr=(0o100600 if relative.startswith('private/') else 0o100644)<<16;z.writestr(info,path.read_bytes())
 raw=archive.read_bytes();manifest={'version':version,'method':'php_zip','minimumPhp':'8.2','filename':name,'size':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'files':files+['INSTALL.md'],'signed':False}
 (staging/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n');os.rename(staging,out)
print('PHP release prepared:',out)
