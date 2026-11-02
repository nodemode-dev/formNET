import { api } from '../../api';
import type { Store } from '../../state';
import type { Status } from '../../types';
import { el, setChecked, setDisabled, setText, setValue } from '../dom';
import { toggleOf } from './controls';
import { toast } from '../toast';

const row = (label: string, control: HTMLElement): HTMLElement => { const r = el(`<label class="cv-dialog-row"><span>${label}</span></label>`); r.appendChild(control); return r; };

/** After a proxy change the status is read again, so every view that shows it follows. */
function reread(store: Store): void {
  api.status().then((status) => store.update({ status })).catch((err: Error) => toast(err.message, 'error'));
}

/** Preferences > Capture > HTTPS: whether the proxy opens HTTPS with the local root, and the root's trust state. */
export class HttpsSection {
  readonly el = el('<div class="cv-panel-section cv-capture-https"></div>');
  private readonly box = el<HTMLInputElement>('<input type="checkbox" aria-label="Open HTTPS with the local root">');
  private readonly note = el('<p class="cv-note" role="status" style="min-height: 2.8em"></p>');

  constructor(private readonly store: Store) {
    const open = row('Open HTTPS with the local root', toggleOf(this.box));
    const trust = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost">Trust the root certificate...</button>');
    const bar = el('<div class="cv-improve"></div>');
    bar.append(trust);
    trust.addEventListener('click', () => document.dispatchEvent(new CustomEvent('formnet:run', { detail: 'help-trust-root' })));
    this.box.addEventListener('change', () => {
      api.updateProxy({ intercept: this.box.checked }).then(() => reread(store)).catch((err: Error) => { toast(err.message, 'error'); reread(store); });
    });
    this.el.append(open, this.note, bar, el('<p class="cv-note">Off, every HTTPS host is tunnelled and only counted by host and bytes. On, formnet signs a certificate per host with its root and shows the request and response; programs that use the system trust store need the root trusted first, and programs that pin their certificates belong in the passthrough list.</p>'));
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    setDisabled(this.box, !status);
    if (!status) { setText(this.note, 'The proxy\'s state is shown once the server has answered.'); return; }
    setChecked(this.box, status.proxy.intercept);
    setText(this.note, status.ca.trusted
      ? `The root is trusted on this computer (${status.ca.fingerprint.slice(0, 23)}...), so browsers and most apps accept the opened connections.`
      : 'The root is not trusted yet: browsers and most apps will refuse opened connections until it is. curl with --cacert works already.');
  }
}

/** Preferences > Capture > Passthrough: the hosts never opened, one per line. */
export class PassthroughSection {
  readonly el = el('<div class="cv-panel-section cv-capture-pass"></div>');
  private readonly text = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="4" spellcheck="false" aria-label="Passthrough hosts" placeholder="one host per line, *.suffix allowed"></textarea>');
  private readonly apply = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost">Apply</button>');
  private readonly note = el('<p class="cv-note" role="status" style="min-height: 1.4em"></p>');

  constructor(private readonly store: Store) {
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.apply);
    this.apply.addEventListener('click', () => {
      const list = this.text.value.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
      api.updateProxy({ passthrough: list }).then(() => { setText(this.note, 'Applied.'); reread(store); }).catch((err: Error) => setText(this.note, err.message));
    });
    this.el.append(this.text, bar, this.note, el('<p class="cv-note">These hosts are tunnelled whatever the HTTPS choice above says; their bytes are still counted. FORMNET_PASSTHROUGH sets the list the server starts with.</p>'));
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    setDisabled(this.apply, !status);
    if (!status || document.activeElement === this.text) return;
    const next = (status.proxy.passthrough ?? []).join('\n');
    if (this.text.value !== next) this.text.value = next;
  }
}

/** Preferences > Capture > Proxy: the address programs point at, and what sets the rest. */
export class ProxySection {
  readonly el = el('<div class="cv-panel-section cv-capture-proxy"></div>');
  private readonly addr = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Proxy address" spellcheck="false">');
  private readonly state = el<HTMLInputElement>('<input class="cv-input" type="text" readonly aria-label="Proxy state" spellcheck="false">');

  constructor(store: Store) {
    this.el.append(row('Proxy address', this.addr), row('State', this.state), el('<p class="cv-note">FORMNET_PROXY_ADDR chooses the address (loopback only), FORMNET_BODY_LIMIT how much of each body is kept, FORMNET_FLOW_LIMIT how many flows are kept before the oldest go; all read when the server starts.</p>'));
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    setValue(this.addr, status ? `http://${status.proxy.addr}` : '');
    setValue(this.state, !status ? '' : status.proxy.running ? 'listening' : `not running${status.proxy.error ? `: ${status.proxy.error}` : ''}`);
  }
}
