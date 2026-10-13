import {useEffect,useState} from 'react';
import type {Domain,Shortlink} from '../../../packages/contracts/src';
import {api} from './api';
import {Field,Panel,Badge,Modal} from './components';
type Access={key:string;expiresAt:string;url:string};
export function DomainOpening({domain,links,onChanged,onError}:{domain:Domain;links:Shortlink[];onChanged:()=>void;onError:(e:unknown)=>boolean}){
 const [target,setTarget]=useState(domain.openingUrl??''),[busy,setBusy]=useState(false),[error,setError]=useState(''),[notice,setNotice]=useState(''),[access,setAccess]=useState<Access>();
 useEffect(()=>{setTarget(domain.openingUrl??'')},[domain.openingUrl]);
 async function run(fn:()=>Promise<void>){if(busy)return;setBusy(true);setError('');try{await fn()}catch(e){if(!onError(e))setError(e instanceof Error?e.message:'Permintaan gagal')}finally{setBusy(false)}}
 return <><Panel title="URL pembuka & manage agent" caption="Pengaturan khusus domain ini. Root dan slug yang tidak ditemukan memakai URL pembuka, tanpa menambah klik shortlink.">
 {error&&<p role="alert" className="error">{error}</p>}{notice&&<p role="status">{notice}</p>}
 <form onSubmit={e=>{e.preventDefault();void run(async()=>{await api<Domain>(`/v1/admin/domains/${domain.id}/opening`,'PATCH',{openingUrl:target.trim(),expectedRevision:domain.desiredRevision});onChanged();setNotice('URL pembuka tersimpan. Agent Go menerapkan setelah sinkronisasi.')})}}><Field label="URL pembuka domain" hint="Kosong berarti root dan slug tidak ditemukan menghasilkan 503. Setup, manage, health dan verifikasi tetap terpisah."><input type="url" value={target} maxLength={4096} placeholder="https://websiteanda.com" onChange={e=>setTarget(e.target.value)}/></Field><button className="primary" disabled={busy}>Simpan URL pembuka</button></form>
 <p className="small muted">Manage: <a href={`https://${domain.hostname}/__agent/manage`} target="_blank" rel="noopener noreferrer">{domain.hostname}/__agent/manage</a>. Gunakan runtime Go dan ZIP PHP terbaru; paket lama belum mempunyai halaman ini.</p>
 <button disabled={busy||['draft','verification_pending'].includes(domain.readiness)} onClick={()=>void run(async()=>{setAccess(await api<Access>(`/v1/admin/domains/${domain.id}/manage-key`,'POST'))})}>Buat akses manage sekali pakai</button>
 </Panel><Panel title="Shortlink domain ini" caption={`${links.filter(l=>l.domainId===domain.id).length} link · hanya domain ${domain.hostname}`}>
 <div className="table-scroll"><table><thead><tr><th>Shortlink</th><th>Tujuan / fallback</th><th>Status</th></tr></thead><tbody>{links.filter(l=>l.domainId===domain.id).map(l=><tr key={l.id}><td>{l.state==='published'?<a href={`https://${domain.hostname}/${l.slug}`} target="_blank" rel="noopener noreferrer">{l.slug} ↗</a>:l.slug}</td><td>{l.destination}{l.routing&&<small>Fallback: {l.routing.fallback}</small>}</td><td><Badge>{l.state}</Badge></td></tr>)}</tbody></table></div>{!links.some(l=>l.domainId===domain.id)&&<p>Belum ada shortlink pada domain ini.</p>}
 </Panel>{access&&<Modal title="Akses manage agent" onClose={()=>setAccess(undefined)}><p>Buka <a href={access.url} target="_blank" rel="noopener noreferrer">halaman manage domain</a>, lalu masukkan key ini. Jangan masukkan ke URL.</p><code style={{overflowWrap:'anywhere'}}>{access.key}</code><p>Berlaku sampai {new Date(access.expiresAt).toLocaleString('id-ID')}. Sekali pakai; sesi manage berlaku 15 menit dan hanya untuk domain ini. Key baru menggantikan key yang belum digunakan.</p><button onClick={()=>setAccess(undefined)}>Tutup dan hapus dari tampilan</button></Modal>}</>
}
