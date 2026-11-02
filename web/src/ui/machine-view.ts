import { h, clear } from "../dom.ts";
import { ago, bytes, clock } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { Connection, Process, Snapshot } from "../types.ts";
import type { MachineSort as SortKey } from "./prefs";

// machineView lists every process with connections; expanding one lists its connections.
export function machineView(store: Store): HTMLElement {
  let sort: SortKey = store.get().machineSort;
  const open = new Set<number>();
  const head = h("div", { class: "machine-head" });
  const list = h("div", { class: "procs" });
  const root = h("section", { class: "view view-machine", role: "tabpanel" }, head, list);

  const sortBtn = (key: SortKey, label: string) =>
    h("button", { type: "button", class: "btn btn-small", "data-sort": key, onClick: () => store.update({ machineSort: key }) }, label);
  const sorters = [sortBtn("connections", "By connections"), sortBtn("bytes", "By bytes"), sortBtn("name", "By name")];
  const info = h("span", { class: "dim small" });
  head.append(h("span", { class: "dim small" }, "Sort:"), ...sorters, info);

  const paint = (snap: Snapshot | null) => {
    for (const b of sorters) b.classList.toggle("on", (b as HTMLElement).dataset["sort"] === sort);
    clear(list);
    if (!snap || new Date(snap.taken).getFullYear() < 1971) {
      list.appendChild(h("p", { class: "dim" }, "Waiting for the first snapshot."));
      info.textContent = "";
      return;
    }
    const procs = snap.processes ?? [];
    const conns = snap.connections ?? [];
    info.textContent = `source ${snap.source || "unknown"}, ${procs.length} processes, ${conns.length} connections, taken ${ago(snap.taken)}`;
    if (!snap.supported || snap.error) {
      list.appendChild(h("p", { class: "warn" }, snap.supported ? `Last read failed: ${snap.error}` : `The machine view is not available here: ${snap.error ?? "unsupported platform"}`));
    }
    if (procs.length === 0) {
      list.appendChild(h("p", { class: "dim" }, "No process with a connection was found."));
      return;
    }
    const byPid = new Map<number, Connection[]>();
    for (const c of conns) byPid.set(c.pid, [...(byPid.get(c.pid) ?? []), c]);
    const table = h(
      "table",
      { class: "procs-table", "aria-label": "Processes with connections" },
      h("thead", {}, h("tr", {}, ...["", "PID", "Name", "User", "Connections", "Bytes in", "Bytes out"].map((c) => h("th", { scope: "col" }, c)))),
      h("tbody", {}, ...sorted(procs, sort).flatMap((p) => procRows(p, byPid.get(p.pid) ?? [], open, () => paint(snap)))),
    );
    list.appendChild(table);
  };

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "machine";
    if (root.hidden) return;
    if (s.machineSort !== sort) sort = s.machineSort;
    if (s.machine !== prev.machine || s.view !== prev.view || s.machineSort !== prev.machineSort) paint(s.machine);
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), machine: null });
  return root;
}

function sorted(procs: Process[], key: SortKey): Process[] {
  const copy = [...procs];
  if (key === "name") return copy.sort((a, b) => a.name.localeCompare(b.name) || a.pid - b.pid);
  if (key === "bytes") return copy.sort((a, b) => b.bytes_in + b.bytes_out - (a.bytes_in + a.bytes_out) || b.connections - a.connections);
  return copy.sort((a, b) => b.connections - a.connections || a.name.localeCompare(b.name));
}

function procRows(p: Process, conns: Connection[], open: Set<number>, repaint: () => void): HTMLElement[] {
  const isOpen = open.has(p.pid);
  const toggle = h("button", { type: "button", class: "btn btn-small", "aria-expanded": isOpen ? "true" : "false", "aria-label": `${isOpen ? "Hide" : "Show"} connections of ${p.name}`, onClick: () => {
    if (isOpen) open.delete(p.pid);
    else open.add(p.pid);
    repaint();
  } }, isOpen ? "v" : ">");
  const main = h(
    "tr",
    { class: "proc-row" },
    h("td", {}, toggle),
    h("td", { class: "num mono" }, p.pid),
    h("td", { title: p.path ?? "" }, p.name),
    h("td", { class: "dim" }, p.user ?? ""),
    h("td", { class: "num mono" }, p.connections),
    h("td", { class: "num mono" }, p.bytes_in ? bytes(p.bytes_in) : ""),
    h("td", { class: "num mono" }, p.bytes_out ? bytes(p.bytes_out) : ""),
  );
  if (!isOpen) return [main];
  const inner = h(
    "table",
    { class: "conns", "aria-label": `Connections of ${p.name}` },
    h("thead", {}, h("tr", {}, ...["Proto", "Local", "Remote", "Remote host", "State", "First seen"].map((c) => h("th", { scope: "col" }, c)))),
    h("tbody", {}, ...conns.map((c) => h(
      "tr",
      {},
      h("td", { class: "mono" }, c.proto),
      h("td", { class: "mono" }, c.local),
      h("td", { class: "mono" }, c.remote || h("span", { class: "dim" }, "listening")),
      h("td", {}, c.remote_host ?? ""),
      h("td", { class: "dim" }, c.state),
      h("td", { class: "mono dim", title: c.first_seen }, clock(c.first_seen)),
    ))),
  );
  const detail = h("tr", { class: "proc-detail" }, h("td", { colspan: "7" }, conns.length ? inner : h("p", { class: "dim" }, "No connection listed for this process in the last snapshot.")));
  return [main, detail];
}
