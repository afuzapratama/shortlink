import { useState, useEffect, useRef, type FormEvent } from 'react';
import { Copy, Pencil, ShieldCheck, ExternalLink } from 'lucide-react';
import type { Domain, Shortlink, LinkRouting, IPQualityFlag } from '../../../packages/contracts/src';
import { COUNTRY_CODES } from './country-options';
import { OS_OPTIONS, BROWSER_OPTIONS } from './visitor-options';
import { Badge, Empty, Field, Modal, Panel } from './components';

const IP_FLAGS:IPQualityFlag[]=['proxy','vpn','tor','datacenter','botnet','spam','malware','attacker'];
const DEFAULT_IP_FLAGS:IPQualityFlag[]=['proxy','vpn','tor','botnet'];

export function LinkList({domains,links,search,setSearch,copy,onEdit,onPublish,onRules,onHistory,onLifecycle,live=false,policyReady=false}:{
 domains:Domain[];links:Shortlink[];search:string;setSearch:(v:string)=>void;live?:boolean;policyReady?:boolean;onHistory?:(l:Shortlink)=>void;
 onLifecycle?:(l:Shortlink,action:'disable'|'enable'|'delete')=>void;
 copy:(v:string)=>void;onEdit:(l:Shortlink)=>void;onPublish:(l:Shortlink)=>void;onRules:(l:Shortlink)=>void;
}){
 const [tab,setTab]=useState('all');const previous=useRef(new Map(links.map(l=>[l.id,l.disabled])));
 useEffect(()=>{if(previous.current.size&&links.some(l=>!previous.current.has(l.id))){setTab('draft');setSearch('')}const changed=links.find(l=>previous.current.has(l.id)&&!!previous.current.get(l.id)!==!!l.disabled);if(changed)setTab(changed.disabled?'disabled':'active');previous.current=new Map(links.map(l=>[l.id,l.disabled]))},[links]);
 const hostname=(id:string)=>domains.find(d=>d.id===id)?.hostname??'Domain tidak ditemukan';
 const group=(l:Shortlink)=>l.disabled?'disabled':l.state==='draft'?'draft':'active';
 const visible=links.filter(l=>(!live||tab==='all'||group(l)===tab)&&(hostname(l.domainId)+l.slug+l.destination+(l.draftDestination??'')+(l.routing?.fallback??'')+(l.draftRouting?.fallback??'')).toLowerCase().includes(search.toLowerCase())).sort((a,b)=>(b.createdAt?Date.parse(b.createdAt):0)-(a.createdAt?Date.parse(a.createdAt):0));
 return <Panel title="Daftar link" caption="Link terbaru di atas. Versi terbit adalah versi di pusat; agent Go mengikuti setelah sinkronisasi. Draft belum aktif.">
 {live&&<div className="actions link-status-tabs" role="group" aria-label="Status shortlink">{[['all','Semua'],['draft','Belum publish'],['active','Aktif'],['disabled','Nonaktif']].map(([id,label])=><button key={id} aria-pressed={tab===id} className={tab===id?'primary':''} onClick={()=>setTab(id)}>{label} ({id==='all'?links.length:links.filter(l=>group(l)===id).length})</button>)}</div>}
 <div className="toolbar"><input aria-label="Cari slug atau tujuan" placeholder="Cari domain, slug atau tujuan…" value={search} onChange={e=>setSearch(e.target.value)}/><Badge>{visible.length} link</Badge></div>
 <div className="table-scroll"><table><thead><tr><th>Shortlink / tujuan</th><th>Status</th><th>Versi aturan link</th><th>Redirect terbit</th><th>Aksi</th></tr></thead><tbody>{visible.map(l=>{
 const changed=l.draftDestination!==undefined||l.draftRouting!==undefined;const domain=domains.find(d=>d.id===l.domainId);const pending=live&&domain?.mode==='local'&&domain.appliedRevision!==domain.desiredRevision;
 return <tr key={l.id}><td><strong>{hostname(l.domainId)}/{l.slug}</strong><small>{l.state==='published'?'Tujuan terbit: ':''}{l.destination}</small>{l.routing&&<small>Fallback: {l.routing.fallback}</small>}{l.draftRouting&&<small className="pending-destination">Draft fallback: {l.draftRouting.fallback}</small>}{l.draftDestination!==undefined&&<small className="pending-destination">Draft tujuan: {l.draftDestination}</small>}{l.createdAt&&<small>Dibuat {new Date(l.createdAt).toLocaleString('id-ID')}</small>}</td>
 <td><Badge tone={l.disabled?'amber':l.state==='published'?'green':'neutral'}>{l.disabled?'Nonaktif':l.state==='draft'?'Draft':live?'Aktif':'Published'}</Badge>{pending&&l.state==='published'&&<small>Menunggu sinkronisasi agent</small>}{changed&&<div className="draft-indicator"><Badge tone="amber">Perubahan belum diterapkan</Badge></div>}</td>
 <td>{l.state==='published'?<>{l.disabled?'Terakhir terbit':'Aktif'} v{l.publishedRevision??l.revision}{changed&&<small>Draft v{l.revision}</small>}</>:<>Draft v{l.revision}</>}</td><td>{new Intl.NumberFormat('id-ID').format(l.clicks)}</td>
 <td><div className="actions link-row-actions">{l.state==='published'&&!l.disabled&&<a className="inline-link" aria-label={`Buka shortlink ${l.slug}`} href={`https://${hostname(l.domainId)}/${l.slug}`} target="_blank" rel="noopener noreferrer"><ExternalLink size={16}/> Buka</a>}<button className="icon-button" aria-label={`Salin ${l.slug}`} onClick={()=>copy(`https://${hostname(l.domainId)}/${l.slug}`)}><Copy size={16}/></button><button aria-label={`Edit tujuan ${l.slug}`} onClick={()=>onEdit(l)}><Pencil size={14}/> Edit</button><button disabled={live&&!policyReady} aria-label={`Aturan ${l.slug}`} onClick={()=>onEdit(l)}><ShieldCheck size={14}/> Pengunjung</button>{(l.state==='draft'||changed)&&<button disabled={live&&(l.disabled||!policyReady||!['preparing','ready','attention'].includes(domains.find(d=>d.id===l.domainId)?.readiness??''))} onClick={()=>onPublish(l)}>{__DASHBOARD_DEMO__&&!live?'Publish demo':(policyReady?'Publish':'Publish belum tersedia')}</button>}{onHistory&&l.state==='published'&&<button onClick={()=>onHistory(l)}>Riwayat</button>}{onLifecycle&&<>{l.state==='published'&&<button aria-label={`${l.disabled?'Aktifkan':'Nonaktifkan'} ${l.slug}`} onClick={()=>onLifecycle(l,l.disabled?'enable':'disable')}>{l.disabled?'Aktifkan':'Nonaktifkan'}</button>}<button className="danger" aria-label={`Hapus shortlink ${l.slug}`} onClick={()=>onLifecycle(l,'delete')}>Hapus</button></>}</div></td></tr>
 })}</tbody></table></div>{!visible.length&&<Empty title="Tidak ada link yang cocok" description={links.length?'Tidak ada link pada status atau pencarian ini.':'Buat shortlink pertama setelah menambahkan domain.'}/>}
 </Panel>;
}

