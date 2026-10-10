import { useEffect, useState } from 'react';
import type { Domain } from '../../../packages/contracts/src';
import { AgentPackages } from './AgentPackages';
import {DomainProtection} from './DomainProtection';
import { InstallationProgress } from './InstallationProgress';
import { api } from './api';
import { Badge, Modal, Panel } from './components';
type Challenge={name:string;value:string;expiresAt:string};
type Token={token:string;expiresAt:string;installationId:string;controlPlaneUrl:string};
type Agent={id:string;method:string;version:string};
type Audit={actorId:string;action:string;occurredAt:string};
const auditLabels:Record<string,string>={domain_created:'Domain ditambahkan',link_created:'Shortlink dibuat',link_draft_updated:'Draft tujuan disimpan',verification_challenge:'Instruksi verifikasi dibuat',domain_verified:'Kepemilikan diverifikasi',install_token_created:'Token instalasi dibuat',agent_enrolled:'Agent terdaftar',agent_revoked:'Kredensial agent dicabut',config_applied:'Konfigurasi diterapkan',readiness_probed:'Kesiapan domain diperiksa',protection_published:'Proteksi traffic dipublish'};
export function DomainEnrollment({domain,onChanged,onError}:{domain:Domain;onChanged:()=>void;onError:(e:unknown)=>void}){
 const [challenge,setChallenge]=useState<Challenge|null>(null),[token,setToken]=useState<Token|null>(null),[agent,setAgent]=useState<Agent|null>(null),[audit,setAudit]=useState<Audit[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState(''),[result,setResult]=useState(''),[confirm,setConfirm]=useState(false);
 async function refresh(){try{const [a,log]=await Promise.all([api<{agent:Agent|null}>(`/v1/admin/domains/${domain.id}/agent`),api<{items:Audit[]}>(`/v1/admin/domains/${domain.id}/audit`)]);setAgent(a.agent);setAudit(log.items)}catch(e){setError(e instanceof Error?e.message:'Gagal memuat instalasi.');onError(e)}}
 useEffect(()=>{void refresh()},[domain.id]);
 async function action(work:()=>Promise<void>){if(busy)return;setBusy(true);setError('');try{await work();await refresh();onChanged()}catch(e){setError(e instanceof Error?e.message:'Permintaan gagal.');onError(e)}finally{setBusy(false)}}
 return <>
 <Panel title="Verifikasi & instalasi" caption="Buktikan kepemilikan domain sebelum menerbitkan token instalasi.">
 {error&&<p role="alert" className="error">{error}</p>}
 {['draft','verification_pending'].includes(domain.readiness)?<>
 <button disabled={busy} onClick={()=>void action(async()=>{setChallenge(await api<Challenge>(`/v1/admin/domains/${domain.id}/verification`,'POST'))})}>Tampilkan instruksi DNS</button>
 {challenge&&<><p className="muted small">Tambahkan record TXT berikut pada pengelola DNS domain, lalu periksa kembali setelah DNS tersebar.</p><dl className="details"><dt>Nama TXT</dt><dd><code>{challenge.name}</code></dd><dt>Nilai</dt><dd><code>{challenge.value}</code></dd><dt>Berlaku sampai</dt><dd>{new Date(challenge.expiresAt).toLocaleString('id-ID')}</dd></dl><button className="primary" disabled={busy} onClick={()=>void action(async()=>{const probe=await api<{checks:{message:string}[]}>(`/v1/admin/domains/${domain.id}/verify`,'POST');setResult(probe.checks[0].message)})}>Periksa DNS sekarang</button></>}
 </>:<Badge tone="green">Kepemilikan terverifikasi</Badge>}
 {result&&<p role="status">{result}</p>}
 {agent?<><dl className="details"><dt>Agent</dt><dd><code>{agent.id}</code></dd><dt>Metode</dt><dd>{agent.method}</dd><dt>Versi</dt><dd>{agent.version}</dd></dl><button className="danger" disabled={busy} onClick={()=>setConfirm(true)}>Cabut kredensial agent</button><p className="small muted">{domain.mode==='local'?'Pencabutan memutus akses ke pusat. Agent offline dapat memakai snapshot terakhir hingga lease berakhir.':'Pencabutan menolak request API berikutnya. Adapter PHP tidak memiliki snapshot atau fallback offline lokal.'}</p></>:!['draft','verification_pending'].includes(domain.readiness)&&<><p className="muted">Pilih token untuk metode {domain.mode==='local'?'CLI VPS':'ZIP PHP'} yang terdaftar.</p><button className="primary" disabled={busy} onClick={()=>void action(async()=>{setToken(await api<Token>('/v1/admin/install-tokens','POST',{domainId:domain.id,method:domain.mode==='local'?'cli':'php_zip'}))})}>Buat token instalasi</button><div className="notice">Installer CLI tersedia pada panel Paket agent; ZIP PHP tersedia pada panel Paket PHP beserta bootstrap dan tutorial. Token berlaku 30 menit dan token baru menggantikan token lama yang belum digunakan.</div></>}
 </Panel>
 <InstallationProgress domain={domain} onChanged={onChanged} onError={onError}/>
 <AgentPackages domain={domain} onError={onError}/>
 <DomainProtection domain={domain} onChanged={onChanged} onError={onError}/>
 <Panel title="Audit domain" caption="Aktivitas verifikasi dan enrollment terbaru.">{audit.length?<ol className="instructions">{audit.map((a,i)=><li key={i}><strong>{auditLabels[a.action]??a.action.replaceAll('_',' ')}</strong><small> · {new Date(a.occurredAt).toLocaleString('id-ID')}</small></li>)}</ol>:<p className="muted">Belum ada aktivitas tercatat.</p>}</Panel>
 {token&&<Modal title="Token instalasi" onClose={()=>setToken(null)}><div className="notice">Simpan token ini di tempat private. Token ditampilkan sekali; menutup dialog akan menghapusnya dari tampilan.</div><dl className="details"><dt>Token</dt><dd><code>{token.token}</code></dd><dt>Installation ID</dt><dd><code>{token.installationId}</code></dd><dt>Server pusat</dt><dd>{token.controlPlaneUrl}</dd><dt>Berlaku sampai</dt><dd>{new Date(token.expiresAt).toLocaleString('id-ID')}</dd></dl><button className="primary" onClick={()=>setToken(null)}>Sudah saya simpan</button></Modal>}
 {confirm&&agent&&<Modal title="Cabut kredensial agent?" onClose={()=>{if(!busy)setConfirm(false)}}><p>Agent tidak dapat mengambil konfigurasi atau mengirim laporan lagi. Enrollment baru memerlukan token baru.</p><div className="actions form-actions"><button disabled={busy} onClick={()=>setConfirm(false)}>Batal</button><button className="danger" disabled={busy} onClick={()=>void action(async()=>{await api(`/v1/admin/agents/${agent.id}/revoke`,'POST');setConfirm(false)})}>Ya, cabut agent</button></div></Modal>}
 </>;
}
