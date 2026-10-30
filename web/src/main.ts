import "./styles/base.css";
import "./styles/layout.css";
import "./styles/tables.css";
import "./styles/detail.css";

import { api, onUnauthorized, openStream, SessionError } from "./api.ts";
import { applySavedTheme, installMenuHook } from "./desktop.ts";
import { h } from "./dom.ts";
import { Store, applyFlowEvents, withFlows } from "./state.ts";
import type { FlowEvent } from "./types.ts";
import { flowDetail } from "./ui/flow-detail.ts";
import { flowsView } from "./ui/flows-view.ts";
import { hostsView } from "./ui/hosts-view.ts";
import { machineView } from "./ui/machine-view.ts";
import { setupView } from "./ui/setup-view.ts";
import { shell } from "./ui/shell.ts";

applySavedTheme();
const store = new Store();
installMenuHook(store);
const root = document.getElementById("app");
if (!root) throw new Error("no #app element");

const main = h("main", { class: "main" });
root.append(shell(store), main);
main.append(flowsView(store), hostsView(store), machineView(store), setupView(store), flowDetail(store));

const sayError = (what: string) => (err: Error) => {
  if (err instanceof SessionError) return;
  store.update({ notice: `${what}: ${err.message}` });
};

function refreshStatus(): void {
  api.status().then((status) => store.update({ status, notice: "" })).catch(sayError("status"));
}

function refreshHosts(): void {
  api.hosts().then((r) => store.update({ hosts: r.hosts ?? [] })).catch(sayError("hosts"));
}

// reloadFlows replaces everything kept with the server's list.
function reloadFlows(): void {
  api
    .flows({ limit: store.get().limit })
    .then((r) => store.update((s) => withFlows(s, r.flows ?? [], r.total)))
    .catch(sayError("flows"));
}

let hostsTimer = 0;
const scheduleHosts = () => {
  if (hostsTimer) return;
  hostsTimer = window.setTimeout(() => {
    hostsTimer = 0;
    refreshHosts();
  }, 500);
};

// Stream events are collected and applied once per animation frame.
let pending: FlowEvent[] = [];
let frame = 0;
const flush = () => {
  frame = 0;
  const batch = pending;
  pending = [];
  if (batch.length === 0) return;
  store.update((s) => applyFlowEvents(s, batch));
  scheduleHosts();
};

const stopStream = openStream({
  hello: (hello) => {
    const s = store.get();
    const epoch = hello.epoch ?? "";
    store.update({ connected: true, epoch });
    // A different epoch means the server restarted or cleared; the seq space is new.
    reloadFlows();
    if (s.epoch && epoch === s.epoch && hello.dropped) store.update({ notice: `${hello.dropped} events were missed; reloaded` });
    refreshStatus();
    refreshHosts();
    api.machine().then((machine) => store.update({ machine })).catch(() => undefined);
  },
  resync: () => {
    pending = [];
    reloadFlows();
  },
  close: () => store.update({ connected: false }),
  unauthorized: () => lostSession(),
  flow: (e) => {
    pending = [...pending, e];
    if (!frame) frame = requestAnimationFrame(flush);
  },
  machine: (machine) => store.update({ machine }),
});

function lostSession(): void {
  if (store.get().sessionLost) return;
  stopStream();
  store.update({ sessionLost: true, connected: false });
  root?.replaceChildren(
    h(
      "div",
      { class: "session-lost", role: "alert" },
      h("h1", {}, "No session"),
      h("p", {}, "This formnet page has no session. Open formnet from the address printed by ", h("code", {}, "formnet serve"), ", or run ", h("code", {}, "formnet open"), "."),
    ),
  );
}

onUnauthorized(lostSession);

window.setInterval(() => {
  if (document.visibilityState === "visible" && !store.get().sessionLost) refreshStatus();
}, 10000);
