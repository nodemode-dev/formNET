import { h } from "../dom.ts";
import { bytes, clock, kindLabel, methodClass, millis, pathWithQuery, shortType, statusClass } from "../format.ts";
import type { State, Store } from "../state.ts";
import { visibleFlows } from "../state.ts";
import type { Flow } from "../types.ts";
import { setText, setTitle } from "./dom";
import { language, onLanguage, t, tn } from "./i18n";
import { icon } from "./icons";

const columns = ["Time", "Process", "Method", "Host", "Path", "Status", "Type", "Sent", "Received", "Duration"];
const HOLD_CAP = 2000;

// The flows the store holds now, so a resent row can name when its original started.
let known: ReadonlyMap<string, Flow> = new Map();

/** The tooltip of a resent row's marker, a whole sentence in the current language. */
export function resentText(f: Flow, flows: ReadonlyMap<string, Flow> = known): string {
  const orig = f.resent_from ? flows.get(f.resent_from) : undefined;
  if (!orig) return t(f.edited ? "Resent from a flow no longer kept, edited" : "Resent from a flow no longer kept");
  return t(f.edited ? "Resent from {time}, edited" : "Resent from {time}").replace("{time}", clock(orig.start));
}

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
    { class: "flows", "aria-label": t("Flows") },
    h("thead", {}, h("tr", {}, ...columns.map((c) => h("th", { scope: "col" }, t(c))))),
    tbody,
  );
  const empty = h(
    "div",
    { class: "cv-hf-empty" },
    h("p", {}, t("No flows yet.")),
    h("p", {}, t("Point a program at the proxy (see Setup) and its requests appear here as they happen.")),
  );
  const root = h("section", { class: "view view-flows", role: "tabpanel" }, note, table, empty);

  const held = h("button", { type: "button", class: "cv-btn flows-held", title: t("Scroll to the newest flows") }) as HTMLButtonElement;
  held.style.visibility = "hidden";
  root.append(held);

  let frozen: Flow[] | null = null; // the row set shown while paused
  let order: string[] = [];
  let drawn = new Map<string, Drawn>();
  let reading = false; // the person scrolled away from the end
  let ownTop = -1; // the scrollTop this view set itself
  let carry = 0;
  let heldText: () => string = () => "";

  const select = (id: string | null) => store.update({ selectedId: id });
  const atEnd = () => root.scrollTop + root.clientHeight >= root.scrollHeight - 2;

  root.addEventListener("scroll", () => {
    if (ownTop >= 0 && Math.abs(root.scrollTop - ownTop) < 1) return;
    ownTop = -1;
    carry = 0;
    const was = reading;
    reading = !atEnd();
    if (was && !reading) render(store.get(), { ...store.get(), flows: new Map() });
  }, { passive: true });
  held.addEventListener("click", () => { root.scrollTop = root.scrollHeight; });

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

  // How many of the drawn rows from all[from] on, at most limit, lie wholly above the visible area.
  const above = (all: Flow[], from: number, limit: number): number => {
    if (limit <= 0) return 0;
    const top = root.getBoundingClientRect().top;
    let n = 0;
    while (n < limit) {
      const row = drawn.get(all[from + n]?.id ?? "")?.row;
      if (!row || row.getBoundingClientRect().bottom > top) break;
      n += 1;
    }
    return n;
  };

  // The slice of all to draw. Following the end, the oldest rows go once they are out of sight; while the person
  // reads further up nothing goes until HOLD_CAP rows, and then only from the end farther from what they read.
  const windowOf = (all: Flow[], max: number, fresh: boolean): [number, number] => {
    let from = fresh || drawn.size === 0 ? -1 : all.findIndex((f) => drawn.has(f.id));
    if (from < 0) return [Math.max(0, all.length - max), all.length];
    let to = all.length;
    const cap = Math.max(HOLD_CAP, max);
    if (!reading) from += above(all, from, to - from - max);
    if (to - from > cap) {
      const r = root.getBoundingClientRect(), mid = r.top + r.height / 2;
      const first = drawn.get(all[from]?.id ?? "")?.row, last = drawn.get(order[order.length - 1] ?? "")?.row;
      const lower = !first || !last || mid - first.getBoundingClientRect().top > last.getBoundingClientRect().bottom - mid;
      if (lower) from += above(all, from, to - from - cap);
      to = Math.min(to, from + cap);
    }
    return [from, to];
  };

  const firstVisible = (): { id: string; y: number } | null => {
    if (!order.length || root.hidden) return null;
    const top = root.getBoundingClientRect().top;
    let lo = 0, hi = order.length - 1, hit = -1;
    while (lo <= hi) {
      const m = (lo + hi) >> 1;
      const row = drawn.get(order[m] ?? "")?.row;
      if (!row) return null;
      if (row.getBoundingClientRect().bottom > top) { hit = m; hi = m - 1; } else lo = m + 1;
    }
    const id = order[hit];
    const row = id ? drawn.get(id)?.row : undefined;
    return id && row ? { id, y: row.getBoundingClientRect().top } : null;
  };

  // The first visible row back where it was, in the same frame, so rows removed above it move nothing on screen.
  const keep = (a: { id: string; y: number } | null) => {
    const row = a ? drawn.get(a.id)?.row : undefined;
    if (!a || !row?.isConnected) return;
    const dy = row.getBoundingClientRect().top - a.y + carry;
    if (Math.abs(dy) < 0.5) { carry = dy; return; }
    const want = root.scrollTop + dy;
    root.scrollTop = want;
    carry = want - root.scrollTop; // the part a whole-pixel scroll position cannot take, kept for the next pass
    ownTop = root.scrollTop;
  };

  let notes = { shown: 0, total: 0, newest: false };
  const paintNotes = (shown: number, total: number, newest: boolean) => {
    notes = { shown, total, newest };
    const n = (x: number) => x.toLocaleString(language());
    setText(note, shown < total ? t(newest ? "showing the newest {shown} of {total} matching flows" : "showing {shown} of {total} matching flows").replace("{shown}", n(shown)).replace("{total}", n(total)) : "");
    const text = heldText();
    setText(held, text);
    const vis = text ? "" : "hidden";
    if (held.style.visibility !== vis) held.style.visibility = vis;
  };

  onLanguage(() => {
    paintNotes(notes.shown, notes.total, notes.newest);
    setTitle(held, t("Scroll to the newest flows"));
    for (const d of drawn.values()) relabel(d.row, d.flow);
  });

  const render = (s: State, prev: State) => {
    known = s.flows;
    root.hidden = s.view !== "flows";
    if (root.hidden) return;
    const listChanged = s.flows !== prev.flows || s.filter !== prev.filter || s.hostFilter !== prev.hostFilter || s.live !== prev.live || s.view !== prev.view || s.maxRows !== prev.maxRows;
    if (listChanged) {
      let flows: Flow[];
      let keepRows = true;
      if (!s.live && frozen && s.filter === prev.filter && s.hostFilter === prev.hostFilter) {
        flows = frozen.map((f) => s.flows.get(f.id) ?? f);
      } else {
        const all = visibleFlows(s);
        const fresh = s.filter !== prev.filter || s.hostFilter !== prev.hostFilter || s.maxRows !== prev.maxRows || s.view !== prev.view;
        keepRows = !fresh;
        const [from, to] = windowOf(all, s.maxRows, fresh);
        flows = all.slice(from, to);
        const newer = all.length - to, older = flows.length - s.maxRows;
        heldText = () => (newer > 0 ? tn(newer, "{n} newer flow held back while you read", "{n} newer flows held back while you read") : older > 0 ? tn(older, "{n} older row kept while you read", "{n} older rows kept while you read") : "");
        paintNotes(flows.length, all.length, from > 0 && newer === 0);
        frozen = s.live ? null : flows;
      }
      const focused = focusedRowId(tbody);
      const anchor = keepRows ? firstVisible() : null;
      drawn = patch(tbody, drawn, flows, s.selectedId, select);
      order = flows.map((f) => f.id);
      keep(anchor);
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

// relabel writes a row's times, sizes and words again in the current language.
function relabel(tr: HTMLTableRowElement, f: Flow): void {
  const c = tr.children;
  setText(c[0] ?? null, clock(f.start));
  const kind = c[4]?.querySelector(".kind");
  if (kind) setText(kind, t(kindLabel(f.kind)));
  const mark = c[4]?.querySelector<HTMLElement>(".resent-mark");
  if (mark) { setTitle(mark, resentText(f)); mark.setAttribute("aria-label", mark.title); }
  if (f.error) setText(c[5] ?? null, t("error"));
  setText(c[7] ?? null, bytes(f.bytes_sent));
  setText(c[8] ?? null, bytes(f.bytes_received));
  setText(c[9] ?? null, f.pending ? "" : millis(f.duration_ms));
}

function resentMark(f: Flow): HTMLElement {
  const text = resentText(f);
  const mark = h("span", { class: "resent-mark", role: "img", title: text, "aria-label": text });
  mark.innerHTML = icon("redo", 12);
  return mark;
}

function buildRow(f: Flow, select: (id: string) => void): HTMLTableRowElement {
  const kind = kindLabel(f.kind);
  const status = f.error ? t("error") : f.status || (f.pending ? "..." : "");
  const tr = h(
    "tr",
    { class: `${f.pending ? "pending" : ""} ${f.error || f.status >= 400 ? "bad" : ""}`.trim(), tabindex: "0", "data-id": f.id, onClick: () => select(f.id) },
    h("td", { class: "mono dim" }, clock(f.start)),
    h("td", { class: "proc", title: f.client_pid ? t("pid {pid}, {client}").replace("{pid}", String(f.client_pid)).replace("{client}", f.client) : f.client }, f.client_process ?? ""),
    h("td", { class: `mono ${methodClass(f.method)}` }, f.method),
    h("td", { class: "host" }, f.host, f.port && f.port !== 443 && f.port !== 80 ? h("span", { class: "dim" }, `:${f.port}`) : null),
    h("td", { class: "path mono", title: f.url }, f.resent_from ? resentMark(f) : null, kind ? h("span", { class: `kind kind-${f.kind}` }, t(kind)) : null, f.kind === "tunnel" || f.kind === "refused" ? "" : pathWithQuery(f.path, f.query)),
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
