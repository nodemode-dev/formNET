import { dataApi } from '../api-data.ts';
import { SessionError } from '../api.ts';
import { bytes, clock } from '../format.ts';
import type { Flow } from '../types.ts';
import type { FlowData, MessagePage, MessageSummary, WSMessage, WSMessageEvent, WSOpcode } from '../types-data.ts';
import { BodyViewer } from './body-viewer';
import { el, q, setClass, setDisabled, setText, setTitle } from './dom';
import { language, onLanguage, t, tn } from './i18n';
import { icon } from './icons';
import { tf } from './panels/controls';

const listeners = new Set<(e: WSMessageEvent) => void>();

/** The stream's flow.message events reach the open panes through here (live.ts calls it, phase B). */
export function emitMessageEvent(e: WSMessageEvent): void { for (const fn of listeners) fn(e); }
export function onMessageEvent(fn: (e: WSMessageEvent) => void): () => void { listeners.add(fn); return () => listeners.delete(fn); }

const ROW_H = 22;
const PAGE = 500;
type Dir = 'all' | 'client' | 'server';
const OPS: ReadonlyArray<readonly [string, string, WSOpcode[]]> = [['text', 'Text', ['text']], ['binary', 'Binary', ['binary']], ['close', 'Close', ['close']], ['control', 'Ping, pong', ['ping', 'pong']]];

interface Item { m: WSMessage; preview: string }

function hexHead(b64: string, n: number): string {
  try {
    const bin = atob(b64.slice(0, Math.ceil(n / 3) * 4 + 4));
    let s = '';
    for (let i = 0; i < Math.min(n, bin.length); i++) s += bin.charCodeAt(i).toString(16).padStart(2, '0') + ' ';
    return s.trim();
  } catch { return ''; }
}

function previewOf(m: WSMessage): string {
  if (m.opcode === 'close') return `${m.close_code ?? ''} ${(m.text ?? '').slice(0, 160)}`.trim();
  if (m.text !== undefined) return m.text.slice(0, 200).replace(/\s+/g, ' ');
  if (m.base64) return hexHead(m.base64, 16);
  return '';
}

/** The messages of one WebSocket flow: a summary, filters by direction and opcode, the list (direction, time, opcode,
 *  size, a one-line preview) and the chosen message in a body viewer. The list is a virtual list of fixed rows patched
 *  in place and keyed by seq; new messages are fetched with `since`, one request at a time, whenever the stream says
 *  the socket moved. Every box has its size from the start. */
export class WsMessagesPane {
  readonly el = el('<div class="fn-ws"></div>');
  private readonly sum1 = el('<p class="fn-ws-sum"></p>');
  private readonly sum2 = el('<p class="fn-ws-sum fn-ws-state"></p>');
  private readonly dirSeg = el('<div class="cv-seg fn-ws-dir" role="radiogroup"></div>');
  private readonly opBox = el('<div class="fn-ws-ops"></div>');
  private readonly shown = el('<span class="fn-ws-shown"></span>');
  private readonly list = el('<div class="fn-ws-list" tabindex="0" role="listbox"></div>');
  private readonly spacer = el('<div class="fn-ws-spacer"></div>');
  private readonly rowsBox = el('<div class="fn-ws-rows"></div>');
  private readonly none = el('<p class="fn-ws-none"></p>');
  private readonly head = el('<p class="fn-ws-head mono"></p>');
  private readonly viewer = new BodyViewer('Message');
  private rows: HTMLElement[] = [];
  private flowId = '';
  private flow: (Flow & FlowData) | null = null;
  private items: Item[] = [];
  private seqs = new Set<number>();
  private view: number[] = [];
  private next = 0;
  private page: MessagePage | null = null;
  private inflight = false;
  private dirty = false;
  private dir: Dir = 'all';
  private ops = new Set<WSOpcode>(['text', 'binary', 'close', 'ping', 'pong']);
  private selected = -1;
  private follow = true;
  private raf = 0;
  private error = '';

