import { cloneElement, useEffect, useId, useRef, type ReactElement, type ReactNode } from 'react';
import { X, ArrowUpRight } from 'lucide-react';

export function Badge({children,tone='neutral'}:{children:ReactNode;tone?:string}) {return <span className={`badge ${tone}`}><span className="badge-dot"/>{children}</span>}
export function Heading({eyebrow,title,description,actions}:{eyebrow:string;title:string;description:string;actions?:ReactNode}) {return <div className="heading"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p className="muted">{description}</p></div><div className="actions">{actions}</div></div>}
export function Panel({title,caption,children,action}:{title:string;caption?:string;children:ReactNode;action?:ReactNode}) {return <section className="panel"><div className="panel-head"><div><h2>{title}</h2>{caption&&<p className="muted small">{caption}</p>}</div>{action}</div>{children}</section>}
export function Empty({title,description,children}:{title:string;description:string;children?:ReactNode}) {return <div className="empty"><ArrowUpRight size={28}/><h3>{title}</h3><p className="muted">{description}</p>{children}</div>}
export function Modal({title,onClose,children}:{title:string;onClose:()=>void;children:ReactNode}) {
  const ref=useRef<HTMLDialogElement>(null);const titleId=useId();
  useEffect(()=>{const trigger=document.activeElement;const el=ref.current;el?.showModal();return()=>{el?.close();if(trigger instanceof HTMLElement&&trigger.isConnected)trigger.focus()}},[]);
  return <dialog ref={ref} onCancel={e=>{e.preventDefault();onClose()}} onKeyDown={e=>{
    if(e.key!=='Tab')return;
    const items=Array.from(e.currentTarget.querySelectorAll<HTMLElement>('button, a[href], input, select, textarea, [tabindex]')).filter(el=>el.tabIndex>=0&&!el.matches(':disabled')&&el.getClientRects().length>0);
    const first=items[0],last=items[items.length-1];
    if(!first){e.preventDefault();e.currentTarget.focus();return}
    if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus()}
    else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus()}
  }} aria-labelledby={titleId}><div className="modal-head"><h2 id={titleId}>{title}</h2><button className="icon-button" aria-label="Tutup dialog" onClick={onClose}><X size={20}/></button></div>{children}</dialog>;
}
export function Field({label,children,hint}:{label:string;children:ReactNode;hint?:string}) {
  const id=useId();
  return <div className="field"><label htmlFor={id}>{label}</label>{cloneElement(children as ReactElement<{id:string;'aria-describedby'?:string}>,{id,'aria-describedby':hint?`${id}-hint`:undefined})}{hint&&<small id={`${id}-hint`} className="muted">{hint}</small>}</div>;
}
