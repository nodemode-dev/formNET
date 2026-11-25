import { trafficApi } from '../../api-traffic';
import { breakpointWatch } from '../../breakpoint-watch';
import type { Store } from '../../state';
import type { Status } from '../../types';
import { trafficOf, type Rule, type RuleAction, type RuleActionType, type RuleMatch, type RulesStatus } from '../../types-traffic';
import { confirmAction } from '../dialogs';
import { el, patchList, q, setClass, setDisabled, setText, setTitle, setValue } from '../dom';
import { icon } from '../icons';
import { onLanguage, t, tn } from '../i18n';
import { ruleDialog } from '../rule-dialog';
import { tf, toggleOf, toggleRow, type Row } from './controls';
import { labelled, markField, rereadStatus, SayLine, trNote } from './traffic-common';

/** What a rule matches, as a line of data: methods, scheme, host, port, path and query. */
export function matchText(m: RuleMatch): string {
  return `${m.method ? `${m.method.toUpperCase()} ` : ''}${m.scheme ? `${m.scheme}://` : ''}${m.host || '*'}${m.port ? `:${m.port}` : ''}${m.path ?? ''}${m.query ? `?${m.query}` : ''}`;
}

/** What a rule does, as a whole sentence in the current language. */
export function actionText(a: RuleAction): string {
  switch (a.type) {
    case 'rewrite': {
      const n = (a.request_headers?.length ?? 0) + (a.response_headers?.length ?? 0) + (a.query?.length ?? 0) + (a.body?.length ?? 0) + (a.host ? 1 : 0) + (a.path ? 1 : 0) + (a.status ? 1 : 0);
      return tn(n, 'Rewrite with {n} change', 'Rewrite with {n} changes');
    }
    case 'map_remote': return tf('Map to {to}', { to: a.to ?? '' });
    case 'map_local': return tf('Answer from {path}', { path: a.local_path ?? '' });
    case 'block': return t(a.drop ? 'Drop the connection' : 'Block with 403');
    case 'breakpoint': return t(a.request && a.response ? 'Pause the request and the response' : a.response ? 'Pause the response' : 'Pause the request');
    default: return '';
  }
}

const ROW_HTML = `<div class="fn-rule" role="listitem" draggable="true">` +
  `<span class="fn-rule-grip" aria-hidden="true">${icon('grip', 14)}</span>` +
  `<span class="fn-rule-switch"></span>` +
  `<div class="fn-rule-text"><span class="fn-rule-name"></span><span class="fn-rule-sum"><span class="fn-rule-act"></span><span class="fn-rule-match mono" translate="no" dir="ltr"></span></span><span class="fn-rule-note"></span></div>` +
  `<div class="fn-rule-tools">` +
  `<button type="button" class="cv-icon-btn" data-act="up">${icon('chevronUp', 14)}</button>` +
  `<button type="button" class="cv-icon-btn" data-act="down">${icon('chevronDown', 14)}</button>` +
  `<button type="button" class="cv-icon-btn" data-act="edit">${icon('edit', 14)}</button>` +
  `<button type="button" class="cv-icon-btn" data-act="duplicate">${icon('duplicate', 14)}</button>` +
  `<button type="button" class="cv-icon-btn" data-act="delete">${icon('delete', 14)}</button>` +
  `</div></div>`;

/** The list's filter: every rule, or one kind (the Tools menu's Rewrite, Map Local, Map Remote, Block List and
 *  Breakpoint Rules open Preferences on one). */
export const RULE_FILTERS: ReadonlyArray<readonly [RuleActionType | '', string]> = [['', 'All'], ['rewrite', 'Rewrite'], ['map_local', 'Map Local'], ['map_remote', 'Map Remote'], ['block', 'Block List'], ['breakpoint', 'Breakpoints']];

const TOOL_TIPS: Record<string, string> = { up: 'Move up', down: 'Move down', edit: 'Edit...', duplicate: 'Duplicate', delete: 'Delete...' };

/** Preferences > Rules > Rules: the switch that turns every rule on or off, the HTTPS note, and the ordered list. Each
 *  row has its own switch, a drag grip and buttons to move, edit, duplicate and delete; Add Rule opens the editor. Every
 *  change sends the whole list (PUT /api/rules) and the row shows the server's answer; nothing waits for a Save. Rows
 *  are keyed by rule id and keep a fixed height, so a list that changes never moves what is around it. */
