import { api } from "../api.ts";
import { copyButton, h, clear } from "../dom.ts";
import { bytes, dateTime, isImageType, isJSONType, isTextType, millis, prettyJSON, shellQuote } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { Body, FlowDetail, Header, Message } from "../types.ts";

type Tab = "summary" | "request" | "response" | "timing";
const tabs: { id: Tab; label: string }[] = [
  { id: "summary", label: "Summary" },
  { id: "request", label: "Request" },
  { id: "response", label: "Response" },
  { id: "timing", label: "Timing" },
];

// flowDetail is the side pane for the selected flow. It fetches the detail
// (with bodies) and re-fetches when the flow updates.
export function flowDetail(store: Store): HTMLElement {
  let tab: Tab = "summary";
  let current: FlowDetail | null = null;
  let loadedFor = "";
  const title = h("div", { class: "detail-title mono" });
  const tabBar = h("div", { class: "subtabs", role: "tablist" });
  const body = h("div", { class: "detail-body" });
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
      b.classList.toggle("on", on);
      b.setAttribute("aria-selected", on ? "true" : "false");
    }
    clear(body);
    if (!current) {
      body.appendChild(h("p", { class: "dim" }, "Loading"));
      return;
    }
    title.textContent = `${current.method} ${current.url}`;
    const f = current;
    if (tab === "summary") body.appendChild(summary(f));
    if (tab === "request") body.appendChild(side(f.request, f.request_body, "Request"));
    if (tab === "response") body.appendChild(f.response ? side(f.response, f.response_body, "Response") : h("p", { class: "dim" }, f.pending ? "No response yet." : f.error ? `Failed: ${f.error}` : "No response was recorded."));
    if (tab === "timing") body.appendChild(timing(f));
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

function kv(rows: [string, string | number | undefined | null][]): HTMLElement {
  const dl = h("dl", { class: "kv" });
  for (const [k, v] of rows) {
    if (v === undefined || v === null || v === "") continue;
    dl.appendChild(h("dt", {}, k));
    dl.appendChild(h("dd", { class: "mono" }, String(v)));
  }
  return dl;
}

function summary(f: FlowDetail): HTMLElement {
  const tls = f.tls ? `${f.tls.version} ${f.tls.cipher}${f.tls.intercepted ? "" : " (not opened)"}` : "";
  return h(
    "div",
    {},
    h("div", { class: "row" }, copyButton("Copy as curl", () => asCurl(f)), copyButton("Copy URL", () => f.url), plainMethod.test(f.method) ? null : h("span", { class: "warn small" }, "method contains unusual characters")),
    kv([
      ["Kind", f.kind],
      ["Status", f.error ? `error: ${f.error}` : f.status || (f.pending ? "pending" : "")],
      ["Protocol", f.protocol],
      ["Started", dateTime(f.start)],
      ["Duration", f.pending ? "pending" : millis(f.duration_ms)],
      ["Client", f.client_process ? `${f.client_process} (pid ${f.client_pid ?? "?"}) from ${f.client}` : f.client],
      ["Host", `${f.host}${f.port ? `:${f.port}` : ""}`],
      ["TLS", tls],
      ["Sent", bytes(f.bytes_sent)],
      ["Received", bytes(f.bytes_received)],
      ["Request type", f.request.content_type],
      ["Response type", f.response?.content_type],
    ]),
  );
}

function side(m: Message, b: Body | undefined, label: string): HTMLElement {
  const wrap = h("div", {});
  wrap.appendChild(h("h3", {}, `${label} headers`, h("span", { class: "dim" }, ` ${bytes(m.header_bytes)}`)));
  wrap.appendChild(headers(m.headers ?? []));
  wrap.appendChild(h("h3", {}, `${label} body`, h("span", { class: "dim" }, ` ${bytes(m.body_bytes)}${m.encoding ? `, ${m.encoding}` : ""}`)));
  wrap.appendChild(bodyView(m, b));
  return wrap;
}

function headers(list: Header[]): HTMLElement {
  if (list.length === 0) return h("p", { class: "dim" }, "none");
  const dl = h("dl", { class: "kv headers" });
  for (const x of list) {
    dl.appendChild(h("dt", { class: "mono" }, x.name));
    dl.appendChild(h("dd", { class: "mono" }, x.value));
  }
  return dl;
}

function bodyView(m: Message, b: Body | undefined): HTMLElement {
  if (!b || b.bytes === 0) return h("p", { class: "dim" }, m.body_bytes > 0 ? `${bytes(m.body_bytes)} not captured` : "empty");
  const wrap = h("div", {});
  if (b.truncated) wrap.appendChild(h("p", { class: "warn" }, `Showing the first ${bytes(b.bytes)} of ${bytes(m.body_bytes)}; the rest was not kept (body limit).`));
  if (b.base64 && isImageType(m.content_type)) {
    const mime = m.content_type.split(";")[0] ?? "image/png";
    wrap.appendChild(h("img", { class: "preview", alt: "response image", src: `data:${mime};base64,${b.base64}` }));
  } else if (b.text !== undefined) {
    const txt = isJSONType(m.content_type) ? prettyJSON(b.text) : b.text;
    wrap.appendChild(h("div", { class: "row" }, copyButton("Copy body", () => b.text ?? "")));
    wrap.appendChild(h("pre", { class: "body mono" }, txt));
  } else {
    const note = isTextType(m.content_type) ? "text that is not valid UTF-8" : "binary";
    wrap.appendChild(h("p", { class: "dim" }, `${bytes(b.bytes)} of ${note} data${m.encoding ? ` (${m.encoding})` : ""}; export a HAR to get the bytes.`));
  }
  return wrap;
}

function timing(f: FlowDetail): HTMLElement {
  const total = Math.max(f.duration_ms, 0.001);
  const parts: [string, number][] = [
    ["connect", f.connect_ms],
    ["waiting", f.ttfb_ms],
    ["receiving", Math.max(0, f.duration_ms - f.connect_ms - f.ttfb_ms)],
  ];
  const bar = h("div", { class: "tbar", role: "img", "aria-label": "timing breakdown" });
  for (const [name, ms] of parts) {
    const seg = h("span", { class: `tseg t-${name}`, title: `${name} ${millis(ms)}` });
    seg.style.width = `${Math.max(0, (ms / total) * 100)}%`;
    bar.appendChild(seg);
  }
  return h(
    "div",
    {},
    f.pending ? h("p", { class: "dim" }, "Still in flight.") : bar,
    kv([
      ["Started", dateTime(f.start)],
      ["Ended", dateTime(f.end)],
      ["Connect (dial and TLS)", f.connect_ms ? millis(f.connect_ms) : "reused or unknown"],
      ["Waiting (first byte)", millis(f.ttfb_ms)],
      ["Receiving", millis(Math.max(0, f.duration_ms - f.connect_ms - f.ttfb_ms))],
      ["Total", millis(f.duration_ms)],
    ]),
  );
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
