import { TrafficError } from '../api-traffic';
import type { BodyOp, HeaderOp, QueryOp, Rule, RuleAction, RuleActionType, RuleMatch } from '../types-traffic';
import { Dialog } from './dialogs';
import { el, q, setClass, setDisabled, setHidden, setText } from './dom';
import { icon } from './icons';
import { onLanguage, t } from './i18n';
import { labelText, tf } from './panels/controls';

/** Saves the edited rule into the list; resolves the server's refusal, or null once it is kept. */
export type SaveRule = (rule: Rule) => Promise<TrafficError | Error | null>;

/** The action types, with the words a person reads. */
export const ACTION_LABELS: ReadonlyArray<readonly [RuleActionType, string]> = [
  ['rewrite', 'Rewrite'],
  ['map_remote', 'Map to another server'],
  ['map_local', 'Answer from a local file or folder'],
  ['block', 'Block'],
  ['breakpoint', 'Breakpoint'],
];

const tr = (english: string): HTMLElement => { const p = el('<p class="cv-note"></p>'); setText(p, t(english)); onLanguage(() => setText(p, t(english))); return p; };
const head = (english: string): HTMLElement => { const p = el('<p class="cv-note-head"></p>'); setText(p, t(english)); onLanguage(() => setText(p, t(english))); return p; };

function fieldRow(label: string, control: HTMLElement): HTMLElement {
  const row = el('<label class="cv-field"><span></span></label>');
  labelText(q(row, 'span'), label);
  row.appendChild(control);
  return row;
}

function input(kind: 'text' | 'number', placeholder = '', ltr = true): HTMLInputElement {
  const i = el<HTMLInputElement>(`<input class="cv-input" type="${kind}" spellcheck="false" autocomplete="off">`);
  if (ltr) i.dir = 'ltr';
  if (placeholder) i.placeholder = placeholder;
  return i;
}

function select(options: ReadonlyArray<readonly [string, string]>): HTMLSelectElement {
  const s = el<HTMLSelectElement>('<select class="cv-input"></select>');
  for (const [v, text] of options) s.add(new Option(t(text), v));
  onLanguage(() => options.forEach(([, text], i) => { const o = s.options[i]; if (o) o.text = t(text); }));
  return s;
}

function check(label: string): { row: HTMLElement; box: HTMLInputElement } {
  const row = el('<label class="cv-field cv-check"><span></span><input type="checkbox"></label>');
  labelText(q(row, 'span'), label);
  return { row, box: q<HTMLInputElement>(row, 'input') };
}

interface OpRow { el: HTMLElement; kind: HTMLSelectElement; a: HTMLInputElement; b: HTMLInputElement; regex: HTMLInputElement | null }

/** One list of changes in a rewrite (header, query or body changes): rows of a choice and two fields, a row added by
 *  Add and taken away by its own button. */
class OpList {
  readonly el = el('<div class="fn-oplist"><div class="fn-oplist-head"><span class="fn-oplist-title"></span><button type="button" class="cv-btn cv-btn-ghost fn-oplist-add"></button></div><div class="fn-oplist-rows"></div></div>');
  readonly rows: OpRow[] = [];
  private readonly box: HTMLElement;

  constructor(private readonly title: string, addLabel: string, private readonly kinds: ReadonlyArray<readonly [string, string]>, private readonly holders: readonly [string, string], private readonly withRegex: boolean, private readonly changed: () => void) {
    this.box = q(this.el, '.fn-oplist-rows');
    const paint = (): void => { setText(q(this.el, '.fn-oplist-title'), t(title)); setText(q(this.el, '.fn-oplist-add'), t(addLabel)); };
    paint();
    onLanguage(paint);
    q(this.el, '.fn-oplist-add').addEventListener('click', () => { const r = this.addRow(); r.a.focus(); this.changed(); });
  }

