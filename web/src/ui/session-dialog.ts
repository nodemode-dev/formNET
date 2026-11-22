import { api, SessionError } from '../api.ts';
import { dataApi } from '../api-data.ts';
import { bytes, dateTime } from '../format.ts';
import type { Store } from '../state.ts';
import { dataOf, type SavedSession, type SessionStatus } from '../types-data.ts';
import { confirmAction, Dialog } from './dialogs';
import { el, patchList, q, setClass, setDisabled, setHidden, setText, setTitle } from './dom';
import { onLanguage, t, tn } from './i18n';
import { icon } from './icons';
import { tf } from './panels/controls';
import { toast } from './toast';

const reread = (store: Store): void => { api.status().then((status) => store.update({ status })).catch(() => undefined); };
const message = (err: unknown): string => (err instanceof Error ? err.message : String(err));

function stamp(): string {
  const d = new Date(), p = (n: number): string => String(n).padStart(2, '0');
  return `session-${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}`;
}

/** A sentence in the current language, re-set when the language changes. */
function said(english: string, cls = 'cv-note'): HTMLElement {
  const p = el(`<p class="${cls}"></p>`);
  setText(p, t(english));
  onLanguage(() => setText(p, t(english)));
  return p;
}

/** File > Save Session: a name, the folder it goes to, and what a session file holds. A name already taken is refused
 *  (a save never replaces a file) and the dialog asks for another. */
class SaveSessionDialog extends Dialog {
  private readonly name = el<HTMLInputElement>('<input class="cv-input" type="text" spellcheck="false" autocomplete="off" maxlength="100" dir="ltr">');
  private readonly folder = el<HTMLInputElement>('<input class="cv-input" type="text" readonly spellcheck="false" dir="ltr">');
  private readonly error = el('<p class="fn-dlg-error" role="alert"></p>');
  private readonly save: HTMLButtonElement;
  private busy = false;

  constructor() {
    super('Save Session', 480);
    q<HTMLElement>(this.scrim, '.cv-dialog').classList.add('fn-data-dlg');
    this.field('Name', this.name);
    this.field('Folder', this.folder);
    this.body.append(said('A session file holds every flow as it was captured, with its bodies and WebSocket messages, and the passwords, tokens and cookies the requests carried. Keep it private and share it only with someone you would give those to.', 'cv-note fn-dlg-warn'), this.error);
    this.button('Cancel', 'ghost', () => this.close());
    this.save = this.button('Save', 'primary', () => this.submit());
    this.name.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); this.submit(); } });
    this.name.addEventListener('input', () => { setText(this.error, ''); setClass(this.name, 'is-invalid', false); });
  }

  ask(): void {
    setText(this.error, '');
    setClass(this.name, 'is-invalid', false);
    this.name.value = stamp();
    if (!this.folder.value) this.folder.value = t('the sessions folder in the data folder');
    dataApi.sessions().then((s) => { this.folder.value = s.folder; this.folder.title = s.folder; }).catch(() => undefined);
    this.open(this.name);
    this.name.select();
  }

  private submit(): void {
    const name = this.name.value.trim();
    if (this.busy || !name) return;
    this.busy = true;
    setDisabled(this.save, true);
    dataApi.saveSession(name)
      .then((r) => {
        this.close();
        toast(tn(r.flows, 'Saved {name} with {n} flow ({size}).', 'Saved {name} with {n} flows ({size}).').replace('{name}', r.name).replace('{size}', bytes(r.bytes)));
      })
      .catch((err: unknown) => {
        if (err instanceof SessionError) return;
        setText(this.error, message(err));
        setClass(this.name, 'is-invalid', true);
        this.name.focus();
        this.name.select();
      })
      .finally(() => { this.busy = false; setDisabled(this.save, false); });
  }
}

/** File > Open Session: the saved sessions (flows, size, date) or a file from anywhere. Opening replaces the recording,
 *  so it asks first while flows are kept. */
