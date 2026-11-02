import { api } from "../api.ts";
import { copyButton, h, clear } from "../dom.ts";
import { dateTime, shellQuote } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { CAInfo, ProxyStatus } from "../types.ts";

// setupView explains how to point a program at the proxy and shows the CA state.
export function setupView(store: Store): HTMLElement {
  const proxyBox = h("div", { class: "card", id: "proxy-card" });
  const caBox = h("div", { class: "card", id: "ca-card" });
  const root = h("section", { class: "view view-setup", role: "tabpanel" }, proxyBox, caBox);
  let lastProxy: ProxyStatus | null = null;
  let lastCA: CAInfo | null = null;

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "setup";
    if (root.hidden) return;
    const st = s.status;
    if (!st) {
      clear(proxyBox);
      proxyBox.appendChild(h("p", { class: "dim" }, "Waiting for the status."));
      return;
    }
    if (st.proxy !== lastProxy || s.view !== prev.view) {
      lastProxy = st.proxy;
      clear(proxyBox);
      proxyBox.appendChild(proxyCard(st.proxy, s.status?.ca.path ?? "", store));
    }
    if (st.ca !== lastCA || s.view !== prev.view) {
      lastCA = st.ca;
      clear(caBox);
      caBox.appendChild(caCard(st.ca));
    }
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), view: "flows" });
  return root;
}

function proxyCard(p: ProxyStatus, caPath: string, store: Store): HTMLElement {
  const url = `http://${p.addr}`;
  const pem = caPath || "~/formnet/ca.pem";
  const examples = [
    `curl -x ${url} http://example.com/`,
    `curl -x ${url} --cacert ${shellQuote(pem)} https://example.com/`,
    `HTTPS_PROXY=${url} HTTP_PROXY=${url} some-program`,
  ];
  const intercept = h("input", { type: "checkbox", id: "intercept" }) as HTMLInputElement;
  intercept.checked = p.intercept;
  intercept.addEventListener("change", () => {
    api.updateProxy({ intercept: intercept.checked }).then(refresh(store)).catch(fail(store));
  });
  const pass = h("textarea", { id: "passthrough", rows: "4", spellcheck: "false", placeholder: "one host per line, *.suffix allowed" }) as HTMLTextAreaElement;
  pass.value = (p.passthrough ?? []).join("\n");
  const save = h("button", { type: "button", class: "btn", onClick: () => {
    const list = pass.value.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
    api.updateProxy({ passthrough: list }).then(refresh(store)).catch(fail(store));
  } }, "Save passthrough");
  return h(
    "div",
    {},
    h("h2", {}, "Proxy"),
    h("div", { class: "row" }, h("code", { class: "big" }, p.addr), copyButton("Copy", () => url), h("span", { class: p.running ? "ok" : "st-error" }, p.running ? "listening" : `not running${p.error ? `: ${p.error}` : ""}`)),
    h("p", {}, "Point a program at it and its requests appear in Flows as they happen:"),
    h("ul", { class: "examples" }, ...examples.map((e) => h("li", {}, h("code", {}, e), copyButton("Copy", () => e)))),
    h("div", { class: "row" }, intercept, h("label", { for: "intercept" }, "Open HTTPS with the local root (off: every HTTPS host is tunnelled and only counted)")),
    h("h3", {}, "Passthrough"),
    h("p", { class: "dim small" }, "Hosts that are never opened, for programs that pin their certificates. They are still counted by host and bytes."),
    pass,
    h("div", { class: "row" }, save),
  );
}

function caCard(ca: CAInfo): HTMLElement {
  return h(
    "div",
    {},
    h("h2", {}, "Root certificate ", h("span", { class: `badge ${ca.trusted ? "ok" : "warnb"}` }, ca.trusted ? "trusted" : "not trusted")),
    h("p", {}, "formNET made this root on this computer. The private key stays in the data folder and is never served. HTTPS is opened only after you make the computer trust the root, and until then every HTTPS host is counted, not read."),
    h("dl", { class: "kv" }, h("dt", {}, "Fingerprint"), h("dd", { class: "mono" }, ca.fingerprint), h("dt", {}, "Path"), h("dd", { class: "mono" }, ca.path), h("dt", {}, "Expires"), h("dd", {}, dateTime(ca.not_after))),
    h("div", { class: "row" }, h("a", { class: "btn", href: api.caURL(), download: "formnet-ca.pem" }, "Download ca.pem")),
    ca.trust_command ? h("div", {}, h("p", {}, ca.trust_hint || "Run this to trust the root. Nothing is installed until you run it yourself:"), h("div", { class: "row" }, h("code", { class: "cmd" }, ca.trust_command), copyButton("Copy", () => ca.trust_command))) : null,
  );
}

function refresh(store: Store): () => void {
  return () => {
    api.status().then((status) => store.update({ status })).catch(fail(store));
  };
}

function fail(store: Store): (err: Error) => void {
  return (err) => store.update({ notice: err.message });
}
