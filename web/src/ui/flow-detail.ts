import { api, HTTPError } from "../api.ts";
import { copyButton, h } from "../dom.ts";
import { bytes, clock, dateTime, millis, shellQuote } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { FlowDetail } from "../types.ts";
import { flowData } from "../types-data.ts";
import { BodyViewer } from "./body-viewer";
import { patchList, setClass, setDisabled, setHidden, setText, setTitle } from "./dom";
import { onLanguage, t } from "./i18n";
import { icon } from "./icons";
import { ResendDialog } from "./resend-dialog";
import { WsMessagesPane } from "./ws-messages";
import { ruleFromFlow } from "./panels/rules";
import { toast } from "./toast";

type Tab = "summary" | "request" | "response" | "messages" | "timing";
const tabs: { id: Tab; label: string }[] = [
  { id: "summary", label: "Summary" },
  { id: "request", label: "Request" },
  { id: "response", label: "Response" },
  { id: "messages", label: "Messages" },
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
  const tabBar = h("div", { class: "cv-seg", role: "tablist", "aria-label": t("Flow details") });
  const loading = h("p", { class: "dim" }, t("Loading"));
  const panes: Record<Tab, Pane> = {
    summary: summaryPane(store),
    request: sidePane("Request", false),
    response: sidePane("Response", true),
    timing: timingPane(),
    messages: messagesPane(),
  };
  const body = h("div", { class: "detail-body" }, loading, ...tabs.map((x) => panes[x.id].el));
  const closeBtn = h("button", { type: "button", class: "cv-icon-btn", title: t("Close details"), "aria-label": t("Close details"), onClick: () => store.update({ selectedId: null }) });
  closeBtn.innerHTML = icon("close", 16);
  const root = h("aside", { class: "detail", "aria-label": t("Flow details") }, h("div", { class: "detail-head" }, title, closeBtn), h("div", { class: "detail-tabs" }, tabBar), body);
  root.hidden = true;

  for (const x of tabs) {
    tabBar.appendChild(
      h("button", { type: "button", role: "tab", "data-tab": x.id, onClick: () => {
        tab = x.id;
        paint();
      } }, t(x.label)),
    );
  }

  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && store.get().selectedId) store.update({ selectedId: null });
  });

  const paint = () => {
    const ws = current?.kind === "websocket";
    if (tab === "messages" && current && !ws) tab = "summary";
    for (const b of tabBar.children) {
      setDisabled(b as HTMLButtonElement, (b as HTMLElement).dataset["tab"] === "messages" && !ws);
      const on = (b as HTMLElement).dataset["tab"] === tab;
      setClass(b, "is-on", on);
      if (b.getAttribute("aria-selected") !== String(on)) b.setAttribute("aria-selected", String(on));
    }
    setHidden(loading, !!current);
    for (const x of tabs) setHidden(panes[x.id].el, !current || x.id !== tab);
    if (!current) return;
    setText(title, `${current.method} ${current.url}`);
    for (const x of tabs) panes[x.id].patch(current);
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
        setClass(body, "is-stale", false);
        paint();
      })
      .catch((err: Error) => {
        if (loadedFor === key) loadedFor = "";
        if (store.get().selectedId === id) store.update({ notice: t("Could not load the flow: {error}").replace("{error}", err.message) });
      });
  };

  const reset = () => {
    current = null;
    loadedFor = "";
    body.scrollTop = 0;
    setClass(body, "is-stale", false);
  };

  // Another selection keeps the panes of the last flow, dimmed, until its own detail arrives: emptying them for
  // "Loading" and filling them again would move everything below the title twice.
  const render = (s: State) => {
    const id = s.selectedId;
    const f = id ? s.flows.get(id) : undefined;
    root.hidden = !id || !f || s.view !== "flows";
    document.body.classList.toggle("has-detail", !root.hidden);
    if (!id || !f) {
      reset();
      return;
    }
    if (current && current.id !== id && !body.classList.contains("is-stale")) {
      loadedFor = "";
      body.scrollTop = 0;
      setClass(body, "is-stale", true);
    }
    if (!current) paint();
    load(id, f.pending ? `${f.seq}` : `${f.seq}:done`);
  };
  store.subscribe(render);
  onLanguage(() => { if (current) paint(); });
  return root;
}

