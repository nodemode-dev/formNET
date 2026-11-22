import type { Store } from '../state.ts';
import { flowFilters, METHODS, parseTyped, STATUS_CLASSES, TYPE_CHIPS, type FilterState, type FlowQuery, type QueryAnswer, type StatusClass, type TypeChip } from '../filters.ts';
import { el, q, setClass, setText, setTitle, setValue } from './dom';
import { language, onLanguage, t, tn } from './i18n';
import { icon } from './icons';
import { labelText } from './panels/controls';

type Chip = { node: HTMLButtonElement; label: string; title: string; on: (s: FilterState) => boolean; run: () => void };

const pressed = (b: HTMLElement, on: boolean): void => {
  setClass(b, 'is-on', on);
  if (b.getAttribute('aria-pressed') !== String(on)) b.setAttribute('aria-pressed', String(on));
};

/** The Flows filters in the options bar: the body switch beside the text box, a lane of quick chips of fixed widths
 *  (status classes and errors, methods, types, from session, rules, throttled) that scrolls inside itself when the window
 *  is narrow, and More filters with the active count in a fixed slot, which opens a popover for size and duration
 *  ranges, the process and the focused hosts. Built once; the model patches it. The bar's height never changes. */
export class FilterBar {
  readonly bodyToggle = el<HTMLButtonElement>('<button type="button" class="fn-flt-body" aria-pressed="false"></button>');
  readonly cutMark = el(`<span class="fn-flt-cut" role="img">${icon('warning', 14)}</span>`);
  readonly chips = el('<div class="fn-flt-chips" role="toolbar"></div>');
  readonly more = el<HTMLButtonElement>(`<button type="button" class="cv-btn fn-flt-more" aria-haspopup="dialog" aria-expanded="false">${icon('filter', 14)}<span class="fn-flt-more-text"></span><span class="fn-flt-count"></span></button>`);
  private readonly pop = el('<div class="fn-flt-pop" role="dialog" hidden></div>');
  private readonly list: Chip[] = [];
  private readonly fields: Record<'minKB' | 'maxKB' | 'minMs' | 'maxMs' | 'process', HTMLInputElement> = {
    minKB: el<HTMLInputElement>('<input class="cv-input" type="number" min="0" step="any" inputmode="decimal" data-plain>'),
    maxKB: el<HTMLInputElement>('<input class="cv-input" type="number" min="0" step="any" inputmode="decimal" data-plain>'),
    minMs: el<HTMLInputElement>('<input class="cv-input" type="number" min="0" step="any" inputmode="decimal" data-plain>'),
    maxMs: el<HTMLInputElement>('<input class="cv-input" type="number" min="0" step="any" inputmode="decimal" data-plain>'),
    process: el<HTMLInputElement>('<input class="cv-input" type="text" spellcheck="false" autocomplete="off" dir="ltr">'),
  };
  private readonly focus = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="3" spellcheck="false" dir="ltr"></textarea>');
  private readonly ignore = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="3" spellcheck="false" dir="ltr"></textarea>');
  private readonly clearAll = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly words: Array<[HTMLElement, string]> = [];
  private timer = 0;