  constructor() {
    for (const [d, label] of [['all', 'All'], ['client', 'Sent'], ['server', 'Received']] as const) {
      const b = el<HTMLButtonElement>(`<button type="button" role="radio" data-dir="${d}"></button>`);
      b.dataset.en = label;
      b.addEventListener('click', () => { this.dir = d; this.refilter(); });
      this.dirSeg.append(b);
    }
    for (const [id, label, codes] of OPS) {
      const b = el<HTMLButtonElement>(`<button type="button" class="fn-chip fn-chip-l" data-op="${id}" aria-pressed="true"></button>`);
      b.dataset.en = label;
      b.addEventListener('click', () => {
        const on = !codes.every((c) => this.ops.has(c));
        for (const c of codes) if (on) this.ops.add(c); else this.ops.delete(c);
        this.refilter();
      });
      this.opBox.append(b);
    }
    const tools = el('<div class="fn-ws-tools"></div>');
    tools.append(this.dirSeg, this.opBox, this.shown);
    this.spacer.append(this.rowsBox);
    this.list.append(this.spacer, this.none);
    const detail = el('<div class="fn-ws-detail"></div>');
    detail.append(this.head, this.viewer.el);
    this.el.append(this.sum1, this.sum2, tools, this.list, detail);
    this.list.addEventListener('scroll', () => {
      this.follow = this.list.scrollTop + this.list.clientHeight >= this.list.scrollHeight - 4;
      this.queue();
    }, { passive: true });
    this.rowsBox.addEventListener('click', (e) => {
      const seq = Number((e.target as HTMLElement).closest<HTMLElement>('.fn-ws-row')?.dataset.seq ?? -1);
      if (seq >= 0) this.select(seq);
    });
    this.list.addEventListener('keydown', (e) => {
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
      e.preventDefault();
      const at = this.view.findIndex((i) => this.items[i]?.m.seq === this.selected);
      const nx = this.view[Math.max(0, Math.min(this.view.length - 1, at + (e.key === 'ArrowDown' ? 1 : -1)))];
      const seq = nx === undefined ? -1 : this.items[nx]?.m.seq ?? -1;
      if (seq >= 0) { this.select(seq); this.scrollTo(seq); }
    });
    onMessageEvent((e) => { if (e.flow_id === this.flowId) this.pull(); });
    new ResizeObserver(() => { this.pool(); this.queue(); }).observe(this.list);
    this.words();
    onLanguage(() => this.words());
  }

  private words(): void {
    for (const b of Array.from(this.dirSeg.children) as HTMLElement[]) setText(b, t(b.dataset.en ?? ''));
    for (const b of Array.from(this.opBox.children) as HTMLElement[]) { setText(b, t(b.dataset.en ?? '')); setTitle(b, tf('Show {kind} messages', { kind: t(b.dataset.en ?? '') })); }
    this.dirSeg.setAttribute('aria-label', t('Direction'));
    this.list.setAttribute('aria-label', t('Messages'));
    this.paint();
  }

  /** The flow to show; another flow starts over, the same one (an update) asks for what is new. */
  show(f: (Flow & FlowData) | null): void {
    const id = f && f.kind === 'websocket' ? f.id : '';
    this.flow = f;
    if (id !== this.flowId) {
      this.flowId = id;
      this.items = [];
      this.seqs.clear();
      this.view = [];
      this.next = 0;
      this.page = null;
      this.selected = -1;
      this.follow = true;
      this.error = '';
      this.dirty = false;
      this.list.scrollTop = 0;
      this.viewer.show('', null, '', 'Choose a message to see it here.');
      this.refilter();
    }
    if (id) this.pull();
  }

  private pull(): void {
    if (!this.flowId) return;
    if (this.inflight) { this.dirty = true; return; }
    this.inflight = true;
    this.dirty = false;
    const id = this.flowId;
    let more = false;
    dataApi.messages(id, this.next, PAGE)
      .then((p) => {
        if (id !== this.flowId) return;
        this.error = '';
        this.page = p;
        const got = p.messages ?? [];
        more = got.length >= PAGE;
        this.next = Math.max(this.next, p.next);
        const added: Item[] = [];
        for (const m of got) if (!this.seqs.has(m.seq)) { this.seqs.add(m.seq); added.push({ m, preview: previewOf(m) }); }
        if (added.length) {
          this.items.push(...added);
          if (added.some((x, i) => i > 0 && x.m.seq < added[i - 1]!.m.seq) || (this.items.length > added.length && added[0]!.m.seq < this.items[this.items.length - added.length - 1]!.m.seq)) this.items.sort((a, b) => a.m.seq - b.m.seq);
          this.refilter(true);
        } else this.paint();
      })
      .catch((err: unknown) => {
        if (id !== this.flowId || err instanceof SessionError) return;
        this.error = err instanceof Error ? err.message : String(err);
        this.paint();
      })
      .finally(() => {
        this.inflight = false;
        if (id === this.flowId && (more || this.dirty)) this.pull();
      });
  }

