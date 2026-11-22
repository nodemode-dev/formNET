import { api, HTTPError, SessionError } from "./api.ts";
import { authHeaders } from "./session.ts";
import type { BreakpointEdit, BreakpointsStatus, RemoteStatus, Rule, RulesStatus, ThrottleConfig, ThrottleStatus } from "./types-traffic.ts";

const base = location.origin;

/** A refusal from the traffic routes. `field` is the path the server named ("rules[2].action.body[0].find"), `reason`
 *  the rest of its sentence; both empty when the message names no field. */
export class TrafficError extends HTTPError {
  readonly field: string;
  readonly reason: string;
  constructor(status: number, message: string) {
    super(status, message);
    this.name = "TrafficError";
    const m = /^([a-z_]+(?:\[\d+\])?(?:\.[a-z_]+(?:\[\d+\])?)*): (.+)$/s.exec(message);
    this.field = m?.[1] ?? "";
    this.reason = m?.[2] ?? message;
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(base + path, {
    method,
    credentials: "omit",
    headers: body === undefined ? authHeaders() : { "Content-Type": "application/json", ...authHeaders() },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (res.status === 401) {
    // The status route reports a lost session to the page the one way api.ts knows.
    void api.status().catch(() => undefined);
    throw new SessionError();
  }
  if (!res.ok) {
    const detail = (await res.json().catch(() => ({}))) as { error?: string };
    throw new TrafficError(res.status, detail.error ?? `${path}: ${res.status}`);
  }
  return (await res.json()) as T;
}

export const trafficApi = {
  rules: () => call<RulesStatus>("GET", "/api/rules"),
  putRules: (rules: Rule[]) => call<RulesStatus>("PUT", "/api/rules", { rules }),
  setRulesEnabled: (enabled: boolean) => call<RulesStatus>("POST", "/api/rules/enabled", { enabled }),
  breakpoints: () => call<BreakpointsStatus>("GET", "/api/breakpoints"),
  setBreakpoints: (s: { timeout_seconds?: number; max_paused?: number }) => call<BreakpointsStatus>("POST", "/api/breakpoints", s),
  continueBreakpoint: (id: string, edit: BreakpointEdit = {}) => call<{ continued: boolean }>("POST", `/api/breakpoints/${encodeURIComponent(id)}/continue`, edit),
  abortBreakpoint: (id: string) => call<{ aborted: boolean }>("POST", `/api/breakpoints/${encodeURIComponent(id)}/abort`, {}),
  throttle: () => call<ThrottleStatus>("GET", "/api/throttle"),
  setThrottle: (c: Partial<ThrottleConfig>) => call<ThrottleStatus>("POST", "/api/throttle", c),
  remote: () => call<RemoteStatus>("GET", "/api/remote"),
  setRemote: (r: { enabled?: boolean; allow?: string[] }) => call<RemoteStatus>("POST", "/api/remote", r),
};
