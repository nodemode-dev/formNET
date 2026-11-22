import { api } from "../api.ts";
import { copyText } from "../dom.ts";
import type { State, Store } from "../state.ts";
import type { Recipe, Recipes, Status } from "../types.ts";
import { el, q, setClass, setDisabled, setText, setTitle } from "./dom";
import { icon } from "./icons";
import { onLanguage, t, tn } from "./i18n";
import { tf } from "./panels/controls";
import { sysproxyPill, sysproxySentence } from "./panels/system-proxy";

/** The runtimes the recipes cover, in the order the server sends them; the names are program names, never translated. */
const RUNTIMES: ReadonlyArray<readonly [string, string]> = [["curl", "curl"], ["node", "Node.js"], ["python", "Python"], ["java", "Java"], ["go", "Go"], ["git", "git"]];
const SHELLS: ReadonlyArray<readonly [Recipe["shell"], string]> = [["sh", "sh"], ["powershell", "PowerShell"]];

/** One answer of GET /api/recipes per proxy address and root path, shared by both Setup instances. */
let recipesFor = "";
let recipesAsked: Promise<Recipes> | null = null;
function loadRecipes(key: string): Promise<Recipes> {
  if (key !== recipesFor || !recipesAsked) {
    recipesFor = key;
    recipesAsked = api.recipes();
    recipesAsked.catch(() => { if (recipesFor === key) recipesAsked = null; });
  }
  return recipesAsked;
}

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
  const sys = block("globe", "System proxy", ["prefs-capture", "System proxy settings..."]);
  q(sys, ".cv-setup-text").classList.add("cv-setup-clamp");
  const point = block("terminal", "Point a program at the proxy", ["view-flows", "Open Flows"], false);
  const recipes = recipeBox(point);
  const wrap = el('<div class="cv-setup-blocks"></div>');
  wrap.append(proxy, https, rootB, sys, point);
  const state = (b: HTMLElement): HTMLElement => q(b, ".cv-setup-state");
  const text = (b: HTMLElement): HTMLElement => q(b, ".cv-setup-text");

  let last: Status | null = null;
  const patch = (st: Status | null): void => {
    last = st;
    if (!st) {
      for (const b of [proxy, https, rootB, sys]) tone(state(b), t("Waiting"), "");
      for (const b of [proxy, https, rootB, sys, point]) setText(text(b), t("Waiting for the server's status."));
      setText(pass, "");
      for (const l of [addr, trust]) { setText(l.code, ""); setDisabled(l.copy, true); }
      recipes.patch(null);
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
    const [pill, pillTone] = sysproxyPill(st.sysproxy);
    tone(state(sys), t(pill), pillTone);
    const sysText = sysproxySentence(st.sysproxy);
    setText(text(sys), sysText);
    setTitle(text(sys), sysText);
    setText(text(point), t("Choose the shell and the program; run the lines in the terminal that starts it, and its requests appear in Flows as they happen."));
    recipes.patch(st);
  };
  patch(null);
  onLanguage(() => patch(last));
  return { el: wrap, patch };
}

interface RecipeBox {
  patch: (st: Status | null) => void;
}

/** Setup's recipes: a choice of shell and of program, the lines for that pair in one code block with Copy, and the
 *  server's note. Built once at its full size; a choice or an answer from the server patches the text in place. The
 *  lines and the program names are data and carry translate="no". */
