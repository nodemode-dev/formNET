import { h } from "../dom.ts";
import { keepOrder, patchList, setClass, setHidden, setText, setTitle } from "./dom";
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

  let groupOrder: string[] = [];
  let hostOrder = new Map<string, string[]>();

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "hosts";
    if (root.hidden || (s.hosts === prev.hosts && s.view === prev.view)) return;
    if (s.view !== prev.view) {
      groupOrder = [];
      hostOrder = new Map();
    }
    setHidden(empty, s.hosts.length > 0);
    const groups = keepOrder([...groupByDomain(s.hosts)], ([domain]) => domain, groupOrder).map(([domain, hosts]): [string, HostSummary[]] => {
      const ordered = keepOrder(hosts, (x) => x.host, hostOrder.get(domain) ?? []);
      hostOrder.set(domain, ordered.map((x) => x.host));
      return [domain, ordered];
    });
    groupOrder = groups.map(([domain]) => domain);
    patchList(body, groups, {
      key: ([domain]) => domain,
      create: ([domain, hosts]) => group(domain, hosts, pick),
      update: (node, [, hosts]) => patchGroup(node, hosts, pick),
    });
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
  const node = h(
    "details",
    { class: "group", open: true },
    h(
      "summary",
      {},
      h("span", { class: "domain" }, domain),
      h("span", { class: "label", title: "label from the known-host list" }),
      h("span", { class: "dim" }),
    ),
    h(
      "table",
      { class: "hosts", "aria-label": `Hosts under ${domain}` },
      h("thead", {}, h("tr", {}, ...columns.map((c) => h("th", { scope: "col" }, c)))),
      h("tbody"),
    ),
  );
  patchGroup(node, hosts, pick);
  return node;
}

function patchGroup(node: HTMLElement, hosts: HostSummary[], pick: (host: string) => void): void {
  const sent = hosts.reduce((n, x) => n + x.bytes_sent, 0);
  const received = hosts.reduce((n, x) => n + x.bytes_received, 0);
  const flows = hosts.reduce((n, x) => n + x.flows, 0);
  const owner = hosts.find((x) => x.owner)?.owner ?? "";
  const label = node.querySelector("summary > .label") as HTMLElement;
  setText(label, owner);
  setHidden(label, !owner);
  setText(node.querySelector("summary > .dim"), ` ${hosts.length} host${hosts.length === 1 ? "" : "s"}, ${flows} flows, ${bytes(sent)} sent, ${bytes(received)} received`);
  patchList(node.querySelector("tbody") as HTMLElement, hosts, {
    key: (x) => x.host,
    create: (x) => hostRow(x, pick),
    update: patchHost,
  });
}

function hostRow(x: HostSummary, pick: (host: string) => void): HTMLElement {
  const host = x.host;
  const row = h(
    "tr",
    {},
    h("td", {}, h("button", { type: "button", class: "linkish mono", title: "Show this host's flows", onClick: () => pick(host) }, host), h("span", { class: "kind kind-tunnel", title: "only counted, never opened" }, "tunnel")),
    h("td", {}, h("span", { class: "label", title: "label from the known-host list" })),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
    h("td", { class: "mono dim" }),
    h("td", { class: "mono dim" }),
    h("td", {}),
  );
  patchHost(row, x);
  return row;
}

function patchHost(row: HTMLElement, x: HostSummary): void {
  const c = row.children;
  setHidden(c[0]?.querySelector(".kind") as HTMLElement, x.intercepted);
  const label = x.category ? `${x.category}${x.owner ? ` (${x.owner})` : ""}` : "";
  const tag = c[1]?.firstElementChild as HTMLElement;
  setText(tag, label);
  setHidden(tag, !label);
  setText(c[2] ?? null, String(x.flows));
  setText(c[3] ?? null, x.pending ? String(x.pending) : "");
  setText(c[4] ?? null, x.errors ? String(x.errors) : "");
  setClass(c[4] ?? null, "st-error", x.errors > 0);
  setText(c[5] ?? null, bytes(x.bytes_sent));
  setText(c[6] ?? null, bytes(x.bytes_received));
  setText(c[7] ?? null, clock(x.first_seen));
  setTitle(c[7] as HTMLElement, x.first_seen);
  setText(c[8] ?? null, ago(x.last_seen));
  setTitle(c[8] as HTMLElement, x.last_seen);
  const procs = (x.processes ?? []).join(", ");
  setText(c[9] ?? null, procs);
  setTitle(c[9] as HTMLElement, procs);
}
