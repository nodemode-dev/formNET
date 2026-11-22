import { api, SessionError } from "./api.ts";
import { TrafficError } from "./api-traffic.ts";
import { authHeaders, withToken } from "./session.ts";
import type { CurlShell, FlowList, ImportAnswer, MessagePage, OpenAnswer, SaveAnswer, SessionList, UpstreamChange, UpstreamStatus } from "./types-data.ts";

const base = location.origin;

/** A list or export query: every filter the server takes, as its query parameters. */
export type FlowParams = Record<string, string>;

export const paramString = (p: FlowParams): string => {
  const s = new URLSearchParams(Object.entries(p).filter(([, v]) => v !== "")).toString();
  return s ? `?${s}` : "";
};

async function call<T>(method: string, path: string, body?: BodyInit, json = true): Promise<T> {
  const headers: Record<string, string> = { ...authHeaders() };
  if (typeof body === "string") headers["Content-Type"] = "application/json";
  const res = await fetch(base + path, { method, credentials: "omit", headers, body });
  if (res.status === 401) {
    void api.status().catch(() => undefined);
    throw new SessionError();
  }
  if (!res.ok) {
    const detail = (await res.json().catch(() => ({}))) as { error?: string };
    throw new TrafficError(res.status, detail.error ?? `${path}: ${res.status}`);
  }
  return (json ? await res.json() : await res.text()) as T;
}

const fileBody = (file: File): FormData => {
  const form = new FormData();
  form.append("file", file, file.name);
  return form;
};

export const dataApi = {
  flows: (p: FlowParams) => call<FlowList>("GET", `/api/flows${paramString(p)}`),
  messages: (id: string, since: number, limit: number) => call<MessagePage>("GET", `/api/flows/${encodeURIComponent(id)}/messages?since=${since}&limit=${limit}`),
  upstream: () => call<UpstreamStatus>("GET", "/api/upstream"),
  setUpstream: (c: UpstreamChange) => call<UpstreamStatus>("POST", "/api/upstream", JSON.stringify(c)),
  sessions: () => call<SessionList>("GET", "/api/sessions"),
  saveSession: (name: string) => call<SaveAnswer>("POST", "/api/session/save", JSON.stringify({ name })),
  openSession: (name: string) => call<OpenAnswer>("POST", "/api/session/open", JSON.stringify({ name })),
  openSessionFile: (file: File) => call<OpenAnswer>("POST", "/api/session/open", fileBody(file)),
  importHAR: (file: File) => call<ImportAnswer>("POST", "/api/import/har", fileBody(file)),
  curl: (p: FlowParams, shell: CurlShell, secrets: boolean) => call<string>("GET", `/api/export/curl${paramString({ ...p, shell, secrets: secrets ? "1" : "" })}`, undefined, false),
  curlURL: (p: FlowParams, shell: CurlShell, secrets: boolean) => withToken(`${base}/api/export/curl${paramString({ ...p, shell, secrets: secrets ? "1" : "" })}`),
  harURL: (p: FlowParams, secrets = false) => withToken(`${base}/api/export/har${paramString({ ...p, secrets: secrets ? "1" : "" })}`),
  sessionExportURL: (p: FlowParams) => withToken(`${base}/api/session/export${paramString(p)}`),
};