/** A definition list with a fixed set of rows: every row is always there, so a value that arrives later fills its
 *  own line and nothing below it moves. */
function kv(labels: string[]): { el: HTMLElement; set: (values: (string | number | undefined | null)[]) => void } {
  const dds = labels.map(() => h("dd", { class: "mono" }));
  const dl = h("dl", { class: "kv" }, ...labels.flatMap((k, i) => [h("dt", {}, t(k)), dds[i] as HTMLElement]));
  return { el: dl, set: (values) => dds.forEach((dd, i) => setText(dd, values[i] === undefined || values[i] === null ? "" : String(values[i]))) };
}

function summaryPane(store: Store): Pane {
  let f: FlowDetail | null = null;
  const odd = h("span", { class: "warn small" }, t("method contains unusual characters"));
  const rows = kv(["Kind", "Status", "Protocol", "Started", "Duration", "Client", "Host", "TLS", "Sent", "Received", "Request type", "Response type", "Rules", "Notes", "Upstream proxy", "Source"]);
  const [rulesDd, notesDd] = Array.from(rows.el.querySelectorAll<HTMLElement>("dd")).slice(12, 14);
  for (const dd of [rulesDd, notesDd]) dd?.classList.add("kv-one");
  const ruleBtn = h("button", { type: "button", class: "cv-btn cv-btn-ghost", onClick: () => { if (f) ruleFromFlow(store, f); } }, t("Make a Rule...")) as HTMLButtonElement;
  const bpBtn = h("button", { type: "button", class: "cv-btn cv-btn-ghost", onClick: () => document.dispatchEvent(new CustomEvent("formnet:run", { detail: "breakpoints-show" })) }, t("Open Breakpoints")) as HTMLButtonElement;
  const refusedError = h("p", { class: "st-error mono" });
  const addBtn = h("button", { type: "button", class: "cv-btn", onClick: () => { if (f) addToPassthrough(store, f.host); } });
  const refused = h(
    "div",
    { class: "refused" },
    h("h3", {}, t("Certificate refused")),
    refusedError,
    h("p", {}, t("The program does not accept FormNET's certificate: it pins its own, or this computer does not trust the root.")),
    h("div", { class: "cv-improve" }, addBtn),
  );
  const ghost = "cv-btn cv-btn-ghost";
  const resendBtn = h("button", { type: "button", class: "cv-btn", onClick: () => { if (f) resendAsCaptured(store, f, resendBtn as HTMLButtonElement); } }) as HTMLButtonElement;
  resendBtn.innerHTML = icon("redo", 14);
  const resendLabel = h("span", {}, t("Resend"));
  resendBtn.append(resendLabel);
  const editBtn = h("button", { type: "button", class: "cv-btn cv-btn-ghost", onClick: () => { if (f) editAndResend(store, f); } }, t("Edit and resend...")) as HTMLButtonElement;
  const resendRow = h("div", { class: "cv-improve detail-resend" }, resendBtn, editBtn, ruleBtn, bpBtn);
  const fromLink = h("button", { type: "button", class: "linkish mono", onClick: () => {
    const orig = f?.resent_from;
    if (orig && store.get().flows.has(orig)) store.update({ selectedId: orig });
  } }) as HTMLButtonElement;
  const fromEdited = h("span", { class: "label" }, t("edited"));
  rows.el.append(h("div", {}, h("dt", {}, t("Resent from")), h("dd", {}, fromLink, fromEdited)));
  const el = h("div", {}, h("div", { class: "cv-improve" }, copyButton("Copy as curl", () => (f ? asCurl(f) : ""), ghost), copyButton("Copy URL", () => f?.url ?? "", ghost), odd), resendRow, rows.el, refused);
  return {
    el,
    patch: (next) => {
      f = next;
      setClass(resendRow, "is-off", f.kind !== "http");
      setDisabled(resendBtn, f.kind !== "http");
      setDisabled(editBtn, f.kind !== "http");
      setText(resendLabel, t("Resend"));
      setText(editBtn, t("Edit and resend..."));
      setDisabled(ruleBtn, f.kind !== "http");
      setText(ruleBtn, t("Make a Rule..."));
      setDisabled(bpBtn, !f.paused);
      setText(bpBtn, t("Open Breakpoints"));
      const orig = f.resent_from ? store.get().flows.get(f.resent_from) : undefined;
      setText(fromLink, !f.resent_from ? "" : orig ? `${clock(orig.start)} ${orig.method} ${orig.url}` : t("a flow no longer kept"));
      setTitle(fromLink, orig ? t("Show the original flow") : "");
      setDisabled(fromLink, !orig);
      setHidden(fromLink, !f.resent_from);
      setText(fromEdited, t("edited"));
      setHidden(fromEdited, !f.edited);
      const tls = !f.tls ? "" : f.tls.intercepted ? `${f.tls.version} ${f.tls.cipher}` : t("{version} {cipher} (not opened)").replace("{version}", f.tls.version).replace("{cipher}", f.tls.cipher);
      setHidden(odd, plainMethod.test(f.method));
      rows.set([
        f.kind,
        f.error ? t("error: {error}").replace("{error}", f.error) : f.paused ? t(f.paused === "response" ? "paused at the response" : "paused at the request") : f.status || (f.pending ? t("pending") : ""),
        protoText(f),
        dateTime(f.start),
        f.pending ? t("pending") : millis(f.duration_ms),
        f.client_process && !f.client ? t("{process} (pid {pid})").replace("{process}", f.client_process).replace("{pid}", String(f.client_pid ?? "?")) : f.client_process ? t("{process} (pid {pid}) from {client}").replace("{process}", f.client_process).replace("{pid}", String(f.client_pid ?? "?")).replace("{client}", f.client) : f.client,
        `${f.host}${f.port ? `:${f.port}` : ""}`,
        tls,
        bytes(f.bytes_sent),
        bytes(f.bytes_received),
        f.request.content_type,
        f.response?.content_type,
        (f.rules ?? []).join(", "),
        (f.notes ?? []).join(" "),
        flowData(f).upstream ?? t("direct"),
        flowData(f).from_session ? t(flowData(f).imported === "har" ? "Imported from a HAR file" : "Opened from a session file") : t("Captured in this run"),
      ]);
      for (const dd of [rulesDd, notesDd]) if (dd) setTitle(dd, dd.textContent ?? "");
      setHidden(refused, f.kind !== "refused");
      setText(refusedError, f.error ?? "");
      setText(addBtn, t("Add {host} to passthrough").replace("{host}", f.host));
    },
  };
}