  constructor(private readonly store: Store, private readonly query: FlowQuery | null = null) {
    this.chips.setAttribute('aria-label', t('Quick filters'));
    const group = (): HTMLElement => { const g = el('<span class="fn-flt-group"></span>'); this.chips.append(g); return g; };
    const add = (g: HTMLElement, size: 's' | 'm' | 'l', label: string, title: string, on: Chip['on'], run: () => void, mono = false): void => {
      const node = el<HTMLButtonElement>(`<button type="button" class="fn-chip fn-chip-${size}${mono ? ' mono' : ''}" aria-pressed="false"></button>`);
      node.addEventListener('click', run);
      g.append(node);
      this.list.push({ node, label, title, on, run });
    };
    const st = group();
    for (const c of STATUS_CLASSES) add(st, 's', c, STATUS_TITLES[c], (s) => s.status.includes(c), () => flowFilters.toggleStatus(c), true);
    add(st, 'm', 'Errors', 'Only flows that failed: no answer, a refused certificate, a dropped connection', (s) => s.errors, () => flowFilters.set('errors', !flowFilters.get().errors));
    const me = group();
    for (const m of METHODS) add(me, 'm', m, 'Only {method} requests; several methods may be chosen', (s) => s.methods.includes(m), () => flowFilters.toggleMethod(m), true);
    const ty = group();
    for (const [c, label] of TYPE_CHIPS) add(ty, 'l', label, TYPE_TITLES[c], (s) => s.type === c, () => flowFilters.set('type', flowFilters.get().type === c ? '' : c));
    const ot = group();
    add(ot, 'l', 'From session', 'Only flows opened from a session file or imported from a HAR', (s) => s.fromSession, () => flowFilters.set('fromSession', !flowFilters.get().fromSession));
    add(ot, 'l', 'Rules', 'Only flows a rule changed', (s) => s.rules, () => flowFilters.set('rules', !flowFilters.get().rules));
    add(ot, 'l', 'Throttled', 'Only flows the throttle slowed', (s) => s.throttled, () => flowFilters.set('throttled', !flowFilters.get().throttled));

    const edges = (): void => {
      const c = this.chips, max = c.scrollWidth - c.clientWidth, at = Math.abs(c.scrollLeft);
      setClass(c, 'is-more-start', max > 1 && at > 1);
      setClass(c, 'is-more-end', max > 1 && at < max - 1);
    };
    this.chips.addEventListener('scroll', edges, { passive: true });
    new ResizeObserver(edges).observe(this.chips);
    this.chips.addEventListener('wheel', (e) => {
      if (Math.abs(e.deltaY) <= Math.abs(e.deltaX) || this.chips.scrollWidth <= this.chips.clientWidth) return;
      e.preventDefault();
      this.chips.scrollLeft += getComputedStyle(this.chips).direction === 'rtl' ? -e.deltaY : e.deltaY;
    }, { passive: false });
    this.bodyToggle.addEventListener('click', () => flowFilters.set('body', !flowFilters.get().body));
    this.buildPop();
    this.more.addEventListener('click', () => (this.pop.hidden ? this.openPop() : this.closePop()));
    this.relabel();
    onLanguage(() => this.relabel());
    flowFilters.subscribe(() => this.paint());
    store.subscribe((s, prev) => { if (s.filter !== prev.filter) this.paint(); });
    query?.subscribe(() => this.paint());
    this.paint();
  }

