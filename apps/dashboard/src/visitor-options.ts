import type { OperatingSystem, BrowserFamily } from '../../../packages/contracts/src';

export const OS_OPTIONS: {value:OperatingSystem;label:string}[] = [
  {value:'ios',label:'iOS / iPadOS · iPhone / iPad'},
  {value:'android',label:'Android'},
  {value:'windows',label:'Windows'},
  {value:'macos',label:'macOS · Mac'},
  {value:'linux',label:'Linux'},
  {value:'chromeos',label:'ChromeOS'},
  {value:'other',label:'OS lainnya'},
  {value:'unknown',label:'Unknown · OS tidak diketahui'},
];
export const BROWSER_OPTIONS: {value:BrowserFamily;label:string}[] = [
  {value:'chrome',label:'Chrome'},
  {value:'safari',label:'Safari'},
  {value:'firefox',label:'Firefox'},
  {value:'edge',label:'Edge'},
  {value:'samsung_internet',label:'Samsung Internet'},
  {value:'opera',label:'Opera'},
  {value:'other',label:'Browser lainnya'},
  {value:'unknown',label:'Unknown · browser tidak diketahui'},
];
export const osLabel=(os:OperatingSystem|'*')=>os==='*'?'Semua OS':OS_OPTIONS.find(o=>o.value===os)?.label??'Unknown';
export const browserLabel=(browser:BrowserFamily|'*')=>browser==='*'?'Semua browser':BROWSER_OPTIONS.find(b=>b.value===browser)?.label??'Unknown';
