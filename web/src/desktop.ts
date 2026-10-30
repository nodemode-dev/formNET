import { api } from "./api.ts";
import type { FlowFilter } from "./api.ts";
import type { State, Store } from "./state.ts";

// exportFilter is the part of the page's filter the server's export honours:
// the text filter (q) and the host chosen in Hosts. Live/paused and the
// visible-row cap are page-side only.
export function exportFilter(s: State): FlowFilter {
  return { ...(s.filter ? { q: s.filter } : {}), ...(s.hostFilter ? { host: s.hostFilter } : {}) };
}

declare global {
  interface Window {
    __formnetRun?: (command: string) => void;
  }
}

const THEME_KEY = "formnet.theme";

function readTheme(): string {
  try {
    return localStorage.getItem(THEME_KEY) ?? "";
  } catch {
    return "";
  }
}

function writeTheme(theme: string): void {
  try {
    if (theme) localStorage.setItem(THEME_KEY, theme);
    else localStorage.removeItem(THEME_KEY);
  } catch {
    // storage may be unavailable; the attribute still applies for this page
  }
}

// applySavedTheme restores a theme chosen through the menu earlier.
export function applySavedTheme(): void {
  const saved = readTheme();
  if (saved === "dark" || saved === "light") document.documentElement.dataset["theme"] = saved;
}

function toggleTheme(): void {
  const current = document.documentElement.dataset["theme"];
  const systemLight = matchMedia("(prefers-color-scheme: light)").matches;
  const isLight = current ? current === "light" : systemLight;
  const next = isLight ? "dark" : "light";
  document.documentElement.dataset["theme"] = next;
  writeTheme(next);
}

function download(href: string, name: string): void {
  const a = document.createElement("a");
  a.href = href;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

function showSetup(store: Store, section: string): void {
  store.update({ view: "setup" });
  requestAnimationFrame(() => document.getElementById(section)?.scrollIntoView({ block: "start" }));
}

// installMenuHook defines window.__formnetRun, which the desktop window's menu calls.
export function installMenuHook(store: Store): void {
  const commands: Record<string, () => void> = {
    "flows.exportHar": () => {
      download(api.harURL(exportFilter(store.get())), "formnet.har");
    },
    "ca.download": () => download(api.caURL(), "formnet-ca.pem"),
    "flows.clear": () => {
      api.clearFlows().catch((err: Error) => store.update({ notice: err.message }));
    },
    "flows.filter": () => {
      store.update({ view: "flows" });
      const input = document.querySelector<HTMLInputElement>("input.filter");
      input?.focus();
      input?.select();
    },
    "view.flows": () => store.update({ view: "flows" }),
    "view.hosts": () => store.update({ view: "hosts" }),
    "view.machine": () => store.update({ view: "machine" }),
    "shell.theme.toggle": toggleTheme,
    "help.proxySetup": () => showSetup(store, "proxy-card"),
    "help.trustRoot": () => showSetup(store, "ca-card"),
  };
  window.__formnetRun = (command: string) => {
    commands[command]?.();
  };
}
