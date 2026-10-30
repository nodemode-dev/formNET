import { h } from "../dom.ts";
import { bytes, clock, kindLabel, methodClass, millis, pathWithQuery, shortType, statusClass } from "../format.ts";
import type { State, Store } from "../state.ts";
import { visibleFlows } from "../state.ts";
import type { Flow } from "../types.ts";

const MAX_ROWS = 500;

const columns = ["Time", "Process", "Method", "Host", "Path", "Status", "Type", "Sent", "Received", "Duration"];

interface Drawn {
  flow: Flow;
  row: HTMLTableRowElement;
}

// flowsView renders the dense table of flows; the newest MAX_ROWS matching
// rows are drawn, and redraws patch rows by id rather than rebuilding the body.
export function flowsView(store: Store): HTMLElement {
  const tbody = h("tbody");
  const note = h("div", { class: "tablenote" });
  const table = h(
    "table",
    { class: "flows", "aria-label": "Flows" },
    h("thead", {}, h("tr", {}, ...columns.map((c) => h("th", { scope: "col" }, c)))),
    tbody,
  );
  const empty = h(
    "div",
    { class: "empty" },
    h("p", {}, "No flows yet."),
    h("p", {}, "Point a program at the proxy (see Setup) and its requests appear here as they happen."),
  );
  const root = h("section", { class: "view view-flows", role: "tabpanel" }, note, table, empty);

  let frozen: Flow[] | null = null; // the row set shown while paused
  let order: string[] = [];
  let drawn = new Map<string, Drawn>();

  const select = (id: string | null) => store.update({ selectedId: id });

  root.addEventListener("keydown", (ev) => {
    if (ev.key !== "ArrowDown" && ev.key !== "ArrowUp") return;
    const i = order.indexOf(store.get().selectedId ?? "");
    const next = ev.key === "ArrowDown" ? Math.min(order.length - 1, i + 1) : Math.max(0, i - 1);
    const id = order[next];
    if (id) {
      ev.preventDefault();
      select(id);
      const row = drawn.get(id)?.row;
      row?.focus();
      row?.scrollIntoView({ block: "nearest" });
    }
  });

  const render = (s: State, prev: State) => {
    root.hidden = s.view !== "flows";
    if (root.hidden) return;
    const listChanged = s.flows !== prev.flows || s.filter !== prev.filter || s.hostFilter !== prev.hostFilter || s.live !== prev.live || s.view !== prev.view;
    if (listChanged) {
      let flows: Flow[];
      if (!s.live && frozen && s.filter === prev.filter && s.hostFilter === prev.hostFilter) {
        flows = frozen.map((f) => s.flows.get(f.id) ?? f);
      } else {
        const all = visibleFlows(s);
        flows = all.slice(Math.max(0, all.length - MAX_ROWS));
        note.textContent = all.length > flows.length ? `showing the newest ${flows.length} of ${all.length} matching flows` : "";
        frozen = s.live ? null : flows;
      }
      const focused = focusedRowId(tbody);
      drawn = patch(tbody, drawn, flows, s.selectedId, select);
      order = flows.map((f) => f.id);
      if (focused) (drawn.get(s.selectedId ?? "") ?? drawn.get(focused))?.row.focus({ preventScroll: true });
      empty.hidden = s.flows.size > 0;
      table.hidden = s.flows.size === 0;
    } else if (s.selectedId !== prev.selectedId) {
      drawn.get(prev.selectedId ?? "")?.row.classList.remove("selected");
      drawn.get(s.selectedId ?? "")?.row.classList.add("selected");
    }
  };
  store.subscribe(render);
  render(store.get(), { ...store.get(), flows: new Map() });
  return root;
}

function focusedRowId(tbody: HTMLElement): string | null {
  const active = document.activeElement;
  if (!(active instanceof HTMLTableRowElement) || !tbody.contains(active)) return null;
  return active.dataset["id"] ?? null;
}

// patch brings tbody in line with flows: unchanged flows keep their row,
// changed ones get a fresh row in place, new ones are added and rows for
// flows no longer listed are removed.
function patch(tbody: HTMLElement, previous: Map<string, Drawn>, flows: Flow[], selectedId: string | null, select: (id: string) => void): Map<string, Drawn> {
  const next = new Map<string, Drawn>();
  const rows: HTMLTableRowElement[] = [];
  for (const f of flows) {
    const old = previous.get(f.id);
    const row = old && old.flow === f ? old.row : buildRow(f, select);
    row.classList.toggle("selected", f.id === selectedId);
    next.set(f.id, { flow: f, row });
    rows.push(row);
  }
  for (const [id, d] of previous) {
    if (!next.has(id) || next.get(id)?.row !== d.row) d.row.remove();
  }
  rows.forEach((row, i) => {
    const at = tbody.children[i];
    if (at !== row) tbody.insertBefore(row, at ?? null);
  });
  while (tbody.children.length > rows.length) tbody.lastElementChild?.remove();
  return next;
}

function buildRow(f: Flow, select: (id: string) => void): HTMLTableRowElement {
  const kind = kindLabel(f.kind);
  const status = f.error ? "error" : f.status || (f.pending ? "..." : "");
  const tr = h(
    "tr",
    { class: `${f.pending ? "pending" : ""} ${f.error || f.status >= 400 ? "bad" : ""}`.trim(), tabindex: "0", "data-id": f.id, onClick: () => select(f.id) },
    h("td", { class: "mono dim" }, clock(f.start)),
    h("td", { class: "proc", title: f.client_pid ? `pid ${f.client_pid}, ${f.client}` : f.client }, f.client_process ?? ""),
    h("td", { class: `mono ${methodClass(f.method)}` }, f.method),
    h("td", { class: "host" }, f.host, f.port && f.port !== 443 && f.port !== 80 ? h("span", { class: "dim" }, `:${f.port}`) : null),
    h("td", { class: "path mono", title: f.url }, kind ? h("span", { class: `kind kind-${f.kind}` }, kind) : null, f.kind === "tunnel" ? "" : pathWithQuery(f.path, f.query)),
    h("td", { class: `status mono ${statusClass(f.status, f.error)}`, title: f.error ?? "" }, status),
    h("td", { class: "type" }, shortType(f.response?.content_type)),
    h("td", { class: "num mono" }, bytes(f.bytes_sent)),
    h("td", { class: "num mono" }, bytes(f.bytes_received)),
    h("td", { class: "num mono" }, f.pending ? "" : millis(f.duration_ms)),
  ) as HTMLTableRowElement;
  tr.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" || ev.key === " ") {
      ev.preventDefault();
      select(f.id);
    }
  });
  return tr;
}