  private refilter(appended = false): void {
    const out: number[] = [];
    this.items.forEach((it, i) => { if ((this.dir === 'all' || it.m.dir === this.dir) && this.ops.has(it.m.opcode)) out.push(i); });
    this.view = out;
    if (!appended) this.follow = this.follow || this.selected < 0;
    this.paint();
    this.queue(appended && this.follow);
  }

  private select(seq: number): void {
    if (seq === this.selected) return;
    this.selected = seq;
    const it = this.items.find((x) => x.m.seq === seq);
    this.paintDetail(it?.m ?? null);
    this.queue();
  }

  private scrollTo(seq: number): void {
    const at = this.view.findIndex((i) => this.items[i]?.m.seq === seq);
    if (at < 0) return;
    const y = at * ROW_H;
    if (y < this.list.scrollTop) this.list.scrollTop = y;
    else if (y + ROW_H > this.list.scrollTop + this.list.clientHeight) this.list.scrollTop = y + ROW_H - this.list.clientHeight;
  }

  private paintDetail(m: WSMessage | null): void {
    if (!m) { setText(this.head, ''); this.viewer.show('', null, '', 'Choose a message to see it here.'); return; }
    const flags: string[] = [];
    if (m.compressed) flags.push(m.not_decoded ? t('compressed, not decoded') : m.inflated_bytes ? tf('compressed, {size} inflated', { size: bytes(m.inflated_bytes) }) : t('compressed'));
    if (m.truncated) flags.push(t('cut at 64 KB'));
    if (m.frames && m.frames > 1) flags.push(tn(m.frames, '{n} frame', '{n} frames'));
    const who = t(m.dir === 'client' ? 'Sent by the program' : 'Received from the server');
    setText(this.head, [`#${m.seq}`, who, clock(m.time), m.opcode, bytes(m.bytes), ...flags].join(' · '));
    const body = m.text !== undefined ? { text: m.text, bytes: m.bytes, truncated: m.truncated } : m.base64 !== undefined ? { base64: m.base64, bytes: m.bytes, truncated: m.truncated } : null;
    this.viewer.show(`${this.flowId}:${m.seq}`, body, '', m.opcode === 'text' || m.opcode === 'binary' ? 'This message carried no payload.' : 'This control message carried no payload.');
  }

  private summary(): MessageSummary | null { return this.page ?? this.flow?.messages ?? null; }

