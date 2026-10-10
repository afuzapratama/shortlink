import type { ApiError, Page } from '../../../packages/contracts/src';
const messages:Record<string,string>={
 rotation_pending:'Agent sudah mempunyai rotasi yang masih menunggu acknowledgment.',
 factor_invalid:'Password atau kode tidak valid, kedaluwarsa, atau sudah dipakai.',
 invalid_filter:'Filter atau periode tidak valid. Gunakan batas menit dalam retensi data.',invalid_cursor:'Halaman sudah tidak sesuai filter. Muat ulang log.',
 artifact_unavailable:'Belum ada rilis agent yang tersedia. Publisher perlu menyiapkan paket terlebih dahulu.',
 capability_missing:'Agent lokal membutuhkan dukungan B13 dan heartbeat terbaru. Mode API dievaluasi pusat; challenge belum tersedia.',domain_not_ready:'Verifikasi domain dan enroll agent sesuai metode instalasi sebelum publish.',rule_set_conflict:'Konfigurasi untuk cakupan ini sudah ada.',
 unauthorized:'Email/password salah atau sesi berakhir. Silakan masuk kembali.',
 forbidden:'Permintaan ditolak. Periksa sesi dan alamat dashboard.',
 domain_conflict:'Domain sudah terdaftar.',slug_conflict:'Slug sudah dipakai pada domain ini.',
 revision_conflict:'Konfigurasi sudah berubah. Muat ulang data dan buka kembali editor sebelum menyimpan.',
 not_found:'Domain atau shortlink tidak ditemukan.',rate_limited:'Terlalu banyak percobaan login. Coba lagi setelah 15 menit.',
 control_unavailable:'Server pusat belum tersedia. Periksa koneksi lalu coba lagi.',
 not_implemented:'Fitur ini belum tersedia.',validation_failed:'Data tidak valid. Periksa kondisi, prioritas, slug, dan URL tujuan.',
};
export class APIError extends Error {constructor(public status:number,public code:string,public requestId?:string){super(messages[code]??'Permintaan gagal. Silakan coba kembali.')}}
let csrf='';
export function setCSRF(value:string){csrf=value}
export async function api<T>(path:string,method='GET',body?:unknown):Promise<T>{
 let response:Response;
 try{response=await fetch(path,{method,credentials:'same-origin',headers:{...(body!==undefined?{'Content-Type':'application/json'}:{}),...(method!=='GET'&&csrf?{'X-CSRF-Token':csrf}:{})},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(12000)})}
 catch{throw new APIError(503,'control_unavailable')}
 let value:unknown;try{value=await response.json()}catch{throw new APIError(response.status>=400?response.status:503,'control_unavailable')}
 if(!response.ok){const error=(value as ApiError)?.error;throw new APIError(response.status,error?.code??'control_unavailable',error?.requestId)}
 return value as T;
}
export async function allPages<T>(path:string):Promise<T[]>{
 const result:T[]=[];let cursor:string|null=null;
 do{const page:Page<T>=await api(`${path}${path.includes('?')?'&':'?'}limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`);result.push(...page.items);cursor=page.nextCursor}while(cursor);
 return result;
}
export async function exportTraffic(path:string):Promise<{blob:Blob;truncated:boolean}>{
 let r:Response;try{r=await fetch(path,{method:'POST',credentials:'same-origin',headers:{'X-CSRF-Token':csrf},signal:AbortSignal.timeout(12000)})}catch{throw new APIError(503,'control_unavailable')}
 if(!r.ok){let e:ApiError|undefined;try{e=await r.json()}catch{}throw new APIError(r.status,e?.error.code??'control_unavailable')}
 return {blob:await r.blob(),truncated:r.headers.get('X-Export-Truncated')==='true'};
}