export class RulesSection {
  readonly el = el('<div class="cv-panel-section fn-rules"></div>');
  private readonly master: Row<boolean>;
  private readonly masterBox: HTMLInputElement;
  private readonly https = el('<p class="cv-note fn-rules-https" role="note"></p>');
  private readonly list = el('<div class="fn-rules-list" role="list"></div>');
  private readonly empty = el('<p class="cv-note fn-rules-empty"></p>');
  private readonly add = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly say = new SayLine();
  private data: RulesStatus | null = null;
  private loadedAt = 0;
  private lastSummary = '';
  private chain: Promise<unknown> = Promise.resolve();
  private dragId = '';
  private only: RuleActionType | null = null;
  private readonly seg = el('<div class="fn-rules-seg" role="radiogroup"></div>');

  constructor(private readonly store: Store) {
    this.master = toggleRow('Apply rules', (on) => this.setEnabled(on));
    this.masterBox = q<HTMLInputElement>(this.master.el, 'input');
    for (const [type] of RULE_FILTERS) {
      const b = el<HTMLButtonElement>('<button type="button" class="fn-rules-segbtn" role="radio"></button>');
      b.dataset.type = type;
      b.addEventListener('click', () => this.showType((type || null) as RuleActionType | null));
      this.seg.append(b);
    }
    const wrap = el('<div class="fn-rules-box"></div>');
    wrap.append(this.list, this.empty);
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.add);
    const paint = (): void => {
      setText(this.add, t('Add Rule...'));
      RULE_FILTERS.forEach(([, label], i) => setText(this.seg.children[i], t(label)));
      this.seg.setAttribute('aria-label', t('Show rules of one kind'));
      if (this.data) this.render(); else this.paintFilter();
    };
    paint();
    onLanguage(paint);
    this.add.addEventListener('click', () => this.openEditor(null));
    this.list.addEventListener('click', (e) => this.onClick(e));
    this.list.addEventListener('dblclick', (e) => { const id = this.rowId(e.target); if (id && !(e.target as HTMLElement).closest('button, input, .cv-toggle')) this.openEditor(id); });
    this.list.addEventListener('change', (e) => {
      const box = e.target as HTMLInputElement;
      const id = this.rowId(box);
      if (id && box.type === 'checkbox') this.change((list) => list.map((r) => (r.id === id ? { ...r, enabled: box.checked } : r)));
    });
    this.bindDrag();
    this.el.append(this.master.el, this.https, this.seg, wrap, bar, this.say.el,
      trNote('Rules run top to bottom. Rewrites add up, the first Map to another server wins, the first Block or Answer from a local file ends the walk, and the first breakpoint of each stage pauses. A rule never applies to FormNET\'s own addresses, and the list is kept in rules.json in the data folder.'));
    setDisabled(this.masterBox, true);
    setDisabled(this.add, true);
    this.refresh(store.get().status);
  }

  /** Called with every status the store gets: the list is read again when the status's summary differs from what is
   *  shown, or it was last read more than two seconds ago and the section is in the page. */
  refresh(status: Status | null): void {
    const s = trafficOf(status).rules;
    const summary = s ? `${s.enabled}|${s.count}` : '';
    const stale = Date.now() - this.loadedAt > 2000;
    if (!this.data || summary !== this.lastSummary || (stale && this.el.isConnected)) this.load();
    this.lastSummary = summary;
  }

  /** Shows only the rules of `type` (every rule when null); Add Rule then starts a rule of that type. */
  showType(type: RuleActionType | null): void {
    if (this.only === type) return;
    this.only = type;
    if (this.data) this.render(); else this.paintFilter();
    this.list.parentElement?.scrollTo({ top: 0 });
  }

  private paintFilter(): void {
    for (const b of Array.from(this.seg.children) as HTMLElement[]) {
      const on = (b.dataset.type || null) === this.only;
      setClass(b, 'is-on', on);
      if (b.getAttribute('aria-checked') !== String(on)) b.setAttribute('aria-checked', String(on));
    }
    setText(this.empty, this.only ? t('No rules of this kind yet. Add Rule makes one.') : t('No rules yet. A rule rewrites, redirects, answers, blocks or pauses the requests it matches, in list order.'));
  }

  load(): void {
    this.loadedAt = Date.now();
    this.chain = this.chain.then(() => trafficApi.rules().then((st) => this.apply(st)).catch((err: unknown) => this.say.fail(err)));
  }

  private apply(st: RulesStatus): void {
    this.data = st;
    this.loadedAt = Date.now();
    this.render();
  }

  private render(): void {
    const st = this.data;
    if (!st) return;
    const rules = (st.rules ?? []).filter((r) => !this.only || r.action.type === this.only);
    this.master.set(st.enabled);
    setDisabled(this.masterBox, false);
    setDisabled(this.add, false);
    setText(this.https, st.https_note);
    setClass(this.https, 'is-warn', !st.intercept);
    setClass(this.list, 'is-off', !st.enabled);
    this.paintFilter();
    setClass(this.empty, 'is-shown', rules.length === 0);
    const notes = st.notes ?? {};
    patchList(this.list, rules.map((r, i) => ({ r, i, n: rules.length, note: notes[r.id] ?? '' })), {
      key: (x) => x.r.id,
      create: (x) => { const row = this.buildRow(); this.fillRow(row, x.r, x.i, x.n, x.note); return row; },
      update: (row, x) => this.fillRow(row, x.r, x.i, x.n, x.note),
    });
  }

  private buildRow(): HTMLElement {
    const row = el(ROW_HTML);
    const box = el<HTMLInputElement>('<input type="checkbox">');
    q(row, '.fn-rule-switch').append(toggleOf(box));
    for (const b of Array.from(row.querySelectorAll<HTMLButtonElement>('[data-act]'))) {
      const tip = t(TOOL_TIPS[b.dataset.act ?? ''] ?? '');
      b.title = tip.replace(/\.\.\.$/, '');
      b.setAttribute('aria-label', b.title);
    }
    return row;
  }

  private fillRow(row: HTMLElement, r: Rule, i: number, n: number, note: string): void {
    const box = q<HTMLInputElement>(row, '.fn-rule-switch input');
    if (box.checked !== r.enabled) box.checked = r.enabled;
    box.setAttribute('aria-label', tf('{name} on', { name: r.name }));
    setClass(row, 'is-off', !r.enabled);
    const name = q(row, '.fn-rule-name');
    setText(name, r.name);
    setTitle(name, r.name);
    const act = actionText(r.action), match = matchText(r.match);
    setText(q(row, '.fn-rule-act'), act);
    setText(q(row, '.fn-rule-match'), match);
    setTitle(q(row, '.fn-rule-sum'), `${act} · ${match}`);
    const noteEl = q(row, '.fn-rule-note');
    setText(noteEl, note);
    setTitle(noteEl, note);
    setDisabled(q<HTMLButtonElement>(row, '[data-act="up"]'), i === 0);
    setDisabled(q<HTMLButtonElement>(row, '[data-act="down"]'), i === n - 1);
  }

  private rowId(target: EventTarget | null): string {
    return (target as HTMLElement | null)?.closest<HTMLElement>('.fn-rule')?.dataset.key ?? '';
  }

  private onClick(e: Event): void {
    const b = (e.target as HTMLElement).closest<HTMLButtonElement>('button[data-act]');
    const id = this.rowId(b);
    if (!b || !id || !this.data) return;
    const rules = this.data.rules ?? [];
    const at = rules.findIndex((r) => r.id === id);
    if (at < 0) return;
    const act = b.dataset.act;
    if (act === 'up' || act === 'down') {
      const shown = rules.filter((r) => !this.only || r.action.type === this.only);
      const at2 = shown.findIndex((r) => r.id === id), other = shown[act === 'up' ? at2 - 1 : at2 + 1];
      if (!other) return;
      this.change((list) => {
        const next = list.filter((r) => r.id !== id), moving = list[at];
        const to = next.findIndex((r) => r.id === other.id);
        next.splice(act === 'up' ? to : to + 1, 0, moving);
        return next;
      });
    } else if (act === 'edit') {
      this.openEditor(id);
    } else if (act === 'duplicate') {
      const src = rules[at];
      const copy: Rule = { ...structuredClone(src), id: '', name: tf('{name} (copy)', { name: src.name }) };
      this.change((list) => { const next = [...list]; next.splice(at + 1, 0, copy); return next; }, 'Duplicated.');
    } else if (act === 'delete') {
      const r = rules[at];
      void confirmAction('Delete the rule?', tf('The rule "{name}" is taken off the list. Requests it matched go on as if it had never been there.', { name: r.name }), 'Delete', true).then((ok) => {
        if (ok) this.change((list) => list.filter((x) => x.id !== id), 'Deleted.');
      });
    }
  }

  private bindDrag(): void {
    const clear = (): void => { for (const r of Array.from(this.list.querySelectorAll('.is-drop-before, .is-drop-after'))) r.classList.remove('is-drop-before', 'is-drop-after'); };
    this.list.addEventListener('dragstart', (e) => {
      const id = this.rowId(e.target);
      if (!id || !e.dataTransfer) return;
      this.dragId = id;
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', id);
      (e.target as HTMLElement).closest('.fn-rule')?.classList.add('is-dragging');
    });
    this.list.addEventListener('dragover', (e) => {
      const row = (e.target as HTMLElement).closest<HTMLElement>('.fn-rule');
      if (!this.dragId || !row) return;
      e.preventDefault();
      const r = row.getBoundingClientRect(), after = e.clientY > r.top + r.height / 2;
      clear();
      if (row.dataset.key !== this.dragId) row.classList.add(after ? 'is-drop-after' : 'is-drop-before');
    });
    this.list.addEventListener('dragleave', (e) => { if (!this.list.contains(e.relatedTarget as Node | null)) clear(); });
    this.list.addEventListener('drop', (e) => {
      const row = (e.target as HTMLElement).closest<HTMLElement>('.fn-rule');
      const from = this.dragId;
      e.preventDefault();
      clear();
      if (!row || !from || row.dataset.key === from) return;
      const r = row.getBoundingClientRect(), after = e.clientY > r.top + r.height / 2, target = row.dataset.key ?? '';
      this.change((list) => {
        const moving = list.find((x) => x.id === from);
        if (!moving) return list;
        const rest = list.filter((x) => x.id !== from);
        const at = rest.findIndex((x) => x.id === target);
        rest.splice(after ? at + 1 : at, 0, moving);
        return rest;
      });
    });
    this.list.addEventListener('dragend', () => { this.dragId = ''; clear(); for (const r of Array.from(this.list.querySelectorAll('.is-dragging'))) r.classList.remove('is-dragging'); });
  }

  private setEnabled(on: boolean): void {
    this.chain = this.chain.then(() => trafficApi.setRulesEnabled(on)
      .then((st) => { this.apply(st); this.say.ok(on ? 'Rules apply.' : 'Rules are off: every request passes unchanged.'); rereadStatus(this.store); })
      .catch((err: unknown) => { this.say.fail(err); this.render(); }));
  }

  /** Sends the list `edit` makes of the current one; the rows show it at once and the server's answer after. */
  private change(edit: (list: Rule[]) => Rule[], done = 'Saved.'): Promise<Error | null> {
    const run = async (): Promise<Error | null> => {
      const cur = this.data;
      if (!cur) return null;
      const next = edit(cur.rules ?? []);
      this.data = { ...cur, rules: next };
      this.render();
      try {
        this.apply(await trafficApi.putRules(next));
        this.say.ok(done);
        rereadStatus(this.store);
        return null;
      } catch (err) {
        this.data = cur;
        this.render();
        this.say.fail(err);
        return err as Error;
      }
    };
    const p = this.chain.then(run, run);
    this.chain = p;
    return p;
  }

  /** The editor for rule `id`, or for a new rule at the end of the list. A refusal stays in the editor, not the list. */
  private openEditor(id: string | null): void {
    const rules = this.data?.rules ?? [];
    const at = id ? rules.findIndex((r) => r.id === id) : -1;
    const start: Rule | null = at >= 0 ? rules[at] : this.only ? { id: '', name: '', enabled: true, match: { host: '' }, action: { type: this.only, ...(this.only === 'breakpoint' ? { request: true } : {}) } } : null;
    ruleDialog().edit(start, (rule) => {
      const run = async (): Promise<Error | null> => {
        const cur = this.data;
        if (!cur) return new Error(t('The rules have not been read from the server yet.'));
        const list = [...(cur.rules ?? [])];
        const i = id ? list.findIndex((r) => r.id === id) : -1;
        if (i >= 0) list[i] = rule; else list.push(rule);
        try {
          this.apply(await trafficApi.putRules(list));
          this.say.ok('Saved.');
          rereadStatus(this.store);
          return null;
        } catch (err) {
          return err as Error;
        }
      };
      const p = this.chain.then(run, run);
      this.chain = p;
      return p;
    }, at < 0);
  }
}