  private addRow(): OpRow {
    const node = el(`<div class="fn-op"></div>`);
    const kind = select(this.kinds);
    kind.setAttribute('aria-label', t(this.title));
    const a = input('text', t(this.holders[0])), b = input('text', t(this.holders[1]));
    a.setAttribute('aria-label', t(this.holders[0]));
    b.setAttribute('aria-label', t(this.holders[1]));
    node.append(kind, a, b);
    let regex: HTMLInputElement | null = null;
    if (this.withRegex) {
      const lab = el('<label class="fn-op-regex"><input type="checkbox"><span></span></label>');
      regex = q<HTMLInputElement>(lab, 'input');
      setText(q(lab, 'span'), t('Regex'));
      lab.title = t('Find is a regular expression (Go syntax); $1 in Replace is its first group');
      node.append(lab);
    }
    const del = el<HTMLButtonElement>(`<button type="button" class="cv-icon-btn fn-op-del">${icon('close', 14)}</button>`);
    del.title = t('Remove this change');
    del.setAttribute('aria-label', del.title);
    node.append(del);
    const row: OpRow = { el: node, kind, a, b, regex };
    const sync = (): void => { if (!this.withRegex) setDisabled(b, kind.value === 'remove'); };
    kind.addEventListener('change', sync);
    del.addEventListener('click', () => { const i = this.rows.indexOf(row); if (i >= 0) this.rows.splice(i, 1); node.remove(); this.changed(); });
    this.box.append(node);
    this.rows.push(row);
    sync();
    return row;
  }

  set(items: ReadonlyArray<{ kind: string; a: string; b: string; regex?: boolean }>): void {
    while (this.rows.length > items.length) this.rows.pop()?.el.remove();
    while (this.rows.length < items.length) this.addRow();
    items.forEach((it, i) => {
      const r = this.rows[i];
      if (!r) return;
      r.kind.value = it.kind;
      r.a.value = it.a;
      r.b.value = it.b;
      if (r.regex) r.regex.checked = !!it.regex;
      r.kind.dispatchEvent(new Event('change'));
    });
  }

  get(): Array<{ kind: string; a: string; b: string; regex: boolean }> {
    return this.rows.map((r) => ({ kind: r.kind.value, a: r.a.value, b: r.b.value, regex: !!r.regex?.checked }));
  }

  /** The control the server named for row `i` ("op", "name", "value", "target", "find", "replace"). */
  field(i: number, sub: string): HTMLElement | null {
    const r = this.rows[i];
    if (!r) return null;
    if (sub === 'op' || sub === 'target') return r.kind;
    if (sub === 'value' || sub === 'replace') return r.b;
    return r.a;
  }
}

const HEADER_OPS: ReadonlyArray<readonly [string, string]> = [['set', 'Set'], ['add', 'Add'], ['remove', 'Remove']];
const QUERY_OPS: ReadonlyArray<readonly [string, string]> = [['set', 'Set'], ['remove', 'Remove']];
const BODY_TARGETS: ReadonlyArray<readonly [string, string]> = [['response', 'Response body'], ['request', 'Request body']];

/** Add Rule and Edit Rule: the match fields, the action's type and the fields of that type, built once. The fields of
 *  every type sit in one box of fixed height, so choosing another type never changes the dialog's size. A refusal from
 *  the server marks the field it names, says why beside it, and repeats it in the reserved line above the buttons. */
export class RuleDialog extends Dialog {
  private readonly name = input('text', '', false);
  private readonly enabled = check('Rule on');
  private readonly scheme = select([['', 'Any scheme'], ['http', 'http'], ['https', 'https']]);
  private readonly host = input('text', '*.example.com');
  private readonly port = input('number', '');
  private readonly path = input('text', '/api/*');
  private readonly method = input('text', 'GET,POST');
  private readonly query = input('text', 'debug&lang=en*');
  private readonly type = select(ACTION_LABELS);
  private readonly panes = new Map<RuleActionType, HTMLElement>();
  private readonly rwHost = input('text', 'staging.example.com:8443');
  private readonly rwPath = input('text', '/v2/items');
  private readonly rwStatus = input('number');
  private readonly reqHeaders: OpList;
  private readonly resHeaders: OpList;
  private readonly queryOps: OpList;
  private readonly bodyOps: OpList;
  private readonly to = input('text', 'https://staging.example.com:8443/v2');
  private readonly keepHost = check('Keep the original Host header');
  private readonly local = input('text', '/Users/[user]/mock/api');
  private readonly drop = check('Close the connection without an answer');
  private readonly bpRequest = check('Pause the request');
  private readonly bpResponse = check('Pause the response');
  private readonly actionBox = el('<div class="fn-rd-action"></div>');
  private readonly error = el('<p class="fn-rd-error" role="alert"></p>');
  private readonly bubble = el('<div class="fn-rd-bubble" role="presentation" hidden></div>');
  private readonly saveBtn: HTMLButtonElement;
  private marked: HTMLElement | null = null;
  private save: SaveRule | null = null;
  private base: Rule | null = null;
  private busy = false;

