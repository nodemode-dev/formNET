import { h } from "../dom.ts";
import { keepOrder, patchList, setClass, setHidden, setText, setTitle } from "./dom";
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

  let resort = true;
  let order: string[] = [];
  const sortBtn = (key: SortKey, label: string) =>
    h("button", { type: "button", class: "btn btn-small", "data-sort": key, title: "Sort now; rows keep their places between samples until the next sort", onClick: () => {
      resort = true;
      if (store.get().machineSort === key) paint(last);
      else store.update({ machineSort: key });
    } }, label);
  const sorters = [sortBtn("connections", "By connections"), sortBtn("bytes", "By bytes"), sortBtn("name", "By name")];
  const info = h("span", { class: "dim small" });
  head.append(h("span", { class: "dim small" }, "Sort:"), ...sorters, info);

  const warn = h("p", { class: "warn" });
  const note = h("p", { class: "dim" }, "Waiting for the first snapshot.");
  const tbody = h("tbody");
  const table = h(
    "table",
    { class: "procs-table", "aria-label": "Processes with connections" },
    h("thead", {}, h("tr", {}, ...["", "PID", "Name", "User", "Connections", "Bytes in", "Bytes out"].map((c) => h("th", { scope: "col" }, c)))),
    tbody,
  );
  warn.hidden = true;
  table.hidden = true;
  list.append(warn, note, table);

  let last: Snapshot | null = null;
  const toggle = (pid: number) => {
    if (open.has(pid)) open.delete(pid);
    else open.add(pid);
    paint(last);
  };

  const paint = (snap: Snapshot | null) => {
    last = snap;
    for (const b of sorters) setClass(b, "on", (b as HTMLElement).dataset["sort"] === sort);
    const ready = !!snap && new Date(snap.taken).getFullYear() >= 1971;
    const procs = ready ? snap.processes ?? [] : [];
    const conns = ready ? snap.connections ?? [] : [];
    setText(info, ready ? `source ${snap.source || "unknown"}, ${procs.length} processes, ${conns.length} connections, taken ${ago(snap.taken)}` : "");
    const failed = ready && (!snap.supported || !!snap.error);
    setText(warn, !failed ? "" : snap.supported ? `Last read failed: ${snap.error}` : `The machine view is not available here: ${snap.error ?? "unsupported platform"}`);
    setHidden(warn, !failed);
    setText(note, !ready ? "Waiting for the first snapshot." : "No process with a connection was found.");
    setHidden(note, ready && procs.length > 0);
    setHidden(table, procs.length === 0);
    const byPid = new Map<number, Connection[]>();
    for (const c of conns) {
      const at = byPid.get(c.pid);
      if (at) at.push(c);
      else byPid.set(c.pid, [c]);
    }
    const ordered = resort ? sorted(procs, sort) : keepOrder(procs, (p) => String(p.pid), order);
    if (ready) {
      resort = false;
      order = ordered.map((p) => String(p.pid));
    }
    const rows: RowItem[] = [];
    for (const p of ordered) {
      const item = { p, conns: byPid.get(p.pid) ?? [], open: open.has(p.pid) };
      rows.push({ ...item, detail: false });
      if (item.open) rows.push({ ...item, detail: true });
    }
    const active = document.activeElement;
    const keep = active instanceof HTMLElement && tbody.contains(active) ? active : null;
    patchList(tbody, rows, {
      key: (r) => `${r.detail ? "d" : "p"}:${r.p.pid}`,
      create: (r) => (r.detail ? detailRow(r) : procRow(r, toggle)),
      update: (node, r) => (r.detail ? patchDetail(node, r) : patchProc(node, r)),
    });
    if (keep && keep.isConnected && document.activeElement !== keep) keep.focus({ preventScroll: true });
  };

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "machine";
    if (root.hidden) return;
    if (s.machineSort !== sort) sort = s.machineSort;
    if (s.view !== prev.view || s.machineSort !== prev.machineSort) resort = true;
    if (s.machine !== prev.machine || s.view !== prev.view || s.machineSort !== prev.machineSort) paint(s.machine);
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), machine: null });
  return root;
}

