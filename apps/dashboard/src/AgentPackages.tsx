import {useEffect,useState} from 'react';
import type {Domain,AgentManifest,DomainBootstrap} from '../../../packages/contracts/src';
import {api} from './api';
import {PHPPackages} from './PHPPackages';
import {Badge,Panel} from './components';
export function AgentPackages({domain,onError}:{domain:Domain;onError:(e:unknown)=>void}){
 const [manifest,setManifest]=useState<AgentManifest>(),[bootstrap,setBootstrap]=useState<DomainBootstrap>(),[busy,setBusy]=useState(false),[error,setError]=useState('');
 async function load(){setBusy(true);setError('');try{const [m,b]=await Promise.all([api<AgentManifest>('/v1/assets/agent/manifest'),api<DomainBootstrap>(`/v1/admin/domains/${domain.id}/bootstrap`)]);setManifest(m);setBootstrap(b)}catch(e){setManifest(undefined);setBootstrap(undefined);onError(e);setError(e instanceof Error?e.message:'Gagal memuat paket')}finally{setBusy(false)}}
 useEffect(()=>{if(domain.mode==='local')void load()},[domain.id,domain.mode]);
 function downloadBootstrap(){if(!bootstrap)return;const url=URL.createObjectURL(new Blob([JSON.stringify(bootstrap,null,2)+'\n'],{type:'application/json'}));const a=document.createElement('a');a.href=url;a.download='bootstrap.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
 if(domain.mode==='api')return <PHPPackages domain={domain} onError={onError}/>;
 return <Panel title="Paket agent" caption="Paket distribusi private dan identitas pusat untuk pemasangan VPS."><>
 <button disabled={busy} onClick={()=>void load()}>{busy?'Memuat paket…':'Muat ulang paket'}</button>
 {error&&<p role="status" className="notice">{error}</p>}
 {manifest&&bootstrap&&<><p><Badge>Rilis {manifest.version}</Badge> · {new Date(manifest.publishedAt).toLocaleString('id-ID')}</p><div className="notice">{manifest.installerReady?'Installer CLI tersedia untuk VPS Linux. Verifikasi paket terlebih dahulu, lalu masukkan token melalui prompt tersembunyi.':'Binary, image Docker dan Compose tersedia. Rilis ini belum memuat installer otomatis.'}</div>
 <dl className="details"><dt>Server pusat</dt><dd><code>{bootstrap.controlPlaneUrl}</code></dd><dt>Kunci verifikasi publik</dt><dd><code>{bootstrap.manifestVerificationKey}</code></dd><dt>Installer otomatis</dt><dd>{manifest.installerReady?'Tersedia':'Menunggu B10'}</dd></dl>
 <div className="actions"><a href="/v1/assets/agent/manifest" download="manifest.json">Download manifest signed</a><button onClick={downloadBootstrap}>Download bootstrap domain</button></div>
 <ol className="instructions"><li>Download binary arsitektur VPS, manifest dan bootstrap domain ini.</li><li>Verifikasi signature, expected version dan checksum di komputer yang dipercaya sebelum menjalankan binary.</li><li>{manifest.installerReady?'Jalankan install dengan bootstrap domain. Installer mengambil image dan MMDB dari pusat; baca INSTALL.md untuk baseline VPS, proxy sendiri, update dan rollback.':'Load image dengan Docker, siapkan enrollment/config private lalu jalankan Compose sesuai panduan manual.'}</li></ol>
 {manifest.installerReady&&<div className="notice"><code>{bootstrap.controlPlaneUrl.startsWith('http://')?'sudo ./agent-linux-amd64 install --bootstrap bootstrap.json --development --proxy external':'sudo ./agent-linux-amd64 install --bootstrap bootstrap.json --install-docker'}</code><p className="small">Gunakan agent-linux-arm64 untuk VPS ARM. Docker yang sudah aktif dipakai kembali. Token tidak dimasukkan ke command. {bootstrap.controlPlaneUrl.startsWith('http://')?'Pusat ini memakai HTTP loopback: command untuk test lokal dengan Docker aktif, tanpa HTTPS. Produksi membutuhkan pusat HTTPS.':'Caddy membutuhkan DNS dan port 80/443; gunakan --proxy external bila proxy sudah tersedia.'}</p></div>}
 <div className="table-scroll"><table><thead><tr><th>File</th><th>Platform</th><th>Ukuran</th><th>Download</th></tr></thead><tbody>{manifest.artifacts.map(f=><tr key={f.name}><td><strong>{f.name}</strong><small>SHA256: {f.sha256}</small>{f.imageId&&<small>Image ID: {f.imageId}</small>}{f.imageReference&&<small>Registry: {f.imageReference}</small>}</td><td>{f.platform}</td><td>{(f.size/1024/1024).toFixed(2)} MiB</td><td><a href={f.downloadUrl} download={f.name}>Download {f.name}</a></td></tr>)}</tbody></table></div>
 <p className="small muted">Token/proof/credential dan MMDB tidak masuk paket. Download memakai sesi owner; agent CLI aktif dapat mengambil paket memakai kredensialnya sendiri.</p>
 </>}
 </></Panel>
}
