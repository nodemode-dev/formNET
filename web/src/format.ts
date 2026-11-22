// Formatting helpers: no DOM, no state. Numbers, times and dates are written
// for the interface language; units (B, KB, ms, s) stay as they are.
import { language, t, tn } from "./ui/i18n";
import { RTL_LANGS } from "./ui/i18n/langs";

const numberFormats = new Map<string, Intl.NumberFormat>();
const dateFormats = new Map<string, Intl.DateTimeFormat>();

function num(v: number, digits: number): string {
  const key = `${language()}|${digits}`;
  let f = numberFormats.get(key);
  if (!f) {
    try { f = new Intl.NumberFormat(language(), { minimumFractionDigits: digits, maximumFractionDigits: digits }); } catch { f = new Intl.NumberFormat("en", { minimumFractionDigits: digits, maximumFractionDigits: digits }); }
    numberFormats.set(key, f);
  }
  return f.format(v);
}

function dates(kind: "clock" | "full"): Intl.DateTimeFormat {
  const key = `${language()}|${kind}`;
  let f = dateFormats.get(key);
  if (!f) {
    const opts: Intl.DateTimeFormatOptions = kind === "clock" ? { hour: "2-digit", minute: "2-digit", second: "2-digit", fractionalSecondDigits: 3, hourCycle: "h23" } : { dateStyle: "medium", timeStyle: "medium" };
    try { f = new Intl.DateTimeFormat(language(), opts); } catch { f = new Intl.DateTimeFormat("en", opts); }
    dateFormats.set(key, f);
  }
  return f;
}

// A number with a Latin unit reads left to right inside a right-to-left
// sentence ("17.7 KB", never "KB 17.7").
const ltr = (s: string): string => (RTL_LANGS.has(language()) ? `⁦${s}⁩` : s);

export function bytes(n: number | undefined): string {
  if (!n || n <= 0) return ltr(`${num(0, 0)} B`);
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return ltr(i === 0 ? `${num(v, 0)} B` : `${num(v, v >= 100 ? 0 : 1)} ${units[i]}`);
}

export function millis(ms: number | undefined): string {
  if (ms === undefined || ms < 0) return "";
  if (ms < 1) return ltr(`<${num(1, 0)} ms`);
  if (ms < 1000) return ltr(`${num(Math.round(ms), 0)} ms`);
  if (ms < 60000) return ltr(`${num(ms / 1000, 2)} s`);
  return ltr(`${num(ms / 60000, 1)} min`);
}

function parse(iso: string | undefined): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) || d.getFullYear() < 1971 ? null : d;
}

export function clock(iso: string | undefined): string {
  const d = parse(iso);
  return d ? dates("clock").format(d) : "";
}

// A date is written in the language's own order, so inside a left-to-right
// value it keeps its right-to-left reading where the language has one.
export function dateTime(iso: string | undefined): string {
  const d = parse(iso);
  if (!d) return "";
  const s = dates("full").format(d);
  return RTL_LANGS.has(language()) ? `⁧${s}⁩` : s;
}

// ago is "just now", then seconds, minutes or hours. A language whose table
// does not word it yet gets Intl's short relative time, which says the same.
export function ago(iso: string | undefined, now = Date.now()): string {
  const d = parse(iso);
  if (!d) return "";
  const s = Math.max(0, Math.round((now - d.getTime()) / 1000));
  if (s < 5) return RTL_LANGS.has(language()) ? `⁧${t("just now")}⁩` : t("just now");
  const [n, key, unit]: [number, string, Intl.RelativeTimeFormatUnit] = s < 60 ? [s, "{n} s ago", "second"] : s < 3600 ? [Math.round(s / 60), "{n} min ago", "minute"] : [Math.round(s / 3600), "{n} h ago", "hour"];
  const lang = language();
  let said = tn(n, key, key);
  if (lang !== "en" && t(key) === key) {
    try { said = new Intl.RelativeTimeFormat(lang, { style: "short", numeric: "always" }).format(-n, unit); } catch { /* the English form */ }
  }
  return RTL_LANGS.has(lang) ? `⁧${said}⁩` : said;
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
  if (kind === "refused") return "refused";
  return "";
}

export function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\\''`)}'`;
}
