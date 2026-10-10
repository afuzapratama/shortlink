import { cloneElement, useEffect, useId, useRef, type ReactElement, type ReactNode } from 'react';
import { X, ArrowUpRight } from 'lucide-react';

export function Badge({children,tone='neutral'}:{children:ReactNode;tone?:string}) {return <span className={`badge ${tone}`}><span className="badge-dot"/>{children}</span>}
export function Heading({eyebrow,title,description,actions}:{eyebrow:string;title:string;description:string;actions?:ReactNode}) {return <div className="heading"><div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1><p className="muted">{description}</p></div><div className="actions">{actions}</div></div>}
export function Panel({title,caption,children,action}:{title:string;caption?:string;children:ReactNode;action?:ReactNode}) {return <section className="panel"><div className="panel-head"><div><h2>{title}</h2>{caption&&<p className="muted small">{caption}</p>}</div>{action}</div>{children}</section>}
export function Empty({title,description,children}:{title:string;description:string;children?:ReactNode}) {return <div className="empty"><ArrowUpRight size={28}/><h3>{title}</h3><p className="muted">{description}</p>{children}</div>}
export function Modal({title,onClose,children}:{title:string;onClose:()=>void;children:ReactNode}) {
  const ref=useRef<HTMLDialogElement>(null);
  useEffect(()=>{ref.current?.showModal();const el=ref.current;return()=>el?.close()},[]);
  return <dialog ref={ref} onCancel={e=>{e.preventDefault();onClose()}} aria-labelledby="dialog-title"><div className="modal-head"><h2 id="dialog-title">{title}</h2><button className="icon-button" aria-label="Tutup dialog" onClick={onClose}><X size={20}/></button></div>{children}</dialog>;
}
export function Field({label,children,hint}:{label:string;children:ReactNode;hint?:string}) {
  const id=useId();
  return <div className="field"><label htmlFor={id}>{label}</label>{cloneElement(children as ReactElement<{id:string;'aria-describedby'?:string}>,{id,'aria-describedby':hint?`${id}-hint`:undefined})}{hint&&<small id={`${id}-hint`} className="muted">{hint}</small>}</div>;
}
