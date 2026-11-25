import { api } from '../../api';
import type { Store } from '../../state';
import type { InterceptMode, Status } from '../../types';
import { copyText } from '../../dom';
import { dateTime } from '../../format';
import { el, q, setDisabled, setText, setValue } from '../dom';
import { onLanguage, t } from '../i18n';
import { labelText, selectRow, tf, type Row } from './controls';
import { toast } from '../toast';

export const row = (label: string, control: HTMLElement): HTMLElement => { const r = el('<label class="cv-dialog-row"><span></span></label>'); labelText(q(r, 'span'), label); r.appendChild(control); return r; };
export const note = (text: string): HTMLElement => { const p = el('<p class="cv-note"></p>'); setText(p, t(text)); onLanguage(() => setText(p, t(text))); return p; };

/** A button that copies `value()` and says so in `say` for a moment; the button's own text never changes, so nothing
 *  beside it moves. */
export function copyInto(label: string, value: () => string, say: HTMLElement, done = 'Copied.'): HTMLButtonElement {
  const b = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  setText(b, t(label));
  let timer = 0;
  b.addEventListener('click', () => {
    const v = value();
    if (!v) return;
    void copyText(v).then((ok) => {
      setText(say, t(ok ? done : 'The clipboard was not reachable: select the text and copy it.'));
      window.clearTimeout(timer);
      timer = window.setTimeout(() => setText(say, ''), 2500);
    });
  });
  return b;
}

/** After a proxy change the status is read again, so every view that shows it follows. */
function reread(store: Store): void {
  api.status().then((status) => store.update({ status })).catch((err: Error) => toast(err.message, 'error'));
}

/** The three HTTPS choices, shared by Preferences and Setup. */
export const INTERCEPT_CHOICES: ReadonlyArray<readonly [InterceptMode, string]> = [['auto', 'Auto'], ['on', 'On'], ['off', 'Off']];
export const AUTO_NOTE = 'Auto: HTTPS is opened once this computer trusts the root, and stops when it no longer does.';

/** Preferences > Capture > HTTPS: whether the proxy opens HTTPS with the local root, and the root's trust state. */
export class HttpsSection {
  readonly el = el('<div class="cv-panel-section cv-capture-https"></div>');
  private readonly mode: Row<string>;
  private readonly select: HTMLSelectElement;
  private readonly note = el('<p class="cv-note" role="status" style="min-height: 2.8em"></p>');

  constructor(private readonly store: Store) {
    this.mode = selectRow('Open HTTPS with the local root', INTERCEPT_CHOICES, (v) => {
      api.updateProxy({ intercept: v as InterceptMode }).then(() => reread(store)).catch((err: Error) => { toast(err.message, 'error'); reread(store); });
    });
    this.select = q<HTMLSelectElement>(this.mode.el, 'select');
    this.el.append(this.mode.el, note(AUTO_NOTE), this.note, note('Off, every HTTPS host is tunnelled and only counted by host and bytes. On, FormNET signs a certificate per host with its root and shows the request and response; programs that use the system trust store need the root trusted first, and programs that pin their certificates belong in the passthrough list. The choice is kept when FormNET restarts.'));
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    setDisabled(this.select, !status);
    if (!status) { setText(this.note, t('The proxy\'s state is shown once the server has answered.')); return; }
    this.mode.set(status.proxy.intercept_mode ?? '');
    setText(this.note, status.ca.trusted
      ? tf('The root is trusted on this computer ({fingerprint}...), so browsers and most apps accept the opened connections.', { fingerprint: status.ca.fingerprint.slice(0, 23) })
      : t('The root is not trusted yet: browsers and most apps will refuse opened connections until it is. Root Certificate below has the command that trusts it; curl with --cacert works already.'));
  }
}

/** Preferences > Capture > Root Certificate: the root's trust state, the file, the command that trusts it and the
 *  download. Setup and Home only show the state and send people here. */
