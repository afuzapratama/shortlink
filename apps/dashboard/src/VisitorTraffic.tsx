import { useState } from 'react';
import type { TrafficEvent } from '../../../packages/contracts/src';
import { events } from './fixtures';
import { Badge, Empty, Field, Heading, Modal, Panel } from './components';
import { OS_OPTIONS, BROWSER_OPTIONS, osLabel, browserLabel } from './visitor-options';

const time=(v:string)=>new Intl.DateTimeFormat('id-ID',{timeZone:'Asia/Jakarta',hour:'2-digit',minute:'2-digit',second:'2-digit'}).format(new Date(v));
export function TrafficPage({domainName}:{domainName:(id:string)=>string}){
  const [action,setAction]=useState('all'),[os,setOs]=useState('all'),[browser,setBrowser]=useState('all'),[device,setDevice]=useState('all');
  const [page,setPage]=useState(0),[selected,setSelected]=useState<TrafficEvent|null>(null);
  const filtered=events.filter(e=>(action==='all'||e.action===action)&&(os==='all'||e.os===os)&&(browser==='all'||e.browser===browser)&&(device==='all'||e.device===device));
  return <><Heading eyebrow="VISITOR INTELLIGENCE" title="Traffic & Log" description="Telusuri keputusan, OS, browser, dan perangkat yang terdeteksi."/>
    <Panel title="Log keputusan" caption="24 event contoh · watermark 11.32 WIB, 10 Oktober 2026">
      <div className="visitor-filters">
        <Field label="Filter tindakan"><select value={action} onChange={e=>{setAction(e.target.value);setPage(0)}}><option value="all">Semua tindakan</option><option value="redirect">Redirect</option><option value="block">Blokir</option><option value="challenge">Challenge</option></select></Field>
        <Field label="Filter OS"><select value={os} onChange={e=>{setOs(e.target.value);setPage(0)}}><option value="all">Semua OS</option>{OS_OPTIONS.map(o=><option key={o.value} value={o.value}>{o.label}</option>)}</select></Field>
        <Field label="Filter browser"><select value={browser} onChange={e=>{setBrowser(e.target.value);setPage(0)}}><option value="all">Semua browser</option>{BROWSER_OPTIONS.map(b=><option key={b.value} value={b.value}>{b.label}</option>)}</select></Field>
        <Field label="Filter perangkat"><select value={device} onChange={e=>{setDevice(e.target.value);setPage(0)}}><option value="all">Semua perangkat</option>{['mobile','tablet','desktop','unknown'].map(d=><option key={d}>{d}</option>)}</select></Field>
      </div>
      <div className="table-scroll"><table><thead><tr><th>Waktu WIB</th><th>Domain</th><th>Negara / perangkat</th><th>OS / browser</th><th>Tindakan</th><th>Alasan</th><th>Detail</th></tr></thead><tbody>{filtered.slice(page*8,page*8+8).map(e=><tr key={e.eventId}>
        <td>{time(e.occurredAt)}</td><td><strong>{domainName(e.domainId)}</strong><small>{e.source==='php'?'API · PHP':'Lokal · Go'}</small></td><td>{e.country} · {e.device}</td>
        <td><strong>{osLabel(e.os)}</strong><small>{browserLabel(e.browser)}</small></td><td><Badge tone={e.action==='block'?'amber':'green'}>{e.action}</Badge></td><td><code>{e.reasonCode}</code></td><td><button onClick={()=>setSelected(e)}>Lihat</button></td>
      </tr>)}</tbody></table></div>
      {!filtered.length&&<Empty title="Tidak ada event" description="Ubah filter untuk melihat log contoh."/>}
      <div className="pagination"><span>{filtered.length} event · halaman {page+1}</span><div className="actions"><button disabled={page===0} onClick={()=>setPage(page-1)}>Sebelumnya</button><button disabled={(page+1)*8>=filtered.length} onClick={()=>setPage(page+1)}>Berikutnya</button></div></div>
    </Panel>
    {selected&&<Modal title="Rincian keputusan" onClose={()=>setSelected(null)}><dl className="details">
      <dt>Event</dt><dd>{selected.eventId}</dd><dt>Action / reason</dt><dd>{selected.action} / {selected.reasonCode}</dd><dt>Rule</dt><dd>{selected.ruleId}</dd><dt>Config revision</dt><dd>v{selected.configRevision}</dd>
      <dt>Jenis perangkat</dt><dd>{selected.device}</dd><dt>Sistem operasi</dt><dd>{osLabel(selected.os)}{selected.osVersion?` ${selected.osVersion}`:''}</dd><dt>Browser</dt><dd>{browserLabel(selected.browser)}{selected.browserVersion?` ${selected.browserVersion}`:''}</dd>
      <dt>Merek perangkat</dt><dd>{selected.deviceBrand??'Unknown · tidak tersedia'}</dd><dt>Model perangkat</dt><dd>{selected.deviceModel??'Unknown · tidak tersedia'}</dd>
      <dt>Geo release</dt><dd>{selected.geoRelease}</dd><dt>Feed</dt><dd>{selected.feedVersion}</dd><dt>Tujuan</dt><dd>{selected.destination??'Tidak ada redirect'}</dd>
    </dl><p className="small muted">Model/versi hanya dicatat bila tersedia. Data ini contoh sintetis, bukan hasil deteksi browser Anda. Respons redirect tidak membuktikan situs tujuan berhasil dibuka.</p></Modal>}
  </>;
}
