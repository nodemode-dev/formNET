import { TrafficError } from '../api-traffic';
import { breakpointWatch, type BreakpointWatch, type Held, type HeldOutcome } from '../breakpoint-watch';
import { bytes } from '../format';
import type { Header } from '../types';
import type { BreakpointEdit } from '../types-traffic';
import { el, q, setClass, setDisabled, setHidden, setText, setTitle, setValue } from './dom';
import { icon } from './icons';
import { onLanguage, t, tn } from './i18n';
import { tf } from './panels/controls';

/** A short word for how an exchange stopped waiting, for its row. */
const OUTCOME_SHORT: Record<HeldOutcome, string> = { continued: 'Continued', edited: 'Edited', aborted: 'Aborted', timeout: 'Timed out', client_gone: 'Program gone', released: 'Released' };

/** The whole sentence for it, under the editor. */
const OUTCOME_LONG: Record<HeldOutcome, string> = {
  continued: 'Continued unchanged.',
  edited: 'Continued with your changes.',
  aborted: 'Aborted: FormNET closed the program\'s connection.',
  timeout: 'Timed out: nobody answered in time, so it went on as it was.',
  client_gone: 'The program went away before an answer, so nothing was sent.',
  released: 'No longer paused: it was answered elsewhere.',
};

const headerText = (hs: readonly Header[] | null): string => (hs ?? []).map((h) => `${h.name}: ${h.value}`).join('\n');
const tidy = (s: string): string => s.split(/\r?\n/).map((l) => l.trim()).filter(Boolean).join('\n');
const secondsLeft = (deadline: string): number => Math.max(0, Math.ceil((Date.parse(deadline) - Date.now()) / 1000));

/** The headers a person typed, one "Name: value" per line; throws naming the first line without a colon. */
function parseHeaders(text: string): Header[] {
  const out: Header[] = [];
  text.split(/\r?\n/).forEach((line, i) => {
    if (!line.trim()) return;
    const at = line.indexOf(':');
    if (at <= 0) throw new Error(tf('Line {n} of the headers needs a name, a colon and a value.', { n: String(i + 1) }));
    out.push({ name: line.slice(0, at).trim(), value: line.slice(at + 1).trim() });
  });
  return out;
}

const SLOT_HTML = '<button class="fn-bp-row" type="button" role="option"><span class="fn-bp-row-top"><span class="fn-bp-stage"></span><span class="fn-bp-row-left"></span></span><span class="fn-bp-row-url mono" translate="no" dir="ltr"></span></button>';

/** The Breakpoints panel: a window-level panel, not modal, that lists the exchanges held at breakpoints (newest first,
 *  a finished one dimmed with what became of it) and edits the chosen one. It opens by itself when an exchange is
 *  paused and never takes the focus. Its frame is a fixed size; list rows are fixed slots whose text is rewritten in
 *  place, so an arrival or a departure moves nothing; the editor keeps one layout for both stages. */
export class BreakpointPanel {
  readonly root = el(`<section class="fn-bp" role="dialog" aria-modal="false" hidden>` +
    `<header class="fn-bp-head"><span class="fn-bp-icon">${icon('pause', 16)}</span><span class="fn-bp-title"></span><span class="fn-bp-count"></span>` +
    `<button class="cv-icon-btn fn-bp-close" type="button">${icon('close', 16)}</button></header>` +
    `<div class="fn-bp-body"><div class="fn-bp-side"><div class="fn-bp-list" role="listbox"></div><p class="cv-note fn-bp-empty"></p>` +
    `<div class="fn-bp-sidefoot"><button class="cv-btn cv-btn-ghost fn-bp-clear" type="button"></button></div></div>` +
    `<div class="fn-bp-main"><p class="fn-bp-what"></p><p class="fn-bp-left"></p>` +
    `<div class="fn-bp-line"><input class="cv-input fn-bp-method" type="text" spellcheck="false" autocomplete="off" dir="ltr"><input class="cv-input fn-bp-status" type="number" min="100" max="999" step="1" dir="ltr"><input class="cv-input fn-bp-url" type="text" spellcheck="false" autocomplete="off" dir="ltr"></div>` +
    `<label class="fn-bp-label"><span class="fn-bp-hlabel"></span><textarea class="cv-input fn-bp-headers" spellcheck="false" dir="ltr"></textarea></label>` +
    `<label class="fn-bp-label"><span class="fn-bp-blabel"></span><textarea class="cv-input fn-bp-bodytext" spellcheck="false" dir="ltr"></textarea></label>` +
    `<p class="cv-note fn-bp-bodynote"></p><p class="cv-note fn-bp-say" role="status"></p>` +
    `<div class="fn-bp-actions"><button class="cv-btn cv-btn-danger fn-bp-abort" type="button"></button><span class="fn-bp-gap"></span>` +
    `<button class="cv-btn cv-btn-ghost fn-bp-plain" type="button"></button><button class="cv-btn cv-btn-primary fn-bp-go" type="button"></button></div></div></div></section>`);
  private readonly list = q(this.root, '.fn-bp-list');
  private readonly method = q<HTMLInputElement>(this.root, '.fn-bp-method');
  private readonly status = q<HTMLInputElement>(this.root, '.fn-bp-status');
  private readonly url = q<HTMLInputElement>(this.root, '.fn-bp-url');
  private readonly headers = q<HTMLTextAreaElement>(this.root, '.fn-bp-headers');
  private readonly body = q<HTMLTextAreaElement>(this.root, '.fn-bp-bodytext');
  private readonly go = q<HTMLButtonElement>(this.root, '.fn-bp-go');
  private readonly plain = q<HTMLButtonElement>(this.root, '.fn-bp-plain');
  private readonly abort = q<HTMLButtonElement>(this.root, '.fn-bp-abort');
  private readonly say = q(this.root, '.fn-bp-say');
  private readonly slots: HTMLButtonElement[] = [];
  private items: Held[] = [];
  private selected = '';
  private shownFor = '';
  private dirty = false;
  private busy = false;
  private drag = { x: 0, y: 0 };