function addToPassthrough(store: Store, host: string): void {
  api
    .proxy()
    .then((p) => {
      const list = p.passthrough ?? [];
      if (list.includes(host)) {
        toast(t("{host} is already in the passthrough list.").replace("{host}", host));
        return;
      }
      return api.updateProxy({ passthrough: [...list, host] }).then(() => {
        toast(t("Added {host} to passthrough: FormNET tunnels it from the next connection and only counts it.").replace("{host}", host));
        api.status().then((status) => store.update({ status })).catch(() => undefined);
      });
    })
    .catch((err: Error) => toast(err.message, "error"));
}

let editor: ResendDialog | null = null;

/** Selects the resent flow once the stream has brought it, and says so. */
function selectWhenKept(store: Store, id: string, edited: boolean): void {
  toast(t(edited ? "Resent with your edits as a new flow." : "Resent as a new flow."));
  let stop: () => void = () => undefined;
  const go = (): boolean => {
    if (!store.get().flows.has(id)) return false;
    stop();
    if (store.get().selectedId !== id) store.update({ selectedId: id });
    return true;
  };
  if (go()) return;
  stop = store.subscribe(() => { go(); });
  window.setTimeout(() => stop(), 15000);
}

function editAndResend(store: Store, f: FlowDetail): void {
  editor ??= new ResendDialog((id, edited) => selectWhenKept(store, id, edited));
  editor.show(f);
}

/** Resend as captured. A conflict (the body was cut at the limit, or the flow is not http) shows the server's words
 *  with the offer to edit the request first. */
function resendAsCaptured(store: Store, f: FlowDetail, btn: HTMLButtonElement): void {
  setDisabled(btn, true);
  api
    .resend(f.id)
    .then(({ id }) => selectWhenKept(store, id, false))
    .catch((err: Error) => {
      if (err instanceof HTTPError && err.status === 409 && f.kind === "http") toast(err.message, "error", { actions: [{ label: t("Edit and resend..."), run: () => editAndResend(store, f) }] });
      else toast(err.message, "error");
    })
    .finally(() => setDisabled(btn, false));
}

