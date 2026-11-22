import { api } from '../api';
import { bytes } from '../format';
import type { FlowDetail, Header, ResendEdit } from '../types';
import { Dialog } from './dialogs';
import { el, patchList, q, setDisabled, setText } from './dom';
import { icon } from './icons';
import { onLanguage, t } from './i18n';
import { tf } from './panels/controls';

type BodyKind = 'text' | 'binary' | 'none';

/** Edit and resend: the captured request's method, URL, headers (in their order) and body, editable, sent through
 *  POST /api/flows/{id}/resend with only the fields the person changed. Built once; each opening fills the same
 *  controls. The header list and the body keep fixed heights and scroll, so adding a header or a long error moves
 *  nothing. Errors from the server show in the dialog's own line. */
export class ResendDialog extends Dialog {
  private readonly method = el<HTMLInputElement>('<input class="cv-input mono" type="text" spellcheck="false" autocomplete="off" dir="ltr">');
  private readonly url = el<HTMLInputElement>('<input class="cv-input mono" type="text" spellcheck="false" autocomplete="off" dir="ltr">');
  private readonly list = el('<div class="cv-resend-headers" translate="no" data-forget></div>');
  private readonly addBtn = el<HTMLButtonElement>(`<button class="cv-btn cv-btn-ghost" type="button">${icon('plus', 14)}<span></span></button>`);
  private readonly headLabel = el('<p class="cv-note-head"></p>');
  private readonly bodyLabel = el('<p class="cv-note-head"></p>');
  private readonly bodyNote = el('<p class="cv-note cv-resend-bodynote"></p>');
  private readonly text = el<HTMLTextAreaElement>('<textarea class="cv-input mono cv-resend-body" rows="8" spellcheck="false" dir="ltr"></textarea>');
  private readonly error = el('<p class="cv-resend-error" role="alert"></p>');
  private readonly send: HTMLButtonElement;
  private flow: FlowDetail | null = null;
  private kind: BodyKind = 'none';
  private original = '';
  private fresh = 0;
  private sending = false;