function recipeBox(host: HTMLElement): RecipeBox {
  const box = el(`<div class="cv-setup-recipes"><div class="cv-setup-pick"><div class="cv-seg cv-setup-shells" role="group" translate="no"></div><div class="cv-seg cv-setup-runtimes" role="group" translate="no"></div></div><div class="cv-setup-recipe"><div class="cv-setup-recipe-head"><span class="cv-setup-recipe-title" translate="no"></span><button class="cv-btn cv-btn-ghost cv-setup-copy" type="button">${icon("copy", 14)}<span></span></button></div><pre class="cv-setup-lines" translate="no" data-no-i18n tabindex="0"></pre><p class="cv-setup-note"></p></div></div>`);
  const shellBar = q<HTMLElement>(box, ".cv-setup-shells"), runBar = q<HTMLElement>(box, ".cv-setup-runtimes");
  const title = q<HTMLElement>(box, ".cv-setup-recipe-title"), pre = q<HTMLElement>(box, ".cv-setup-lines"), note = q<HTMLElement>(box, ".cv-setup-note");
  const copy = q<HTMLButtonElement>(box, ".cv-setup-copy"), copyLabel = q<HTMLElement>(copy, "span:not(.icon)");
  const seg = (bar: HTMLElement, items: ReadonlyArray<readonly [string, string]>, pick: (id: string) => void): Map<string, HTMLButtonElement> => {
    const out = new Map<string, HTMLButtonElement>();
    for (const [id, name] of items) {
      const b = el<HTMLButtonElement>('<button type="button" aria-pressed="false" disabled></button>');
      b.textContent = name;
      b.addEventListener("click", () => pick(id));
      bar.appendChild(b);
      out.set(id, b);
    }
    return out;
  };
  let chosenShell: Recipe["shell"] | null = null;
  let runtime = "curl";
  let data: Recipes | null = null;
  let shownKey = "";
  let last: Status | null = null;
  let failed = "";
  const shells = seg(shellBar, SHELLS, (id) => { chosenShell = id as Recipe["shell"]; render(); });
  const runtimes = seg(runBar, RUNTIMES, (id) => { runtime = id; render(); });
  let timer = 0;
  copy.addEventListener("click", () => {
    const v = pre.textContent ?? "";
    if (!v) return;
    void copyText(v).then((ok) => {
      setText(copyLabel, t(ok ? "Copied" : "Copy failed"));
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setText(copyLabel, t("Copy")), 1200);
    });
  });
  const mark = (b: HTMLButtonElement, on: boolean, enabled: boolean): void => {
    setClass(b, "is-on", on);
    if (b.getAttribute("aria-pressed") !== String(on)) b.setAttribute("aria-pressed", String(on));
    setDisabled(b, !enabled);
  };
  const render = (): void => {
    const list = data?.recipes ?? [];
    const have = new Set(list.map((r) => r.shell));
    let shell: Recipe["shell"] = chosenShell ?? (data?.platform === "windows" ? "powershell" : "sh");
    if (!have.has(shell)) shell = list[0]?.shell ?? shell;
    for (const [id, b] of shells) mark(b, !!data && id === shell, have.has(id as Recipe["shell"]));
    const byId = new Map(list.filter((r) => r.shell === shell).map((r) => [r.id.replace(/-(sh|powershell)$/, ""), r] as const));
    if (data && !byId.has(runtime)) runtime = byId.keys().next().value ?? runtime;
    for (const [id, b] of runtimes) mark(b, !!data && id === runtime, byId.has(id));
    const r = data ? byId.get(runtime) : undefined;
    setText(title, r?.title ?? "");
    const lines = r ? r.lines.join("\n") : "";
    setText(pre, lines);
    setText(note, r ? r.note : failed ? tf("The recipes could not be read: {error}", { error: failed }) : t("Waiting for the server's status."));
    setDisabled(copy, !lines);
  };
  const paintWords = (): void => {
    shellBar.setAttribute("aria-label", t("Shell"));
    runBar.setAttribute("aria-label", t("Program"));
    setText(copyLabel, t("Copy"));
  };
  paintWords();
  onLanguage(paintWords);
  host.appendChild(box);
  render();
  return {
    patch: (st) => {
      last = st;
      if (!st) { render(); return; }
      const key = `${st.proxy.addr}|${st.ca.path}`;
      if (key === shownKey) { render(); return; }
      void loadRecipes(key).then((r) => {
        if (last && `${last.proxy.addr}|${last.ca.path}` !== key) return;
        data = r;
        shownKey = key;
        failed = "";
        render();
      }).catch((err: Error) => { failed = err.message; render(); });
    },
  };
}
