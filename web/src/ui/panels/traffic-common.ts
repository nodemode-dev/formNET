import { api, SessionError } from '../../api';
import { TrafficError } from '../../api-traffic';
import type { Store } from '../../state';
import { el, q, setClass, setText } from '../dom';
import { onLanguage, t } from '../i18n';
import { labelText } from './controls';

/** A note in the current language, re-set when the language changes. */
export function trNote(english: string, cls = 'cv-note'): HTMLElement {
  const p = el(`<p class="${cls}"></p>`);
  setText(p, t(english));
  onLanguage(() => setText(p, t(english)));
  return p;
}

/** A labelled row, the label kept in English for the dialog's relabel pass. */
export function labelled(label: string, control: HTMLElement, cls = 'cv-field'): HTMLElement {
  const row = el(`<label class="${cls}"><span></span></label>`);
  labelText(q(row, 'span'), label);
  row.appendChild(control);
  return row;
}

/** After a change the status is read again, so the status bar's chips and every view follow. */
export function rereadStatus(store: Store): void {
  api.status().then((status) => store.update({ status })).catch(() => undefined);
}

/** The non-empty lines of a text box, trimmed. */
export const linesOf = (box: HTMLTextAreaElement): string[] => box.value.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);

/** A one-line status slot of reserved height: what a change did, or why it was refused (in the danger colour). */
export class SayLine {
  readonly el = el('<p class="cv-note fn-say" role="status"></p>');
  private timer = 0;

  ok(english: string): void {
    setClass(this.el, 'is-error', false);
    setText(this.el, t(english));
    window.clearTimeout(this.timer);
    this.timer = window.setTimeout(() => setText(this.el, ''), 2500);
  }

  fail(err: unknown): void {
    if (err instanceof SessionError) return;
    window.clearTimeout(this.timer);
    setClass(this.el, 'is-error', true);
    setText(this.el, err instanceof Error ? err.message : String(err));
  }

  clear(): void {
    window.clearTimeout(this.timer);
    setClass(this.el, 'is-error', false);
    setText(this.el, '');
  }
}

/** Marks the control a refusal names (the server's field, mapped by `controls`) until it is next edited. */
export function markField(err: unknown, controls: Record<string, HTMLElement | undefined>): void {
  if (!(err instanceof TrafficError) || !err.field) return;
  const key = err.field.replace(/\[\d+\]$/, '');
  const c = controls[err.field] ?? controls[key];
  if (!c) return;
  setClass(c, 'is-invalid', true);
  c.setAttribute('aria-invalid', 'true');
  const off = (): void => { setClass(c, 'is-invalid', false); c.removeAttribute('aria-invalid'); c.removeEventListener('input', off); };
  c.addEventListener('input', off);
}
