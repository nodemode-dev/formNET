import { copyText } from "../dom.ts";
import { shellQuote } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { Status } from "../types.ts";
import { el, q, setClass, setDisabled, setText, setTitle } from "./dom";
import { icon } from "./icons";
import { onLanguage, t, tn } from "./i18n";
import { tf } from "./panels/controls";

/** The Setup view: what the proxy's address is, whether HTTPS is opened or tunnelled and why, whether this computer
 *  trusts the root and the command that trusts it, and the lines that point a program at the proxy. No setting lives
 *  here: each block's button opens Preferences > Capture, where the HTTPS choice, the passthrough list and the root
 *  certificate are. Built once; a status refresh patches text and classes only. */
export function setupView(store: Store): HTMLElement {
  const guide = setupGuide(true);
  const root = el(`<section class="view view-setup" role="tabpanel"><div class="cv-setup"><div class="cv-setup-intro"><h1 class="cv-setup-h1"></h1><p class="cv-setup-sub"></p></div></div></section>`);
  setText(q(root, ".cv-setup-h1"), t("Setup"));
  setText(q(root, ".cv-setup-sub"), t("Point a program at the proxy, and trust the root if you want HTTPS opened. Settings are in Preferences > Capture."));
  q(root, ".cv-setup").appendChild(guide.el);
  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "setup";
    if (root.hidden || (s.status === prev.status && s.view === prev.view)) return;
    guide.patch(s.status);
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), view: "flows" });
  return root;
}

export interface SetupGuide {
  el: HTMLElement;
  patch: (st: Status | null) => void;
}

type Tone = "ok" | "warn" | "bad" | "";

/** The four blocks of Setup, shared by the Setup view and Home's Setup page (one instance each). `ids` gives the proxy
 *  block the id Help > Proxy Setup scrolls to, and the root block its own. */