  constructor() {
    super('Rule', 680);
    const frame = q<HTMLElement>(this.scrim, '.cv-dialog');
    frame.classList.add('fn-rule-dlg');
    this.port.min = '1';
    this.port.max = '65535';
    this.rwStatus.min = '100';
    this.rwStatus.max = '999';
    this.method.setAttribute('aria-label', t('Methods'));
    const changed = (): void => this.unmark();
    this.reqHeaders = new OpList('Request headers', 'Add header change', HEADER_OPS, ['Header name', 'Value'], false, changed);
    this.resHeaders = new OpList('Response headers', 'Add header change', HEADER_OPS, ['Header name', 'Value'], false, changed);
    this.queryOps = new OpList('Query parameters', 'Add parameter change', QUERY_OPS, ['Parameter name', 'Value'], false, changed);
    this.bodyOps = new OpList('Body', 'Add find and replace', BODY_TARGETS, ['Find', 'Replace with'], true, changed);
    const cols = (...c: HTMLElement[]): HTMLElement => { const d = el('<div class="fn-rd-cols"></div>'); d.append(...c); return d; };
    const pane = (type: RuleActionType, ...c: HTMLElement[]): HTMLElement => { const p = el('<div class="fn-rd-pane"></div>'); p.dataset.type = type; p.append(...c); this.panes.set(type, p); this.actionBox.append(p); return p; };
    pane('rewrite',
      tr('Every change below applies, in list order, to each request the match takes. Leave a field empty to keep it as it is.'),
      cols(fieldRow('Host', this.rwHost), fieldRow('Status', this.rwStatus)),
      fieldRow('Path', this.rwPath),
      this.reqHeaders.el, this.resHeaders.el, this.queryOps.el, this.bodyOps.el,
      tr('Find and replace works on text bodies (text, JSON, XML, JavaScript, forms, SVG) up to 8 MiB, on the decoded form; a changed body is sent without its Content-Encoding. A body that is larger, binary or does not decode passes unchanged, and the flow says so.'));
    pane('map_remote',
      fieldRow('Send to', this.to),
      this.keepHost.row,
      tr('The request goes to this server instead. A path in the address takes the place of the part of the request path the match names before its first *; the rest of the path and the query are kept.'),
      tr('Off, the Host header names the new server; on, it keeps the original name, for a server that answers several.'));
    pane('map_local',
      fieldRow('File or folder', this.local),
      tr('A file answers every request the rule matches.'),
      tr('A folder maps the matched path under it: the part of the path after the match\'s fixed beginning (the path pattern up to its first *) is looked up inside the folder, and index.html answers for a folder.'),
      tr('The path must be absolute and exist on this computer. FormNET\'s data folder, and anything inside it, is refused. A path with .. or one that leads outside the folder is answered with 403, a missing file with 404.'));
    pane('block',
      this.drop.row,
      tr('Off, FormNET answers 403 with a short text saying which rule blocked the request. On, it closes the connection without an answer, as a network that drops it would.'));
    pane('breakpoint',
      this.bpRequest.row, this.bpResponse.row,
      tr('A paused exchange waits in the Breakpoints panel, which opens by itself, for you to continue it, edit it or abort it. After the time set in Preferences > Rules it goes on as it was, so a forgotten breakpoint never holds a program for ever.'));
    this.body.append(
      cols(fieldRow('Name', this.name), this.enabled.row),
      head('Match'),
      cols(fieldRow('Scheme', this.scheme), fieldRow('Port', this.port)),
      fieldRow('Host', this.host),
      fieldRow('Path', this.path),
      cols(fieldRow('Methods', this.method), fieldRow('Query', this.query)),
      tr('Host is required: a name, or a pattern such as *.example.com, which also matches example.com. Path is a pattern on the path alone, where * crosses slashes. Methods may be a list such as GET,POST. Query lists name or name=pattern pairs joined by &, and each must be present.'),
      head('Action'),
      fieldRow('Type', this.type),
      this.actionBox,
      this.error);
    frame.append(this.bubble);
    this.type.addEventListener('change', () => { this.showPane(); this.unmark(); });
    this.button('Cancel', 'ghost', () => this.close());
    this.saveBtn = this.button('Save', 'primary', () => void this.submit());
    this.body.addEventListener('input', () => this.unmark());
    this.body.addEventListener('scroll', () => this.hideBubble(), { passive: true, capture: true });
    this.body.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && e.target instanceof HTMLInputElement && e.target.type !== 'checkbox') { e.preventDefault(); void this.submit(); }
    });
    this.onClose = () => { this.hideBubble(); this.save = null; };
  }

  /** Opens the editor on `rule` (a new rule when null, or when `fresh`: a new one filled from a template); `save` keeps it. */
  edit(rule: Rule | null, save: SaveRule, fresh = !rule): void {
    this.setTitle(fresh ? 'Add Rule' : 'Edit Rule');
    this.base = rule;
    this.save = save;
    const r: Rule = rule ?? { id: '', name: '', enabled: true, match: { host: '' }, action: { type: 'rewrite' } };
    const m = r.match, a = r.action;
    this.name.value = r.name;
    this.enabled.box.checked = r.enabled;
    this.scheme.value = m.scheme ?? '';
    this.host.value = m.host ?? '';
    this.port.value = m.port ? String(m.port) : '';
    this.path.value = m.path ?? '';
    this.method.value = m.method ?? '';
    this.query.value = m.query ?? '';
    this.type.value = a.type;
    this.rwHost.value = a.type === 'rewrite' ? a.host ?? '' : '';
    this.rwPath.value = a.type === 'rewrite' ? a.path ?? '' : '';
    this.rwStatus.value = a.type === 'rewrite' && a.status ? String(a.status) : '';
    const ops = (list: Array<HeaderOp | QueryOp> | undefined) => (list ?? []).map((o) => ({ kind: o.op, a: o.name, b: o.value ?? '' }));
    this.reqHeaders.set(ops(a.request_headers));
    this.resHeaders.set(ops(a.response_headers));
    this.queryOps.set(ops(a.query));
    this.bodyOps.set((a.body ?? []).map((b) => ({ kind: b.target ?? 'response', a: b.find, b: b.replace, regex: b.regex })));
    this.to.value = a.to ?? '';
    this.keepHost.box.checked = !!a.keep_host;
    this.local.value = a.local_path ?? '';
    this.drop.box.checked = !!a.drop;
    this.bpRequest.box.checked = a.type === 'breakpoint' ? !!a.request : true;
    this.bpResponse.box.checked = !!a.response;
    this.showPane();
    this.unmark();
    this.busy = false;
    setDisabled(this.saveBtn, false);
    this.actionBox.scrollTop = 0;
    this.body.scrollTop = 0;
    this.open(this.name);
  }

  private showPane(): void {
    for (const [type, p] of this.panes) setHidden(p, type !== this.type.value);
    this.actionBox.scrollTop = 0;
  }

  /** The rule as the fields hold it, with only the chosen type's fields. */
  private read(): Rule {
    const match: RuleMatch = { host: this.host.value.trim() };
    if (this.scheme.value) match.scheme = this.scheme.value;
    if (this.port.value.trim()) match.port = Number(this.port.value);
    if (this.path.value.trim()) match.path = this.path.value.trim();
    if (this.method.value.trim()) match.method = this.method.value.trim();
    if (this.query.value.trim()) match.query = this.query.value.trim();
    const type = this.type.value as RuleActionType;
    const action: RuleAction = { type };
    if (type === 'rewrite') {
      const hops = (l: OpList): HeaderOp[] => l.get().map((o) => ({ op: o.kind as HeaderOp['op'], name: o.a.trim(), ...(o.kind !== 'remove' ? { value: o.b } : {}) }));
      const req = hops(this.reqHeaders), res = hops(this.resHeaders);
      const qs: QueryOp[] = this.queryOps.get().map((o) => ({ op: o.kind as QueryOp['op'], name: o.a.trim(), ...(o.kind !== 'remove' ? { value: o.b } : {}) }));
      const body: BodyOp[] = this.bodyOps.get().map((o) => ({ target: o.kind as BodyOp['target'], find: o.a, replace: o.b, ...(o.regex ? { regex: true } : {}) }));
      if (req.length) action.request_headers = req;
      if (res.length) action.response_headers = res;
      if (qs.length) action.query = qs;
      if (body.length) action.body = body;
      if (this.rwHost.value.trim()) action.host = this.rwHost.value.trim();
      if (this.rwPath.value.trim()) action.path = this.rwPath.value.trim();
      if (this.rwStatus.value.trim()) action.status = Number(this.rwStatus.value);
    } else if (type === 'map_remote') {
      action.to = this.to.value.trim();
      if (this.keepHost.box.checked) action.keep_host = true;
    } else if (type === 'map_local') {
      action.local_path = this.local.value.trim();
    } else if (type === 'block') {
      if (this.drop.box.checked) action.drop = true;
    } else if (type === 'breakpoint') {
      if (this.bpRequest.box.checked) action.request = true;
      if (this.bpResponse.box.checked) action.response = true;
    }
    return { id: this.base?.id ?? '', name: this.name.value.trim(), enabled: this.enabled.box.checked, match, action };
  }

  private async submit(): Promise<void> {
    if (this.busy || !this.save) return;
    this.busy = true;
    setDisabled(this.saveBtn, true);
    this.unmark();
    const err = await this.save(this.read()).catch((e: Error) => e);
    this.busy = false;
    setDisabled(this.saveBtn, false);
    if (!err) { this.close(); return; }
    this.showError(err);
  }

  /** The control a server field path names, past the "rules[i]." prefix. */
  private controlFor(path: string): HTMLElement | null {
    const direct: Record<string, HTMLElement> = {
      id: this.name, name: this.name, match: this.host, 'match.scheme': this.scheme, 'match.host': this.host, 'match.port': this.port, 'match.path': this.path, 'match.method': this.method, 'match.query': this.query,
      action: this.type, 'action.type': this.type, 'action.host': this.rwHost, 'action.path': this.rwPath, 'action.status': this.rwStatus, 'action.to': this.to, 'action.local_path': this.local,
    };
    if (direct[path]) return direct[path];
    const m = /^action\.(request_headers|response_headers|query|body)\[(\d+)\](?:\.(\w+))?$/.exec(path);
    if (!m) return null;
    const list = { request_headers: this.reqHeaders, response_headers: this.resHeaders, query: this.queryOps, body: this.bodyOps }[m[1] as 'body'];
    return list.field(Number(m[2]), m[3] ?? '');
  }

  private showError(err: Error): void {
    const field = err instanceof TrafficError ? err.field : '';
    const own = /^rules\[\d+\]\.(.+)$/.exec(field)?.[1] ?? '';
    const control = own ? this.controlFor(own) : null;
    if (!control || !(err instanceof TrafficError)) { setText(this.error, err.message); return; }
    const label = control.closest('.cv-field')?.querySelector('span')?.textContent ?? control.getAttribute('aria-label') ?? own;
    setText(this.error, tf('{field}: {reason}', { field: label, reason: err.reason }));
    this.marked = control;
    setClass(control, 'is-invalid', true);
    control.setAttribute('aria-invalid', 'true');
    control.scrollIntoView({ block: 'nearest' });
    control.focus();
    requestAnimationFrame(() => this.placeBubble(control, err.reason));
  }

  private placeBubble(control: HTMLElement, text: string): void {
    if (this.marked !== control) return;
    const frame = q<HTMLElement>(this.scrim, '.cv-dialog');
    const F = frame.getBoundingClientRect(), R = control.getBoundingClientRect();
    setText(this.bubble, text);
    this.bubble.style.left = `${Math.round(R.left - F.left)}px`;
    this.bubble.style.top = `${Math.round(R.bottom - F.top + 4)}px`;
    this.bubble.style.maxWidth = `${Math.max(160, Math.round(F.right - R.left - 16))}px`;
    setHidden(this.bubble, false);
  }

  private hideBubble(): void { setHidden(this.bubble, true); }

  private unmark(): void {
    this.hideBubble();
    if (this.marked) { setClass(this.marked, 'is-invalid', false); this.marked.removeAttribute('aria-invalid'); this.marked = null; }
    setText(this.error, '');
  }
}

let shared: RuleDialog | null = null;

/** The one rule editor, built the first time it is asked for. */
export function ruleDialog(): RuleDialog {
  shared ??= new RuleDialog();
  return shared;
}