class OpenSessionDialog extends Dialog {
  private readonly folder = el('<p class="cv-note fn-dlg-path mono" dir="ltr"></p>');
  private readonly list = el('<div class="fn-sess-list" role="listbox"></div>');
  private readonly none = el('<p class="fn-sess-none"></p>');
  private readonly error = el('<p class="fn-dlg-error" role="alert"></p>');
  private readonly file = el<HTMLInputElement>('<input type="file" accept=".formnet,.json,.gz,application/gzip,application/json" hidden>');
  private readonly choose = el<HTMLButtonElement>(`<button type="button" class="cv-btn cv-btn-ghost">${icon('folderOpen', 14)}<span></span></button>`);
  private readonly openBtn: HTMLButtonElement;
  private chosen = '';
  private sessions: SavedSession[] = [];
  private busy = false;

  constructor(private readonly store: Store) {
    super('Open Session', 560);
    q<HTMLElement>(this.scrim, '.cv-dialog').classList.add('fn-data-dlg');
    const box = el('<div class="fn-sess-box"></div>');
    box.append(this.list, this.none);
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.choose, this.file);
    this.body.append(said('Opening a session replaces the recording with the flows in the file. Capture goes on into the same list.'), this.folder, box, bar, this.error);
    this.button('Cancel', 'ghost', () => this.close());
    this.openBtn = this.button('Open', 'primary', () => { if (this.chosen) void this.openNamed(this.chosen); });
    this.choose.addEventListener('click', () => this.file.click());
    this.file.addEventListener('change', () => { const f = this.file.files?.[0]; this.file.value = ''; if (f) void this.openFile(f); });
    this.list.addEventListener('click', (e) => { const name = (e.target as HTMLElement).closest<HTMLElement>('.fn-sess-row')?.dataset.name; if (name !== undefined) this.pick(name); });
    this.list.addEventListener('dblclick', (e) => { const name = (e.target as HTMLElement).closest<HTMLElement>('.fn-sess-row')?.dataset.name; if (name) void this.openNamed(name); });
    this.list.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && this.chosen) { e.preventDefault(); void this.openNamed(this.chosen); }
      if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return;
      e.preventDefault();
      const ok = this.sessions.filter((s) => !s.error), at = ok.findIndex((s) => s.name === this.chosen);
      const next = ok[Math.max(0, Math.min(ok.length - 1, at + (e.key === 'ArrowDown' ? 1 : -1)))];
      if (next) { this.pick(next.name); this.list.querySelector<HTMLElement>(`[data-key="${CSS.escape(next.name)}"]`)?.focus(); }
    });
    const words = (): void => { setText(q(this.choose, 'span:not(.icon)'), t('Choose a file...')); this.list.setAttribute('aria-label', t('Saved sessions')); this.paint(); };
    words();
    onLanguage(words);
  }

  ask(): void {
    setText(this.error, '');
    this.chosen = '';
    setText(this.none, t('Reading the saved sessions...'));
    this.paint();
    dataApi.sessions()
      .then((r) => {
        this.sessions = r.sessions ?? [];
        setText(this.folder, r.folder);
        setTitle(this.folder, r.folder);
        setText(this.none, this.sessions.length ? '' : t('No sessions saved in this folder yet. Choose a file to open one from elsewhere.'));
        this.paint();
      })
      .catch((err: unknown) => { setText(this.none, ''); setText(this.error, message(err)); });
    this.open(this.choose);
  }

  private pick(name: string): void {
    if (this.sessions.find((s) => s.name === name)?.error) return;
    this.chosen = name;
    this.paint();
  }

  private paint(): void {
    patchList(this.list, this.sessions, {
      key: (s) => s.name,
      create: () => el('<div class="fn-sess-row" role="option" tabindex="-1"><span class="fn-sess-name mono" dir="ltr"></span><span class="fn-sess-meta"></span></div>'),
      update: () => undefined,
    });
    for (const row of Array.from(this.list.children) as HTMLElement[]) {
      const s = this.sessions.find((x) => x.name === row.dataset.key);
      if (!s) continue;
      row.dataset.name = s.name;
      setText(q(row, '.fn-sess-name'), s.name);
      setText(q(row, '.fn-sess-meta'), s.error ? t('Not a readable FormNET session file.') : [tn(s.flows, '{n} flow', '{n} flows'), bytes(s.bytes), dateTime(s.saved), s.app_version ? tf('FormNET {version}', { version: s.app_version }) : ''].filter(Boolean).join(' · '));
      setTitle(row, s.path);
      setClass(row, 'is-bad', !!s.error);
      const on = s.name === this.chosen;
      setClass(row, 'is-selected', on);
      if (row.getAttribute('aria-selected') !== String(on)) row.setAttribute('aria-selected', String(on));
      row.tabIndex = on || (!this.chosen && row === this.list.firstElementChild) ? 0 : -1;
    }
    const vis = this.none.textContent ? '' : 'hidden';
    if (this.none.style.visibility !== vis) this.none.style.visibility = vis;
    setDisabled(this.openBtn, !this.chosen || this.busy);
    setDisabled(this.choose, this.busy);
  }

  /** Asks before the recording is replaced, while it holds flows. */
  private async replaceOk(name: string): Promise<boolean> {
    const n = this.store.get().flows.size;
    if (!n) return true;
    return confirmAction('Replace the recording?', tn(n, 'Opening {name} replaces the {n} flow kept now. Save a session first to keep it.', 'Opening {name} replaces the {n} flows kept now. Save a session first to keep them.').replace('{name}', name), t('Open and replace'), true, t('Cancel'));
  }

  private async openNamed(name: string): Promise<void> {
    if (this.busy || !(await this.replaceOk(name))) return;
    await this.run(dataApi.openSession(name), name);
  }

  private async openFile(f: File): Promise<void> {
    if (this.busy || !(await this.replaceOk(f.name))) return;
    await this.run(dataApi.openSessionFile(f), f.name);
  }

  private async run(p: ReturnType<typeof dataApi.openSession>, name: string): Promise<void> {
    this.busy = true;
    setText(this.error, '');
    this.paint();
    try {
      const r = await p;
      this.close();
      const lines = [tn(r.flows, 'Opened {name}: {n} flow.', 'Opened {name}: {n} flows.').replace('{name}', r.session.name || name)];
      if (r.dropped) lines.push(tn(r.dropped, '{n} older flow did not fit the limits and was left out.', '{n} older flows did not fit the limits and were left out.'));
      toast(lines.join(' '));
      reread(this.store);
    } catch (err) {
      if (!(err instanceof SessionError)) setText(this.error, message(err));
    } finally {
      this.busy = false;
      this.paint();
    }
  }
}

