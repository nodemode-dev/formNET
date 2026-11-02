import type { Flow, FlowEvent, HostSummary, Snapshot, Status } from "./types.ts";
import type { MachineSort } from "./ui/prefs";

export type View = "flows" | "hosts" | "machine" | "setup";

export interface State {
  view: View;
  filter: string;
  hostFilter: string;
  live: boolean;
  connected: boolean;
  sessionLost: boolean;
  epoch: string;
  flows: ReadonlyMap<string, Flow>; // insertion order is arrival order
  lastSeq: number;
  total: number; // the server's count at the last list response
  limit: number;
  maxRows: number; // rows the Flows table draws, from Preferences
  machineSort: MachineSort; // the Machine view's order, from Preferences
  selectedId: string | null;
  hosts: readonly HostSummary[];
  machine: Snapshot | null;
  status: Status | null;
  notice: string;
}

export const initialState: State = {
  view: "flows",
  filter: "",
  hostFilter: "",
  live: true,
  connected: false,
  sessionLost: false,
  epoch: "",
  flows: new Map(),
  lastSeq: 0,
  total: 0,
  limit: 5000,
  maxRows: 500,
  machineSort: "connections",
  selectedId: null,
  hosts: [],
  machine: null,
  status: null,
  notice: "",
};

type Listener = (state: State, previous: State) => void;

// Store holds one immutable State; every update replaces it with a new object.
export class Store {
  private state: State;
  private listeners = new Set<Listener>();

  constructor(state: State = initialState) {
    this.state = state;
  }

  get(): State {
    return this.state;
  }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  update(patch: Partial<State> | ((s: State) => Partial<State>)): void {
    const previous = this.state;
    const next = { ...previous, ...(typeof patch === "function" ? patch(previous) : patch) };
    this.state = next;
    for (const fn of this.listeners) fn(next, previous);
  }
}

function maxSeq(flows: Iterable<Flow>, start: number): number {
  let seq = start;
  for (const f of flows) if (f.seq > seq) seq = f.seq;
  return seq;
}

// withFlows replaces the flow map with a full list from the server (arrival order).
export function withFlows(_state: State, flows: Flow[], total: number): Partial<State> {
  const map = new Map<string, Flow>();
  for (const f of flows) map.set(f.id, f);
  return { flows: map, lastSeq: maxSeq(flows, 0), total };
}

// mergeFlows folds a partial list (flows since a seq) into what is kept. An
// entry the list does not mention stays; a kept flow that already completed is
// not replaced by a pending copy of itself.
export function mergeFlows(state: State, flows: Flow[], total: number): Partial<State> {
  const map = new Map(state.flows);
  for (const f of flows) {
    const kept = map.get(f.id);
    if (kept && !kept.pending && f.pending) continue;
    map.set(f.id, f);
  }
  return { flows: trim(map, state.limit), lastSeq: maxSeq(flows, state.lastSeq), total };
}

// applyFlowEvents applies a batch of stream events in order.
export function applyFlowEvents(state: State, events: readonly FlowEvent[]): Partial<State> {
  let map: Map<string, Flow> | null = null;
  let lastSeq = state.lastSeq;
  let selectedId = state.selectedId;
  let cleared = false;
  for (const e of events) {
    if (e.type === "flow.cleared") {
      map = new Map();
      lastSeq = 0;
      selectedId = null;
      cleared = true;
      continue;
    }
    if (!e.flow) continue;
    map ??= new Map(state.flows);
    map.set(e.flow.id, e.flow);
    if (e.flow.seq > lastSeq) lastSeq = e.flow.seq;
  }
  if (!map) return {};
  return { flows: trim(map, state.limit), lastSeq, selectedId, ...(cleared ? { total: 0 } : {}) };
}

function trim(map: Map<string, Flow>, limit: number): Map<string, Flow> {
  if (map.size <= limit) return map;
  const out = new Map<string, Flow>();
  const skip = map.size - limit;
  let i = 0;
  for (const [id, f] of map) {
    if (i >= skip) out.set(id, f);
    i += 1;
  }
  return out;
}

// visibleFlows filters client-side so typing never waits on the server.
export function visibleFlows(state: State): Flow[] {
  const q = state.filter.trim().toLowerCase();
  const host = state.hostFilter.toLowerCase();
  const out: Flow[] = [];
  for (const f of state.flows.values()) {
    if (host && f.host.toLowerCase() !== host) continue;
    if (q && !matches(f, q)) continue;
    out.push(f);
  }
  return out;
}

function matches(f: Flow, q: string): boolean {
  const hay = [f.url, f.method, String(f.status), f.client_process ?? "", f.kind, f.response?.content_type ?? "", f.error ?? ""]
    .join(" ")
    .toLowerCase();
  return hay.includes(q);
}