export class RootSection {
  readonly el = el('<div class="cv-panel-section cv-capture-root"></div>');
  private readonly state = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Trust" spellcheck="false">');
  private readonly fingerprint = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Fingerprint" spellcheck="false" dir="ltr">');
  private readonly path = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Certificate file" spellcheck="false" dir="ltr">');
  private readonly expires = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Expires" spellcheck="false">');
  private readonly command = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Trust command" spellcheck="false" dir="ltr">');
  private readonly hint = el('<p class="cv-note" style="min-height: 2.9em"></p>');
  private readonly said = el('<p class="cv-note" role="status" style="min-height: 1.45em"></p>');

  constructor(store: Store) {
    const download = el<HTMLAnchorElement>('<a class="cv-btn cv-btn-ghost" download="formnet-ca.pem"></a>');
    setText(download, t('Download ca.pem'));
    download.href = api.caURL();
    const bar = el('<div class="cv-improve"></div>');
    bar.append(copyInto('Copy command', () => this.command.value, this.said), download);
    this.el.append(row('Trust', this.state), row('Fingerprint', this.fingerprint), row('Certificate file', this.path), row('Expires', this.expires), this.hint, row('Command', this.command), bar, this.said,
      note('FormNET made this root on this computer. Its private key stays in the data folder and is never served; only the certificate can be downloaded. Nothing is installed until you run the command yourself, and in Auto HTTPS is opened only once this computer trusts the root.'));
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    const ca = status?.ca;
    setValue(this.state, !ca ? '' : t(ca.trusted ? 'Trusted on this computer' : 'Not trusted yet'));
    setValue(this.fingerprint, ca?.fingerprint ?? '');
    setValue(this.path, ca?.path ?? '');
    setValue(this.expires, dateTime(ca?.not_after));
    setValue(this.command, ca?.trust_command ?? '');
    setText(this.hint, !ca ? t('The root\'s state is shown once the server has answered.')
      : ca.trust_hint || (ca.trust_command ? t('Run this command in a terminal to trust the root. It asks for your password; nothing is installed until you run it:') : t('This system has no command FormNET knows: add ca.pem to its trusted certificates with the system\'s own tool.')));
  }
}

/** Preferences > Capture > Passthrough: the hosts never opened, one per line. */
export class PassthroughSection {
  readonly el = el('<div class="cv-panel-section cv-capture-pass"></div>');
  private readonly text = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="4" spellcheck="false" dir="ltr"></textarea>');
  private readonly apply = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly note = el('<p class="cv-note" role="status" style="min-height: 1.4em"></p>');

  constructor(private readonly store: Store) {
    this.text.setAttribute('aria-label', t('Passthrough hosts'));
    this.text.placeholder = t('one host per line, *.suffix allowed');
    setText(this.apply, t('Apply'));
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.apply);
    this.apply.addEventListener('click', () => {
      const list = this.text.value.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
      api.updateProxy({ passthrough: list }).then(() => { this.edited = false; setText(this.note, t('Applied.')); reread(store); }).catch((err: Error) => setText(this.note, err.message));
    });
    this.text.addEventListener('input', () => { this.edited = true; });
    this.el.append(this.text, bar, this.note, note('These hosts are tunnelled whatever the HTTPS choice above says; their bytes are still counted. The list is kept when FormNET restarts.'));
    this.refresh(store.get().status);
  }

  private edited = false;

  refresh(status: Status | null): void {
    setDisabled(this.apply, !status);
    if (!status || this.edited) return;
    setValue(this.text, (status.proxy.passthrough ?? []).join('\n'));
  }
}

/** Preferences > Capture > Proxy: the address programs point at, and what sets the rest. */
export class ProxySection {
  readonly el = el('<div class="cv-panel-section cv-capture-proxy"></div>');
  private readonly addr = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Proxy address" spellcheck="false" dir="ltr">');
  private readonly state = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Proxy state" spellcheck="false">');

  constructor(store: Store) {
    this.el.append(row('Proxy address', this.addr), row('State', this.state), note('FORMNET_PROXY_ADDR chooses the address (loopback only), FORMNET_BODY_LIMIT how much of each body is kept, FORMNET_FLOW_LIMIT how many flows are kept before the oldest go; all read when the server starts.'));
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    setValue(this.addr, status ? `http://${status.proxy.addr}` : '');
    setValue(this.state, !status ? '' : status.proxy.running ? t('Listening') : status.proxy.error ? tf('Not running: {error}', { error: status.proxy.error }) : t('Not running'));
  }
}
