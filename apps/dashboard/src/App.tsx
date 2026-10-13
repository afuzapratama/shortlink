import {lazy,Suspense} from 'react';
import {LiveApp} from './LiveApp';
// The production/staging build removes this branch and the entire demo module.
const Prototype=__DASHBOARD_DEMO__?lazy(()=>import('./PrototypeApp').then(m=>({default:m.PrototypeApp}))):null;
export function App(){
 if(Prototype&&new URLSearchParams(location.search).get('demo')==='1')return <Suspense fallback={<p>Memuat preview…</p>}><Prototype/></Suspense>;
 return <LiveApp/>;
}