/** File > Import HAR: a HAR from FormNET, a browser or Charles, added to the recording (nothing is replaced). */
class ImportHarDialog extends Dialog {
  private readonly file = el<HTMLInputElement>('<input type="file" accept=".har,.json,application/json" hidden>');
  private readonly choose = el<HTMLButtonElement>(`<button type="button" class="cv-btn">${icon('upload', 14)}<span></span></button>`);
  private readonly result = el('<p class="fn-imp-result" role="status"></p>');
  private busy = false;

  constructor(private readonly store: Store) {
    super('Import HAR', 480);
    q<HTMLElement>(this.scrim, '.cv-dialog').classList.add('fn-data-dlg');
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.choose, this.file);
    this.body.append(said('The requests in the HAR are added to the recording, marked as from a session; nothing is replaced. Bodies longer than the body limit are cut, and the oldest flows go when the recording is full.'), bar, this.result);
    this.button('Close', 'primary', () => this.close());
    this.choose.addEventListener('click', () => this.file.click());
    this.file.addEventListener('change', () => { const f = this.file.files?.[0]; this.file.value = ''; if (f) this.importFile(f); });
    const words = (): void => setText(q(this.choose, 'span:not(.icon)'), t('Choose a HAR file...'));
    words();
    onLanguage(words);
  }

  ask(): void {
    setText(this.result, '');
    setClass(this.result, 'is-error', false);
    this.open(this.choose);
  }

  private importFile(f: File): void {
    if (this.busy) return;
    this.busy = true;
    setDisabled(this.choose, true);
    setClass(this.result, 'is-error', false);
    setText(this.result, tf('Importing {name}...', { name: f.name }));
    dataApi.importHAR(f)
      .then((r) => {
        const lines = [tn(r.imported, 'Imported {n} flow.', 'Imported {n} flows.')];
        if (r.skipped) lines.push(tn(r.skipped, '{n} entry could not be read and was skipped.', '{n} entries could not be read and were skipped.'));
        if (r.dropped) lines.push(tn(r.dropped, '{n} flow did not fit the limits and was left out.', '{n} flows did not fit the limits and were left out.'));
        if (r.messages) lines.push(tn(r.messages, '{n} WebSocket message came with them.', '{n} WebSocket messages came with them.'));
        lines.push(r.creator ? tf('Made by {creator}.', { creator: r.creator }) : t('The file does not say what made it.'));
        setText(this.result, lines.join(' '));
        reread(this.store);
      })
      .catch((err: unknown) => {
        if (err instanceof SessionError) return;
        setClass(this.result, 'is-error', true);
        setText(this.result, message(err));
      })
      .finally(() => { this.busy = false; setDisabled(this.choose, false); });
  }
}

