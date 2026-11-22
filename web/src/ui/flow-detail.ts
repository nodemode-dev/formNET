import { api } from "../api.ts";
import { copyButton, h } from "../dom.ts";
import { bytes, dateTime, isImageType, isJSONType, isTextType, millis, prettyJSON, shellQuote } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { Body, FlowDetail, Message } from "../types.ts";
import { patchList, setClass, setHidden, setText, setTitle, swapMedia } from "./dom";
import { toast } from "./toast";

type Tab = "summary" | "request" | "response" | "timing";
const tabs: { id: Tab; label: string }[] = [
  { id: "summary", label: "Summary" },
  { id: "request", label: "Request" },
  { id: "response", label: "Response" },
  { id: "timing", label: "Timing" },
];

interface Pane {
  el: HTMLElement;
  patch: (f: FlowDetail) => void;
}

// flowDetail is the side pane for the selected flow. It fetches the detail
// (with bodies) and re-fetches when the flow updates; the four panes are built
// once and patched in place.
export function flowDetail(store: Store): HTMLElement {
  let tab: Tab = "summary";
  let current: FlowDetail | null = null;
  let loadedFor = "";
  const title = h("div", { class: "detail-title mono" });
  const tabBar = h("div", { class: "subtabs", role: "tablist" });
  const loading = h("p", { class: "dim" }, "Loading");
  const panes: Record<Tab, Pane> = {
    summary: summaryPane(store),
    request: sidePane("Request", false),
    response: sidePane("Response", true),
    timing: timingPane(),
  };
  const body = h("div", { class: "detail-body" }, loading, ...tabs.map((t) => panes[t.id].el));
  const closeBtn = h("button", { type: "button", class: "btn btn-small", "aria-label": "Close details", onClick: () => store.update({ selectedId: null }) }, "Close");
  const root = h("aside", { class: "detail", "aria-label": "Flow details" }, h("div", { class: "detail-head" }, title, closeBtn), tabBar, body);
  root.hidden = true;

  for (const t of tabs) {
    tabBar.appendChild(
      h("button", { type: "button", class: "subtab", role: "tab", "data-tab": t.id, onClick: () => {
        tab = t.id;
        paint();
      } }, t.label),
    );
  }

  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && store.get().selectedId) store.update({ selectedId: null });
  });

  const paint = () => {
    for (const b of tabBar.children) {
      const on = (b as HTMLElement).dataset["tab"] === tab;
      setClass(b, "on", on);
      if (b.getAttribute("aria-selected") !== String(on)) b.setAttribute("aria-selected", String(on));
    }
    setHidden(loading, !!current);
    for (const t of tabs) setHidden(panes[t.id].el, !current || t.id !== tab);
    if (!current) return;
    setText(title, `${current.method} ${current.url}`);
    for (const t of tabs) panes[t.id].patch(current);
  };

  const load = (id: string, version: string) => {
    const key = `${id}:${version}`;
    if (key === loadedFor) return;
    loadedFor = key;
    api
      .flow(id)
      .then((d) => {
        // Answers for a flow that is no longer selected are dropped.
        if (store.get().selectedId !== id || d.id !== id) return;
        current = d;
        paint();
      })
      .catch((err: Error) => {
        if (loadedFor === key) loadedFor = "";
        if (store.get().selectedId === id) store.update({ notice: `Could not load the flow: ${err.message}` });
      });
  };

  const reset = () => {
    current = null;
    loadedFor = "";
    body.scrollTop = 0;
  };

  const render = (s: State) => {
    const id = s.selectedId;
    const f = id ? s.flows.get(id) : undefined;
    root.hidden = !id || !f || s.view !== "flows";
    document.body.classList.toggle("has-detail", !root.hidden);
    if (!id || !f) {
      reset();
      return;
    }
    if (current && current.id !== id) reset();
    if (!current) paint();
    load(id, f.pending ? `${f.seq}` : `${f.seq}:done`);
  };
  store.subscribe(render);
  return root;
}

/** A definition list with a fixed set of rows: every row is always there, so a value that arrives later fills its
 *  own line and nothing below it moves. */
function kv(labels: string[]): { el: HTMLElement; set: (values: (string | number | undefined | null)[]) => void } {
  const dds = labels.map(() => h("dd", { class: "mono" }));
  const dl = h("dl", { class: "kv" }, ...labels.flatMap((k, i) => [h("dt", {}, k), dds[i] as HTMLElement]));
  return { el: dl, set: (values) => dds.forEach((dd, i) => setText(dd, values[i] === undefined || values[i] === null ? "" : String(values[i]))) };
}