/** Opens Add Rule filled from a flow (its scheme, host, port, path and method), set to pause the request; Save appends
 *  the rule to the end of the list. For the flow detail's "Make a Rule..." button. */
export function ruleFromFlow(store: Store, f: { scheme: string; host: string; port: number; path: string; method: string }): void {
  const port = f.port && !((f.scheme === 'http' && f.port === 80) || (f.scheme === 'https' && f.port === 443)) ? f.port : undefined;
  const template: Rule = {
    id: '', name: `${f.method} ${f.host}${f.path}`.slice(0, 200), enabled: true,
    match: { scheme: f.scheme || undefined, host: f.host, port, path: f.path || undefined, method: f.method || undefined },
    action: { type: 'breakpoint', request: true },
  };
  ruleDialog().edit(template, async (rule) => {
    try {
      const cur = await trafficApi.rules();
      await trafficApi.putRules([...(cur.rules ?? []), rule]);
      rereadStatus(store);
      return null;
    } catch (err) {
      return err as Error;
    }
  }, true);
}

/** Preferences > Rules > Breakpoints: how long an exchange waits at a breakpoint before it goes on by itself, and how
 *  many may wait at once. Each field is sent when it is changed. */
export class BreakpointSettingsSection {
  readonly el = el('<div class="cv-panel-section fn-bp-settings"></div>');
  private readonly timeout = el<HTMLInputElement>('<input class="cv-input" type="number" min="1" max="3600" step="1">');
  private readonly max = el<HTMLInputElement>('<input class="cv-input" type="number" min="1" max="200" step="1">');
  private readonly say = new SayLine();

