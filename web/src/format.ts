// Pure formatting helpers; no DOM, no state.

export function bytes(n: number | undefined): string {
  if (!n || n <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return i === 0 ? `${v} B` : `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`;
}

export function millis(ms: number | undefined): string {
  if (ms === undefined || ms < 0) return "";
  if (ms < 1) return "<1 ms";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  if (ms < 60000) return `${(ms / 1000).toFixed(2)} s`;
  return `${(ms / 60000).toFixed(1)} min`;
}

export function clock(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return "";
  const pad = (n: number, w = 2) => String(n).padStart(w, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

export function dateTime(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 1971) return "";
  return d.toLocaleString();
}

export function ago(iso: string | undefined, now = Date.now()): string {
  if (!iso) return "";
  const d = new Date(iso);
  const t = d.getTime();
  if (Number.isNaN(t) || d.getFullYear() < 1971) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 5) return "just now";
  if (s < 60) return `${s} s ago`;
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  return `${Math.round(s / 3600)} h ago`;
}

export function statusClass(status: number, error?: string): string {
  if (error) return "st-error";
  if (status === 0) return "st-none";
  if (status < 300) return "st-ok";
  if (status < 400) return "st-redirect";
  if (status < 500) return "st-client";
  return "st-server";
}

export function methodClass(method: string): string {
  const m = method.toUpperCase();
  if (m === "GET" || m === "HEAD" || m === "OPTIONS") return "m-read";
  if (m === "CONNECT") return "m-connect";
  return "m-write";
}

// shortType turns "application/json; charset=utf-8" into "json".
export function shortType(contentType: string | undefined): string {
  if (!contentType) return "";
  const mime = contentType.split(";")[0]?.trim().toLowerCase() ?? "";
  const sub = mime.split("/")[1] ?? mime;
  const plus = sub.split("+");
  const last = plus[plus.length - 1] ?? sub;
  return last.replace(/^x-/, "").replace(/^vnd\./, "");
}

export function isJSONType(contentType: string | undefined): boolean {
  const t = shortType(contentType);
  return t === "json" || t === "ld+json";
}

export function isImageType(contentType: string | undefined): boolean {
  return (contentType ?? "").toLowerCase().startsWith("image/");
}

export function isTextType(contentType: string | undefined): boolean {
  const ct = (contentType ?? "").toLowerCase();
  return ct.startsWith("text/") || /json|xml|javascript|x-www-form-urlencoded|graphql/.test(ct);
}

// prettyJSON re-indents a body that parses as JSON; otherwise returns it unchanged.
export function prettyJSON(body: string): string {
  try {
    return JSON.stringify(JSON.parse(body), null, 2);
  } catch {
    return body;
  }
}

export function pathWithQuery(path: string, query: string): string {
  const p = path || "/";
  return query ? `${p}?${query}` : p;
}

export function kindLabel(kind: string): string {
  if (kind === "tunnel") return "tunnel";
  if (kind === "websocket") return "ws";
  return "";
}

export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}
