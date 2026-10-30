import { h, clear } from "../dom.ts";
import { ago, bytes, clock } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { HostSummary } from "../types.ts";

const columns = ["Host", "Label", "Flows", "Pending", "Errors", "Sent", "Received", "First seen", "Last seen", "Processes"];

// hostsView groups the per-host totals by domain. Clicking a host filters the flows table to it.
export function hostsView(store: Store): HTMLElement {
  const body = h("div", { class: "groups" });
  const empty = h("div", { class: "empty" }, h("p", {}, "No hosts yet."));
  const intro = h("p", { class: "dim small" }, "Labels come from a short list of widely documented third parties and are reading aids, not judgements. An unlabelled host is simply not on the list.");
  const root = h("section", { class: "view view-hosts", role: "tabpanel" }, intro, body, empty);

  const pick = (host: string) => store.update({ hostFilter: host, view: "flows" });

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "hosts";
    if (root.hidden || (s.hosts === prev.hosts && s.view === prev.view)) return;
    clear(body);
    empty.hidden = s.hosts.length > 0;
    for (const [domain, hosts] of groupByDomain(s.hosts)) {
      body.appendChild(group(domain, hosts, pick));
    }
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), hosts: [] });
  return root;
}

function groupByDomain(hosts: readonly HostSummary[]): Map<string, HostSummary[]> {
  const map = new Map<string, HostSummary[]>();
  for (const hst of hosts) {
    const key = hst.domain || hst.host;
    map.set(key, [...(map.get(key) ?? []), hst]);
  }
  return map;
}

function group(domain: string, hosts: HostSummary[], pick: (host: string) => void): HTMLElement {
  const sent = hosts.reduce((n, x) => n + x.bytes_sent, 0);
  const received = hosts.reduce((n, x) => n + x.bytes_received, 0);
  const flows = hosts.reduce((n, x) => n + x.flows, 0);
  const owner = hosts.find((x) => x.owner)?.owner;
  const table = h(
    "table",
    { class: "hosts", "aria-label": `Hosts under ${domain}` },
    h("thead", {}, h("tr", {}, ...columns.map((c) => h("th", { scope: "col" }, c)))),
    h("tbody", {}, ...hosts.map((x) => hostRow(x, pick))),
  );
  return h(
    "details",
    { class: "group", open: true },
    h(
      "summary",
      {},
      h("span", { class: "domain" }, domain),
      owner ? h("span", { class: "label", title: "label from the known-host list" }, owner) : null,
      h("span", { class: "dim" }, ` ${hosts.length} host${hosts.length === 1 ? "" : "s"}, ${flows} flows, ${bytes(sent)} sent, ${bytes(received)} received`),
    ),
    table,
  );
}

function hostRow(x: HostSummary, pick: (host: string) => void): HTMLElement {
  const label = x.category ? `${x.category}${x.owner ? ` (${x.owner})` : ""}` : "";
  return h(
    "tr",
    {},
    h("td", {}, h("button", { type: "button", class: "linkish mono", title: "Show this host's flows", onClick: () => pick(x.host) }, x.host), x.intercepted ? null : h("span", { class: "kind kind-tunnel", title: "only counted, never opened" }, "tunnel")),
    h("td", {}, label ? h("span", { class: "label", title: "label from the known-host list" }, label) : ""),
    h("td", { class: "num mono" }, x.flows),
    h("td", { class: "num mono" }, x.pending || ""),
    h("td", { class: `num mono ${x.errors ? "st-error" : ""}` }, x.errors || ""),
    h("td", { class: "num mono" }, bytes(x.bytes_sent)),
    h("td", { class: "num mono" }, bytes(x.bytes_received)),
    h("td", { class: "mono dim", title: x.first_seen }, clock(x.first_seen)),
    h("td", { class: "mono dim", title: x.last_seen }, ago(x.last_seen)),
    h("td", {}, (x.processes ?? []).join(", ")),
  );
}