export function setupGuide(ids = false): SetupGuide {
  const block = (ic: string, title: string, chip: [string, string] | null, stated = true): HTMLElement => {
    const b = el(`<section class="cv-setup-block"><div class="cv-setup-head"><span class="cv-setup-icon">${icon(ic, 18)}</span><h2 class="cv-setup-title"></h2>${stated ? '<span class="cv-setup-state"></span>' : ""}</div><p class="cv-setup-text"></p></section>`);
    setText(q(b, ".cv-setup-title"), t(title));
    if (chip) {
      const c = el<HTMLButtonElement>(`<button class="cv-btn cv-btn-ghost cv-setup-chip" type="button">${icon(chip[0] === "view-flows" ? "flows" : "settings", 14)}<span></span></button>`);
      c.dataset.cmd = chip[0];
      setText(q(c, "span:not(.icon)"), t(chip[1]));
      q(b, ".cv-setup-head").appendChild(c);
    }
    return b;
  };
  const line = (host: HTMLElement): { code: HTMLElement; copy: HTMLButtonElement } => {
    const l = el(`<div class="cv-setup-line"><code class="cv-setup-code" data-no-i18n></code><button class="cv-btn cv-btn-ghost cv-setup-copy" type="button">${icon("copy", 14)}<span></span></button></div>`);
    const code = q<HTMLElement>(l, "code"), copy = q<HTMLButtonElement>(l, "button"), label = q<HTMLElement>(copy, "span:not(.icon)");
    setText(label, t("Copy"));
    let timer = 0;
    copy.addEventListener("click", () => {
      const v = code.textContent ?? "";
      if (!v) return;
      void copyText(v).then((ok) => {
        setText(label, t(ok ? "Copied" : "Copy failed"));
        window.clearTimeout(timer);
        timer = window.setTimeout(() => setText(label, t("Copy")), 1200);
      });
    });
    host.appendChild(l);
    return { code, copy };
  };
  const tone = (n: HTMLElement, text: string, to: Tone): void => {
    setText(n, text);
    for (const k of ["ok", "warn", "bad"] as const) setClass(n, `is-${k}`, to === k);
  };

  const proxy = block("network", "Proxy address", ["prefs-capture", "Proxy settings..."]);
  if (ids) proxy.id = "proxy-card";
  const addr = line(proxy);
  addr.code.classList.add("cv-setup-addr");
  const https = block("lock", "HTTPS", ["prefs-capture", "HTTPS settings..."]);
  const pass = el('<p class="cv-setup-meta"></p>');
  https.appendChild(pass);
  const rootB = block("shield", "Root certificate", ["prefs-capture", "Certificate settings..."]);
  if (ids) rootB.id = "ca-card";
  const trust = line(rootB);
  const point = block("terminal", "Point a program at the proxy", ["view-flows", "Open Flows"], false);
  const examples = [line(point), line(point), line(point)];
  const wrap = el('<div class="cv-setup-blocks"></div>');
  wrap.append(proxy, https, rootB, point);
  const state = (b: HTMLElement): HTMLElement => q(b, ".cv-setup-state");
  const text = (b: HTMLElement): HTMLElement => q(b, ".cv-setup-text");

  let last: Status | null = null;
  const patch = (st: Status | null): void => {
    last = st;
    if (!st) {
      for (const b of [proxy, https, rootB]) tone(state(b), t("Waiting"), "");
      for (const b of [proxy, https, rootB, point]) setText(text(b), t("Waiting for the server's status."));
      setText(pass, "");
      for (const l of [addr, trust, ...examples]) { setText(l.code, ""); setDisabled(l.copy, true); }
      return;
    }
    const p = st.proxy, ca = st.ca, url = `http://${p.addr}`;
    tone(state(proxy), t(p.running ? "Listening" : "Not running"), p.running ? "ok" : "bad");
    setText(text(proxy), p.running
      ? t("Programs on this computer send their traffic to this address. It listens on this computer only; other computers cannot reach it.")
      : p.error ? tf("The proxy is not running: {error}", { error: p.error }) : t("The proxy is not running. FORMNET_PROXY_ADDR chooses its address when the server starts."));
    setText(addr.code, url);
    setDisabled(addr.copy, false);
    tone(state(https), t(p.intercept ? "Opened" : "Tunnelled"), p.intercept ? "ok" : "warn");
    const mode = p.intercept_mode;
    setText(text(https), t(
      mode === "on" ? "HTTPS is opened because the HTTPS choice is On. A program that does not trust the root refuses the connection."
      : mode === "off" ? "HTTPS is tunnelled because the HTTPS choice is Off: every HTTPS host is counted by host and bytes, never read."
      : mode === "auto" && p.intercept ? "HTTPS is opened because the HTTPS choice is Auto and this computer trusts the root."
      : mode === "auto" ? "HTTPS is tunnelled until this computer trusts the root (the HTTPS choice is Auto). Until then every HTTPS host is counted by host and bytes, never read."
      : p.intercept ? "HTTPS is opened with the local root." : "HTTPS is tunnelled: every HTTPS host is counted by host and bytes, never read."));
    const n = p.passthrough?.length ?? 0;
    setText(pass, n ? tn(n, "{n} host in the passthrough list is always tunnelled.", "{n} hosts in the passthrough list are always tunnelled.") : t("The passthrough list is empty: every host follows the HTTPS choice."));
    tone(state(rootB), t(ca.trusted ? "Trusted" : "Not trusted"), ca.trusted ? "ok" : "warn");
    setText(text(rootB), ca.trusted
      ? t("This computer trusts the root, so browsers and most apps accept what FormNET opens. The command below is the one that trusted it.")
      : t("Run this in a terminal to trust the root. Nothing is installed until you run it yourself; Certificate settings says what it does."));
    setText(trust.code, ca.trust_command || t("No command is known for this system: add ca.pem to its trusted certificates with the system's own tool."));
    setTitle(trust.code, ca.trust_command);
    setDisabled(trust.copy, !ca.trust_command);
    const pem = ca.path || "~/formnet/ca.pem";
    setText(text(point), t("Point a program at the proxy and its requests appear in Flows as they happen:"));
    const lines = [
      `curl -x ${url} http://example.com/`,
      `curl -x ${url} --cacert ${shellQuote(pem)} https://example.com/`,
      `HTTPS_PROXY=${url} HTTP_PROXY=${url} some-program`,
    ];
    examples.forEach((l, i) => { setText(l.code, lines[i] ?? ""); setTitle(l.code, lines[i] ?? ""); setDisabled(l.copy, false); });
  };
  patch(null);
  onLanguage(() => patch(last));
  return { el: wrap, patch };
}
