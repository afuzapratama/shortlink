import { PrototypeApp } from './PrototypeApp';
import { LiveApp } from './LiveApp';
export function App(){return new URLSearchParams(location.search).get('demo')==='1'?<PrototypeApp/>:<LiveApp/>;}
