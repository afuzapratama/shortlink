import { useState, type FormEvent } from 'react';
import { Copy, Pencil, ShieldCheck } from 'lucide-react';
import type { Domain, Shortlink } from '../../../packages/contracts/src';
import { Badge, Empty, Field, Modal, Panel } from './components';

export function LinkList({domains,links,search,setSearch,copy,onEdit,onPublish,onRules,onHistory,live=false,policyReady=false}:{
  domains:Domain[];links:Shortlink[];search:string;setSearch:(v:string)=>void;live?:boolean;policyReady?:boolean;onHistory?:(l:Shortlink)=>void;
  copy:(v:string)=>void;onEdit:(l:Shortlink)=>void;onPublish:(l:Shortlink)=>void;onRules:(l:Shortlink)=>void;
}){
  const hostname=(id:string)=>domains.find(d=>d.id===id)?.hostname??'Domain tidak ditemukan';
  const visible=links.filter(l=>(l.slug+l.destination+(l.draftDestination??'')).toLowerCase().includes(search.toLowerCase()));
  return <Panel title="Daftar link" caption="Edit tujuan tanpa mengubah domain atau slug. Tujuan published tetap berlaku hingga draft dipublish.">
    <div className="toolbar"><input aria-label="Cari slug atau tujuan" placeholder="Cari slug atau tujuan…" value={search} onChange={e=>setSearch(e.target.value)}/><Badge>{links.length} link</Badge></div>
    <div className="table-scroll"><table><thead><tr><th>Shortlink / tujuan</th><th>Status</th><th>Revision</th><th>{live?'Redirect terbit':'Redirect contoh'}</th><th>Aksi</th></tr></thead><tbody>{visible.map(l=><tr key={l.id}>
      <td><strong>{hostname(l.domainId)}/{l.slug}</strong><small>{l.state==='published'?'Published: ':''}{l.destination}</small>{l.draftDestination!==undefined&&<small className="pending-destination">Draft: {l.draftDestination}</small>}</td>
      <td><Badge tone={l.state==='published'?'green':'neutral'}>{l.state==='draft'?'Draft':'Published'}</Badge>{l.draftDestination!==undefined&&<div className="draft-indicator"><Badge tone="amber">Perubahan draft</Badge></div>}</td>
      <td>v{l.revision}</td><td>{new Intl.NumberFormat('id-ID').format(l.clicks)}</td>
      <td><div className="actions"><button className="icon-button" aria-label={`Salin ${l.slug}`} onClick={()=>copy(`https://${hostname(l.domainId)}/${l.slug}`)}><Copy size={16}/></button><button aria-label={`Edit tujuan ${l.slug}`} onClick={()=>onEdit(l)}><Pencil size={14}/> Edit</button><button disabled={live&&!policyReady} title={live&&!policyReady?'Aturan belum tersedia':undefined} aria-label={`Aturan ${l.slug}`} onClick={()=>onRules(l)}><ShieldCheck size={14}/> Aturan</button>{(l.state==='draft'||l.draftDestination!==undefined)&&<button disabled={live&&(!policyReady||!['preparing','ready','attention'].includes(domains.find(d=>d.id===l.domainId)?.readiness??''))} title={live?'Memerlukan domain terverifikasi dan agent lokal':undefined} onClick={()=>onPublish(l)}>{live?(policyReady?'Publish':'Publish belum tersedia'):'Publish demo'}</button>}{onHistory&&l.state==='published'&&<button onClick={()=>onHistory(l)}>Riwayat</button>}</div></td>
    </tr>)}</tbody></table></div>{!visible.length&&<Empty title="Tidak ada link yang cocok" description={links.length?"Coba kata pencarian lain.":"Buat shortlink pertama setelah menambahkan domain."}/>}
  </Panel>;
}

export function LinkForm({domains,links,existing,onClose,onSave}:{domains:Domain[];links:Shortlink[];existing?:Shortlink;onClose:()=>void;onSave:(l:Shortlink)=>void|Promise<void>}){
  const [domainId,setDomainId]=useState(existing?.domainId??domains[0]?.id??'');
  const [slug,setSlug]=useState(existing?.slug??'');
  const [url,setUrl]=useState(existing?.draftDestination??existing?.destination??'');
  const [error,setError]=useState(''),[busy,setBusy]=useState(false);
  async function submit(e:FormEvent){
    e.preventDefault();if(busy)return;const s=slug.trim()||`link-${crypto.randomUUID().slice(0,6)}`;
    if(!/^[a-zA-Z0-9_-]{1,80}$/.test(s)){setError('Slug hanya boleh huruf, angka, tanda - dan _.');return}
    if(links.some(l=>l.id!==existing?.id&&l.domainId===domainId&&l.slug===s)){setError('Slug sudah dipakai pada domain ini.');return}
    let destination:string;
    try{const u=new URL(url.trim());if(!['http:','https:'].includes(u.protocol)||u.username||u.password)throw Error();if(u.hostname===domains.find(d=>d.id===domainId)?.hostname){setError('Tujuan pada domain shortlink sendiri berpotensi membuat loop. Pilih domain tujuan lain.');return}destination=u.href}
    catch{setError('Masukkan URL HTTP/HTTPS valid tanpa username/password.');return}
    setError('');setBusy(true);
    try{if(existing){
      await onSave(existing.state==='published'?{...existing,draftDestination:destination===existing.destination?undefined:destination}:{...existing,destination});
    }else await onSave({id:crypto.randomUUID(),domainId,slug:s,destination,state:'draft',revision:0,clicks:0});
    }catch(err){setError(err instanceof Error?err.message:'Gagal menyimpan link.')}finally{setBusy(false)}
  }
  return <Modal title={existing?'Edit tujuan shortlink':'Buat shortlink'} onClose={()=>{if(!busy)onClose()}}><form onSubmit={submit}>
    <Field label="Domain"><select disabled={!!existing} value={domainId} onChange={e=>setDomainId(e.target.value)}>{domains.map(d=><option key={d.id} value={d.id}>{d.hostname}</option>)}</select></Field>
    <Field label="Slug" hint={existing?'URL shortlink yang dibagikan tetap sama.':'Kosongkan untuk membuat slug otomatis.'}><input disabled={!!existing} autoFocus={!existing} value={slug} onChange={e=>setSlug(e.target.value)} placeholder="promo-oktober"/></Field>
    <Field label="URL tujuan"><input autoFocus={!!existing} value={url} onChange={e=>setUrl(e.target.value)} placeholder="https://websiteanda.com/promo" required/></Field>
    {existing?.state==='published'&&<p className="small muted">Tujuan published saat ini: <code>{existing.destination}</code></p>}
    <div className="notice">{existing?.state==='published'?'Perubahan disimpan sebagai draft. Publish untuk menerapkan tujuan baru. Aturan yang memakai URL tujuan khusus tetap perlu diedit terpisah.':'Disimpan sebagai draft. Publish baru tersedia setelah domain siap.'}</div>
    {error&&<p className="error" role="alert">{error}</p>}<div className="actions form-actions"><button type="button" disabled={busy} onClick={onClose}>Batal</button><button className="primary" disabled={!domainId||busy}>{busy?'Menyimpan…':'Simpan draft link'}</button></div>
  </form></Modal>;
}