  constructor() {
    const send = (which: 'timeout_seconds' | 'max_paused', box: HTMLInputElement): void => {
      const v = box.valueAsNumber;
      if (!Number.isFinite(v)) return;
      trafficApi.setBreakpoints({ [which]: Math.round(v) })
        .then((st) => { breakpointWatch.timeoutSeconds = st.timeout_seconds; breakpointWatch.maxPaused = st.max_paused; this.paint(); this.say.ok('Saved.'); })
        .catch((err: unknown) => { this.say.fail(err); markField(err, { timeout_seconds: this.timeout, max_paused: this.max }); this.load(); });
    };
    this.timeout.addEventListener('change', () => send('timeout_seconds', this.timeout));
    this.max.addEventListener('change', () => send('max_paused', this.max));
    for (const box of [this.timeout, this.max]) box.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); box.blur(); } });
    const cols = el('<div class="cv-prefs-cols"></div>');
    cols.append(labelled('Goes on by itself after (seconds)', this.timeout), labelled('Paused at once, at most', this.max));
    this.el.append(cols, this.say.el,
      trNote('A paused exchange goes on as it was once this time has passed, so a forgotten breakpoint never holds a program for ever. When as many exchanges as allowed are already paused, the next match passes with a note on its flow. Both are kept when FormNET restarts.'));
    this.paint();
  }

  refresh(_status: Status | null): void {
    if (!this.el.isConnected) return;
    this.load();
  }

  private load(): void {
    trafficApi.breakpoints().then((st) => { breakpointWatch.timeoutSeconds = st.timeout_seconds; breakpointWatch.maxPaused = st.max_paused; this.paint(); }).catch(() => undefined);
  }

  private paint(): void {
    setValue(this.timeout, String(breakpointWatch.timeoutSeconds));
    setValue(this.max, String(breakpointWatch.maxPaused));
    this.timeout.setAttribute('aria-label', t('Goes on by itself after (seconds)'));
    this.max.setAttribute('aria-label', t('Paused at once, at most'));
  }
}
