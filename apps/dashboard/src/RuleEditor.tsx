import { useState, type FormEvent } from 'react';
import { ArrowRight, Plus } from 'lucide-react';
import type { Domain, Shortlink, ScopedRule, Rule, RuleBinding, RuleScope, Device, OperatingSystem, BrowserFamily } from '../../../packages/contracts/src';
import { applicableRules, simulate } from './fixtures';
import { OS_OPTIONS, BROWSER_OPTIONS, osLabel, browserLabel } from './visitor-options';
import { Badge, Empty, Field, Heading, Modal, Panel } from './components';

export function RuleForm({domains,links,binding,existing,onClose,onSave,live=false,countries=["ID","SG"],initialPriority=50}:{domains:Domain[];links:Shortlink[];binding:RuleBinding;existing?:ScopedRule;onClose:()=>void;onSave:(r:ScopedRule)=>void|Promise<void>;live?:boolean;countries?:string[];initialPriority?:number}){
  const [scope,setScope]=useState<RuleScope>(existing?.scope??binding.scope);
  const [scopeId,setScopeId]=useState(existing?.scopeId??binding.scopeId??'');
  const [name,setName]=useState(existing?.name??'');
  const [country,setCountry]=useState(existing?.country??'*');
  const [device,setDevice]=useState<Rule['device']>(existing?.device??'*');
  const [os,setOs]=useState<Rule['os']>(existing?.os??'*');
  const [browser,setBrowser]=useState<Rule['browser']>(existing?.browser??'*');
  const [asn,setASN]=useState(existing?.asn?.toString()??'');
  const [risk,setRisk]=useState<Rule['risk']>(existing?.risk??'*'),[botCategory,setBotCategory]=useState<NonNullable<Rule['botCategory']>>(existing?.botCategory??'*');
  const [action,setAction]=useState<Rule['action']>(existing?.action??'block');
  const [source,setSource]=useState<'link'|'custom'>(existing?.destinationSource??'link');
  const [destination,setDestination]=useState(existing?.destination??'');
  const [error,setError]=useState(''),[busy,setBusy]=useState(false),[priority,setPriority]=useState(existing?.priority??initialPriority),[mandatory,setMandatory]=useState(existing?.mandatory??false);
  async function submit(e:FormEvent){
    e.preventDefault();if(busy)return;
    if(scope!=='global'&&!(scope==='domain'?domains:links).some(x=>x.id===scopeId)){setError('Pilih target cakupan yang valid.');return}
    if((action==='redirect'||action==='fallback')&&source==='custom'){
      try{const u=new URL(destination.trim());if(!['http:','https:'].includes(u.protocol)||u.username||u.password)throw Error()}catch{setError('Tujuan harus berupa URL HTTP/HTTPS tanpa kredensial.');return}
    }
    setBusy(true);setError('');try{await onSave({id:existing?.id??crypto.randomUUID(),scope,scopeId:scope==='global'?null:scopeId,name:name.trim(),priority,...(asn?{asn:Number(asn)}:{}),country,device,os,browser,risk,...(live?{botCategory:risk==='known_bot'?botCategory:'*'}:{}),action,enabled:existing?.enabled??true,mandatory:scope==='global'&&action==='block'&&risk==='*'&&mandatory,
      ...((action==='redirect'||action==='fallback')?{destinationSource:source,...(source==='custom'?{destination:destination.trim()}:{})}:{}),
    });}catch(e){setError(e instanceof Error?e.message:"Gagal menyimpan aturan.")}finally{setBusy(false)}
  }
  return <Modal title={existing?'Edit aturan draft':'Tambah aturan draft'} onClose={()=>{if(!busy)onClose()}}><form onSubmit={submit}>
    <Field label="Cakupan aturan"><select disabled={live} value={scope} onChange={e=>{const v=e.target.value as RuleScope;setScope(v);setScopeId(v==='domain'?domains[0]?.id??'':v==='link'?links[0]?.id??'':'')}}><option value="global">Global · seluruh domain/link</option><option value="domain">Domain · semua link pada domain</option><option value="link">Shortlink · satu link tertentu</option></select></Field>
    {scope==='domain'&&<Field label="Target domain"><select required value={scopeId} onChange={e=>setScopeId(e.target.value)}><option value="">Pilih domain</option>{domains.map(d=><option value={d.id} key={d.id}>{d.hostname}</option>)}</select></Field>}
    {scope==='link'&&<Field label="Target shortlink"><select required value={scopeId} onChange={e=>setScopeId(e.target.value)}><option value="">Pilih shortlink</option>{links.map(l=><option value={l.id} key={l.id}>{domains.find(d=>d.id===l.domainId)?.hostname}/{l.slug}</option>)}</select></Field>}
    <Field label="Nama aturan"><input autoFocus required value={name} onChange={e=>setName(e.target.value)}/></Field>
    <Field label="Negara"><select value={country} onChange={e=>setCountry(e.target.value)}><option value="*">Semua negara</option>{countries.map(code=><option key={code} value={code}>{new Intl.DisplayNames(['id'],{type:'region'}).of(code)??code}</option>)}<option value="unknown">Tidak diketahui</option></select></Field>
    <Field label="Perangkat"><select value={device} onChange={e=>setDevice(e.target.value as Rule['device'])}><option value="*">Semua perangkat</option><option value="mobile">Mobile</option><option value="desktop">Desktop</option><option value="tablet">Tablet</option><option value="unknown">Unknown</option></select></Field>
    <Field label="Sistem operasi"><select value={os} onChange={e=>setOs(e.target.value as Rule['os'])}><option value="*">Semua OS</option>{OS_OPTIONS.map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></Field>
    <Field label="Browser"><select value={browser} onChange={e=>setBrowser(e.target.value as Rule['browser'])}><option value="*">Semua browser</option>{BROWSER_OPTIONS.map(b=><option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
    <Field label="Sinyal bot"><select value={risk} onChange={e=>{setRisk(e.target.value as Rule['risk']);setBotCategory('*')}}><option value="*">Semua request</option><option value="known_bot">Pola bot/crawler</option><option value="suspected_automation" disabled={!live}>Sinyal otomatisasi</option><option value="tor_exit" disabled={!live}>Exit Tor terdaftar</option><option value="reputation_flag" disabled={!live}>Reputasi IP terdaftar</option><option value="rate_limited" disabled={!live}>Batas laju lokal</option></select></Field>
    {live&&risk==='known_bot'&&<Field label="Kategori bot"><select value={botCategory} onChange={e=>setBotCategory(e.target.value as NonNullable<Rule['botCategory']>)}><option value="*">Semua kategori</option><option value="search">Crawler pencarian (UA)</option><option value="preview">Link preview (UA)</option><option value="automation">Otomatisasi lain</option><option value="unknown">Kategori belum diketahui</option></select></Field>}
 <Field label="Tindakan"><select value={action} onChange={e=>setAction(e.target.value as Rule['action'])}><option value="block">Blokir</option><option value="redirect">Redirect</option><option value="fallback">Fallback</option><option value="challenge" disabled>Challenge · provider belum tersedia</option></select></Field>
    {(action==='redirect'||action==='fallback')&&<><Field label="Sumber URL tujuan"><select value={source} onChange={e=>setSource(e.target.value as 'link'|'custom')}><option value="link">Ikuti tujuan shortlink yang dipublish</option><option value="custom">URL tujuan khusus aturan ini</option></select></Field>{source==='custom'&&<Field label="URL tujuan khusus"><input required value={destination} onChange={e=>setDestination(e.target.value)}/></Field>}</>}
    <Field label="ASN opsional"><input type="number" min="1" max="4294967295" value={asn} onChange={e=>setASN(e.target.value)} placeholder="Semua ASN"/></Field>
    <Field label="Prioritas"><input type="number" min="1" max="1000" required value={priority} onChange={e=>setPriority(Number(e.target.value))}/></Field>
    {scope==='global'&&action==='block'&&risk==='*'&&<label className="checkbox"><input type="checkbox" checked={mandatory} onChange={e=>setMandatory(e.target.checked)}/> Blokir wajib global</label>}
    <div className="notice">Aturan hanya berlaku pada cakupannya. Simulator menggabungkan global, domain terpilih, dan shortlink terpilih berdasarkan prioritas.</div>
    {error&&<p role="alert" className="error">{error}</p>}<div className="actions form-actions"><button type="button" disabled={busy} onClick={onClose}>Batal</button><button className="primary" disabled={busy}>{existing?'Simpan draft aturan':'Tambah draft'}</button></div>
  </form></Modal>;
}

export function RulePage({domains,links,rules,setRules,context,onAdd,onEdit,notify}:{domains:Domain[];links:Shortlink[];rules:ScopedRule[];setRules:(r:ScopedRule[])=>void;context:RuleBinding;onAdd:(b:RuleBinding)=>void;onEdit:(r:ScopedRule)=>void;notify:(s:string)=>void}){
  const [scope,setScope]=useState<RuleScope>(context.scope),[scopeId,setScopeId]=useState(context.scopeId??'');
  const [linkId,setLinkId]=useState(context.scope==='link'?context.scopeId??links[0]?.id??'':links.find(l=>context.scope!=='domain'||l.domainId===context.scopeId)?.id??'');
  const [country,setCountry]=useState('ID'),[device,setDevice]=useState<Device>('mobile'),[bot,setBot]=useState(false);
  const [os,setOs]=useState<OperatingSystem>('android'),[browser,setBrowser]=useState<BrowserFamily>('chrome');
  const [result,setResult]=useState<ReturnType<typeof simulate>|null>(null),[revision,setRevision]=useState(12);
  const scoped=rules.filter(r=>r.scope===scope&&(scope==='global'||r.scopeId===scopeId));
  const selectedLink=links.find(l=>l.id===linkId);
  const merged=selectedLink?applicableRules(selectedLink,rules):[];
  const nameOf=(r:ScopedRule)=>r.scope==='global'?'Global':r.scope==='domain'?domains.find(d=>d.id===r.scopeId)?.hostname:links.find(l=>l.id===r.scopeId)?.slug;
  function setScopeTarget(v:RuleScope){setScope(v);setScopeId(v==='domain'?domains[0]?.id??'':v==='link'?links[0]?.id??'':'');if(v==='domain')setLinkId(links.find(l=>l.domainId===domains[0]?.id)?.id??'');if(v==='link')setLinkId(links[0]?.id??'');setResult(null)}
  function publish(){
    if(scope!=='global'&&!scopeId){notify('Pilih target aturan sebelum publish.');return}
    if(new Set(scoped.map(r=>r.priority)).size!==scoped.length){notify('Publish ditolak: prioritas aturan harus unik dalam cakupan ini.');return}
    setRevision(revision+1);notify(`Draft ${scope==='global'?'global':scope==='domain'?'domain':'shortlink'} dipublish pada simulasi revision ${revision+1}; belum diterapkan agent nyata.`);
  }
  return <><Heading eyebrow="TRAFFIC POLICIES" title="Aturan Traffic" description="Aturan global menjadi dasar; pilih domain atau shortlink untuk aturan khusus." actions={<><button onClick={()=>onAdd({scope,scopeId:scope==='global'?null:scopeId})}><Plus size={16}/> Tambah aturan</button><button className="primary" onClick={publish}>Publish draft demo</button></>}/>
    <Panel title="Cakupan pengelolaan" caption="Hanya aturan milik cakupan ini yang diedit. Simulator tetap menyertakan aturan yang diwarisi."><div className="scope-fields"><Field label="Cakupan aturan"><select value={scope} onChange={e=>setScopeTarget(e.target.value as RuleScope)}><option value="global">Global</option><option value="domain">Domain</option><option value="link">Shortlink</option></select></Field>{scope==='domain'&&<Field label="Domain aturan"><select value={scopeId} onChange={e=>{setScopeId(e.target.value);setLinkId(links.find(l=>l.domainId===e.target.value)?.id??'');setResult(null)}}>{domains.map(d=><option key={d.id} value={d.id}>{d.hostname}</option>)}</select></Field>}{scope==='link'&&<Field label="Shortlink aturan"><select value={scopeId} onChange={e=>{setScopeId(e.target.value);setLinkId(e.target.value);setResult(null)}}>{links.map(l=><option key={l.id} value={l.id}>{domains.find(d=>d.id===l.domainId)?.hostname}/{l.slug}</option>)}</select></Field>}</div></Panel>
    <div className="detail-grid"><Panel title="Prioritas aturan" caption={`Draft cakupan ${scope} · contoh revision ${revision}. Kondisi dalam satu baris harus semuanya cocok.`}>
      {[...scoped].sort((a,b)=>a.priority-b.priority).map(r=><div className="rule-row" key={r.id}><label className="priority"><span className="sr-only">Prioritas {r.name}</span><input type="number" min="1" max="1000" value={r.priority} onChange={e=>{setRules(rules.map(x=>x.id===r.id?{...x,priority:Math.max(1,Math.min(1000,Number(e.target.value)||1))}:x));setResult(null)}}/></label><div className="rule-info"><strong>{r.name}</strong><small>{nameOf(r)} · {r.risk==='known_bot'?'Crawler':`${r.country==='*'?'Semua negara':r.country} · ${r.device==='*'?'Semua perangkat':r.device}`}</small><small>{osLabel(r.os)} · {browserLabel(r.browser)}</small>{(r.action==='redirect'||r.action==='fallback')&&<small>{r.destinationSource==='custom'?r.destination:'Ikuti tujuan shortlink'}</small>}</div><Badge tone={r.action==='block'?'amber':'green'}>{r.action}</Badge><button aria-label={`Edit aturan ${r.name}`} onClick={()=>onEdit(r)}>Edit</button><input type="checkbox" aria-label={`Aktifkan ${r.name}`} checked={r.enabled} onChange={e=>{setRules(rules.map(x=>x.id===r.id?{...x,enabled:e.target.checked}:x));setResult(null)}}/></div>)}
      {!scoped.length&&<Empty title="Belum ada aturan khusus" description="Aturan global dan domain tetap diwarisi. Tambahkan aturan untuk cakupan ini."/>}<p className="small muted">Blokir wajib global diperiksa dahulu. Lalu angka kecil dievaluasi dahulu. Prioritas harus unik dalam satu cakupan. Jika sama lintas cakupan, shortlink lalu domain lalu global menjadi tie-break.</p>
    </Panel><Panel title="Simulator keputusan" caption="Pilih shortlink agar simulasi memakai domain dan tujuan yang benar."><form onSubmit={e=>{e.preventDefault();if(selectedLink)setResult(simulate({country,device,os,browser,bot},rules,selectedLink))}}>
      <Field label="Shortlink simulasi"><select value={linkId} onChange={e=>{setLinkId(e.target.value);setResult(null)}}>{links.map(l=><option key={l.id} value={l.id}>{domains.find(d=>d.id===l.domainId)?.hostname}/{l.slug}</option>)}</select></Field>
      {selectedLink&&<p className="small muted simulator-context">{merged.length} aturan berlaku: global + domain + shortlink. Tujuan {selectedLink.state==='published'?'published':'draft awal'}: {selectedLink.destination}{selectedLink.draftDestination!==undefined?' · perubahan tujuan belum dipublish.':''}</p>}
      <Field label="Negara"><select value={country} onChange={e=>{setCountry(e.target.value);setResult(null)}}><option value="ID">Indonesia</option><option value="SG">Singapura</option><option value="unknown">Unknown</option></select></Field>
      <Field label="Perangkat"><select value={device} onChange={e=>{setDevice(e.target.value as Device);setResult(null)}}>{['mobile','desktop','tablet','unknown'].map(x=><option key={x}>{x}</option>)}</select></Field>
      <Field label="Sistem operasi"><select value={os} onChange={e=>{setOs(e.target.value as OperatingSystem);setResult(null)}}>{OS_OPTIONS.map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></Field>
      <Field label="Browser"><select value={browser} onChange={e=>{setBrowser(e.target.value as BrowserFamily);setResult(null)}}>{BROWSER_OPTIONS.map(b=><option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
      <label className="checkbox"><input type="checkbox" checked={bot} onChange={e=>{setBot(e.target.checked);setResult(null)}}/> Crawler dikenali</label><button className="primary wide" disabled={!selectedLink}>Jalankan simulasi <ArrowRight size={16}/></button>
    </form>{result&&<div className="simulation-result" role="status"><Badge tone={result.action==='block'?'amber':'green'}>{result.action}</Badge><h3>{result.name}</h3><p>Aturan cocok: {result.ruleId??'Tujuan dasar shortlink'}</p>{result.destination&&<code>{result.destination}</code>}</div>}</Panel></div>
  </>;
}
