import { h } from "../dom.ts";
import { keepOrder, patchList, setClass, setHidden, setStyleVar, setText, setTitle } from "./dom";
import { ago, bytes, clock } from "../format.ts";
import type { State, Store } from "../state.ts";
import type { Connection, Process, Snapshot } from "../types.ts";
import type { MachineSort as SortKey } from "./prefs";
import { onLanguage, t, tn } from "./i18n";
import { icon } from "./icons";

// machineView lists every process with connections; expanding one lists its connections.
export function machineView(store: Store): HTMLElement {
  let sort: SortKey = store.get().machineSort;
  const open = new Set<number>();
  const head = h("div", { class: "machine-head" });
  const list = h("div", { class: "procs" });
  const root = h("section", { class: "view view-machine", role: "tabpanel" }, head, list);

  let resort = true;
  let order: string[] = [];
  const seen = new Map<number, Process>();
  const sortBtn = (key: SortKey, label: string) =>
    h("button", { type: "button", "data-sort": key, title: t("Sort now; rows keep their places between samples until the next sort"), onClick: () => {
      resort = true;
      if (store.get().machineSort === key) paint(last);
      else store.update({ machineSort: key });
    } }, t(label));
  const sorters = [sortBtn("connections", "By connections"), sortBtn("bytes", "By bytes"), sortBtn("name", "By name")];
  const info = h("span", { class: "cv-note machine-msg" });
  const warn = h("span", { class: "warn small machine-msg" });
  head.append(h("span", { class: "cv-note" }, t("Sort:")), h("div", { class: "cv-seg", role: "group", "aria-label": t("Sort") }, ...sorters), info, warn);

  const note = h("p", { class: "dim" }, t("Waiting for the first snapshot."));
  const tbody = h("tbody");
  const table = h(
    "table",
    { class: "procs-table", "aria-label": t("Processes with connections") },
    h("thead", {}, h("tr", {}, ...["", "PID", "Name", "User", "Connections", "Bytes in", "Bytes out"].map((c) => h("th", { scope: "col" }, c && t(c))))),
    tbody,
  );
  warn.hidden = true;
  table.hidden = true;
  list.append(note, table);

  let last: Snapshot | null = null;
  const toggle = (pid: number) => {
    if (open.has(pid)) open.delete(pid);
    else open.add(pid);
    paint(last);
  };

  const paint = (snap: Snapshot | null) => {
    last = snap;
    for (const b of sorters) {
      const on = (b as HTMLElement).dataset["sort"] === sort;
      setClass(b, "is-on", on);
      if (b.getAttribute("aria-pressed") !== String(on)) b.setAttribute("aria-pressed", String(on));
    }
    const ready = !!snap && new Date(snap.taken).getFullYear() >= 1971;
    const procs = ready ? snap.processes ?? [] : [];
    const conns = ready ? snap.connections ?? [] : [];
    const infoText = ready ? t("source {source}, {processes}, {connections}, taken {age}").replace("{source}", snap.source || t("unknown")).replace("{processes}", tn(procs.length, "{n} process", "{n} processes")).replace("{connections}", tn(conns.length, "{n} connection", "{n} connections")).replace("{age}", ago(snap.taken)) : "";
    setText(info, infoText);
    setTitle(info, infoText);
    const failed = ready && (!snap.supported || !!snap.error);
    const warnText = !failed ? "" : snap.supported ? t("Last read failed: {error}").replace("{error}", snap.error ?? "") : t("The machine view is not available here: {error}").replace("{error}", snap.error ?? t("unsupported platform"));
    setText(warn, warnText);
    setTitle(warn, warnText);
    setHidden(warn, !failed);
    setHidden(info, failed);
    setText(note, t(!ready ? "Waiting for the first snapshot." : "No process with a connection was found."));
    const byPid = new Map<number, Connection[]>();
    for (const c of conns) {
      const at = byPid.get(c.pid);
      if (at) at.push(c);
      else byPid.set(c.pid, [c]);
    }
    const live = new Set(procs.map((p) => p.pid));
    if (resort) seen.clear();
    const held = resort ? [] : order.map((k) => seen.get(Number(k))).filter((p): p is Process => !!p && !live.has(p.pid)).map((p) => ({ ...p, connections: 0, bytes_in: 0, bytes_out: 0 }));
    for (const p of procs) seen.set(p.pid, p);
    const ordered = resort ? sorted(procs, sort) : keepOrder([...procs, ...held], (p) => String(p.pid), order);
    if (ready) {
      resort = false;
      order = ordered.map((p) => String(p.pid));
    }
    setHidden(table, ordered.length === 0);
    setHidden(note, ordered.length > 0);
    const rows: RowItem[] = [];
    for (const p of ordered) {
      const item = { p, conns: byPid.get(p.pid) ?? [], open: open.has(p.pid), gone: !live.has(p.pid) };
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
  onLanguage(() => { if (!root.hidden) paint(last); });
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
  gone: boolean;
  detail: boolean;
}

function procRow(r: RowItem, toggle: (pid: number) => void): HTMLElement {
  const pid = r.p.pid;
  const tr = h(
    "tr",
    { class: "proc-row" },
    h("td", {}, h("button", { type: "button", class: "cv-icon-btn", onClick: () => toggle(pid) })),
    h("td", { class: "num mono" }, pid),
    h("td", {}),
    h("td", { class: "dim" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
    h("td", { class: "num mono" }),
  );
  (tr.firstElementChild?.firstElementChild as HTMLElement).innerHTML = icon("chevronRight", 14);
  patchProc(tr, r);
  return tr;
}

function patchProc(tr: HTMLElement, r: RowItem): void {
  const { p } = r;
  const cells = tr.children;
  const btn = cells[0]?.firstElementChild as HTMLElement;
  if (btn.getAttribute("aria-expanded") !== String(r.open)) btn.setAttribute("aria-expanded", String(r.open));
  const label = t(r.open ? "Hide connections of {name}" : "Show connections of {name}").replace("{name}", p.name);
  if (btn.getAttribute("aria-label") !== label) btn.setAttribute("aria-label", label);
  setTitle(btn, label);
  setClass(tr, "is-gone", r.gone);
  setTitle(tr, r.gone ? t("No connection in the last sample; the row keeps its place until the next sort") : "");
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
    h("thead", {}, h("tr", {}, ...["Proto", "Local", "Remote", "Remote host", "State", "First seen"].map((c) => h("th", { scope: "col" }, t(c))))),
    tbody,
  );
  const none = h("p", { class: "dim" }, t("No connection listed for this process in the last snapshot."));
  const box = h("div", { class: "conns-box" }, inner, none);
  setStyleVar(box, "--rows", String(Math.min(10, Math.max(3, r.conns.length))));
  const tr = h("tr", { class: "proc-detail" }, h("td", { colspan: "7" }, box));
  patchDetail(tr, r);
  return tr;
}

function patchDetail(tr: HTMLElement, r: RowItem): void {
  const inner = tr.querySelector("table.conns") as HTMLElement;
  const none = tr.querySelector(".conns-box > p") as HTMLElement;
  const label = t("Connections of {name}").replace("{name}", r.p.name);
  if (inner.getAttribute("aria-label") !== label) inner.setAttribute("aria-label", label);
  setHidden(none, r.conns.length > 0);
  const seen = new Map<string, number>();
  const now = new Map<string, Connection>();
  for (const c of r.conns) {
    const base = `${c.proto}|${c.local}|${c.remote}`;
    const n = seen.get(base) ?? 0;
    seen.set(base, n + 1);
    now.set(n ? `${base}#${n}` : base, c);
  }
  const slots = slotsOf.get(tr) ?? [];
  slotsOf.set(tr, slots);
  const placed = new Set<string>();
  for (const s of slots) {
    const c = now.get(s.k);
    s.gone = !c;
    if (c) { s.c = c; placed.add(s.k); }
  }
  for (const [k, c] of now) {
    if (placed.has(k)) continue;
    const free = slots.find((s) => s.gone);
    if (free) { free.k = k; free.c = c; free.gone = false; } else slots.push({ k, c, gone: false });
  }
  patchList(inner.querySelector("tbody") as HTMLElement, slots.map((s, i) => ({ s, i })), {
    key: (x) => `s${x.i}`,
    create: ({ s }) => {
      const row = h("tr", {}, h("td", { class: "mono" }), h("td", { class: "mono" }), h("td", { class: "mono" }), h("td", {}), h("td", { class: "dim" }), h("td", { class: "mono dim" }));
      patchConn(row, s);
      return row;
    },
    update: (row, x) => patchConn(row, x.s),
  });
}

/** An open process's connection rows are slots: a connection keeps its row while it lasts, a closed one stays dimmed
 *  until a new connection takes its row, so no row of the list ever moves. */
interface Slot { k: string; c: Connection; gone: boolean }
const slotsOf = new WeakMap<HTMLElement, Slot[]>();

function patchConn(row: HTMLElement, s: Slot): void {
  const { c } = s;
  const cells = row.children;
  setClass(row, "is-gone", s.gone);
  setTitle(row, s.gone ? t("Closed since the last sample") : "");
  setText(cells[0] ?? null, c.proto);
  setText(cells[1] ?? null, c.local);
  setText(cells[2] ?? null, c.remote || t("listening"));
  setClass(cells[2] ?? null, "dim", !c.remote);
  setText(cells[3] ?? null, c.remote_host ?? "");
  setText(cells[4] ?? null, c.state);
  setText(cells[5] ?? null, clock(c.first_seen));
  setTitle(cells[5] as HTMLElement, c.first_seen);
}
