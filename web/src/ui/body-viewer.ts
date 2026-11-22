import { copyText } from '../dom.ts';
import { bytes } from '../format.ts';
import { el, patchList, q, setClass, setDisabled, setText, setTitle, swapMedia } from './dom';
import { language, onLanguage, t, tn } from './i18n';
import { icon } from './icons';
import { tf } from './panels/controls';

/** A captured body as the server sends it (types.ts Body, or a WebSocket message's payload). */
export interface ViewBody {
  text?: string;
  base64?: string;
  bytes: number;
  truncated: boolean;
  decoded?: boolean;
  encoding?: string;
}

export type ViewMode = 'pretty' | 'raw' | 'hex' | 'image' | 'form';

const MODES: ReadonlyArray<readonly [ViewMode, string]> = [['pretty', 'Pretty'], ['raw', 'Raw'], ['hex', 'Hex'], ['image', 'Image'], ['form', 'Form']];
const LINE_H = 18;
const MAX_COL = 2000;
const MAX_SPACER = 8_000_000;
const SLICE_MS = 8;
const MAX_MATCHES = 10_000;
const VOID_TAGS = new Set(['area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link', 'meta', 'source', 'track', 'wbr']);

/** Display lines, built a slice at a time: the text of each line, its depth, the line that closes it (folding) and the
 *  number shown in the gutter (0 for the continuation of a line longer than MAX_COL). */
class LineStore {
  readonly texts: string[] = [];
  readonly depths: number[] = [];
  readonly closes: number[] = [];
  readonly nums: number[] = [];
  done = false;
  failed = false;
  cols = 0;
  pos = 0;
  private n = 0;

  constructor(readonly kind: 'json' | 'markup' | 'text') {}

  get count(): number { return this.texts.length; }
  text(i: number): string { return this.texts[i] ?? ''; }

  push(text: string, depth: number): number {
    const at = this.texts.length;
    this.n += 1;
    for (let off = 0; off === 0 || off < text.length; off += MAX_COL) {
      this.texts.push(text.length > MAX_COL ? text.slice(off, off + MAX_COL) : text);
      this.depths.push(depth);
      this.closes.push(-1);
      this.nums.push(off === 0 ? this.n : 0);
      if (text.length <= MAX_COL) break;
    }
    const w = Math.min(text.length, MAX_COL) + depth * 2;
    if (w > this.cols) this.cols = w;
    return at;
  }

  appendLast(s: string): void {
    const i = this.texts.length - 1;
    if (i >= 0) this.texts[i] += s;
  }
}

/** Fixed-width rows of a byte array: offset, sixteen bytes in hex, the same bytes as ASCII. Rows are written when
 *  shown, never all at once. */
class HexStore {
  done = true;
  failed = false;
  readonly cols = 77;
  constructor(readonly data: Uint8Array) {}
  get count(): number { return Math.ceil(this.data.length / 16); }
  text(i: number): string {
    const off = i * 16, end = Math.min(off + 16, this.data.length);
    let hex = '', asc = '';
    for (let k = off; k < off + 16; k++) {
      if (k === off + 8) hex += ' ';
      if (k < end) {
        const b = this.data[k] ?? 0;
        hex += (b < 16 ? '0' : '') + b.toString(16) + ' ';
        asc += b >= 32 && b < 127 ? String.fromCharCode(b) : '.';
      } else hex += '   ';
    }
    return `${off.toString(16).padStart(8, '0')}  ${hex} ${asc}`;
  }
}

type Source = LineStore | HexStore;

const isWS = (c: number): boolean => c === 32 || c === 10 || c === 13 || c === 9;

function stringEnd(s: string, i: number): number {
  let j = i + 1;
  for (;;) {
    const k = s.indexOf('"', j);
    if (k < 0) return -1;
    let b = k - 1, n = 0;
    while (b > i && s.charCodeAt(b) === 92) { n += 1; b -= 1; }
    if (n % 2 === 0) return k;
    j = k + 1;
  }
}

const SCALAR = /-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?|true|false|null/y;

/** JSON pretty-printed one token at a time, without JSON.parse, so a large body never blocks the page. Returns false
 *  for text that is not JSON. */