let saveDlg: SaveSessionDialog | null = null;
let openDlg: OpenSessionDialog | null = null;
let importDlg: ImportHarDialog | null = null;

export function saveSession(): void { (saveDlg ??= new SaveSessionDialog()).ask(); }
export function openSession(store: Store): void { (openDlg ??= new OpenSessionDialog(store)).ask(); }
export function importHar(store: Store): void { (importDlg ??= new ImportHarDialog(store)).ask(); }

/** Closing a session clears the recording, so it asks first. */
export function closeSession(store: Store): void {
  const s = dataOf(store.get().status).session;
  void confirmAction('Close the session?', tf('Closing clears the recording: the flows opened from {name} and everything captured since. Save a session first to keep them.', { name: s?.name ?? '' }), t('Close and clear'), true, t('Cancel'))
    .then((ok) => {
      if (!ok) return;
      api.clearFlows().then(() => reread(store)).catch((err: Error) => toast(err.message, 'error'));
    });
}

/** The open session's name with Close, for the end of the options bar: it lies over the counts' fixed slot while a
 *  session is open (the status bar still reads the counts), so its coming and going moves nothing. */
export class SessionChip {
  readonly el = el(`<span class="fn-session" hidden><span class="fn-session-icon">${icon('folderOpen', 14)}</span><span class="fn-session-name"></span><button type="button" class="fn-session-close">${icon('close', 12)}</button></span>`);
  private last: SessionStatus | null = null;

  constructor(private readonly store: Store) {
    q(this.el, '.fn-session-close').addEventListener('click', () => closeSession(store));
    store.subscribe((s, prev) => { if (s.status !== prev.status) this.paint(); });
    onLanguage(() => { this.last = null; this.paint(); });
    this.paint();
  }

  private paint(): void {
    const s = dataOf(this.store.get().status).session ?? null;
    const open = !!s?.open;
    setHidden(this.el, !open);
    if (!s || !open || (this.last && JSON.stringify(this.last) === JSON.stringify(s))) return;
    this.last = s;
    setText(q(this.el, '.fn-session-name'), tf('Session: {name}', { name: s.name }));
    const lines = [tf('Opened {date} from {name}', { date: dateTime(s.opened ?? undefined), name: s.name }), tn(s.flows, '{n} flow opened', '{n} flows opened')];
    if (s.saved) lines.push(tf('Saved {date}', { date: dateTime(s.saved) }));
    if (s.app_version) lines.push(tf('Saved by FormNET {version}', { version: s.app_version }));
    if (s.dropped) lines.push(tn(s.dropped, '{n} flow left out', '{n} flows left out'));
    setTitle(this.el, lines.join('\n'));
    const close = q<HTMLButtonElement>(this.el, '.fn-session-close');
    setTitle(close, t('Close the session (clears the recording)'));
    close.setAttribute('aria-label', close.title);
  }
}
