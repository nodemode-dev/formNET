// JSON shapes of rules, breakpoints, the throttle and capture from other devices; see internal/server/rules_routes.go.
import type { Header, Status } from "./types.ts";

export type RuleActionType = "rewrite" | "map_remote" | "map_local" | "block" | "breakpoint";

export const RULE_ACTIONS: readonly RuleActionType[] = ["rewrite", "map_remote", "map_local", "block", "breakpoint"];

export interface RuleMatch {
  scheme?: string;
  host: string;
  port?: number;
  path?: string;
  method?: string;
  query?: string;
}

export interface HeaderOp {
  op: "add" | "set" | "remove";
  name: string;
  value?: string;
}

export interface QueryOp {
  op: "set" | "remove";
  name: string;
  value?: string;
}

export interface BodyOp {
  target?: "request" | "response";
  find: string;
  replace: string;
  regex?: boolean;
}

export interface RuleAction {
  type: RuleActionType;
  request_headers?: HeaderOp[];
  response_headers?: HeaderOp[];
  host?: string;
  path?: string;
  query?: QueryOp[];
  status?: number;
  body?: BodyOp[];
  to?: string;
  keep_host?: boolean;
  local_path?: string;
  drop?: boolean;
  request?: boolean;
  response?: boolean;
}

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  match: RuleMatch;
  action: RuleAction;
}

export interface RulesStatus {
  version: number;
  enabled: boolean;
  rules: Rule[] | null;
  intercept: boolean;
  https_note: string;
  notes: Record<string, string> | null;
}

export type BreakpointStage = "request" | "response";

export interface PausedExchange {
  id: string;
  flow_id: string;
  stage: BreakpointStage;
  rule: string;
  since: string;
  deadline: string;
  method?: string;
  url: string;
  status?: number;
  headers: Header[] | null;
  body_text?: string;
  body_base64?: string;
  body_bytes: number;
  body_decoded: boolean;
}

export interface BreakpointsStatus {
  paused: PausedExchange[] | null;
  timeout_seconds: number;
  max_paused: number;
}

export interface BreakpointEdit {
  method?: string;
  url?: string;
  status?: number;
  headers?: Header[];
  body_text?: string;
  body_base64?: string;
}

export type BreakpointOutcome = "continued" | "edited" | "aborted" | "timeout" | "client_gone";

export interface BreakpointEvent {
  type: "breakpoint.paused" | "breakpoint.resumed";
  id: string;
  flow_id: string;
  stage: BreakpointStage;
  rule: string;
  deadline?: string;
  outcome?: BreakpointOutcome;
}

export interface ThrottlePreset {
  down_kbps: number;
  up_kbps: number;
  latency_ms: number;
}

export interface ThrottleConfig {
  enabled: boolean;
  preset: string;
  down_kbps: number;
  up_kbps: number;
  latency_ms: number;
  hosts: string[] | null;
}

export interface ThrottleStatus extends ThrottleConfig {
  presets: Record<string, ThrottlePreset> | null;
}

export interface RemoteStatus {
  enabled: boolean;
  allow: string[] | null;
  from_env: boolean;
  port: number;
  addresses: string[] | null;
  listening: string[] | null;
  refused: number;
  last_refused?: string;
  cert_url: string;
  error?: string;
}

export interface RulesSummary {
  enabled: boolean;
  count: number;
}

/** What /api/status adds; absent from a server without these routes. */
export interface TrafficStatus {
  rules?: RulesSummary;
  throttle?: ThrottleStatus;
  remote?: RemoteStatus;
}

export const trafficOf = (status: Status | null): TrafficStatus => (status ?? {}) as TrafficStatus;

/** What a flow adds when a rule, a breakpoint or the throttle touched it. */
export interface FlowTraffic {
  rules?: string[];
  paused?: BreakpointStage;
  throttled?: boolean;
  notes?: string[];
}