/** One side of the exchange. The response side keeps room for its headers and body from the start, so a pending flow
 *  that completes fills the reserved boxes instead of pushing the pane's content down. */
const SIDE_TEXT = {
  Request: { headers: "Request headers", body: "Request body", image: "request image" },
  Response: { headers: "Response headers", body: "Response body", image: "response image" },
} as const;

function sidePane(label: "Request" | "Response", reserve: boolean): Pane {
  const words = SIDE_TEXT[label];
  const headSize = h("span", { class: "dim" });
  const bodySize = h("span", { class: "dim" });
  const waiting = h("p", { class: "dim" });
  const none = h("p", { class: "dim" }, t("none"));
  const list = h("dl", { class: "kv headers" });
  const viewer = new BodyViewer(words.body);
  none.hidden = true;
  const el = h(
    "div",
    {},
    h("h3", {}, t(words.headers), headSize),
    h("div", { class: reserve ? "reserve-headers" : "" }, waiting, none, list),
    h("h3", {}, t(words.body), bodySize),
    viewer.el,
  );
  return {
    el,
    patch: (f) => {
      const m = label === "Request" ? f.request : f.response;
      const b = label === "Request" ? f.request_body : f.response_body;
      setText(waiting, m ? "" : f.pending ? t("No response yet.") : f.error ? t("Failed: {error}").replace("{error}", f.error) : t("No response was recorded."));
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
      const empty = !m ? t(f.pending ? "No response yet." : "No response was recorded.") : m.body_bytes > 0 ? t("{size} not captured").replace("{size}", bytes(m.body_bytes)) : t("empty");
      viewer.show(`${f.id}:${f.pending ? "pending" : "done"}:${b?.bytes ?? -1}`, b ?? null, m?.content_type ?? "", empty);
    },
  };
}

/** The Messages tab: a WebSocket's messages, fetched as the stream says they arrive. */
function messagesPane(): Pane {
  const pane = new WsMessagesPane();
  return { el: pane.el, patch: (f) => pane.show(flowData(f)) };
}

const SEGMENT_TEXT = ["connect {time}", "waiting {time}", "receiving {time}"];

function timingPane(): Pane {
  const names = ["connect", "waiting", "receiving"];
  const segs = names.map((name) => h("span", { class: `tseg t-${name}` }));
  const bar = h("div", { class: "tbar", role: "img", "aria-label": t("timing breakdown") }, ...segs);
  const flight = h("p", { class: "dim" }, t("Still in flight."));
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
        setTitle(seg, t(SEGMENT_TEXT[i] ?? "").replace("{time}", millis(ms)));
        const w = `${Math.max(0, (ms / total) * 100)}%`;
        if (seg.style.width !== w) seg.style.width = w;
      });
      rows.set([
        dateTime(f.start),
        f.pending ? "" : dateTime(f.end),
        f.connect_ms ? millis(f.connect_ms) : t("reused or unknown"),
        f.pending ? "" : millis(f.ttfb_ms),
        f.pending ? "" : millis(receiving),
        f.pending ? "" : millis(f.duration_ms),
      ]);
    },
  };
}

const skipInCurl = new Set(["host", "content-length", "connection", "proxy-connection", "accept-encoding"]);

const plainMethod = /^[A-Za-z]+$/;

/** What the program spoke to FormNET and what the server answered with, as one line. */
function protoText(f: FlowDetail): string {
  const d = flowData(f);
  if (d.client_proto && d.server_proto && d.client_proto !== d.server_proto) return t("{client} from the program, {server} to the server").replace("{client}", d.client_proto).replace("{server}", d.server_proto);
  return d.client_proto || d.server_proto || f.protocol;
}

function asCurl(f: FlowDetail): string {
  const parts = ["curl", "-X", shellQuote(f.method), shellQuote(f.url)];
  for (const x of f.request.headers ?? []) {
    if (skipInCurl.has(x.name.toLowerCase())) continue;
    parts.push("-H", shellQuote(`${x.name}: ${x.value}`));
  }
  if (f.request_body?.text) parts.push("--data-raw", shellQuote(f.request_body.text));
  return parts.join(" ");
}
