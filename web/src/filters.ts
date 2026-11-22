import { dataApi, type FlowParams } from "./api-data.ts";
import { SessionError } from "./api.ts";
import type { State, Store } from "./state.ts";
import type { Flow } from "./types.ts";
import { flowData } from "./types-data.ts";

export type StatusClass = "2xx" | "3xx" | "4xx" | "5xx";
export type TypeChip = "json" | "html" | "js" | "css" | "image" | "media" | "websocket" | "other";

/** Every filter of the Flows list beyond the text box and the host chosen in Hosts. Empty values filter nothing. */
export interface FilterState {
  body: boolean;
  status: StatusClass[];
  errors: boolean;
  methods: string[];
  type: TypeChip | "";
  fromSession: boolean;
  rules: boolean;
  throttled: boolean;
  minKB: string;
  maxKB: string;
  minMs: string;
  maxMs: string;
  process: string;
  focus: string;
  ignore: string;
}

export const EMPTY_FILTERS: FilterState = { body: false, status: [], errors: false, methods: [], type: "", fromSession: false, rules: false, throttled: false, minKB: "", maxKB: "", minMs: "", maxMs: "", process: "", focus: "", ignore: "" };

export const STATUS_CLASSES: readonly StatusClass[] = ["2xx", "3xx", "4xx", "5xx"];
export const METHODS: readonly string[] = ["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"];
export const TYPE_CHIPS: ReadonlyArray<readonly [TypeChip, string]> = [["json", "JSON"], ["html", "HTML"], ["js", "JS"], ["css", "CSS"], ["image", "Images"], ["media", "Media"], ["websocket", "WebSocket"], ["other", "Other"]];

/** The content-type prefix each type chip means; the server takes one prefix, so JS, media and other are applied in the
 *  page only (the query then leaves the type out and is wider than what is shown). */
const TYPE_PREFIX: Partial<Record<TypeChip, string>> = { json: "application/json", html: "text/html", css: "text/css", image: "image/" };
const JS_TYPES = ["application/javascript", "text/javascript", "application/x-javascript", "application/ecmascript", "text/ecmascript"];
const MEDIA_TYPES = ["video/", "audio/"];

const KEY = "formnet.flowFilters";

const num = (s: string): number | null => {
  const v = Number(s.trim());
  return s.trim() !== "" && Number.isFinite(v) && v >= 0 ? v : null;
};

/** Host globs as the server reads them: * any run, ? one character, *.example.com also matches example.com. */
function hostGlob(pattern: string): RegExp | null {
  const p = pattern.trim().toLowerCase().replace(/^\[|\]$/g, "");
  if (!p) return null;
  const quote = (s: string): string => s.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
  const suffix = p.startsWith("*.") ? p.slice(2) : "";
  if (suffix && !/[*?]/.test(suffix)) return new RegExp(`^(?:.*\\.)?${quote(suffix)}$`);
  return new RegExp(`^${quote(p)}$`);
}

const contentType = (f: Flow): string => (f.response?.content_type ?? "").trim().toLowerCase();
const hostOf = (f: Flow): string => f.host.toLowerCase().replace(/^\[|\]$/g, "").replace(/\.$/, "");
const bodySize = (f: Flow): number => (f.response ? f.response.body_bytes : f.bytes_received);
const hostList = (s: string): string[] => s.split(/[\s,]+/).map((x) => x.trim()).filter(Boolean);
const globs = (list: readonly string[]): RegExp[] => list.map(hostGlob).filter((r): r is RegExp => !!r);

/** The filters typed into the Flows text box, each null while the text does not set it; `not` is a leading -. */
export interface TypedFilter {
  status: { value: string; lo: number; hi: number; not: boolean } | null;
  method: { list: string[]; not: boolean } | null;
  domain: { globs: string[]; not: boolean } | null;
  type: { prefix: string; not: boolean } | null;
  size: { bytes: number; not: boolean } | null;
  process: { text: string; not: boolean } | null;
}

export interface ParsedText { rest: string; typed: TypedFilter; invalid: string[] }