function sorted(procs: Process[], key: SortKey): Process[] {
  const copy = [...procs];
  if (key === "name") return copy.sort((a, b) => a.name.localeCompare(b.name) || a.pid - b.pid);
  if (key === "bytes") return copy.sort((a, b) => b.bytes_in + b.bytes_out - (a.bytes_in + a.bytes_out) || b.connections - a.connections || a.pid - b.pid);
  return copy.sort((a, b) => b.connections - a.connections || a.name.localeCompare(b.name) || a.pid - b.pid);
}

interface RowItem {
  p: Process;
  conns: Connection[];
  open: boolean;
  detail: boolean;
}

function procRow(r: RowItem, toggle: (pid: number) => void): HTMLElement {
  const pid = r.p.pid;
  const tr = h(
    "tr",
    { class: "proc-row" },
    h("td", {}, h("button", { type: "button", class: "btn btn-small", onClick: () => toggle(pid) })),
    h("td", { class: "num mono" }, pid),
    h("td", {}),
    h("td", { class: "dim" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
  );
  patchProc(tr, r);
  return tr;
}

function patchProc(tr: HTMLElement, r: RowItem): void {
  const { p } = r;
  const cells = tr.children;
  const btn = cells[0]?.firstElementChild as HTMLElement;
  setText(btn, r.open ? "v" : ">");
  if (btn.getAttribute("aria-expanded") !== String(r.open)) btn.setAttribute("aria-expanded", String(r.open));
  const label = `${r.open ? "Hide" : "Show"} connections of ${p.name}`;
  if (btn.getAttribute("aria-label") !== label) btn.setAttribute("aria-label", label);
  setText(cells[2] ?? null, p.name);
  setTitle(cells[2] as HTMLElement, p.path ?? "");
  setText(cells[3] ?? null, p.user ?? "");
  setText(cells[4] ?? null, String(p.connections));
  setText(cells[5] ?? null, p.bytes_in ? bytes(p.bytes_in) : "");
  setText(cells[6] ?? null, p.bytes_out ? bytes(p.bytes_out) : "");
}

function detailRow(r: RowItem): HTMLElement {
  const tbody = h("tbody");
  const inner = h(
    "table",
    { class: "conns" },
    h("thead", {}, h("tr", {}, ...["Proto", "Local", "Remote", "Remote host", "State", "First seen"].map((c) => h("th", { scope: "col" }, c)))),
    tbody,
  );
  const none = h("p", { class: "dim" }, "No connection listed for this process in the last snapshot.");
  const tr = h("tr", { class: "proc-detail" }, h("td", { colspan: "7" }, inner, none));
  patchDetail(tr, r);
  return tr;
}

function patchDetail(tr: HTMLElement, r: RowItem): void {
  const inner = tr.querySelector("table.conns") as HTMLElement;
  const none = tr.querySelector("td > p") as HTMLElement;
  const label = `Connections of ${r.p.name}`;
  if (inner.getAttribute("aria-label") !== label) inner.setAttribute("aria-label", label);
  setHidden(inner, r.conns.length === 0);
  setHidden(none, r.conns.length > 0);
  const seen = new Map<string, number>();
  const items = r.conns.map((c) => {
    const base = `${c.proto}|${c.local}|${c.remote}`;
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    return { c, k: n ? `${base}#${n}` : base };
  });
  patchList(inner.querySelector("tbody") as HTMLElement, items, {
    key: (x) => x.k,
    create: ({ c }) => {
      const row = h("tr", {}, h("td", { class: "mono" }, c.proto), h("td", { class: "mono" }, c.local), h("td", { class: "mono" }, c.remote || h("span", { class: "dim" }, "listening")), h("td", {}), h("td", { class: "dim" }), h("td", { class: "mono dim" }));
      patchConn(row, c);
      return row;
    },
    update: (row, x) => patchConn(row, x.c),
  });
}

function patchConn(row: HTMLElement, c: Connection): void {
  const cells = row.children;
  setText(cells[3] ?? null, c.remote_host ?? "");
  setText(cells[4] ?? null, c.state);
  setText(cells[5] ?? null, clock(c.first_seen));
  setTitle(cells[5] as HTMLElement, c.first_seen);
}