function summaryPane(store: Store): Pane {
  let f: FlowDetail | null = null;
  const odd = h("span", { class: "warn small" }, "method contains unusual characters");
  const rows = kv(["Kind", "Status", "Protocol", "Started", "Duration", "Client", "Host", "TLS", "Sent", "Received", "Request type", "Response type"]);
  const refusedError = h("p", { class: "st-error mono" });
  const addBtn = h("button", { type: "button", class: "btn", onClick: () => { if (f) addToPassthrough(store, f.host); } });
  const refused = h(
    "div",
    { class: "refused" },
    h("h3", {}, "Certificate refused"),
    refusedError,
    h("p", {}, "The program does not accept FormNET's certificate: it pins its own, or this computer does not trust the root."),
    h("div", { class: "row" }, addBtn),
  );
  const el = h("div", {}, h("div", { class: "row" }, copyButton("Copy as curl", () => (f ? asCurl(f) : "")), copyButton("Copy URL", () => f?.url ?? ""), odd), rows.el, refused);
  return {
    el,
    patch: (next) => {
      f = next;
      const tls = f.tls ? `${f.tls.version} ${f.tls.cipher}${f.tls.intercepted ? "" : " (not opened)"}` : "";
      setHidden(odd, plainMethod.test(f.method));
      rows.set([
        f.kind,
        f.error ? `error: ${f.error}` : f.status || (f.pending ? "pending" : ""),
        f.protocol,
        dateTime(f.start),
        f.pending ? "pending" : millis(f.duration_ms),
        f.client_process ? `${f.client_process} (pid ${f.client_pid ?? "?"}) from ${f.client}` : f.client,
        `${f.host}${f.port ? `:${f.port}` : ""}`,
        tls,
        bytes(f.bytes_sent),
        bytes(f.bytes_received),
        f.request.content_type,
        f.response?.content_type,
      ]);
      setHidden(refused, f.kind !== "refused");
      setText(refusedError, f.error ?? "");
      setText(addBtn, `Add ${f.host} to passthrough`);
    },
  };
}

function addToPassthrough(store: Store, host: string): void {
  api
    .proxy()
    .then((p) => {
      const list = p.passthrough ?? [];
      if (list.includes(host)) {
        toast(`${host} is already in the passthrough list.`);
        return;
      }
      return api.updateProxy({ passthrough: [...list, host] }).then(() => {
        toast(`Added ${host} to passthrough: FormNET tunnels it from the next connection and only counts it.`);
        api.status().then((status) => store.update({ status })).catch(() => undefined);
      });
    })
    .catch((err: Error) => toast(err.message, "error"));
}

/** One side of the exchange. The response side keeps room for its headers and body from the start, so a pending flow
 *  that completes fills the reserved boxes instead of pushing the pane's content down. */