  constructor(private readonly watch: BreakpointWatch) {
    document.getElementById('overlay-root')?.appendChild(this.root);
    q(this.root, '.fn-bp-close').addEventListener('click', () => this.hide());
    q(this.root, '.fn-bp-clear').addEventListener('click', () => { this.watch.clearFinished(); });
    this.list.addEventListener('click', (e) => {
      const b = (e.target as HTMLElement).closest<HTMLButtonElement>('.fn-bp-row');
      const id = b?.dataset.id;
      if (id) this.select(id);
    });
    this.list.addEventListener('keydown', (e) => {
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
      e.preventDefault();
      const at = this.items.findIndex((h) => h.p.id === this.selected);
      const next = this.items[Math.min(this.items.length - 1, Math.max(0, at + (e.key === 'ArrowDown' ? 1 : -1)))];
      if (next) { this.select(next.p.id); this.slots[this.items.indexOf(next)]?.focus(); }
    });
    for (const f of [this.method, this.status, this.url, this.headers, this.body]) f.addEventListener('input', () => { this.dirty = true; this.clearSay(); });
    this.go.addEventListener('click', () => void this.answer('edit'));
    this.plain.addEventListener('click', () => void this.answer('plain'));
    this.abort.addEventListener('click', () => void this.answer('abort'));
    this.root.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); this.hide(); }
      if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); void this.answer('edit'); }
    });
    this.bindMove();
    watch.subscribe((arrived) => { this.sync(); if (arrived) this.show(); });
    onLanguage(() => { this.label(); this.sync(); });
    window.setInterval(() => { if (!this.root.hidden && this.items.some((h) => !h.outcome)) this.tick(); }, 250);
    this.label();
    this.sync();
  }

  get isOpen(): boolean { return !this.root.hidden; }

  /** Shows the panel where it was; the focus stays where the person left it. */
  show(): void { setHidden(this.root, false); this.tick(); }

  hide(): void {
    if (this.root.contains(document.activeElement)) (document.activeElement as HTMLElement).blur();
    setHidden(this.root, true);
  }

  toggle(): void { if (this.isOpen) this.hide(); else this.show(); }

  private label(): void {
    setText(q(this.root, '.fn-bp-title'), t('Breakpoints'));
    this.root.setAttribute('aria-label', t('Breakpoints'));
    const close = q(this.root, '.fn-bp-close');
    close.title = t('Hide');
    close.setAttribute('aria-label', t('Hide the Breakpoints panel'));
    setText(q(this.root, '.fn-bp-empty'), t('Nothing is paused. An exchange a breakpoint rule matches waits here for you.'));
    setText(q(this.root, '.fn-bp-clear'), t('Clear finished'));
    setText(q(this.root, '.fn-bp-hlabel'), t('Headers, one "Name: value" per line'));
    setText(q(this.root, '.fn-bp-blabel'), t('Body'));
    setText(this.go, t('Continue'));
    this.go.title = t('Send it on with your changes (Ctrl+Enter); only the fields you changed are sent');
    setText(this.plain, t('Continue unchanged'));
    setText(this.abort, t('Abort'));
    this.abort.title = t('Close the program\'s connection without sending anything');
    this.method.setAttribute('aria-label', t('Method'));
    this.status.setAttribute('aria-label', t('Status'));
    this.url.setAttribute('aria-label', t('URL'));
    this.headers.setAttribute('aria-label', t('Headers'));
    this.body.setAttribute('aria-label', t('Body'));
  }

  private sync(): void {
    const items = this.watch.list();
    this.items = items;
    const focusId = this.slots.find((s) => s === document.activeElement)?.dataset.id ?? '';
    const cur = items.find((h) => h.p.id === this.selected);
    const newest = items.find((h) => !h.outcome);
    if (!cur || (cur.outcome && !this.dirty && newest)) this.select(newest?.p.id ?? items[0]?.p.id ?? '', false);
    while (this.slots.length < items.length) { const s = el<HTMLButtonElement>(SLOT_HTML); this.list.append(s); this.slots.push(s); }
    while (this.slots.length > items.length) this.slots.pop()?.remove();
    items.forEach((h, i) => this.fillSlot(this.slots[i], h));
    if (focusId) this.slots[items.findIndex((h) => h.p.id === focusId)]?.focus();
    const paused = this.watch.pausedCount();
    setText(q(this.root, '.fn-bp-count'), paused ? tn(paused, '{n} paused', '{n} paused') : '');
    setClass(q(this.root, '.fn-bp-empty'), 'is-shown', items.length === 0);
    setDisabled(q<HTMLButtonElement>(this.root, '.fn-bp-clear'), !items.some((h) => h.outcome));
    this.fillEditor();
  }

  private fillSlot(s: HTMLButtonElement, h: Held): void {
    const p = h.p;
    if (s.dataset.id !== p.id) s.dataset.id = p.id;
    const on = p.id === this.selected;
    setClass(s, 'is-selected', on);
    if (s.getAttribute('aria-selected') !== String(on)) s.setAttribute('aria-selected', String(on));
    setClass(s, 'is-done', !!h.outcome);
    setText(q(s, '.fn-bp-stage'), p.stage === 'request' ? tf('Request · {method}', { method: p.method ?? '' }) : tf('Response · {status}', { status: String(p.status ?? '') }));
    setText(q(s, '.fn-bp-row-left'), h.outcome ? t(OUTCOME_SHORT[h.outcome]) : tf('{n} s', { n: String(secondsLeft(p.deadline)) }));
    setClass(q(s, '.fn-bp-row-left'), 'is-timeout', h.outcome === 'timeout' || h.outcome === 'client_gone');
    setText(q(s, '.fn-bp-row-url'), p.url);
    setTitle(s, p.url);
  }

  private select(id: string, mark = true): void {
    if (this.selected === id) return;
    if (this.selected) this.watch.keep.delete(this.selected);
    this.selected = id;
    if (id) this.watch.keep.add(id);
    this.dirty = false;
    this.clearSay();
    if (mark) { this.slots.forEach((s, i) => { const on = this.items[i]?.p.id === id; setClass(s, 'is-selected', on); s.setAttribute('aria-selected', String(on)); }); this.fillEditor(); }
  }

  /** Writes the chosen exchange into the editor when the choice changes; afterwards only its state (waiting, finished)
   *  is patched, so what the person types stays. */
  private fillEditor(): void {
    const h = this.items.find((x) => x.p.id === this.selected);
    const p = h?.p;
    const req = !p || p.stage === 'request';
    setHidden(this.method, !req);
    setHidden(this.status, req);
    if (p && this.shownFor !== p.id) {
      this.shownFor = p.id;
      this.method.value = p.method ?? '';
      this.status.value = p.status ? String(p.status) : '';
      this.url.value = p.url;
      this.headers.value = headerText(p.headers);
      this.body.value = p.body_text ?? '';
      this.headers.scrollTop = 0;
      this.body.scrollTop = 0;
    } else if (!p && this.shownFor) {
      this.shownFor = '';
      for (const f of [this.method, this.status, this.url, this.headers, this.body]) f.value = '';
    }
    const binary = !!p?.body_base64;
    this.body.placeholder = binary ? tf('Binary body, {size}: it goes on unchanged.', { size: bytes(p?.body_bytes ?? 0) }) : p ? t('No body') : '';
    setText(q(this.root, '.fn-bp-bodynote'), p?.body_decoded ? t('The body is shown decoded; if you change it, it is sent without its Content-Encoding.') : '');
    const open = !!p && !h?.outcome && !this.busy;
    const off = (f: HTMLInputElement | HTMLTextAreaElement, v: boolean): void => { if (f.disabled !== v) f.disabled = v; };
    for (const f of [this.method, this.status, this.headers]) off(f, !open);
    off(this.url, !open || !req);
    if (this.url.readOnly !== !req) this.url.readOnly = !req;
    off(this.body, !open || binary);
    for (const b of [this.go, this.plain, this.abort]) setDisabled(b, !open);
    const what = q(this.root, '.fn-bp-what');
    setText(what, !p ? t('Choose a paused exchange in the list.') : tf(p.stage === 'request' ? 'The request is paused by the rule "{rule}".' : 'The response is paused by the rule "{rule}".', { rule: p.rule }));
    setTitle(what, what.textContent ?? '');
    this.tick();
  }

  /** The countdowns: each waiting row's seconds and the line above the editor. */
  private tick(): void {
    this.items.forEach((h, i) => { if (!h.outcome && this.slots[i]) setText(q(this.slots[i], '.fn-bp-row-left'), tf('{n} s', { n: String(secondsLeft(h.p.deadline)) })); });
    const h = this.items.find((x) => x.p.id === this.selected);
    const left = q(this.root, '.fn-bp-left');
    setClass(left, 'is-done', !!h?.outcome);
    setClass(left, 'is-timeout', h?.outcome === 'timeout' || h?.outcome === 'client_gone');
    setText(left, !h ? '' : h.outcome ? t(OUTCOME_LONG[h.outcome]) : tf('Goes on as it was in {n} s unless you answer.', { n: String(secondsLeft(h.p.deadline)) }));
  }

  /** Only what the person changed is sent; Continue with nothing changed is the same as Continue unchanged. */
  private edit(): BreakpointEdit {
    const h = this.items.find((x) => x.p.id === this.selected);
    if (!h) return {};
    const p = h.p, e: BreakpointEdit = {};
    if (p.stage === 'request') {
      const m = this.method.value.trim().toUpperCase();
      if (m && m !== (p.method ?? '')) e.method = m;
      const u = this.url.value.trim();
      if (u && u !== p.url) e.url = u;
    } else {
      const s = this.status.valueAsNumber;
      if (Number.isFinite(s) && s !== p.status) e.status = Math.round(s);
    }
    if (tidy(this.headers.value) !== tidy(headerText(p.headers))) e.headers = parseHeaders(this.headers.value);
    if (!p.body_base64 && this.body.value !== (p.body_text ?? '')) e.body_text = this.body.value;
    return e;
  }

  private async answer(kind: 'edit' | 'plain' | 'abort'): Promise<void> {
    const id = this.selected;
    const h = this.items.find((x) => x.p.id === id);
    if (!h || h.outcome || this.busy) return;
    let change: BreakpointEdit = {};
    try { if (kind === 'edit') change = this.edit(); } catch (err) { this.sayError((err as Error).message); return; }
    this.busy = true;
    this.fillEditor();
    try {
      if (kind === 'abort') await this.watch.abort(id);
      else await this.watch.continue(id, change);
      this.dirty = false;
    } catch (err) {
      this.sayError(err instanceof TrafficError && err.status === 404 ? t('It is no longer paused: it was answered, timed out or its program went away.') : (err as Error).message);
      this.watch.poke();
    } finally {
      this.busy = false;
      this.fillEditor();
    }
  }

  private sayError(text: string): void { setText(this.say, text); setClass(this.say, 'is-error', true); }
  private clearSay(): void { setText(this.say, ''); setClass(this.say, 'is-error', false); }

  /** The head drags the panel; it stays where it was left while the page is open. */
  private bindMove(): void {
    const head = q(this.root, '.fn-bp-head');
    head.addEventListener('pointerdown', (e) => {
      if (e.button !== 0 || (e.target as HTMLElement).closest('button')) return;
      const sx = e.clientX - this.drag.x, sy = e.clientY - this.drag.y;
      head.setPointerCapture(e.pointerId);
      const r = this.root.getBoundingClientRect();
      const left = r.left - this.drag.x, top = r.top - this.drag.y;
      const move = (ev: PointerEvent): void => {
        const x = Math.min(Math.max(Math.round(ev.clientX - sx), 80 - r.width - left), window.innerWidth - 80 - left);
        const y = Math.min(Math.max(Math.round(ev.clientY - sy), -top), window.innerHeight - 44 - top);
        this.drag = { x, y };
        this.root.style.translate = `${x}px ${y}px`;
      };
      const up = (): void => { head.removeEventListener('pointermove', move); head.removeEventListener('pointerup', up); head.removeEventListener('pointercancel', up); };
      head.addEventListener('pointermove', move);
      head.addEventListener('pointerup', up);
      head.addEventListener('pointercancel', up);
    });
  }
}

let panel: BreakpointPanel | null = null;

/** Builds the panel once (hidden) beside the page's dialogs; later calls return the same one. */
export function mountBreakpointPanel(): BreakpointPanel {
  panel ??= new BreakpointPanel(breakpointWatch);
  return panel;
}

/** Shows the Breakpoints panel, for the status bar's chip and the command. */
export function showBreakpoints(): void {
  mountBreakpointPanel().show();
}