/** The keys the text box reads, each with the forms its value takes (shown as typed, never translated). */
export const TYPED_FORMS: Readonly<Record<string, string>> = {
  "status-code": "status-code:404, status-code:4xx, status-code:400-499",
  method: "method:POST, method:GET,POST",
  domain: "domain:example.com, domain:*.example.com",
  "mime-type": "mime-type:image/, mime-type:application/json",
  "larger-than": "larger-than:500, larger-than:100k, larger-than:2m",
  process: "process:curl",
};
export const TYPED_SAMPLES = "status-code:404, method:POST, domain:*.example.com, mime-type:image/, larger-than:100k, process:curl";

const typedToken = /^(-?)([a-z-]+):(.*)$/i;

/** The forms a token's key takes, for the sentence that says a token could not be read. */
export const typedForms = (token: string): string => TYPED_FORMS[typedToken.exec(token)?.[2]?.toLowerCase() ?? ""] ?? TYPED_SAMPLES;

function readStatus(v: string): [number, number] | null {
  if (/^[1-9]xx$/.test(v)) return [Number(v[0]) * 100, Number(v[0]) * 100 + 99];
  const m = /^([1-9]\d\d)(?:-([1-9]\d\d))?$/.exec(v);
  if (!m) return null;
  const lo = Number(m[1]), hi = Number(m[2] ?? m[1]);
  return lo <= hi ? [lo, hi] : null;
}

function readSize(v: string): number | null {
  const m = /^(\d+(?:\.\d+)?)(k|kb|m|mb)?$/i.exec(v);
  if (!m) return null;
  const unit = (m[2] ?? "").toLowerCase();
  const n = Math.round(Number(m[1]) * (unit.startsWith("k") ? 1024 : unit.startsWith("m") ? 1024 * 1024 : 1));
  return Number.isSafeInteger(n) ? n : null;
}

const validGlob = (g: string): boolean => !/[/\\]/.test(g) && (!g.includes(":") || /^\[?[0-9a-f:.]+\]?$/i.test(g)) && !!hostGlob(g);

let lastParse: { text: string; out: ParsedText } | null = null;

/** Reads the Flows text box: words split on spaces (a double-quoted run stays together), each `key:value` or
 *  `-key:value` with a known key taken out as a typed filter, everything else left in `rest` as typed. A token that
 *  starts with a quote is always plain text. A known key with a value it cannot read goes into `invalid`. */
