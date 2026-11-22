// JSON shapes the server sends; see internal/server/server.go and internal/flow/flow.go.
import type { RemoteStatus, RulesSummary, ThrottleStatus } from "./types-traffic.ts";

export interface Header {
  name: string;
  value: string;
}

export interface Message {
  headers: Header[] | null;
  header_bytes: number;
  body_bytes: number;
  body_truncated: boolean;
  content_type: string;
  encoding: string;
}

export interface TLSInfo {
  server_name: string;
  version: string;
  cipher: string;
  intercepted: boolean;
}

export type FlowKind = "http" | "tunnel" | "websocket" | "refused";

export interface Flow {
  id: string;
  seq: number;
  kind: FlowKind;
  start: string;
  end?: string;
  pending: boolean;
  client: string;
  client_pid?: number;
  client_process?: string;
  scheme: string;
  method: string;
  host: string;
  port: number;
  path: string;
  query: string;
  url: string;
  protocol: string;
  tls?: TLSInfo;
  request: Message;
  response?: Message;
  status: number;
  bytes_sent: number;
  bytes_received: number;
  connect_ms: number;
  ttfb_ms: number;
  duration_ms: number;
  error?: string;
  resent_from?: string;
  edited?: boolean;
  rules?: string[];
  paused?: "request" | "response";
  throttled?: boolean;
  notes?: string[];
}

export interface Body {
  text?: string;
  base64?: string;
  bytes: number;
  truncated: boolean;
  decoded?: boolean;
  encoding?: string;
}

export interface FlowDetail extends Flow {
  request_body?: Body;
  response_body?: Body;
}

export interface HostSummary {
  host: string;
  domain: string;
  flows: number;
  pending: number;
  errors: number;
  bytes_sent: number;
  bytes_received: number;
  first_seen: string;
  last_seen: string;
  intercepted: boolean;
  processes: string[] | null;
  category?: string;
  owner?: string;
}

export interface Process {
  pid: number;
  name: string;
  path?: string;
  user?: string;
  bytes_in: number;
  bytes_out: number;
  connections: number;
}

export interface Connection {
  proto: string;
  pid: number;
  process: string;
  local: string;
  remote: string;
  remote_host?: string;
  state: string;
  first_seen: string;
  last_seen: string;
}

export interface Snapshot {
  taken: string;
  source: string;
  supported: boolean;
  error?: string;
  processes: Process[] | null;
  connections: Connection[] | null;
}

export type InterceptMode = "auto" | "on" | "off";

export interface ProxyStatus {
  addr: string;
  running: boolean;
  intercept: boolean;
  intercept_mode?: InterceptMode;
  passthrough: string[] | null;
  error?: string;
}

export interface CAInfo {
  path: string;
  fingerprint: string;
  not_after: string;
  trusted: boolean;
  trust_command: string;
  trust_hint: string;
}

export interface MachineStatus {
  supported: boolean;
  source: string;
  taken: string;
  processes: number;
  connections: number;
  error?: string;
}

export interface Status {
  product: string;
  version: string;
  addr: string;
  flows: number;
  hosts: number;
  pending: number;
  proxy: ProxyStatus;
  ca: CAInfo;
  machine: MachineStatus;
  notices?: string[] | null;
  now: string;
  sysproxy?: SysProxyStatus;
  rules?: RulesSummary;
  throttle?: ThrottleStatus;
  remote?: RemoteStatus;
}

export interface SysProxyService {
  name: string;
  web: string;
  secure: string;
}

export interface SysProxyStatus {
  supported: boolean;
  reason: string;
  enabled: boolean;
  active: boolean;
  address: string;
  services: SysProxyService[] | null;
  auto_config: boolean;
  error: string;
}

export interface Recipe {
  id: string;
  title: string;
  shell: "sh" | "powershell";
  lines: string[];
  note: string;
}

export interface Recipes {
  proxy_addr: string;
  ca_path: string;
  platform: "darwin" | "linux" | "windows" | string;
  recipes: Recipe[] | null;
}

export interface ResendEdit {
  method?: string;
  url?: string;
  headers?: Header[];
  body_text?: string;
  body_base64?: string;
}

export interface FlowEvent {
  type: "flow.added" | "flow.updated" | "flow.cleared";
  flow?: Flow;
}