function* jsonLines(s: string, out: LineStore): Generator<void, boolean> {
  const n = s.length;
  const stack: { open: number; obj: boolean; key: boolean; items: number }[] = [];
  let i = 0, prefix = '', after = false, tick = 0;
  for (;;) {
    if (++tick % 512 === 0) { out.pos = i; yield; }
    while (i < n && isWS(s.charCodeAt(i))) i++;
    if (i >= n) break;
    const c = s[i];
    const top = stack[stack.length - 1];
    if (c === ',') {
      if (!after || !top) return false;
      out.appendLast(',');
      after = false;
      if (top.obj) top.key = true;
      i++;
      continue;
    }
    if (c === '}' || c === ']') {
      if (!top || (c === '}') !== top.obj || (!after && top.items > 0) || prefix) return false;
      stack.pop();
      const line = out.push(c, stack.length);
      out.closes[top.open] = line;
      after = true;
      i++;
      continue;
    }
    if (after) return false;
    if (top?.obj && top.key) {
      if (c !== '"') return false;
      const end = stringEnd(s, i);
      if (end < 0) return false;
      const key = s.slice(i, end + 1);
      i = end + 1;
      while (i < n && isWS(s.charCodeAt(i))) i++;
      if (s[i] !== ':') return false;
      i++;
      prefix = `${key}: `;
      top.key = false;
      continue;
    }
    if (top) top.items += 1;
    if (c === '{' || c === '[') {
      let j = i + 1;
      while (j < n && isWS(s.charCodeAt(j))) j++;
      const close = c === '{' ? '}' : ']';
      if (s[j] === close) { out.push(prefix + c + close, stack.length); prefix = ''; i = j + 1; after = true; continue; }
      const line = out.push(prefix + c, stack.length);
      prefix = '';
      stack.push({ open: line, obj: c === '{', key: c === '{', items: 0 });
      i++;
      continue;
    }
    let tok: string;
    if (c === '"') {
      const end = stringEnd(s, i);
      if (end < 0) return false;
      tok = s.slice(i, end + 1);
      i = end + 1;
    } else {
      SCALAR.lastIndex = i;
      const m = SCALAR.exec(s);
      if (!m) return false;
      tok = m[0];
      i += tok.length;
    }
    out.push(prefix + tok, stack.length);
    prefix = '';
    after = true;
  }
  return stack.length === 0 && after;
}

/** XML or HTML, one element per line, indented by nesting; script and style contents are kept as their own lines. */
function* markupLines(s: string, out: LineStore, html: boolean): Generator<void, boolean> {
  const re = /<!--[\s\S]*?-->|<!\[CDATA\[[\s\S]*?\]\]>|<[^>]*>|[^<]+/g;
  const open: { line: number; name: string }[] = [];
  let m: RegExpExecArray | null, tick = 0, lower: string | null = null;
  const text = (chunk: string, depth: number): void => {
    for (const ln of chunk.split('\n')) { const v = ln.trim(); if (v) out.push(v, depth); }
  };
  while ((m = re.exec(s))) {
    if (++tick % 256 === 0) { out.pos = re.lastIndex; yield; }
    const tok = m[0];
    if (tok[0] !== '<' || tok.startsWith('<!') || tok.startsWith('<?')) { text(tok, open.length); continue; }
    const name = /^<\/?\s*([^\s/>]+)/.exec(tok)?.[1]?.toLowerCase() ?? '';
    if (tok[1] === '/') {
      let at = open.length - 1;
      while (at >= 0 && open[at]?.name !== name) at--;
      if (at < 0) { out.push(tok, open.length); continue; }
      const opener = open[at]!;
      open.length = at;
      out.closes[opener.line] = out.push(tok, at);
      continue;
    }
    const line = out.push(tok, open.length);
    if (tok.endsWith('/>') || (html && VOID_TAGS.has(name))) continue;
    open.push({ line, name });
    if (html && (name === 'script' || name === 'style')) {
      lower ??= s.toLowerCase();
      const end = lower.indexOf(`</${name}`, re.lastIndex);
      const stop = end < 0 ? s.length : end;
      for (const ln of s.slice(re.lastIndex, stop).split('\n')) if (ln.trim()) out.push(ln.replace(/\s+$/, ''), open.length);
      re.lastIndex = stop;
    }
  }
  return true;
}

function* textLines(s: string, out: LineStore): Generator<void, boolean> {
  let at = 0, tick = 0;
  while (at <= s.length) {
    if (++tick % 2048 === 0) { out.pos = at; yield; }
    const nl = s.indexOf('\n', at);
    const end = nl < 0 ? s.length : nl;
    out.push(s.charCodeAt(end - 1) === 13 ? s.slice(at, end - 1) : s.slice(at, end), 0);
    if (nl < 0) break;
    at = nl + 1;
  }
  return true;
}

/** Base64 to bytes in slices of 1 MiB of text, so a large body never blocks the page. */
function* fromBase64(b64: string, sink: (u: Uint8Array) => void): Generator<void, boolean> {
  const clean = b64.replace(/\s+/g, '');
  const total = Math.floor((clean.length * 3) / 4) - (clean.endsWith('==') ? 2 : clean.endsWith('=') ? 1 : 0);
  const outBytes = new Uint8Array(Math.max(0, total));
  let w = 0;
  const step = 1 << 20;
  for (let off = 0; off < clean.length; off += step) {
    let bin: string;
    try { bin = atob(clean.slice(off, off + step)); } catch { return false; }
    for (let k = 0; k < bin.length; k++) outBytes[w++] = bin.charCodeAt(k);
    yield;
  }
  sink(w === outBytes.length ? outBytes : outBytes.subarray(0, w));
  return true;
}

