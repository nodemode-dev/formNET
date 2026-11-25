// JSON shapes of Milestone 9: protocols, WebSocket messages, the upstream proxy, sessions, imports and the filtered
// list; see internal/server/data_routes.go, protocol_routes.go and internal/flow/messages.go.
import type { Flow, Status } from "./types.ts";

export interface MessageSummary {
  count: number;
  bytes: number;
  dropped: number;
  deflate?: boolean;
  close_code?: number;
  close_reason?: string;
  closed_by?: "client" | "server" | string;
  parse_error?: string;
}

/** What a flow carries beyond types.ts once the server is Milestone 9's. */
export interface FlowData {
  client_proto?: string;
  server_proto?: string;
  upstream?: string;
  messages?: MessageSummary;
  from_session?: boolean;
  imported?: "har" | string;
}

export const flowData = (f: Flow): Flow & FlowData => f as Flow & FlowData;

export type WSOpcode = "text" | "binary" | "close" | "ping" | "pong";

export interface WSMessage {
  seq: number;
  dir: "client" | "server";
  time: string;
  opcode: WSOpcode;
  text?: string;
  base64?: string;
  bytes: number;
  truncated: boolean;
  compressed: boolean;
  not_decoded?: boolean;
  inflated_bytes?: number;
  frames?: number;
  close_code?: number;
}

export interface MessagePage extends MessageSummary {
  flow_id: string;
  messages: WSMessage[] | null;
  next: number;
  open: boolean;
  evicted: boolean;
}

/** The stream's flow.message event: the last message of an interval and how many it stands for. */
export interface WSMessageEvent {
  flow_id: string;
  seq: number;
  dir: string;
  opcode: string;
  bytes: number;
  coalesced: number;
}

export interface UpstreamStatus {
  enabled: boolean;
  url: string;
  bypass: string[] | null;
  username: string;
  has_password: boolean;
  from_env: boolean;
  active: boolean;
  error?: string;
  error_at?: string;
}

export interface UpstreamChange {
  enabled?: boolean;
  url?: string;
  bypass?: string[];
  username?: string;
  password?: string;
}

export interface SessionStatus {
  open: boolean;
  name: string;
  saved: string | null;
  flows: number;
  dropped: number;
  app_version?: string;
  opened: string | null;
}

export interface SavedSession {
  name: string;
  path: string;
  saved: string;
  flows: number;
  bytes: number;
  app_version: string;
  error?: string;
}

export interface SessionList {
  folder: string;
  sessions: SavedSession[] | null;
}

export interface SaveAnswer {
  name: string;
  path: string;
  flows: number;
  bytes: number;
  saved: string;
}

export interface OpenAnswer {
  session: SessionStatus;
  flows: number;
  dropped: number;
  messages: number;
}

export interface ImportAnswer {
  imported: number;
  skipped: number;
  dropped: number;
  messages: number;
  creator: string;
}

export interface FlowList {
  flows: Flow[] | null;
  total: number;
  body_scanned_bytes?: number;
  body_scan_cut?: boolean;
}

export type CurlShell = "sh" | "powershell" | "cmd";

interface DataStatus extends Status {
  session?: SessionStatus;
  upstream?: UpstreamStatus;
}

/** The status's Milestone 9 parts; absent from an older server. */
export const dataOf = (status: Status | null): Partial<Pick<DataStatus, "session" | "upstream">> => (status ?? {}) as DataStatus;