export function parseTyped(text: string): ParsedText {
  if (lastParse?.text === text) return lastParse.out;
  const typed: TypedFilter = { status: null, method: null, domain: null, type: null, size: null, process: null };
  const invalid: string[] = [], plain: string[] = [];
  let found = false;
  for (const [raw] of text.matchAll(/(?:[^\s"]+|"[^"]*"?)+/g)) {
    const m = raw.startsWith('"') ? null : typedToken.exec(raw.replace(/"/g, ""));
    const key = m?.[2]?.toLowerCase() ?? "";
    if (!m || !(key in TYPED_FORMS)) { plain.push(raw); continue; }
    found = true;
    const not = m[1] === "-", v = (m[3] ?? "").trim();
    const list = v.split(",").map((x) => x.trim()).filter(Boolean);
    if (!list.length) continue;
    let ok = !v.startsWith("!");
    if (key === "status-code") {
      const r = readStatus(v.toLowerCase());
      if (r) typed.status = { value: v.toLowerCase(), lo: r[0], hi: r[1], not }; else ok = false;
    } else if (key === "method" && ok) {
      const add = list.map((x) => x.toUpperCase());
      typed.method = { list: typed.method?.not === not ? [...new Set([...typed.method.list, ...add])] : add, not };
    } else if (key === "domain") {
      const add = list.map((x) => x.toLowerCase());
      if (add.every(validGlob)) typed.domain = { globs: typed.domain?.not === not ? [...new Set([...typed.domain.globs, ...add])] : add, not }; else ok = false;
    } else if (key === "mime-type" && ok) {
      typed.type = { prefix: v.toLowerCase(), not };
    } else if (key === "larger-than") {
      const n = readSize(v);
      if (n !== null) typed.size = { bytes: n, not }; else ok = false;
    } else if (key === "process" && ok) {
      typed.process = { text: v.toLowerCase(), not };
    }
    if (!ok) invalid.push(raw);
  }
  const out = { rest: found ? plain.join(" ") : text.trim(), typed, invalid };
  lastParse = { text, out };
  return out;
}

function typeMatches(chip: TypeChip, f: Flow): boolean {
  if (chip === "websocket") return f.kind === "websocket";
  const ct = contentType(f);
  const prefix = TYPE_PREFIX[chip];
  if (prefix) return !!f.response && ct.startsWith(prefix);
  if (chip === "js") return JS_TYPES.some((p) => ct.startsWith(p));
  if (chip === "media") return MEDIA_TYPES.some((p) => ct.startsWith(p));
  return f.kind !== "websocket" && !TYPE_CHIPS.some(([c]) => c !== "other" && c !== "websocket" && typeMatches(c, f));
}

/** The status chips as one range (the server takes one): neighbouring classes join, 2xx and 3xx read 200-399. */
export function statusRange(classes: readonly StatusClass[]): [number, number] | null {
  if (!classes.length) return null;
  const firsts = classes.map((c) => Number(c[0]) * 100);
  return [Math.min(...firsts), Math.max(...firsts) + 99];
}

/** A keyed store of the filters, kept for this page's session (sessionStorage) so a reload keeps them. */
export class FilterModel {
  private state: FilterState;
  private readonly listeners = new Set<(s: FilterState, prev: FilterState) => void>();
  private compiled: { focus: RegExp[]; ignore: RegExp[]; process: string; size: [number | null, number | null]; ms: [number | null, number | null] } | null = null;
  private compiledText: { text: string; typed: TypedFilter; domain: RegExp[]; on: boolean } | null = null;

  constructor() {
    let saved: Partial<FilterState> = {};
    try { saved = JSON.parse(sessionStorage.getItem(KEY) ?? "{}") as Partial<FilterState>; } catch { saved = {}; }
    this.state = { ...EMPTY_FILTERS };
    for (const k of Object.keys(EMPTY_FILTERS) as (keyof FilterState)[]) {
      const v = saved[k], d = EMPTY_FILTERS[k];
      if (typeof v === typeof d && Array.isArray(v) === Array.isArray(d)) (this.state as unknown as Record<string, unknown>)[k] = v;
    }
    this.state.status = this.state.status.filter((c) => STATUS_CLASSES.includes(c));
    this.state.methods = this.state.methods.filter((m) => METHODS.includes(m));
    if (this.state.type && !TYPE_CHIPS.some(([c]) => c === this.state.type)) this.state.type = "";
  }

  get(): FilterState { return this.state; }

  subscribe(fn: (s: FilterState, prev: FilterState) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  set<K extends keyof FilterState>(key: K, value: FilterState[K]): void { this.update({ [key]: value } as Partial<FilterState>); }

  update(patch: Partial<FilterState>): void {
    const prev = this.state;
    const next = { ...prev, ...patch };
    if ((Object.keys(patch) as (keyof FilterState)[]).every((k) => JSON.stringify(prev[k]) === JSON.stringify(next[k]))) return;
    this.state = next;
    this.compiled = null;
    try { sessionStorage.setItem(KEY, JSON.stringify(next)); } catch { /* this page only */ }
    for (const fn of this.listeners) fn(next, prev);
  }

  reset(): void { this.update({ ...EMPTY_FILTERS, body: this.state.body }); }

  /** A status class pressed: next to the chosen range it joins it, elsewhere it starts a new one; pressed again it leaves
   *  the range from that end (a class in the middle leaves the range to its lower side). */
  toggleStatus(c: StatusClass): void {
    const cur = this.state.status, has = cur.includes(c), n = Number(c[0]);
    const nums = cur.map((x) => Number(x[0])).sort();
    let next: number[];
    if (has) next = nums.filter((x) => x < n || (x > n && n === nums[0]));
    else if (nums.length && (n === nums[0]! - 1 || n === nums[nums.length - 1]! + 1)) next = [...nums, n];
    else next = [n];
    this.set("status", next.sort().map((x) => `${x}xx` as StatusClass));
  }

  toggleMethod(m: string): void {
    const cur = this.state.methods;
    this.set("methods", cur.includes(m) ? cur.filter((x) => x !== m) : METHODS.filter((x) => x === m || cur.includes(x)));
  }

  /** How many filters are on, for the fixed count slot. The body switch counts only while there is text to look for. */
  activeCount(text = ""): number {
    const s = this.state;
    return [s.status.length > 0, s.errors, s.methods.length > 0, !!s.type, s.fromSession, s.rules, s.throttled, num(s.minKB) !== null || num(s.maxKB) !== null, num(s.minMs) !== null || num(s.maxMs) !== null, !!s.process.trim(), this.focusList().length > 0, this.ignoreList().length > 0, s.body && !!parseTyped(text).rest].filter(Boolean).length;
  }

  focusList(): string[] { return hostList(this.state.focus); }

  ignoreList(): string[] { return hostList(this.state.ignore); }

  /** The query parameters for the list and the exports: the plain words of the text box (as `q`, or as `body` with the
   *  body switch on), the host from Hosts, and every filter the server can apply. A filter typed in the text box is sent
   *  in place of the chip or More filters field that sets the same parameter (the server takes one value); the ignored
   *  hosts of both join. */
  params(text: string, host: string): FlowParams {
    const s = this.state, p: FlowParams = {};
    const { rest, typed: k } = parseTyped(text);
    const bang = (not: boolean, v: string): string => (not ? `!${v}` : v);
    if (rest) p[s.body ? "body" : "q"] = rest;
    if (host) p.host = host;
    const range = statusRange(s.status);
    if (k.status) p.status = bang(k.status.not, k.status.value);
    else if (range) p.status = range[0] + 99 === range[1] ? `${range[0] / 100}xx` : `${range[0]}-${range[1]}`;
    if (s.errors) p.error = "1";
    if (k.method) p.method = bang(k.method.not, k.method.list.join(","));
    else if (s.methods.length) p.method = s.methods.join(",");
    if (s.type === "websocket") p.kind = "websocket";
    if (k.type) p.type = bang(k.type.not, k.type.prefix);
    else if (s.type && TYPE_PREFIX[s.type]) p.type = TYPE_PREFIX[s.type] ?? "";
    if (s.fromSession) p.from_session = "1";
    if (s.rules) p.rules = "1";
    if (s.throttled) p.throttled = "1";
    const minKB = num(s.minKB), maxKB = num(s.maxKB), minMs = num(s.minMs), maxMs = num(s.maxMs);
    if (k.size && !k.size.not) p.min_size = String(k.size.bytes + 1);
    else if (minKB !== null) p.min_size = String(Math.round(minKB * 1024));
    if (k.size?.not) p.max_size = String(k.size.bytes);
    else if (maxKB !== null) p.max_size = String(Math.round(maxKB * 1024));
    if (minMs !== null) p.min_ms = String(minMs);
    if (maxMs !== null) p.max_ms = String(maxMs);
    if (k.process) p.process = bang(k.process.not, k.process.text);
    else if (s.process.trim()) p.process = s.process.trim();
    const focus = k.domain && !k.domain.not ? k.domain.globs : this.focusList();
    if (focus.length) p.focus = focus.join(",");
    const ignore = [...this.ignoreList(), ...(k.domain?.not ? k.domain.globs : [])];
    if (ignore.length) p.ignore = ignore.join(",");
    return p;
  }

  /** Filters the query leaves out, so an export holds more than the list shows; empty when none. A type chip the server
   *  cannot apply (js, media, other) is named by its chip; a chip or field a typed filter took the place of is named
   *  status, method, type, size, process or focus. */
  pageOnly(text = ""): string[] {
    const s = this.state, k = parseTyped(text).typed, out: string[] = [];
    if (s.type === "js" || s.type === "media" || s.type === "other") out.push(s.type);
    if (k.status && s.status.length) out.push("status");
    if (k.method && s.methods.length) out.push("method");
    if (k.type && s.type && TYPE_PREFIX[s.type]) out.push("type");
    if (k.size && num(k.size.not ? s.maxKB : s.minKB) !== null) out.push("size");
    if (k.process && s.process.trim()) out.push("process");
    if (k.domain && !k.domain.not && this.focusList().length) out.push("focus");
    return out;
  }

  /** Whether a flow passes every property filter, the way the server's flow.Filter decides it: the chips and More
   *  filters, then the filters typed in `text`. The plain words of the text box and the body search are not judged here. */
  matches(f: Flow, now = Date.now(), text = ""): boolean {
    const s = this.state;
    const c = (this.compiled ??= {
      focus: globs(this.focusList()),
      ignore: globs(this.ignoreList()),
      process: s.process.trim().toLowerCase(),
      size: [num(s.minKB) === null ? null : Math.round(num(s.minKB)! * 1024), num(s.maxKB) === null ? null : Math.round(num(s.maxKB)! * 1024)],
      ms: [num(s.minMs), num(s.maxMs)],
    });
    const range = statusRange(s.status);
    if (range && (f.status < range[0] || f.status > range[1])) return false;
    if (s.errors && !f.error) return false;
    if (s.methods.length && !s.methods.includes(f.method.toUpperCase())) return false;
    if (s.type && !typeMatches(s.type, f)) return false;
    const d = flowData(f);
    if (s.fromSession && !d.from_session) return false;
    if (s.rules && !f.rules?.length) return false;
    if (s.throttled && !f.throttled) return false;
    if (c.size[0] !== null || c.size[1] !== null) {
      const size = bodySize(f);
      if ((c.size[0] !== null && size < c.size[0]) || (c.size[1] !== null && size > c.size[1])) return false;
    }
    if (c.ms[0] !== null || c.ms[1] !== null) {
      const ms = f.pending ? now - Date.parse(f.start) : f.duration_ms;
      if ((c.ms[0] !== null && ms < c.ms[0]) || (c.ms[1] !== null && ms > c.ms[1])) return false;
    }
    if (c.process && !(f.client_process ?? "").toLowerCase().includes(c.process)) return false;
    if (c.focus.length && !c.focus.some((r) => r.test(hostOf(f)))) return false;
    if (c.ignore.length && c.ignore.some((r) => r.test(hostOf(f)))) return false;
    if (!text) return true;
    if (this.compiledText?.text !== text) {
      const typed = parseTyped(text).typed;
      this.compiledText = { text, typed, domain: globs(typed.domain?.globs ?? []), on: Object.values(typed).some(Boolean) };
    }
    const k = this.compiledText.typed;
    if (!this.compiledText.on) return true;
    if (k.status && (f.status >= k.status.lo && f.status <= k.status.hi) === k.status.not) return false;
    if (k.method && k.method.list.includes(f.method.toUpperCase()) === k.method.not) return false;
    if (k.type && (!!f.response && contentType(f).startsWith(k.type.prefix)) === k.type.not) return false;
    if (k.size && (bodySize(f) > k.size.bytes) === k.size.not) return false;
    if (k.process && (f.client_process ?? "").toLowerCase().includes(k.process.text) === k.process.not) return false;
    if (k.domain && this.compiledText.domain.some((r) => r.test(hostOf(f))) === k.domain.not) return false;
    return true;
  }
}

export const flowFilters = new FilterModel();

let current: FlowQuery | null = null;

/** The Flows list's query, made once by the app; `onChange` runs when what the list shows may have changed. */
export function startFlowQuery(store: Store, onChange?: () => void): FlowQuery {
  current ??= new FlowQuery(store, onChange);
  return current;
}

/** The query startFlowQuery made, or null before (state.ts asks it which flows are shown). */
export const flowQuery = (): FlowQuery | null => current;

/** What the last server query said: the flows it matched (null while no query stands), the seq it covered, and whether
 *  the body search stopped at its bound. */
export interface QueryAnswer {
  hits: ReadonlySet<string> | null;
  seq: number;
  bodyCut: boolean;
  bodyScanned: number;
  busy: boolean;
}

/** The Flows list's server query. Whenever a filter or the text changes (250 ms after the last change) it asks
 *  GET /api/flows with every filter and the list's limit; the answer is the set of flows that match. Flows that arrive
 *  later are judged by `flowFilters.matches` until the next answer; with the body search on, which only the server can
 *  judge, new flows ask again (at most every 1.5 s). */
export class FlowQuery {
  private answer: QueryAnswer = { hits: null, seq: 0, bodyCut: false, bodyScanned: 0, busy: false };
  private readonly listeners = new Set<(a: QueryAnswer) => void>();
  private timer = 0;
  private gen = 0;
  private inflight = false;
  private again = false;
  private lastKey = "";

  constructor(private readonly store: Store, private readonly onChange: () => void = () => undefined) {
    flowFilters.subscribe(() => { this.changed(); this.onChange(); });
    store.subscribe((s, prev) => {
      if (s.filter !== prev.filter || s.hostFilter !== prev.hostFilter) this.changed();
      else if (s.flows !== prev.flows && this.bodySearch(s)) this.schedule(1500, true);
    });
  }

  get(): QueryAnswer { return this.answer; }

  subscribe(fn: (a: QueryAnswer) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private bodySearch(s: State): boolean { return flowFilters.get().body && !!parseTyped(s.filter).rest; }

  private key(): string {
    const s = this.store.get();
    return JSON.stringify(flowFilters.params(s.filter, s.hostFilter));
  }

  /** A filter or the text changed: the standing answer was to another question, so it stands aside at once (the page
   *  judges alone until the next answer) and the server is asked 250 ms after the last change. A body search keeps the
 *  answer it has, since the page cannot judge bodies. */
  private changed(): void {
    if (this.answer.hits && !this.bodySearch(this.store.get()) && this.key() !== this.lastKey) {
      this.answer = { ...this.answer, hits: null, seq: 0, bodyCut: false, bodyScanned: 0 };
      for (const fn of this.listeners) fn(this.answer);
    }
    this.schedule(250);
  }

  /** Whether a flow is shown: it passes the page's filters and, while a server answer stands, the answer named it or it
   *  arrived after the answer (the body search excepted: a flow the server has not judged waits for the next answer). */
  shows(f: Flow, s: State): boolean {
    if (!flowFilters.matches(f, undefined, s.filter)) return false;
    const a = this.answer;
    if (!a.hits) return !this.bodySearch(s);
    return a.hits.has(f.id) || (f.seq > a.seq && !this.bodySearch(s));
  }

  /** Ask now (a reconnect or a reload of the list). */
  reload(): void { this.schedule(0); }

  private schedule(ms: number, keep = false): void {
    if (keep && this.timer) return;
    window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => { this.timer = 0; this.run(); }, ms);
  }

  private set(a: Partial<QueryAnswer>): void {
    const was = this.answer;
    this.answer = { ...was, ...a };
    for (const fn of this.listeners) fn(this.answer);
    if (was.hits !== this.answer.hits || was.bodyCut !== this.answer.bodyCut) this.onChange();
  }

  private run(): void {
    const s = this.store.get();
    const params = flowFilters.params(s.filter, s.hostFilter);
    const active = Object.keys(params).some((k) => k !== "q" && k !== "host");
    const key = JSON.stringify(params);
    if (!active) {
      this.lastKey = key;
      if (this.answer.hits || this.answer.bodyCut || this.answer.busy) this.set({ hits: null, seq: 0, bodyCut: false, bodyScanned: 0, busy: false });
      return;
    }
    if (this.inflight) { this.again = true; return; }
    this.inflight = true;
    const gen = ++this.gen;
    const seq = s.lastSeq;
    if (key !== this.lastKey) this.set({ busy: true });
    this.lastKey = key;
    dataApi.flows({ ...params, limit: String(s.limit) })
      .then((r) => {
        if (gen !== this.gen || key !== this.key()) return;
        this.set({ hits: new Set((r.flows ?? []).map((f) => f.id)), seq, bodyCut: !!r.body_scan_cut, bodyScanned: r.body_scanned_bytes ?? 0, busy: false });
      })
      .catch((err: unknown) => {
        this.set({ busy: false });
        if (!(err instanceof SessionError)) this.store.update({ notice: err instanceof Error ? err.message : String(err) });
      })
      .finally(() => {
        this.inflight = false;
        if (this.again) { this.again = false; this.run(); }
      });
  }
}
