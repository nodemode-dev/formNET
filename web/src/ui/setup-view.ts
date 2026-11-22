import { api } from "../api.ts";
import { copyButton, h } from "../dom.ts";
import { dateTime, shellQuote } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { InterceptMode, Status } from "../types.ts";
import { setClass, setDisabled, setHidden, setText, setValue } from "./dom";
import { AUTO_NOTE, INTERCEPT_CHOICES } from "./panels/capture";
import { selectRow } from "./panels/controls";

// setupView explains how to point a program at the proxy and shows the CA state.
export function setupView(store: Store): HTMLElement {
  const proxy = proxyCard(store);
  const ca = caCard();
  const root = h("section", { class: "view view-setup", role: "tabpanel" }, proxy.el, ca.el);

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "setup";
    if (root.hidden || (s.status === prev.status && s.view === prev.view)) return;
    proxy.patch(s.status);
    ca.patch(s.status);
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), view: "flows" });
  return root;
}

interface Card {
  el: HTMLElement;
  patch: (st: Status | null) => void;
}

function proxyCard(store: Store): Card {
  let url = "";
  let edited = false;
  const addr = h("code", { class: "big" });
  const state = h("span", { class: "dim" });
  const examples = [h("code", {}), h("code", {}), h("code", {})];
  const mode = selectRow("Open HTTPS with the local root", INTERCEPT_CHOICES, (v) => {
    api.updateProxy({ intercept: v as InterceptMode }).then(refresh(store)).catch(fail(store));
  });
  const select = mode.el.querySelector("select") as HTMLSelectElement;
  const pass = h("textarea", { id: "passthrough", rows: "4", spellcheck: "false", placeholder: "one host per line, *.suffix allowed" }) as HTMLTextAreaElement;
  pass.addEventListener("input", () => { edited = true; });
  const save = h("button", { type: "button", class: "btn", onClick: () => {
    const list = pass.value.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
    api.updateProxy({ passthrough: list }).then(() => { edited = false; refresh(store)(); }).catch(fail(store));
  } }, "Save passthrough");
  const el = h(
    "div",
    { class: "card", id: "proxy-card" },
    h("h2", {}, "Proxy"),
    h("div", { class: "row" }, addr, copyButton("Copy", () => url), state),
    h("p", {}, "Point a program at it and its requests appear in Flows as they happen:"),
    h("ul", { class: "examples" }, ...examples.map((c) => h("li", {}, c, copyButton("Copy", () => c.textContent ?? "")))),
    mode.el,
    h("p", { class: "dim small" }, AUTO_NOTE),
    h("h3", {}, "Passthrough"),
    h("p", { class: "dim small" }, "Hosts that are never opened, for programs that pin their certificates. They are still counted by host and bytes, and the list is kept when FormNET restarts."),
    pass,
    h("div", { class: "row" }, save),
  );
  const patch = (st: Status | null) => {
    setDisabled(select, !st);
    setDisabled(save as HTMLButtonElement, !st);
    if (!st) {
      setText(state, "waiting for the status");
      return;
    }
    const p = st.proxy;
    url = `http://${p.addr}`;
    const pem = st.ca.path || "~/formnet/ca.pem";
    setText(addr, p.addr);
    setText(state, p.running ? "listening" : `not running${p.error ? `: ${p.error}` : ""}`);
    setClass(state, "dim", false);
    setClass(state, "ok", p.running);
    setClass(state, "st-error", !p.running);
    const lines = [
      `curl -x ${url} http://example.com/`,
      `curl -x ${url} --cacert ${shellQuote(pem)} https://example.com/`,
      `HTTPS_PROXY=${url} HTTP_PROXY=${url} some-program`,
    ];
    examples.forEach((c, i) => setText(c, lines[i] ?? ""));
    mode.set(p.intercept_mode ?? "");
    if (!edited) setValue(pass, (p.passthrough ?? []).join("\n"));
  };
  return { el, patch };
}

function caCard(): Card {
  const badge = h("span", { class: "badge" });
  const fingerprint = h("dd", { class: "mono" });
  const path = h("dd", { class: "mono" });
  const expires = h("dd", {});
  const hint = h("p", {});
  const command = h("code", { class: "cmd" });
  const trust = h("div", {}, hint, h("div", { class: "row" }, command, copyButton("Copy", () => command.textContent ?? "")));
  trust.hidden = true;
  const el = h(
    "div",
    { class: "card", id: "ca-card" },
    h("h2", {}, "Root certificate ", badge),
    h("p", {}, "formNET made this root on this computer. The private key stays in the data folder and is never served. In Auto, HTTPS is opened only once this computer trusts the root, and until then every HTTPS host is counted, not read."),
    h("dl", { class: "kv" }, h("dt", {}, "Fingerprint"), fingerprint, h("dt", {}, "Path"), path, h("dt", {}, "Expires"), expires),
    h("div", { class: "row" }, h("a", { class: "btn", href: api.caURL(), download: "formnet-ca.pem" }, "Download ca.pem")),
    trust,
  );
  const patch = (st: Status | null) => {
    const ca = st?.ca;
    setText(badge, !ca ? "" : ca.trusted ? "trusted" : "not trusted");
    setClass(badge, "ok", !!ca?.trusted);
    setClass(badge, "warnb", !!ca && !ca.trusted);
    setText(fingerprint, ca?.fingerprint ?? "");
    setText(path, ca?.path ?? "");
    setText(expires, dateTime(ca?.not_after));
    setText(hint, ca?.trust_hint || "Run this to trust the root. Nothing is installed until you run it yourself:");
    setText(command, ca?.trust_command ?? "");
    setHidden(trust, !ca?.trust_command);
  };
  return { el, patch };
}

function refresh(store: Store): () => void {
  return () => {
    api.status().then((status) => store.update({ status })).catch(fail(store));
  };
}

function fail(store: Store): (err: Error) => void {
  return (err) => store.update({ notice: err.message });
}
