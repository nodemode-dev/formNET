import { authHeaders, withToken } from "./session.ts";
import type { CAInfo, Flow, FlowDetail, FlowEvent, HostSummary, ProxyStatus, Snapshot, Status } from "./types.ts";

const base = location.origin;

// SessionError is thrown when the server answers 401: the page has no session cookie.
export class SessionError extends Error {
  constructor() {
    super("no session");
    this.name = "SessionError";
  }
}

let unauthorized: () => void = () => undefined;

// onUnauthorized registers what happens on the first 401 from any route.
export function onUnauthorized(fn: () => void): void {
  unauthorized = fn;
}

function check(res: Response, path: string): void {
  if (res.status === 401) {
    unauthorized();
    throw new SessionError();
  }
  if (!res.ok) throw new Error(`${path}: ${res.status}`);
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(base + path, { credentials: "omit", headers: authHeaders() });
  check(res, path);
  return (await res.json()) as T;
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(base + path, {
    method: "POST",
    credentials: "omit",
    headers: { "Content-Type": "application/json", ...authHeaders() },
    body: JSON.stringify(body),
  });
  if (res.status === 401) check(res, path);
  if (!res.ok) {
    const detail = (await res.json().catch(() => ({}))) as { error?: string };
    throw new Error(detail.error ?? `${path}: ${res.status}`);
  }
  return (await res.json()) as T;
}

// FlowFilter holds only what the server's list route understands.
export interface FlowFilter {
  q?: string;
  host?: string;
  domain?: string;
  kind?: string;
  since?: number;
  limit?: number;
}

export function filterQuery(f: FlowFilter): string {
  const p = new URLSearchParams();
  if (f.q) p.set("q", f.q);
  if (f.host) p.set("host", f.host);
  if (f.domain) p.set("domain", f.domain);
  if (f.kind) p.set("kind", f.kind);
  if (f.since) p.set("since", String(f.since));
  if (f.limit) p.set("limit", String(f.limit));
  const s = p.toString();
  return s ? `?${s}` : "";
}

export const api = {
  status: () => getJSON<Status>("/api/status"),
  flows: (f: FlowFilter = {}) => getJSON<{ flows: Flow[] | null; total: number }>(`/api/flows${filterQuery(f)}`),
  flow: (id: string) => getJSON<FlowDetail>(`/api/flows/${encodeURIComponent(id)}`),
  clearFlows: () => postJSON<{ cleared: boolean }>("/api/flows/clear", {}),
  hosts: () => getJSON<{ hosts: HostSummary[] | null }>("/api/hosts"),
  machine: () => getJSON<Snapshot>("/api/machine"),
  proxy: () => getJSON<ProxyStatus>("/api/proxy"),
  updateProxy: (u: { intercept?: boolean; passthrough?: string[] }) => postJSON<ProxyStatus>("/api/proxy", u),
  ca: () => getJSON<CAInfo>("/api/ca"),
  harURL: (f: FlowFilter) => withToken(`${base}/api/export/har${filterQuery(f)}`),
  caURL: () => withToken(`${base}/ca.pem`),
};

export interface Hello {
  version: string;
  flows: number;
  epoch?: string;
  dropped?: number;
}

export interface StreamHandlers {
  hello: (h: Hello) => void;
  resync: (dropped: number) => void;
  flow: (e: FlowEvent) => void;
  machine: (s: Snapshot) => void;
  close: () => void;
  unauthorized: () => void;
}

// openStream subscribes to the server-sent events and reconnects with backoff
// when the connection drops. A drop caused by a missing session stops it for
// good. The returned function stops it.
export function openStream(handlers: StreamHandlers): () => void {
  let source: EventSource | null = null;
  let retry = 1000;
  let stopped = false;
  let timer = 0;

  const parse = <T>(ev: Event): T | null => {
    try {
      return JSON.parse((ev as MessageEvent<string>).data) as T;
    } catch {
      return null;
    }
  };

  const stop = () => {
    stopped = true;
    window.clearTimeout(timer);
    source?.close();
    source = null;
  };

  const dropped = async () => {
    source?.close();
    source = null;
    handlers.close();
    if (stopped) return;
    try {
      const res = await fetch(`${base}/api/status`, { credentials: "omit", headers: authHeaders() });
      if (res.status === 401) {
        stop();
        handlers.unauthorized();
        return;
      }
    } catch {
      // the server is away; keep retrying
    }
    if (stopped) return;
    timer = window.setTimeout(connect, retry);
    retry = Math.min(retry * 2, 15000);
  };

  const connect = () => {
    if (stopped) return;
    source = new EventSource(withToken(`${base}/api/stream`));
    source.addEventListener("hello", (ev) => {
      retry = 1000;
      handlers.hello(parse<Hello>(ev) ?? { version: "", flows: 0 });
    });
    source.addEventListener("resync", (ev) => {
      handlers.resync(parse<{ dropped: number }>(ev)?.dropped ?? 0);
    });
    for (const type of ["flow.added", "flow.updated", "flow.cleared"]) {
      source.addEventListener(type, (ev) => {
        const e = parse<FlowEvent>(ev);
        if (e) handlers.flow(e);
      });
    }
    source.addEventListener("machine.snapshot", (ev) => {
      const s = parse<Snapshot>(ev);
      if (s) handlers.machine(s);
    });
    source.onerror = () => {
      void dropped();
    };
  };

  connect();
  return stop;
}