/** Text to UTF-8 bytes in slices of 256 Ki characters, never splitting a surrogate pair. */
function* toUTF8(s: string, sink: (u: Uint8Array) => void): Generator<void, boolean> {
  const enc = new TextEncoder();
  const parts: Uint8Array[] = [];
  let total = 0;
  for (let off = 0; off < s.length;) {
    let end = Math.min(s.length, off + (1 << 18));
    const c = s.charCodeAt(end - 1);
    if (end < s.length && c >= 0xd800 && c <= 0xdbff) end -= 1;
    const u = enc.encode(s.slice(off, end));
    parts.push(u);
    total += u.length;
    off = end;
    yield;
  }
  const out = new Uint8Array(total);
  let w = 0;
  for (const p of parts) { out.set(p, w); w += p.length; }
  sink(out);
  return true;
}

const ctOf = (ct: string): string => (ct.split(';')[0] ?? '').trim().toLowerCase();
const isJSONct = (ct: string): boolean => /(^|\/|\+)json$/.test(ctOf(ct)) || ctOf(ct) === 'application/json';
const isMarkupCt = (ct: string): 'html' | 'xml' | '' => { const c = ctOf(ct); return c.includes('html') ? 'html' : c.includes('xml') || c.endsWith('+xml') ? 'xml' : ''; };
const isFormCt = (ct: string): 'url' | 'multipart' | '' => { const c = ctOf(ct); return c === 'application/x-www-form-urlencoded' ? 'url' : c.startsWith('multipart/') ? 'multipart' : ''; };

interface FormField { name: string; value: string; meta: string }

function formFields(kind: 'url' | 'multipart', ct: string, text: string): FormField[] {
  if (kind === 'url') return Array.from(new URLSearchParams(text.trim())).map(([name, value]) => ({ name, value, meta: '' }));
  const boundary = /boundary="?([^";]+)"?/i.exec(ct)?.[1];
  if (!boundary) return [];
  const out: FormField[] = [];
  for (const part of text.split(`--${boundary}`)) {
    if (!part.trim() || part.startsWith('--')) continue;
    const split = part.indexOf('\r\n\r\n') >= 0 ? part.indexOf('\r\n\r\n') : part.indexOf('\n\n');
    if (split < 0) continue;
    const head = part.slice(0, split), value = part.slice(split).replace(/^\r?\n\r?\n/, '').replace(/\r?\n$/, '');
    const disp = /content-disposition:[^\n]*/i.exec(head)?.[0] ?? '';
    const name = /\bname="([^"]*)"/i.exec(disp)?.[1] ?? '';
    const file = /\bfilename="([^"]*)"/i.exec(disp)?.[1];
    const type = /content-type:\s*([^\r\n]*)/i.exec(head)?.[1]?.trim() ?? '';
    const binary = /[\x00-\x08\x0e-\x1f]/.test(value.slice(0, 2048));
    out.push({ name, value: binary ? '' : value, meta: [file !== undefined ? tf('file {name}', { name: file }) : '', type, binary || file !== undefined ? bytes(value.length) : ''].filter(Boolean).join(' · ') });
  }
  return out;
}

const SYNTAX = /("(?:[^"\\]|\\.)*")(\s*:)?|(-?\d[\d.eE+-]*)|\b(true|false|null)\b/g;

/** One body, shown in the mode that suits it (Pretty, Raw, Hex, Image or Form) and switchable, with a find bar. The box
 *  has a fixed height from the start; every text mode is a virtual list of fixed-height rows patched in place, built a
 *  slice of about 8 ms at a time, so a body of megabytes paints its first rows at once and never blocks the page.
 *  Bodies are data and are never translated. */
export class BodyViewer {
  readonly el = el('<div class="fn-bv" translate="no"></div>');
  private readonly modeBar = el('<div class="cv-seg fn-bv-modes" role="tablist"></div>');
  private readonly modeBtns = new Map<ViewMode, HTMLButtonElement>();
  private readonly findBox = el<HTMLInputElement>('<input class="cv-input fn-bv-find" type="search" spellcheck="false" autocomplete="off">');
  private readonly findCount = el('<span class="fn-bv-count" aria-live="polite"></span>');
  private readonly prevBtn = el<HTMLButtonElement>(`<button type="button" class="cv-icon-btn fn-bv-step">${icon('chevronUp', 14)}</button>`);
  private readonly nextBtn = el<HTMLButtonElement>(`<button type="button" class="cv-icon-btn fn-bv-step">${icon('chevronDown', 14)}</button>`);
  private readonly copyBtn = el<HTMLButtonElement>(`<button type="button" class="cv-icon-btn fn-bv-step">${icon('copy', 14)}</button>`);
  private readonly info = el('<p class="fn-bv-info"></p>');
  private readonly pane = el('<div class="fn-bv-pane"></div>');
  private readonly scroller = el('<div class="fn-bv-scroll" tabindex="0"></div>');
  private readonly spacer = el('<div class="fn-bv-spacer"></div>');
  private readonly rowsBox = el('<div class="fn-bv-rows"></div>');
  private readonly imageBox = el('<div class="fn-bv-image" hidden><p class="fn-bv-dims"></p><img alt=""></div>');
  private readonly formBox = el('<div class="fn-bv-form" hidden><table class="fn-bv-table"><tbody></tbody></table></div>');
  private readonly empty = el('<p class="fn-bv-empty" hidden></p>');
  private rows: HTMLElement[] = [];
  private key = '';
  private body: ViewBody | null = null;
  private ct = '';
  private mode: ViewMode = 'raw';
  private avail = new Set<ViewMode>();
  private sources = new Map<ViewMode, Source>();
  private bytesCache: Uint8Array | null = null;
  private building = new Map<ViewMode, number>();
  private gen = 0;
  private collapsed = new Set<number>();
  private vis: Int32Array | null = null;
  private visDirty = false;
  private raf = 0;
  private matches: { line: number[]; col: number[]; len: number; more: boolean; cur: number; gen: number; needle: string; done: boolean } = { line: [], col: [], len: 0, more: false, cur: -1, gen: 0, needle: '', done: true };
  private findTimer = 0;
  private note = '';
  private progress = -1;
  private imgSize = '';

