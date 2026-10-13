/** B02 wire types. No credentials are included in management responses. */
export type AgentMode = 'local' | 'api';
export type Readiness = 'draft' | 'verification_pending' | 'installation_pending' | 'preparing' | 'ready' | 'attention' | 'revoked';
export type Action = 'redirect' | 'block' | 'challenge' | 'fallback';
export type Device = 'mobile' | 'desktop' | 'tablet' | 'unknown';
export type OperatingSystem = 'ios' | 'android' | 'windows' | 'macos' | 'linux' | 'chromeos' | 'other' | 'unknown';
export type BrowserFamily = 'chrome' | 'safari' | 'firefox' | 'edge' | 'samsung_internet' | 'opera' | 'other' | 'unknown';
export interface Capabilities { localGeo: boolean; persistentQueue: boolean; challenge: boolean; }
export interface Domain {
 openingUrl?:string;
  id: string; hostname: string; mode: AgentMode; readiness: Readiness;
  connection: 'online' | 'idle' | 'offline' | 'unknown';
  desiredRevision: number; appliedRevision: number | null;
  lastSeenAt: string | null; capabilities: Capabilities;
}
export interface ReadinessCheck { code:string; status:'pass'|'fail'|'not_applicable'; message:string; }
export interface InstallationStatus { domain:Domain;agentId:string;version:string;geoRelease:string|null;checkedAt:string;checks:ReadinessCheck[];lastProbe:{checkedAt:string;checks:ReadinessCheck[]}|null; }
export type IPQualityFlag='proxy'|'vpn'|'tor'|'datacenter'|'botnet'|'spam'|'malware'|'attacker';
export interface LinkRouting {
 ipQualityBlock?:IPQualityFlag[];
 proxyCheck?:boolean;proxyUnknownFallback?:boolean;fallback:string; country:string; device:Device|'*'; os:OperatingSystem|'*'; browser:BrowserFamily|'*';
}
export interface Shortlink {
 routing?:LinkRouting; draftRouting?:LinkRouting;
  id: string; domainId: string; slug: string; destination: string;
  state: 'draft' | 'published'; revision: number; clicks: number;
  /** Published destination stays unchanged until a destination draft is published. */
  draftDestination?: string;
}
export interface Rule {
  id: string; name: string; priority: number; asn?:number; country: string | '*';
  device: Device | '*'; os: OperatingSystem | '*'; browser: BrowserFamily | '*';
  risk: 'known_bot' | 'suspected_automation' | 'tor_exit' | 'reputation_flag' | 'rate_limited' | '*';
 botCategory?:'*'|'search'|'preview'|'automation'|'unknown'; action: Action;
  destinationSource?: 'link' | 'custom'; destination?: string; enabled: boolean;
  mandatory?: boolean;
}
export type RuleScope = 'global' | 'domain' | 'link';
export interface RuleBinding { scope: RuleScope; scopeId: string | null; }
/** UI projection of RuleSet scope onto each contained rule. */
export interface ScopedRule extends Rule, RuleBinding {}
export interface RuleSet extends RuleBinding {
  id: string; revision: number; publishedRevision?:number|null; state: 'draft' | 'published'; rules: Rule[];
}
export interface Decision {
 proxy?:{status:string;detected:boolean};
  eventId: string; action: Action; reasonCode: string; ruleId: string | null;
  configRevision: number; geoRelease: string | null; feedVersion: string | null;
  destination: string | null;
 risk?:RiskObservation;
}
export interface TrafficEvent extends Decision {
 clientIp?:string; asn?:number; asnOrganization?:string;
  occurredAt: string; domainId: string; linkId: string;
  country: string; device: Device; os: OperatingSystem; browser: BrowserFamily; source: 'go' | 'php';
  osVersion?: string | null; browserVersion?: string | null;
  deviceBrand?: string | null; deviceModel?: string | null;
}
export interface ApiError {
  error: { code: string; message: string; requestId: string; fields?: Record<string, string> };
}
export interface Page<T> { items: T[]; nextCursor: string | null; }
export interface LoginRequest { email: string; password: string; }
export interface LoginResult { status: 'authenticated' | 'mfa_required'; challengeId: string | null; }
/** Auth-only response. Keep CSRF in memory; the session cookie is HttpOnly. */
export interface Session { userId: string; email: string; role: 'owner'; mfaEnabled: boolean; csrfToken: string; }

export interface PublishResult { revision:number; domainRevisions:{domainId:string;revision:number}[]; pendingAgentIds:string[]; publishedAt:string; }
export interface PublishHistory {revision:number;publishedAt:string;}

export interface AgentArtifact {
 name:string;version:string;platform:string;size:number;sha256:string;downloadUrl:string;
 imageId:string|null;imageConfigDigest:string|null;imageReference:string|null;imageDigest:string|null;
}
export interface AgentManifest {
 version:string;publishedAt:string;installerReady:boolean;artifacts:AgentArtifact[];
 signature:string;signedPayload:string;
}
export interface DomainBootstrap {
 domainId:string;hostname:string;controlPlaneUrl:string;fallbackUrls:string[];
 manifestVerificationKey:string;artifactVersion:string;publicUrl?:string;trustedProxies?:string[];developmentLoopback?:boolean;method:'cli'|'php_zip';
}

export interface EventHealth {
 activeAgents:number; staleAgents:number;pendingEvents:number;oldestPendingReceivedAt:string|null;watermark:string|null;queueDepth:number;queueBytes:number;droppedEvents:number;bufferedEvents:number;queueWriteErrors:number;}

export interface ProtectionSettings {mode:'off'|'observe'|'enforce';ratePerSecond:number;burst:number;}
export interface ProtectionStatus {settings:ProtectionSettings;revision:number;appliedRevision:number|null;supported:boolean;lastSeenAt:string|null;feedVersion:string|null;}
export interface RiskObservation {mode:'observe'|'enforce';signals:string[];botCategory:'search'|'preview'|'automation'|'unknown';feedStatus:string;torStatus:string;reputationStatus:string;candidateAction:Action;candidateRuleId:string|null;candidateReasonCode:string;}
export interface RiskSummary {signal:string;mode:string;candidateAction:Action;reasonCode:string;ruleId:string;events:number;}
export interface FeedStatus {available:boolean;version:string|null;publishedAt:string|null;expiresAt:string|null;torFetchedAt:string|null;reputationFetchedAt:string|null;torEntries:number;reputationEntries:number;}

export interface TrafficRecord {id:number;receivedAt:string;agentId:string;processed:boolean;event:TrafficEvent}
export interface TrafficPage {items:TrafficRecord[];nextCursor:string|null;watermark:string|null}
export interface TrafficAnalytics {buckets:{bucket:string;action:string;events:number;botEvents:number}[];health:EventHealth;filter:Record<string,string>;rawRetentionDays:number;aggregateRetentionDays:number;uncollected:string[]}

/** B15: values forwarded by authenticated adapter; all classifications come from the center. */
export interface DecisionRequest {eventId:string;occurredAt:string;domainId:string;slug:string;signals:{ip:string;userAgent:string}}
export interface AdapterProbeRequest {domainId:string;hostname:string;nonce:string;version:string}
export interface AdapterProbeState {nonce:string;domainId:string;revision:number;geoRelease:string|null;ready:boolean;version:string}

export interface PHPManifest {version:string;publishedAt:string;method:'php_zip';minimumPhp:string;artifacts:AgentArtifact[];signature:string;signedPayload:string;}