  private paint(): void {
    const s = this.summary();
    const fmt = (n: number): string => n.toLocaleString(language());
    const line1: string[] = [];
    if (s) {
      line1.push(tn(s.count, '{n} message', '{n} messages'), bytes(s.bytes));
      if (s.dropped) line1.push(tn(s.dropped, '{n} counted but not kept', '{n} counted but not kept'));
      if (s.deflate) line1.push(t('permessage-deflate'));
    } else line1.push(this.flowId ? t('Reading the messages...') : '');
    setText(this.sum1, line1.join(' · '));
    const open = this.page?.open ?? !!this.flow?.pending;
    const line2: string[] = [];
    if (this.error) line2.push(this.error);
    else if (s?.close_code) line2.push(tf(s.closed_by === 'server' ? 'Closed by the server: {code} {reason}' : 'Closed by the program: {code} {reason}', { code: String(s.close_code), reason: s.close_reason ?? '' }).trim());
    else if (this.flowId) line2.push(t(open ? 'Still open' : 'Ended without a close message'));
    if (s?.parse_error) line2.push(tf('Frames could not be read from here on: {error}', { error: s.parse_error }));
    if (this.page?.evicted) line2.push(t('The messages were let go to keep memory bounded; the counts remain.'));
    setText(this.sum2, line2.join(' · '));
    setTitle(this.sum2, line2.join(' · '));
    setClass(this.sum2, 'is-error', !!this.error || !!s?.parse_error);
    for (const b of Array.from(this.dirSeg.children) as HTMLElement[]) {
      const on = b.dataset.dir === this.dir;
      setClass(b, 'is-on', on);
      if (b.getAttribute('aria-checked') !== String(on)) b.setAttribute('aria-checked', String(on));
    }
    for (const b of Array.from(this.opBox.children) as HTMLButtonElement[]) {
      const codes = OPS.find(([id]) => id === b.dataset.op)?.[2] ?? [];
      const on = codes.every((c) => this.ops.has(c));
      setClass(b, 'is-on', on);
      if (b.getAttribute('aria-pressed') !== String(on)) b.setAttribute('aria-pressed', String(on));
      setDisabled(b, !this.flowId);
    }
    setText(this.shown, this.view.length === this.items.length ? '' : tf('{shown} of {total}', { shown: fmt(this.view.length), total: fmt(this.items.length) }));
    setText(this.none, !this.flowId ? '' : this.items.length ? (this.view.length ? '' : t('No message matches the filters.')) : t('No messages yet.'));
    const vis = this.none.textContent ? '' : 'hidden';
    if (this.none.style.visibility !== vis) this.none.style.visibility = vis;
  }

  private pool(): void {
    const want = Math.ceil((this.list.clientHeight || 200) / ROW_H) + 2;
    while (this.rows.length < want) {
      const r = el('<div class="fn-ws-row" role="option"><span class="fn-ws-arrow"></span><span class="fn-ws-time mono"></span><span class="fn-ws-op mono"></span><span class="fn-ws-size mono"></span><span class="fn-ws-prev mono"></span></div>');
      this.rowsBox.append(r);
      this.rows.push(r);
    }
  }

  private queue(toEnd = false): void {
    if (toEnd) this.pendingEnd = true;
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => { this.raf = 0; this.render(); });
  }

  private pendingEnd = false;

  private render(): void {
    if (!this.rows.length) this.pool();
    const h = `${this.view.length * ROW_H}px`;
    if (this.spacer.style.height !== h) this.spacer.style.height = h;
    if (this.pendingEnd) { this.pendingEnd = false; const end = Math.max(0, this.view.length * ROW_H - this.list.clientHeight); if (this.list.scrollTop !== end) this.list.scrollTop = end; }
    const first = Math.floor(this.list.scrollTop / ROW_H);
    const ty = `translateY(${first * ROW_H}px)`;
    if (this.rowsBox.style.transform !== ty) this.rowsBox.style.transform = ty;
    this.rows.forEach((row, k) => {
      const it = this.items[this.view[first + k] ?? -1];
      if (!it) { if (row.style.visibility !== 'hidden') row.style.visibility = 'hidden'; row.dataset.seq = ''; return; }
      if (row.style.visibility) row.style.visibility = '';
      const m = it.m, sel = m.seq === this.selected;
      setClass(row, 'is-selected', sel);
      if (row.getAttribute('aria-selected') !== String(sel)) row.setAttribute('aria-selected', String(sel));
      if (row.dataset.seq === String(m.seq) && row.dataset.lang === language()) return;
      row.dataset.seq = String(m.seq);
      row.dataset.lang = language();
      const [arrow, time, op, size, prev] = Array.from(row.children) as HTMLElement[];
      if (!arrow || !time || !op || !size || !prev) return;
      if (arrow.dataset.dir !== m.dir) { arrow.dataset.dir = m.dir; arrow.innerHTML = icon(m.dir === 'client' ? 'arrowUp' : 'arrowDown', 12); }
      const dirText = t(m.dir === 'client' ? 'Sent by the program' : 'Received from the server');
      setTitle(arrow, dirText);
      arrow.setAttribute('aria-label', dirText);
      setClass(row, 'is-sent', m.dir === 'client');
      setText(time, clock(m.time));
      setText(op, m.opcode);
      setClass(op, 'is-control', m.opcode !== 'text' && m.opcode !== 'binary');
      setText(size, bytes(m.bytes));
      setText(prev, it.preview);
      setTitle(prev, it.preview);
    });
  }
}