  constructor(private readonly onSent: (id: string, edited: boolean) => void) {
    super('Edit and resend', 680);
    q<HTMLElement>(this.scrim, '.cv-dialog').classList.add('cv-resend-dlg');
    this.field('Method', this.method);
    this.field('URL', this.url);
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.addBtn);
    this.body.append(this.headLabel, this.list, bar, this.bodyLabel, this.bodyNote, this.text, this.error);
    this.button('Cancel', 'ghost', () => this.close());
    this.send = this.button('Send', 'primary', () => this.submit());
    this.addBtn.addEventListener('click', () => {
      const row = this.row(`n${this.fresh++}`, { name: '', value: '' });
      this.list.appendChild(row);
      q<HTMLInputElement>(row, 'input').focus();
      row.scrollIntoView({ block: 'nearest' });
    });
    this.list.addEventListener('click', (e) => {
      const b = (e.target as HTMLElement).closest<HTMLButtonElement>('.cv-resend-remove');
      const row = b?.closest<HTMLElement>('.cv-resend-row');
      if (!row) return;
      const next = row.nextElementSibling?.querySelector<HTMLInputElement>('input') ?? row.previousElementSibling?.querySelector<HTMLInputElement>('input') ?? this.addBtn;
      row.remove();
      next.focus();
      setText(this.error, '');
    });
    this.text.addEventListener('input', () => this.paintBodyNote());
    this.scrim.addEventListener('input', () => setText(this.error, ''));
    this.paintWords();
    onLanguage(() => this.paintWords());
  }

  private paintWords(): void {
    setText(this.headLabel, t('Headers'));
    setText(this.bodyLabel, t('Body'));
    setText(q(this.addBtn, 'span:not(.icon)'), t('Add header'));
    this.text.setAttribute('aria-label', t('Request body'));
    for (const row of Array.from(this.list.children)) this.label(row as HTMLElement);
    this.paintBodyNote();
  }

  private label(row: HTMLElement): void {
    const [name, value] = Array.from(row.querySelectorAll<HTMLInputElement>('input'));
    name?.setAttribute('aria-label', t('Header name'));
    value?.setAttribute('aria-label', t('Header value'));
    const rm = q<HTMLButtonElement>(row, '.cv-resend-remove');
    rm.title = t('Remove header');
    rm.setAttribute('aria-label', rm.title);
  }

  private row(key: string, h: Header): HTMLElement {
    const row = el(`<div class="cv-resend-row"><input class="cv-input mono" type="text" spellcheck="false" autocomplete="off" dir="ltr"><input class="cv-input mono" type="text" spellcheck="false" autocomplete="off" dir="ltr"><button class="cv-icon-btn cv-resend-remove" type="button">${icon('close', 14)}</button></div>`);
    row.dataset.key = key;
    this.fill(row, h);
    this.label(row);
    return row;
  }

  private fill(row: HTMLElement, h: Header): void {
    const [name, value] = Array.from(row.querySelectorAll<HTMLInputElement>('input'));
    if (name) name.value = h.name;
    if (value) value.value = h.value;
  }

  /** Opens the dialog for `f`, every control filled with the request as captured. */
  show(f: FlowDetail): void {
    this.flow = f;
    this.method.value = f.method;
    this.url.value = f.url;
    patchList(this.list, (f.request.headers ?? []).map((h, i) => ({ h, k: `h${i}` })), {
      key: (x) => x.k,
      create: (x) => this.row(x.k, x.h),
      update: (node, x) => this.fill(node, x.h),
    });
    this.list.scrollTop = 0;
    const b = f.request_body;
    this.kind = b?.text !== undefined && b.bytes > 0 ? 'text' : f.request.body_bytes > 0 ? 'binary' : 'none';
    this.original = this.kind === 'text' ? b?.text ?? '' : '';
    this.text.value = this.original;
    this.text.scrollTop = 0;
    setText(this.error, '');
    this.sending = false;
    setDisabled(this.send, false);
    this.paintBodyNote();
    this.open(this.url);
  }

  private paintBodyNote(): void {
    const f = this.flow;
    if (!f) { setText(this.bodyNote, ''); return; }
    const b = f.request_body;
    const changed = this.kind === 'text' ? this.text.value !== this.original : this.text.value !== '';
    let s = '';
    if (this.kind === 'none') s = t('The request had no body. Type one here to send it.');
    else if (this.kind === 'binary') s = changed ? t('The text here replaces the captured body.') : tf('The captured body ({size}) is not text, so it is sent as captured unless you type a replacement here.', { size: bytes(f.request.body_bytes) });
    else if (b?.truncated && !changed) s = tf('Only the first {kept} of {size} were kept: change the body to send it, since a cut body is not sent as captured.', { kept: bytes(b.bytes), size: bytes(f.request.body_bytes) });
    else if (b?.decoded && b.encoding) s = tf('Shown decoded ({encoding}). It is encoded again while Content-Encoding is left as it was.', { encoding: b.encoding });
    setText(this.bodyNote, s);
  }

  /** Only what differs from the capture: an absent field is sent as captured. */
  private changes(f: FlowDetail): ResendEdit {
    const edit: ResendEdit = {};
    const method = this.method.value.trim(), url = this.url.value.trim();
    if (method !== f.method) edit.method = method;
    if (url !== f.url) edit.url = url;
    const now: Header[] = [];
    for (const row of Array.from(this.list.children)) {
      const [name, value] = Array.from(row.querySelectorAll<HTMLInputElement>('input'));
      const n = name?.value.trim() ?? '';
      if (n) now.push({ name: n, value: value?.value ?? '' });
    }
    const was = f.request.headers ?? [];
    if (now.length !== was.length || now.some((h, i) => h.name !== was[i]?.name || h.value !== was[i]?.value)) edit.headers = now;
    if (this.kind === 'text' ? this.text.value !== this.original : this.text.value !== '') edit.body_text = this.text.value;
    return edit;
  }

  private submit(): void {
    const f = this.flow;
    if (!f || this.sending) return;
    const edit = this.changes(f);
    this.sending = true;
    setDisabled(this.send, true);
    setText(this.error, '');
    api.resend(f.id, edit)
      .then(({ id }) => {
        this.sending = false;
        this.close();
        this.onSent(id, Object.keys(edit).length > 0);
      })
      .catch((err: Error) => {
        this.sending = false;
        setDisabled(this.send, false);
        setText(this.error, err.message);
      });
  }
}
