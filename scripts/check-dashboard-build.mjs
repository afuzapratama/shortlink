import {readdir,readFile} from 'node:fs/promises';
import {join} from 'node:path';
const roots=process.argv.slice(2);
if(!roots.length)roots.push('apps/dashboard/dist');
const markers=['Buka preview fitur','/?demo=1','Publish demo','Publish draft demo','DEMO-INSTALL','go.nusa.example','s.karya.example','Simulasikan acknowledgement','Contoh volume request','Lihat preview fitur dengan data contoh'];
for(const root of roots){
 const assets=join(root,'assets');let inspected=0;
 for(const file of await readdir(assets)){
  if(!file.endsWith('.js'))continue;
  inspected++;const source=await readFile(join(assets,file),'utf8');
  for(const marker of markers)if(source.includes(marker))throw Error(`${root}: preview marker included: ${marker}`);
 }
 if(!inspected)throw Error(`${root}: no JavaScript build found`);
 console.log(`${root}: ${inspected} JavaScript bundles checked; preview fixtures excluded.`);
}