export function LinkForm({domains,links,existing,onClose,onSave}:{domains:Domain[];links:Shortlink[];existing?:Shortlink;onClose:()=>void;onSave:(l:Shortlink)=>void|Promise<void>}){
  const [domainId,setDomainId]=useState(existing?.domainId??domains[0]?.id??'');
  const [slug,setSlug]=useState(existing?.slug??'');
  const [url,setUrl]=useState(existing?.draftDestination??existing?.destination??'');
  const [routing,setRouting]=useState<LinkRouting>(existing?.draftRouting??existing?.routing??{proxyCheck:false,proxyUnknownFallback:true,fallback:'',country:'*',device:'*',os:'*',browser:'*'});
  const [error,setError]=useState(''),[busy,setBusy]=useState(false);
  async function submit(e:FormEvent){
    e.preventDefault();if(busy)return;const s=slug.trim()||`link-${crypto.randomUUID().slice(0,6)}`;
    if(s==='__agent'){setError('Slug ini digunakan untuk pengelolaan agent. Pilih slug lain.');return}
    if(!/^[a-zA-Z0-9_-]{1,80}$/.test(s)){setError('Slug hanya boleh huruf, angka, tanda - dan _.');return}
    if(links.some(l=>l.id!==existing?.id&&l.domainId===domainId&&l.slug===s)){setError('Slug sudah dipakai pada domain ini.');return}
    let destination:string;
    try{const u=new URL(url.trim());if(!['http:','https:'].includes(u.protocol)||u.username||u.password)throw Error();if(u.hostname===domains.find(d=>d.id===domainId)?.hostname){setError('Tujuan pada domain shortlink sendiri berpotensi membuat loop. Pilih domain tujuan lain.');return}destination=u.href}
    catch{setError('Masukkan URL HTTP/HTTPS valid tanpa username/password.');return}
    let configured:LinkRouting|undefined;
    if(routing.fallback.trim()){
      try{const u=new URL(routing.fallback.trim());if(!['http:','https:'].includes(u.protocol)||u.username||u.password||u.hostname.toLowerCase().replace(/\.$/,'')===domains.find(d=>d.id===domainId)?.hostname)throw Error();configured={...routing,...(routing.proxyCheck?{ipQualityBlock:routing.ipQualityBlock??DEFAULT_IP_FLAGS}:{}),fallback:u.href}}catch{setError('Fallback harus URL HTTP/HTTPS valid di luar domain shortlink.');return}
    }else if(!existing||existing.routing||routing.proxyCheck||Object.entries(routing).some(([key,value])=>['country','device','os','browser'].includes(key)&&value!=='*')){setError('Isi URL fallback untuk pengunjung yang tidak sesuai, termasuk bot.');return}
    setError('');setBusy(true);
    try{if(existing){
      await onSave(existing.state==='published'?{...existing,...(configured?{draftRouting:configured}:{}),draftDestination:destination===existing.destination?undefined:destination}:{...existing,destination,...(configured?{routing:configured}:{})});
    }else await onSave({id:crypto.randomUUID(),domainId,slug:s,destination,...(configured?{routing:configured}:{}),state:'draft',revision:0,clicks:0});
    }catch(err){setError(err instanceof Error?err.message:'Gagal menyimpan link.')}finally{setBusy(false)}
  }
  return <Modal title={existing?'Edit tujuan shortlink':'Buat shortlink'} onClose={()=>{if(!busy)onClose()}}><form onSubmit={submit}>
    <Field label="Domain"><select disabled={!!existing} value={domainId} onChange={e=>setDomainId(e.target.value)}>{domains.map(d=><option key={d.id} value={d.id}>{d.hostname}</option>)}</select></Field>
    <Field label="Slug" hint={existing?'URL shortlink yang dibagikan tetap sama.':'Kosongkan untuk membuat slug otomatis.'}><input disabled={!!existing} autoFocus={!existing} value={slug} onChange={e=>setSlug(e.target.value)} placeholder="promo-oktober"/></Field>
    <Field label="URL tujuan"><input autoFocus={!!existing} value={url} onChange={e=>setUrl(e.target.value)} placeholder="https://websiteanda.com/promo" required/></Field>
    <Field label="URL fallback" hint="Pengunjung yang tidak sesuai aturan dan bot/crawler diarahkan ke sini."><input required={!existing||!!existing.routing} value={routing.fallback} onChange={e=>setRouting({...routing,fallback:e.target.value})} placeholder="https://websiteanda.com/alternatif"/></Field>
    <details open={routing.proxyCheck||Object.entries(routing).some(([key,value])=>['country','device','os','browser'].includes(key)&&value!=='*')}><summary>Atur pengunjung tujuan utama</summary><p className="small muted">Semua kondisi harus cocok. Bot tetap menuju fallback. Aturan domain dan global juga berlaku.</p>
    <Field label="Negara yang diizinkan"><select value={routing.country} onChange={e=>setRouting({...routing,country:e.target.value})}><option value="*">Semua negara</option>{COUNTRY_CODES.map(c=><option key={c} value={c}>{new Intl.DisplayNames(['id'],{type:'region'}).of(c)}</option>)}<option value="unknown">Tidak diketahui</option></select></Field>
    <Field label="Perangkat yang diizinkan"><select value={routing.device} onChange={e=>setRouting({...routing,device:e.target.value as LinkRouting['device']})}><option value="*">Semua perangkat</option>{['mobile','tablet','desktop','unknown'].map(c=><option key={c}>{c}</option>)}</select></Field>
    <Field label="OS yang diizinkan"><select value={routing.os} onChange={e=>setRouting({...routing,os:e.target.value as LinkRouting['os']})}><option value="*">Semua OS</option>{OS_OPTIONS.map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></Field>
    <Field label="Browser yang diizinkan"><select value={routing.browser} onChange={e=>setRouting({...routing,browser:e.target.value as LinkRouting['browser']})}><option value="*">Semua browser</option>{BROWSER_OPTIONS.map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></Field><label className="checkbox"><input type="checkbox" checked={routing.proxyCheck??false} onChange={e=>setRouting({...routing,proxyCheck:e.target.checked,proxyUnknownFallback:routing.proxyUnknownFallback??true})}/> Periksa IP dengan BEON-IPQuality</label>{routing.proxyCheck&&<><fieldset><legend>Flag IP yang diarahkan ke fallback</legend><p className="small muted">Centang untuk menolak flag yang true. Flag yang tidak dicentang tidak menolak akses; aturan pengunjung lainnya tetap berlaku.</p>{IP_FLAGS.map(flag=><label className="checkbox" key={flag}><input type="checkbox" checked={(routing.ipQualityBlock??DEFAULT_IP_FLAGS).includes(flag)} onChange={e=>{const selected=routing.ipQualityBlock??DEFAULT_IP_FLAGS;setRouting({...routing,ipQualityBlock:e.target.checked?[...selected,flag]:selected.filter(v=>v!==flag)})}}/> Tolak {flag}</label>)}</fieldset><label className="checkbox"><input type="checkbox" checked={routing.proxyUnknownFallback??false} onChange={e=>setRouting({...routing,proxyUnknownFallback:e.target.checked})}/> Arahkan hasil IP yang belum diketahui ke fallback</label><p className="small muted">Konfigurasikan URL dan key BEON-IPQuality pada Sistem. Satu flag terpilih yang true sudah cukup menuju fallback. Skor/risk level tidak digunakan sebagai flag. API gagal mengikuti pilihan unknown di atas.</p></>}</details>
    {existing?.state==='published'&&<p className="small muted">Tujuan published saat ini: <code>{existing.destination}</code></p>}
    <div className="notice">{existing?.state==='published'?'Tujuan, fallback dan pengunjung disimpan sebagai draft. Publish untuk menerapkan semuanya.':'Tujuan utama untuk pengunjung yang cocok; selain itu ke fallback. Simpan draft lalu publish setelah domain siap.'}</div>
    {error&&<p className="error" role="alert">{error}</p>}<div className="actions form-actions"><button type="button" disabled={busy} onClick={onClose}>Batal</button><button className="primary" disabled={!domainId||busy}>{busy?'Menyimpan…':'Simpan draft link'}</button></div>
  </form></Modal>;
}