  private buildPop(): void {
    const row = (label: string, ...controls: HTMLElement[]): HTMLElement => {
      const r = el('<div class="fn-flt-row"><span class="fn-flt-label"></span><span class="fn-flt-ctl"></span></div>');
      labelText(q(r, '.fn-flt-label'), label);
      q(r, '.fn-flt-ctl').append(...controls);
      return r;
    };
    const dash = (): HTMLElement => el('<span class="fn-flt-dash" aria-hidden="true">-</span>');
    const head = el('<p class="fn-flt-head"></p>');
    const note = el('<p class="cv-note fn-flt-note"></p>');
    this.words.push([head, 'More filters'], [note, 'Every filter must match. Sizes are of the response body as it was sent; a pending request counts the time so far. Hosts are globs such as *.example.com, separated by commas or spaces.']);
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.clearAll);
    this.pop.append(head,
      row('Response size (KB)', this.fields.minKB, dash(), this.fields.maxKB),
      row('Duration (ms)', this.fields.minMs, dash(), this.fields.maxMs),
      row('Process', this.fields.process),
      row('Focus on hosts', this.focus),
      row('Ignore hosts', this.ignore),
      note, bar);
    for (const [k, input] of Object.entries(this.fields) as Array<[keyof typeof this.fields, HTMLInputElement]>) {
      input.addEventListener('input', () => this.later({ [k]: input.value }));
      input.addEventListener('change', () => { window.clearTimeout(this.timer); flowFilters.update({ [k]: input.value }); });
    }
    this.focus.addEventListener('input', () => this.later({ focus: this.focus.value }));
    this.ignore.addEventListener('input', () => this.later({ ignore: this.ignore.value }));
    this.clearAll.addEventListener('click', () => { flowFilters.reset(); for (const input of Object.values(this.fields)) input.value = ''; this.focus.value = ''; this.ignore.value = ''; });
    this.pop.addEventListener('keydown', (e) => { if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); this.closePop(true); } });
    document.addEventListener('pointerdown', (e) => {
      if (this.pop.hidden) return;
      const n = e.target as Node;
      if (!this.pop.contains(n) && !this.more.contains(n)) this.closePop();
    }, true);
    window.addEventListener('resize', () => { if (!this.pop.hidden) this.place(); });
    (document.getElementById('overlay-root') ?? document.body).appendChild(this.pop);
  }

  private later(patch: Partial<FilterState>): void {
    window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => flowFilters.update(patch), 300);
  }

  private place(): void {
    const r = this.more.getBoundingClientRect();
    const w = this.pop.offsetWidth || 340;
    const rtl = getComputedStyle(this.more).direction === 'rtl';
    const left = Math.max(8, Math.min(window.innerWidth - w - 8, rtl ? r.left : r.right - w));
    const top = `${Math.round(r.bottom + 6)}px`, l = `${Math.round(left)}px`;
    if (this.pop.style.top !== top) this.pop.style.top = top;
    if (this.pop.style.left !== l) this.pop.style.left = l;
  }

  openPop(): void {
    const s = flowFilters.get();
    for (const k of Object.keys(this.fields) as (keyof typeof this.fields)[]) setValue(this.fields[k], s[k]);
    setValue(this.focus, s.focus);
    setValue(this.ignore, s.ignore);
    this.pop.style.visibility = 'hidden';
    this.pop.hidden = false;
    this.place();
    this.pop.style.visibility = '';
    this.more.setAttribute('aria-expanded', 'true');
    this.fields.minKB.focus();
  }

  closePop(refocus = false): void {
    if (this.pop.hidden) return;
    window.clearTimeout(this.timer);
    flowFilters.update({ minKB: this.fields.minKB.value, maxKB: this.fields.maxKB.value, minMs: this.fields.minMs.value, maxMs: this.fields.maxMs.value, process: this.fields.process.value, focus: this.focus.value, ignore: this.ignore.value });
    this.pop.hidden = true;
    this.more.setAttribute('aria-expanded', 'false');
    if (refocus) this.more.focus();
  }

  private relabel(): void {
    for (const c of this.list) {
      setText(c.node, c.label.includes('xx') || METHODS.includes(c.label) ? c.label : t(c.label));
      setTitle(c.node, t(c.title).replace('{method}', c.label));
    }
    setText(q(this.more, '.fn-flt-more-text'), t('More filters'));
    this.pop.setAttribute('aria-label', t('More filters'));
    setText(this.clearAll, t('Clear all filters'));
    this.fields.minKB.setAttribute('aria-label', t('Smallest response size in KB'));
    this.fields.maxKB.setAttribute('aria-label', t('Largest response size in KB'));
    this.fields.minMs.setAttribute('aria-label', t('Shortest duration in ms'));
    this.fields.maxMs.setAttribute('aria-label', t('Longest duration in ms'));
    this.fields.minKB.placeholder = this.fields.minMs.placeholder = t('min');
    this.fields.maxKB.placeholder = this.fields.maxMs.placeholder = t('max');
    this.fields.process.placeholder = t('part of the name, such as curl');
    this.focus.placeholder = t('api.example.com, *.cdn.example.net');
    this.ignore.placeholder = t('Host names or globs such as *.example.com, separated by commas');
    this.ignore.setAttribute('aria-label', t('Ignore hosts'));
    for (const [n, english] of this.words) setText(n, t(english));
    this.paint();
  }

  /** The short reading of what is on, for a tooltip or a status line. */
  summary(): string {
    const n = flowFilters.activeCount(this.store.get().filter);
    return n ? tn(n, '{n} filter on', '{n} filters on') : t('No filters on');
  }

  private paint(): void {
    const s = flowFilters.get();
    for (const c of this.list) pressed(c.node, c.on(s));
    pressed(this.bodyToggle, s.body);
    setText(this.bodyToggle, t('Body'));
    setTitle(this.bodyToggle, t(s.body ? 'Searching the captured request and response bodies for the text; press to search URLs again' : 'Search the captured request and response bodies for the text instead of URLs'));
    const n = flowFilters.activeCount(this.store.get().filter);
    const count = q(this.more, '.fn-flt-count');
    setText(count, n ? n.toLocaleString(language()) : '');
    const vis = n ? '' : 'hidden';
    if (count.style.visibility !== vis) count.style.visibility = vis;
    setClass(this.more, 'is-on', n > 0);
    setTitle(this.more, this.summary());
    const a: QueryAnswer | null = this.query?.get() ?? null;
    const cut = !!a?.bodyCut && s.body && !!parseTyped(this.store.get().filter).rest;
    const cutText = t('Body search stopped early: it read the newest bodies up to its limit, and older flows were not searched.');
    setTitle(this.cutMark, cut ? cutText : '');
    if (cut) this.cutMark.setAttribute('aria-label', cutText); else this.cutMark.removeAttribute('aria-label');
    const cv = cut ? '' : 'hidden';
    if (this.cutMark.style.visibility !== cv) this.cutMark.style.visibility = cv;
    setClass(this.chips, 'is-busy', !!a?.busy);
  }
}

const STATUS_TITLES: Record<StatusClass, string> = {
  '2xx': 'Only answers with a 2xx status (success); a neighbouring class joins the range',
  '3xx': 'Only answers with a 3xx status (redirects); a neighbouring class joins the range',
  '4xx': 'Only answers with a 4xx status (refused by the server); a neighbouring class joins the range',
  '5xx': 'Only answers with a 5xx status (the server failed); a neighbouring class joins the range',
};

const TYPE_TITLES: Record<TypeChip, string> = {
  json: 'Only JSON responses',
  html: 'Only HTML pages',
  js: 'Only scripts',
  css: 'Only style sheets',
  image: 'Only images',
  media: 'Only audio and video',
  websocket: 'Only WebSockets',
  other: 'Only responses of any other type, and requests without a response',
};