  constructor(private readonly label = 'Body') {
    for (const [m, name] of MODES) {
      const b = el<HTMLButtonElement>(`<button type="button" role="tab" class="fn-bv-mode" data-mode="${m}"></button>`);
      b.dataset.en = name;
      b.addEventListener('click', () => this.setMode(m));
      this.modeBar.append(b);
      this.modeBtns.set(m, b);
    }
    const find = el('<div class="fn-bv-findbar"></div>');
    find.append(this.findBox, this.findCount, this.prevBtn, this.nextBtn, this.copyBtn);
    const bar = el('<div class="fn-bv-bar"></div>');
    bar.append(this.modeBar, find);
    this.spacer.append(this.rowsBox);
    this.scroller.append(this.spacer);
    this.pane.append(this.scroller, this.imageBox, this.formBox, this.empty);
    this.el.append(bar, this.info, this.pane);
    this.scroller.addEventListener('scroll', () => this.queue(), { passive: true });
    this.rowsBox.addEventListener('click', (e) => {
      const f = (e.target as HTMLElement).closest<HTMLElement>('.fn-bv-fold');
      const line = Number(f?.dataset.line ?? -1);
      if (f && line >= 0) this.toggleFold(line);
    });
    this.findBox.addEventListener('input', () => { window.clearTimeout(this.findTimer); this.findTimer = window.setTimeout(() => this.find(), 150); });
    this.findBox.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); this.step(e.shiftKey ? -1 : 1); }
      if (e.key === 'Escape' && this.findBox.value) { e.preventDefault(); e.stopPropagation(); this.findBox.value = ''; this.find(); }
    });
    this.prevBtn.addEventListener('click', () => this.step(-1));
    this.nextBtn.addEventListener('click', () => this.step(1));
    this.copyBtn.addEventListener('click', () => { void copyText(this.allText()).then((ok) => { this.note = t(ok ? 'Copied' : 'Copy failed'); this.paintInfo(); window.setTimeout(() => { this.note = ''; this.paintInfo(); }, 1200); }); });
    q<HTMLImageElement>(this.imageBox, 'img').addEventListener('load', (e) => {
      const img = e.target as HTMLImageElement;
      this.imgSize = img.naturalWidth ? tf('{w} × {h} px', { w: String(img.naturalWidth), h: String(img.naturalHeight) }) : '';
      setText(q(this.imageBox, '.fn-bv-dims'), this.imgSize);
    });
    new ResizeObserver(() => { this.pool(); this.queue(); }).observe(this.scroller);
    this.words();
    onLanguage(() => this.words());
  }

  private words(): void {
    for (const b of this.modeBtns.values()) setText(b, t(b.dataset.en ?? ''));
    this.modeBar.setAttribute('aria-label', tf('{label} view', { label: t(this.label) }));
    this.findBox.placeholder = t('Find in body');
    this.findBox.setAttribute('aria-label', t('Find in body'));
    for (const [b, w] of [[this.prevBtn, 'Previous match (Shift+Enter)'], [this.nextBtn, 'Next match (Enter)'], [this.copyBtn, 'Copy the text shown']] as const) { setTitle(b, t(w)); b.setAttribute('aria-label', t(w)); }
    this.scroller.setAttribute('aria-label', t(this.label));
    this.paintInfo();
    this.paintCount();
  }

  /** Shows a body. The same key (a flow id and its version) keeps what is built, the mode and the scroll. */
  show(key: string, body: ViewBody | null, contentType: string, emptyText = 'No body was captured.'): void {
    if (key === this.key && body === this.body) return;
    const same = key === this.key;
    this.key = key;
    this.body = body;
    this.ct = contentType;
    if (same && body && this.sources.size) return;
    this.gen += 1;
    this.sources.clear();
    this.building.clear();
    this.bytesCache = null;
    this.collapsed.clear();
    this.vis = null;
    this.progress = -1;
    this.imgSize = '';
    this.note = '';
    this.matches = { line: [], col: [], len: 0, more: false, cur: -1, gen: this.gen, needle: '', done: true };
    const has = !!body && body.bytes > 0 && (body.text !== undefined || body.base64 !== undefined);
    this.avail = has ? this.available(body, contentType) : new Set();
    setText(this.empty, has ? '' : t(emptyText));
    this.setMode(has ? this.preferred() : 'raw', true);
    if (this.findBox.value) this.find();
  }

  private available(b: ViewBody, ct: string): Set<ViewMode> {
    const a = new Set<ViewMode>(['raw', 'hex']);
    const head = (b.text ?? '').slice(0, 64).trimStart();
    if (b.text !== undefined && (isJSONct(ct) || isMarkupCt(ct) || /^[[{]/.test(head) || /^<[!?a-zA-Z]/.test(head))) a.add('pretty');
    if (ctOf(ct).startsWith('image/') && (b.base64 !== undefined || ctOf(ct) === 'image/svg+xml')) a.add('image');
    if (isFormCt(ct)) a.add('form');
    return a;
  }

  private preferred(): ViewMode {
    const b = this.body;
    if (this.avail.has('image')) return 'image';
    if (this.avail.has('form')) return 'form';
    if (this.avail.has('pretty')) return 'pretty';
    return b?.text !== undefined ? 'raw' : 'hex';
  }

  /** The mode to show; one not offered for this body stays disabled. */
  setMode(m: ViewMode, fresh = false): void {
    if (!fresh && (!this.avail.has(m) || m === this.mode)) return;
    this.mode = m;
    for (const [k, b] of this.modeBtns) {
      const on = k === m && this.avail.size > 0;
      setClass(b, 'is-on', on);
      if (b.getAttribute('aria-selected') !== String(on)) b.setAttribute('aria-selected', String(on));
      setDisabled(b, !this.avail.has(k));
    }
    const has = this.avail.size > 0;
    const textMode = has && (m === 'pretty' || m === 'raw' || m === 'hex');
    this.scroller.style.visibility = textMode ? '' : 'hidden';
    this.imageBox.hidden = !(has && m === 'image');
    this.formBox.hidden = !(has && m === 'form');
    this.empty.hidden = has;
    setClass(this.el, 'is-hex', m === 'hex');
    setClass(this.el, 'is-plain', m !== 'pretty');
    setDisabled(this.findBox, !textMode);
    setDisabled(this.copyBtn, !textMode || m === 'hex');
    this.collapsed.clear();
    this.vis = null;
    this.scroller.scrollTop = 0;
    this.scroller.scrollLeft = 0;
    if (textMode) this.ensure(m);
    else if (has && m === 'image') this.paintImage();
    else if (has && m === 'form') this.paintForm();
    this.queue();
    this.paintInfo();
    if (this.findBox.value && textMode) this.find();
  }

  private text(): string {
    const b = this.body;
    if (!b) return '';
    if (b.text !== undefined) return b.text;
    const data = this.bytesCache;
    return data ? new TextDecoder('utf-8').decode(data) : '';
  }

  private withBytes(gen: number, then: (data: Uint8Array) => void): void {
    if (this.bytesCache) { then(this.bytesCache); return; }
    const b = this.body;
    if (!b) return;
    const it = b.text !== undefined ? toUTF8(b.text, (u) => { this.bytesCache = u; }) : fromBase64(b.base64 ?? '', (u) => { this.bytesCache = u; });
    this.run(it, gen, () => { if (this.bytesCache) then(this.bytesCache); });
  }

  private ensure(m: ViewMode): void {
    if (this.sources.has(m) || this.building.has(m)) return;
    const gen = this.gen;
    this.building.set(m, gen);
    if (m === 'hex') {
      this.withBytes(gen, (data) => { if (gen !== this.gen) return; this.sources.set('hex', new HexStore(data)); this.building.delete('hex'); this.queue(); this.paintInfo(); if (this.findBox.value && this.mode === 'hex') this.find(); });
      return;
    }
    const start = (src: string): void => {
      const kind = m === 'pretty' ? (isMarkupCt(this.ct) || (!isJSONct(this.ct) && src.trimStart().startsWith('<')) ? 'markup' : 'json') : 'text';
      const out = new LineStore(kind);
      this.sources.set(m, out);
      const it = kind === 'json' ? jsonLines(src, out) : kind === 'markup' ? markupLines(src, out, isMarkupCt(this.ct) !== 'xml') : textLines(src, out);
      const total = src.length;
      this.run(it, gen, (ok) => {
        out.done = true;
        this.building.delete(m);
        this.progress = -1;
        if (!ok && m === 'pretty') {
          out.failed = true;
          this.avail.delete('pretty');
          this.sources.delete('pretty');
          this.note = t('Not valid JSON: shown as text.');
          if (this.mode === 'pretty') this.setMode('raw', true);
          return;
        }
        this.visDirty = true;
        this.queue();
        this.paintInfo();
        if (this.findBox.value && this.mode === m) this.find();
      }, () => { this.progress = total ? Math.min(99, Math.floor((out.pos / total) * 100)) : -1; this.visDirty = true; this.queue(); this.paintInfo(); });
    };
    if (this.body?.text !== undefined) start(this.body.text);
    else this.withBytes(gen, () => { if (gen === this.gen) start(this.text()); });
  }

  /** Runs a builder in slices of about 8 ms; a newer body (gen) stops it. */
  private run(it: Generator<void, boolean>, gen: number, done: (ok: boolean) => void, progress?: () => void): void {
    const step = (): void => {
      if (gen !== this.gen) return;
      const until = performance.now() + SLICE_MS;
      let r: IteratorResult<void, boolean>;
      do { r = it.next(); } while (!r.done && performance.now() < until);
      if (r.done) { done(r.value); return; }
      progress?.();
      window.setTimeout(step, 0);
    };
    step();
  }

  private source(): Source | null {
    return this.mode === 'pretty' || this.mode === 'raw' || this.mode === 'hex' ? this.sources.get(this.mode) ?? null : null;
  }

  private visibleCount(src: Source): number {
    if (this.visDirty) { this.visDirty = false; this.rebuildVis(src); }
    return this.vis ? this.vis.length : src.count;
  }

  private rebuildVis(src: Source): void {
    if (!this.collapsed.size || !(src instanceof LineStore)) { this.vis = null; return; }
    const out = new Int32Array(src.count);
    let n = 0;
    for (let i = 0; i < src.count; i++) {
      out[n++] = i;
      if (this.collapsed.has(i) && src.closes[i]! > i) i = src.closes[i]!;
    }
    this.vis = out.subarray(0, n);
  }

  private toggleFold(line: number): void {
    const src = this.source();
    if (!(src instanceof LineStore) || !(src.closes[line]! > line)) return;
    if (this.collapsed.has(line)) this.collapsed.delete(line); else this.collapsed.add(line);
    this.visDirty = true;
    this.queue();
  }

  private pool(): void {
    const want = Math.ceil((this.scroller.clientHeight || 300) / LINE_H) + 2;
    while (this.rows.length < want) {
      const r = el(`<div class="fn-bv-row"><span class="fn-bv-ln"></span><span class="fn-bv-fold" role="button">${icon('chevronDown', 12)}</span><span class="fn-bv-code"></span></div>`);
      this.rowsBox.append(r);
      this.rows.push(r);
    }
  }

  private queue(): void {
    if (this.raf) return;
    this.raf = requestAnimationFrame(() => { this.raf = 0; this.render(); });
  }

  private render(): void {
    const src = this.source();
    if (!this.rows.length) this.pool();
    const count = src ? this.visibleCount(src) : 0;
    const total = count * LINE_H;
    const h = `${Math.min(total, MAX_SPACER)}px`;
    if (this.spacer.style.height !== h) this.spacer.style.height = h;
    const w = src ? `calc(${src.cols + 12}ch + 40px)` : '0px';
    if (this.spacer.style.width !== w) this.spacer.style.width = w;
    const view = this.scroller.clientHeight;
    const top = this.scroller.scrollTop;
    let first: number, y: number;
    if (total <= MAX_SPACER) { first = Math.floor(top / LINE_H); y = first * LINE_H; }
    else { const span = Math.max(1, MAX_SPACER - view); first = Math.round((Math.min(top, span) / span) * Math.max(0, count - Math.floor(view / LINE_H))); y = top; }
    const ty = `translateY(${y}px)`;
    if (this.rowsBox.style.transform !== ty) this.rowsBox.style.transform = ty;
    const cur = this.matches.cur >= 0 ? this.matches.line[this.matches.cur] : -1;
    this.rows.forEach((row, k) => {
      const vi = first + k;
      const line = !src || vi >= count ? -1 : this.vis ? this.vis[vi]! : vi;
      this.paintRow(row, src, line, cur ?? -1);
    });
  }

  private paintRow(row: HTMLElement, src: Source | null, line: number, cur: number): void {
    const key = line < 0 || !src ? '' : `${this.gen}:${this.mode}:${line}:${this.collapsed.has(line) ? 1 : 0}:${this.matches.gen}:${line === cur ? 1 : 0}:${src instanceof LineStore ? src.texts[line]?.length : 0}:${src instanceof LineStore ? src.closes[line] : 0}`;
    if (row.dataset.k === key) return;
    row.dataset.k = key;
    const [ln, fold, code] = Array.from(row.children) as HTMLElement[];
    if (!ln || !fold || !code) return;
    if (line < 0 || !src) { row.style.visibility = 'hidden'; return; }
    row.style.visibility = '';
    const store = src instanceof LineStore ? src : null;
    setText(ln, store && store.nums[line] ? String(store.nums[line]) : '');
    const canFold = !!store && store.closes[line]! > line && this.mode === 'pretty';
    const folded = canFold && this.collapsed.has(line);
    fold.dataset.line = canFold ? String(line) : '';
    setClass(fold, 'is-on', canFold);
    setClass(fold, 'is-folded', folded);
    let text = src.text(line);
    if (folded && store) text += ` … ${store.text(store.closes[line]!).trim()}`;
    const indent = store ? '  '.repeat(store.depths[line] ?? 0) : '';
    setClass(row, 'is-match', this.mode === 'hex' && this.hasMatch(line));
    setClass(row, 'is-current', line === cur);
    const marks = this.mode !== 'hex' ? this.marksIn(line) : [];
    if (!marks.length && (this.mode !== 'pretty' || store?.kind !== 'json' || text.length > 400)) {
      if (code.childElementCount || code.textContent !== indent + text) code.textContent = indent + text;
      return;
    }
    const frag = document.createDocumentFragment();
    frag.append(indent);
    if (marks.length) {
      let at = 0;
      for (const [c, idx] of marks) {
        if (c > at) frag.append(text.slice(at, c));
        const m = document.createElement('mark');
        if (idx === this.matches.cur) m.className = 'is-current';
        m.textContent = text.slice(c, c + this.matches.len);
        frag.append(m);
        at = c + this.matches.len;
      }
      frag.append(text.slice(at));
    } else {
      let at = 0;
      SYNTAX.lastIndex = 0;
      for (let m = SYNTAX.exec(text); m; m = SYNTAX.exec(text)) {
        if (m.index > at) frag.append(text.slice(at, m.index));
        const span = document.createElement('span');
        span.className = m[1] ? (m[2] ? 'fn-j-key' : 'fn-j-str') : m[3] ? 'fn-j-num' : 'fn-j-lit';
        span.textContent = m[1] ?? m[0];
        frag.append(span);
        if (m[2]) frag.append(m[2]);
        at = m.index + m[0].length;
      }
      frag.append(text.slice(at));
    }
    code.replaceChildren(frag);
  }

  private firstMatchAt(line: number): number {
    const L = this.matches.line;
    let lo = 0, hi = L.length;
    while (lo < hi) { const mid = (lo + hi) >> 1; if (L[mid]! < line) lo = mid + 1; else hi = mid; }
    return lo;
  }

  private hasMatch(line: number): boolean { const i = this.firstMatchAt(line); return this.matches.line[i] === line; }

  private marksIn(line: number): Array<[number, number]> {
    const out: Array<[number, number]> = [];
    if (!this.matches.len) return out;
    for (let i = this.firstMatchAt(line); i < this.matches.line.length && this.matches.line[i] === line; i++) out.push([this.matches.col[i]!, i]);
    return out;
  }

  /** Finds the text in the whole body (case-insensitive; in Hex, the bytes of the text), a slice at a time. */
  private find(): void {
    const needle = this.findBox.value;
    const gen = this.matches.gen + 1;
    this.matches = { line: [], col: [], len: needle.length, more: false, cur: -1, gen, needle, done: !needle };
    this.paintCount();
    this.queue();
    const src = this.source();
    if (!needle || !src) return;
    const m = this.matches;
    const add = (line: number, col: number): boolean => { if (m.line.length >= MAX_MATCHES) { m.more = true; return false; } m.line.push(line); m.col.push(col); return true; };
    const it = (function* (): Generator<void, boolean> {
      if (src instanceof HexStore) {
        const pat = new TextEncoder().encode(needle), data = src.data;
        let at = data.indexOf(pat[0] ?? 0), tick = 0;
        while (at >= 0) {
          if (++tick % 4096 === 0) yield;
          let k = 1;
          while (k < pat.length && data[at + k] === pat[k]) k++;
          if (k === pat.length && !add(Math.floor(at / 16), at % 16)) break;
          at = data.indexOf(pat[0] ?? 0, at + 1);
        }
        return true;
      }
      const low = needle.toLowerCase();
      for (let i = 0; i < src.count; i++) {
        if (i % 2048 === 0) yield;
        const text = src.text(i).toLowerCase();
        let c = text.indexOf(low);
        while (c >= 0) { if (!add(i, c)) return true; c = text.indexOf(low, c + low.length); }
      }
      return true;
    })();
    const gen0 = this.gen;
    this.run(it, gen0, () => {
      if (m !== this.matches) return;
      m.done = true;
      if (m.line.length && m.cur < 0) { m.cur = 0; this.reveal(m.line[0]!); }
      this.paintCount();
      this.queue();
    }, () => { if (m === this.matches) this.paintCount(); });
  }

  private step(by: number): void {
    const m = this.matches;
    if (!m.line.length) return;
    m.cur = (m.cur + by + m.line.length) % m.line.length;
    this.matches = { ...m, gen: m.gen + 1 };
    this.reveal(m.line[m.cur]!);
    this.paintCount();
    this.queue();
  }

  /** Unfolds what hides the line and scrolls it to the middle of the box. */
  private reveal(line: number): void {
    const src = this.source();
    if (src instanceof LineStore && this.collapsed.size) {
      for (const o of Array.from(this.collapsed)) if (o < line && src.closes[o]! >= line) this.collapsed.delete(o);
      this.visDirty = true;
      this.visibleCount(src);
    }
    let vi = line;
    if (this.vis) { let lo = 0, hi = this.vis.length; while (lo < hi) { const mid = (lo + hi) >> 1; if (this.vis[mid]! < line) lo = mid + 1; else hi = mid; } vi = lo; }
    const count = src ? (this.vis ? this.vis.length : src.count) : 0;
    const total = count * LINE_H, view = this.scroller.clientHeight;
    const target = total <= MAX_SPACER ? vi * LINE_H - view / 2 + LINE_H / 2 : (vi / Math.max(1, count - Math.floor(view / LINE_H))) * Math.max(1, MAX_SPACER - view);
    this.scroller.scrollTop = Math.max(0, target);
    const col = this.matches.col[this.matches.cur] ?? 0;
    if (this.mode !== 'hex') {
      const code = this.rows[0]?.querySelector<HTMLElement>('.fn-bv-code');
      const ch = 7.2, x = (code?.offsetLeft ?? 0) + (col + (src instanceof LineStore ? (src.depths[line] ?? 0) * 2 : 0)) * ch;
      if (x < this.scroller.scrollLeft || x > this.scroller.scrollLeft + this.scroller.clientWidth - 80) this.scroller.scrollLeft = Math.max(0, x - 80);
    }
  }

  private paintCount(): void {
    const m = this.matches;
    const n = m.line.length;
    const fmt = (x: number): string => x.toLocaleString(language());
    const text = !m.needle ? '' : !m.done && !n ? t('Finding...') : !n ? t('No match') : m.more ? tf('{current} of {count}+', { current: fmt(m.cur + 1), count: fmt(n) }) : tf('{current} of {count}', { current: fmt(m.cur + 1), count: fmt(n) });
    setText(this.findCount, text);
    setDisabled(this.prevBtn, n === 0);
    setDisabled(this.nextBtn, n === 0);
  }

  private paintInfo(): void {
    const b = this.body;
    const parts: string[] = [];
    if (b && b.bytes > 0) {
      parts.push(bytes(b.bytes));
      if (b.truncated) parts.push(t('cut at the body limit'));
      if (b.encoding) parts.push(tf(b.decoded ? '{encoding}, decoded' : '{encoding}, not decoded', { encoding: b.encoding }));
      const src = this.source();
      if (src instanceof LineStore && src.done) parts.push(tn(src.nums[src.count - 1] || src.count, '{n} line', '{n} lines'));
      if (this.building.has(this.mode)) parts.push(this.progress >= 0 ? tf('formatting {pct}%', { pct: String(this.progress) }) : t('formatting'));
    }
    if (this.note) parts.push(this.note);
    setText(this.info, parts.join(' · '));
    setTitle(this.info, parts.join(' · '));
  }

  private paintImage(): void {
    const b = this.body;
    const img = q<HTMLImageElement>(this.imageBox, 'img');
    img.alt = t(this.label);
    if (!b) return;
    const type = ctOf(this.ct) || 'image/png';
    const src = b.base64 !== undefined ? `data:${type};base64,${b.base64}` : `data:${type};charset=utf-8,${encodeURIComponent(b.text ?? '')}`;
    setText(q(this.imageBox, '.fn-bv-dims'), this.imgSize);
    if (img.getAttribute('src') !== src) void swapMedia(img, src).catch(() => { this.note = t('The image could not be decoded.'); this.paintInfo(); });
  }

  private paintForm(): void {
    const kind = isFormCt(this.ct);
    if (!kind) return;
    const fill = (text: string): void => {
      const fields = formFields(kind, this.ct, text);
      patchList(q(this.formBox, 'tbody'), fields.map((f, i) => ({ f, k: `${i}:${f.name}` })), {
        key: (x) => x.k,
        create: ({ f }) => el(`<tr><th class="mono" scope="row"></th><td class="mono"><span class="fn-bv-fv"></span><span class="fn-bv-fm"></span></td></tr>`),
        update: () => undefined,
      });
      Array.from(q(this.formBox, 'tbody').children).forEach((tr, i) => {
        const f = fields[i];
        if (!f) return;
        setText(tr.querySelector('th'), f.name);
        setText(tr.querySelector('.fn-bv-fv'), f.value);
        setText(tr.querySelector('.fn-bv-fm'), f.meta);
      });
      this.note = fields.length ? '' : t('No fields could be read.');
      this.paintInfo();
    };
    if (this.body?.text !== undefined) fill(this.body.text);
    else this.withBytes(this.gen, (data) => { let s = ''; for (let i = 0; i < data.length; i += 8192) s += String.fromCharCode(...data.subarray(i, i + 8192)); fill(s); });
  }

  private allText(): string {
    const src = this.source();
    if (!src) return '';
    if (this.mode === 'raw') return this.text();
    if (src instanceof LineStore) { const out: string[] = []; for (let i = 0; i < src.count; i++) out.push(src.nums[i] ? `\n${'  '.repeat(src.depths[i] ?? 0)}${src.text(i)}` : src.text(i)); return out.join('').replace(/^\n/, ''); }
    return '';
  }
}
