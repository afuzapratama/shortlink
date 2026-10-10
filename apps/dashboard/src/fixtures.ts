import type { Domain, ScopedRule, Shortlink, TrafficEvent, Device, OperatingSystem, BrowserFamily, Action } from '../../../packages/contracts/src';

export const domains: Domain[] = [
  { id: 'd1', hostname: 'go.nusa.example', mode: 'local', readiness: 'ready', connection: 'online', desiredRevision: 12, appliedRevision: 12, lastSeenAt: '2026-10-10T04:32:00Z', capabilities: {localGeo: true, persistentQueue: true, challenge: false} },
  { id: 'd2', hostname: 's.karya.example', mode: 'api', readiness: 'ready', connection: 'idle', desiredRevision: 8, appliedRevision: null, lastSeenAt: '2026-10-10T04:20:00Z', capabilities: {localGeo: false, persistentQueue: false, challenge: false} },
  { id: 'd3', hostname: 'link.studio.example', mode: 'local', readiness: 'attention', connection: 'offline', desiredRevision: 5, appliedRevision: 4, lastSeenAt: '2026-10-10T03:50:00Z', capabilities: {localGeo: true, persistentQueue: true, challenge: false} },
];
export const links: Shortlink[] = [
  {id:'l1',domainId:'d1',slug:'launch',destination:'https://nusa.example/launch',state:'published',revision:12,clicks:12480},
  {id:'l2',domainId:'d1',slug:'catalog',destination:'https://nusa.example/catalog',state:'published',revision:12,clicks:8192},
  {id:'l3',domainId:'d2',slug:'portfolio',destination:'https://karya.example/work',state:'published',revision:8,clicks:4630},
  {id:'l4',domainId:'d3',slug:'early-access',destination:'https://studio.example/join',state:'draft',revision:4,clicks:0},
];
export const rules: ScopedRule[] = [
  {id:'r1',scope:'global',scopeId:null,mandatory:true,name:'Crawler yang dikenali',priority:10,country:'*',device:'*',os:'*',browser:'*',risk:'known_bot',action:'block',enabled:true},
  {id:'r2',scope:'domain',scopeId:'d1',name:'Pengunjung mobile Indonesia',priority:20,country:'ID',device:'mobile',os:'android',browser:'*',risk:'*',action:'redirect',destinationSource:'custom',destination:'https://nusa.example/mobile',enabled:true},
  {id:'r4',scope:'domain',scopeId:'d1',name:'Pengunjung iOS Indonesia',priority:21,country:'ID',device:'*',os:'ios',browser:'*',risk:'*',action:'redirect',destinationSource:'custom',destination:'https://nusa.example/ios',enabled:true},
  {id:'r3',scope:'global',scopeId:null,name:'Tujuan umum',priority:100,country:'*',device:'*',os:'*',browser:'*',risk:'*',action:'redirect',destinationSource:'link',enabled:true},
];
const visitorProfiles: Pick<TrafficEvent,'device'|'os'|'browser'|'deviceBrand'|'deviceModel'>[] = [
  {device:'mobile',os:'android',browser:'chrome',deviceBrand:'Samsung',deviceModel:'SM-A546B'},
  {device:'mobile',os:'ios',browser:'safari',deviceBrand:'Apple',deviceModel:'iPhone'},
  {device:'tablet',os:'ios',browser:'safari',deviceBrand:'Apple',deviceModel:'iPad'},
  {device:'desktop',os:'windows',browser:'edge',deviceBrand:null,deviceModel:null},
  {device:'desktop',os:'macos',browser:'safari',deviceBrand:'Apple',deviceModel:null},
  {device:'unknown',os:'unknown',browser:'unknown',deviceBrand:null,deviceModel:null},
];
export const events: TrafficEvent[] = Array.from({length:24},(_,i) => {
  const profile=visitorProfiles[i%visitorProfiles.length];
  const link=links.find(l=>l.id===(i%3===0?'l3':'l1'))!;
  const country=i%4===0?'SG':'ID';
  const decision=simulate({country,...profile,bot:i%5===0},rules,link);
  return {
    eventId:`example-event-${i}`, occurredAt:`2026-10-10T04:${String(32-Math.floor(i/3)).padStart(2,'0')}:${String(55-i%3*15).padStart(2,'0')}Z`,
    domainId:link.domainId,linkId:link.id,country,...profile,source:link.domainId==='d2'?'php':'go',
    action:decision.action,reasonCode:decision.action==='block'?'known_crawler':'routing_match',
    ruleId:decision.ruleId,configRevision:link.domainId==='d2'?8:12,geoRelease:'geo-example-20261009',feedVersion:'feed-example-01',destination:decision.destination,
  };
});
export function applicableRules(link: Shortlink, list: ScopedRule[]) {
  return list.filter(r=>r.scope==='global'||(r.scope==='domain'&&r.scopeId===link.domainId)||(r.scope==='link'&&r.scopeId===link.id));
}
export function simulate(input: {country:string;device:Device;os:OperatingSystem;browser:BrowserFamily;bot:boolean}, list:ScopedRule[], link:Shortlink) {
  const rule=applicableRules(link,list).sort((a,b)=>Number(b.scope==='global'&&b.mandatory)-Number(a.scope==='global'&&a.mandatory)||a.priority-b.priority||({link:0,domain:1,global:2}[a.scope]-{link:0,domain:1,global:2}[b.scope])||a.id.localeCompare(b.id)).find(r=>r.enabled&&(r.country==='*'||r.country===input.country)&&(r.device==='*'||r.device===input.device)&&(r.os==='*'||r.os===input.os)&&(r.browser==='*'||r.browser===input.browser)&&(r.risk==='*'||input.bot));
  const action=(rule?.action??'redirect') as Action;
  const destination=action==='redirect'||action==='fallback'?(rule?.destinationSource==='custom'?rule.destination??null:link.destination):null;
  return {action,ruleId:rule?.id??null,name:rule?.name??'Tujuan shortlink',destination};
}