function sidePane(label: "Request" | "Response", reserve: boolean): Pane {
  let text = "";
  let shownBody = "";
  const headSize = h("span", { class: "dim" });
  const bodySize = h("span", { class: "dim" });
  const waiting = h("p", { class: "dim" });
  const none = h("p", { class: "dim" }, "none");
  const list = h("dl", { class: "kv headers" });
  const enc = h("p", { class: "small" });
  const trunc = h("p", { class: "warn" });
  const empty = h("p", { class: "dim" });
  const img = h("img", { class: "preview", alt: `${label.toLowerCase()} image` }) as HTMLImageElement;
  const copyRow = h("div", { class: "row" }, copyButton("Copy body", () => text));
  const pre = h("pre", { class: "body mono" });
  const binary = h("p", { class: "dim" });
  for (const n of [none, enc, trunc, empty, img, copyRow, pre, binary]) n.hidden = true;
  const el = h(
    "div",
    {},
    h("h3", {}, `${label} headers`, headSize),
    h("div", { class: reserve ? "reserve-headers" : "" }, waiting, none, list),
    h("h3", {}, `${label} body`, bodySize),
    h("div", { class: reserve ? "reserve-body" : "" }, enc, trunc, empty, img, copyRow, pre, binary),
  );
  return {
    el,
    patch: (f) => {
      const m = label === "Request" ? f.request : f.response;
      const b = label === "Request" ? f.request_body : f.response_body;
      setText(waiting, m ? "" : f.pending ? "No response yet." : f.error ? `Failed: ${f.error}` : "No response was recorded.");
      setHidden(waiting, !!m);
      setText(headSize, m ? ` ${bytes(m.header_bytes)}` : "");
      const hs = m?.headers ?? [];
      setHidden(none, !m || hs.length > 0);
      patchList(list, hs.map((x, i) => ({ x, k: `${i}:${x.name}` })), {
        key: (e) => e.k,
        create: ({ x }) => h("div", {}, h("dt", { class: "mono" }, x.name), h("dd", { class: "mono" }, x.value)),
        update: (node, { x }) => setText(node.lastElementChild, x.value),
      });
      setText(bodySize, m ? ` ${bytes(m.body_bytes)}${m.encoding && !b?.encoding ? `, ${m.encoding}` : ""}` : "");
      patchBody(m, b);
    },
  };

  function patchBody(m: Message | undefined, b: Body | undefined): void {
    const has = !!m && !!b && b.bytes > 0;
    const image = has && !!b?.base64 && isImageType(m?.content_type);
    const isText = has && !image && b?.text !== undefined;
    setText(empty, !m ? "" : m.body_bytes > 0 ? `${bytes(m.body_bytes)} not captured` : "empty");
    setHidden(empty, !m || has);
    const encoding = has ? b?.encoding ?? "" : "";
    setText(enc, !encoding ? "" : b?.decoded ? `${encoding}, decoded` : `${encoding}, not decoded`);
    setClass(enc, "warn", !!encoding && !b?.decoded);
    setClass(enc, "dim", !!encoding && !!b?.decoded);
    setHidden(enc, !encoding);
    setText(trunc, has && b?.truncated && m ? `Showing the first ${bytes(b.bytes)} of ${bytes(m.body_bytes)}; the rest was not kept (body limit).` : "");
    setHidden(trunc, !(has && b?.truncated));
    setHidden(img, !image);
    if (image && m && b?.base64) {
      const src = `data:${m.content_type.split(";")[0] ?? "image/png"};base64,${b.base64}`;
      if (img.getAttribute("src") !== src) void swapMedia(img, src).catch(() => undefined);
    }
    text = isText ? b?.text ?? "" : "";
    const shown = isText && m ? (isJSONType(m.content_type) ? prettyJSON(text) : text) : "";
    if (shown !== shownBody) {
      shownBody = shown;
      pre.textContent = shown;
    }
    setHidden(copyRow, !isText);
    setHidden(pre, !isText);
    const bin = has && !image && !isText && m && b;
    setText(binary, bin ? `${bytes(b.bytes)} of ${isTextType(m.content_type) ? "text that is not valid UTF-8" : "binary"} data${m.encoding ? ` (${m.encoding})` : ""}; export a HAR to get the bytes.` : "");
    setHidden(binary, !bin);
  }
}

function timingPane(): Pane {
  const names = ["connect", "waiting", "receiving"];
  const segs = names.map((name) => h("span", { class: `tseg t-${name}` }));
  const bar = h("div", { class: "tbar", role: "img", "aria-label": "timing breakdown" }, ...segs);
  const flight = h("p", { class: "dim" }, "Still in flight.");
  const rows = kv(["Started", "Ended", "Connect (dial and TLS)", "Waiting (first byte)", "Receiving", "Total"]);
  const el = h("div", {}, h("div", { class: "tslot" }, flight, bar), rows.el);
  return {
    el,
    patch: (f) => {
      const receiving = Math.max(0, f.duration_ms - f.connect_ms - f.ttfb_ms);
      const total = Math.max(f.duration_ms, 0.001);
      setHidden(flight, !f.pending);
      setHidden(bar, f.pending);
      [f.connect_ms, f.ttfb_ms, receiving].forEach((ms, i) => {
        const seg = segs[i] as HTMLElement;
        setTitle(seg, `${names[i]} ${millis(ms)}`);
        const w = `${Math.max(0, (ms / total) * 100)}%`;
        if (seg.style.width !== w) seg.style.width = w;
      });
      rows.set([
        dateTime(f.start),
        f.pending ? "" : dateTime(f.end),
        f.connect_ms ? millis(f.connect_ms) : "reused or unknown",
        f.pending ? "" : millis(f.ttfb_ms),
        f.pending ? "" : millis(receiving),
        f.pending ? "" : millis(f.duration_ms),
      ]);
    },
  };
}

const skipInCurl = new Set(["host", "content-length", "connection", "proxy-connection", "accept-encoding"]);

const plainMethod = /^[A-Za-z]+$/;

function asCurl(f: FlowDetail): string {
  const parts = ["curl", "-X", shellQuote(f.method), shellQuote(f.url)];
  for (const x of f.request.headers ?? []) {
    if (skipInCurl.has(x.name.toLowerCase())) continue;
    parts.push("-H", shellQuote(`${x.name}: ${x.value}`));
  }
  if (f.request_body?.text) parts.push("--data-raw", shellQuote(f.request_body.text));
  return parts.join(" ");
}
