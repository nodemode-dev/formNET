import { SessionError } from "./api.ts";
import { trafficApi } from "./api-traffic.ts";
import type { Flow } from "./types.ts";
import type { BreakpointEdit, BreakpointEvent, BreakpointOutcome, FlowTraffic, PausedExchange } from "./types-traffic.ts";

/** How an exchange stopped waiting: the server's outcome, or `released` when the page saw it leave the list without one
 *  (answered from another window or by a script). */
export type HeldOutcome = BreakpointOutcome | "released";

export interface Held {
  readonly p: PausedExchange;
  readonly outcome: HeldOutcome | null;
  readonly endedAt: number;
}

type Listener = (arrived: boolean) => void;

/** How long a finished exchange stays listed (dimmed, with its outcome) before it goes. */
const KEEP_FINISHED_MS = 30000;

/** The exchanges held at breakpoints, newest first, and the ones that finished a moment ago. Fed by GET
 *  /api/breakpoints whenever a flow's `paused` changes on the stream, and by the stream's breakpoint events when the
 *  page subscribes to them. One instance for the page. */
export class BreakpointWatch {
  private held = new Map<string, Held>();
  private mine = new Map<string, BreakpointOutcome>();
  private listeners = new Set<Listener>();
  private flows = new Set<string>();
  private timer = 0;
  private sweepTimer = 0;
  private busy = false;
  private again = false;
  private started = false;
  timeoutSeconds = 60;
  maxPaused = 20;
  error = "";

  /** Begins once the stream is up; refreshes at once. */
  start(): void {
    if (this.started) return;
    this.started = true;
    this.sweepTimer = window.setInterval(() => this.sweep(), 1000);
    this.refresh();
  }

  stop(): void {
    this.started = false;
    window.clearTimeout(this.timer);
    window.clearInterval(this.sweepTimer);
  }

  subscribe(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => { this.listeners.delete(fn); };
  }

  /** Newest first. */
  list(): Held[] {
    return Array.from(this.held.values()).sort((a, b) => (a.p.since < b.p.since ? 1 : a.p.since > b.p.since ? -1 : a.p.id < b.p.id ? 1 : -1));
  }

  get(id: string): Held | undefined {
    return this.held.get(id);
  }

  pausedCount(): number {
    let n = 0;
    for (const h of this.held.values()) if (!h.outcome) n++;
    return n;
  }

  /** A flow from the stream: a pause starting or ending on it asks for the list again. */
  sawFlow(flow: Flow): void {
    const paused = (flow as Flow & FlowTraffic).paused;
    if (paused) this.waiting.set(flow.id, 5);
    else this.waiting.delete(flow.id);
    if (paused || this.flows.has(flow.id)) this.poke();
  }

  /** Flows the stream showed paused that the list did not hold yet, with the tries left (the flow is stored a moment
   *  before its pause is registered). */
  private waiting = new Map<string, number>();

  /** A breakpoint event from the stream. */
  event(e: BreakpointEvent): void {
    if (e.type === "breakpoint.resumed" && e.outcome) this.finish(e.id, e.outcome);
    this.poke();
  }

  poke(): void {
    if (!this.started || this.timer) return;
    this.timer = window.setTimeout(() => { this.timer = 0; this.refresh(); }, 60);
  }

  refresh(): void {
    if (this.busy) { this.again = true; return; }
    this.busy = true;
    trafficApi.breakpoints()
      .then((st) => { this.error = ""; this.timeoutSeconds = st.timeout_seconds; this.maxPaused = st.max_paused; this.merge(st.paused ?? []); })
      .catch((err: Error) => { if (!(err instanceof SessionError)) { this.error = err.message; this.emit(false); } })
      .finally(() => { this.busy = false; if (this.again) { this.again = false; this.refresh(); } });
  }

  /** Lets the held exchange go on, with only the fields given changed. */
  async continue(id: string, edit: BreakpointEdit): Promise<void> {
    const outcome: BreakpointOutcome = Object.keys(edit).length ? "edited" : "continued";
    this.mine.set(id, outcome);
    try {
      await trafficApi.continueBreakpoint(id, edit);
    } catch (err) {
      this.mine.delete(id);
      throw err;
    }
    this.finish(id, outcome);
    this.poke();
  }

  async abort(id: string): Promise<void> {
    this.mine.set(id, "aborted");
    try {
      await trafficApi.abortBreakpoint(id);
    } catch (err) {
      this.mine.delete(id);
      throw err;
    }
    this.finish(id, "aborted");
    this.poke();
  }

  /** Takes every finished exchange off the list. */
  clearFinished(): void {
    let changed = false;
    for (const [id, h] of this.held) if (h.outcome) { this.held.delete(id); changed = true; }
    if (changed) this.emit(false);
  }

  private finish(id: string, outcome: HeldOutcome): void {
    const h = this.held.get(id);
    if (!h || h.outcome) return;
    this.held.set(id, { p: h.p, outcome, endedAt: Date.now() });
    this.mine.delete(id);
    this.relist();
    this.emit(false);
  }

  private merge(paused: PausedExchange[]): void {
    const now = Date.now();
    const live = new Set(paused.map((p) => p.id));
    let arrived = false, changed = false;
    for (const p of paused) {
      const was = this.held.get(p.id);
      if (!was) { this.held.set(p.id, { p, outcome: null, endedAt: 0 }); arrived = true; changed = true; }
      else if (!was.outcome && was.p.deadline !== p.deadline) { this.held.set(p.id, { ...was, p }); changed = true; }
      this.waiting.delete(p.flow_id);
    }
    let retry = false;
    for (const [flow, tries] of this.waiting) {
      if (tries <= 1) this.waiting.delete(flow);
      else { this.waiting.set(flow, tries - 1); retry = true; }
    }
    if (retry) window.setTimeout(() => this.poke(), 250);
    for (const [id, h] of this.held) {
      if (h.outcome || live.has(id)) continue;
      const late = now >= Date.parse(h.p.deadline) - 1500;
      this.held.set(id, { p: h.p, outcome: this.mine.get(id) ?? (late ? "timeout" : "released"), endedAt: now });
      this.mine.delete(id);
      changed = true;
    }
    this.relist();
    if (changed) this.emit(arrived);
  }

  /** The flows that have an exchange waiting now (a request and then its response share one flow). */
  private relist(): void {
    this.flows.clear();
    for (const h of this.held.values()) if (!h.outcome) this.flows.add(h.p.flow_id);
  }

  private sweep(): void {
    const now = Date.now(), cut = now - KEEP_FINISHED_MS;
    if (Array.from(this.held.values()).some((h) => !h.outcome && now > Date.parse(h.p.deadline) + 1000)) this.poke();
    let changed = false;
    for (const [id, h] of this.held) if (h.outcome && h.endedAt < cut && !this.keep.has(id)) { this.held.delete(id); changed = true; }
    if (changed) this.emit(false);
  }

  /** Exchanges the panel shows selected stay listed after they finish, until another is chosen. */
  readonly keep = new Set<string>();

  private emit(arrived: boolean): void {
    for (const fn of this.listeners) fn(arrived);
  }
}

export const breakpointWatch = new BreakpointWatch();
