import { api } from '../../api';
import type { Store } from '../../state';
import type { Status, SysProxyStatus } from '../../types';
import { el, patchList, q, setDisabled, setHidden, setText, setTitle } from '../dom';
import { onLanguage, t } from '../i18n';
import { toast } from '../toast';
import { richText, tf, toggleRow, type Row } from './controls';

export type SysProxyTone = 'ok' | 'warn' | 'bad' | '';

/** The short state of the system proxy for a pill, and its tone. */
export function sysproxyPill(sp: SysProxyStatus | undefined): [string, SysProxyTone] {
  if (!sp) return ['Waiting', ''];
  if (!sp.supported) return ['Not supported', ''];
  if (sp.enabled && sp.active) return ['On', 'ok'];
  if (sp.enabled) return ['Not active', sp.error ? 'bad' : 'warn'];
  if (sp.error) return ['Off', 'bad'];
  return ['Off', ''];
}

/** One or two whole sentences on the system proxy's state: the saved choice, whether the system points at FormNET now,
 *  and why not, in the current language. The server's reason and error are shown as it words them. */
export function sysproxySentence(sp: SysProxyStatus | undefined): string {
  if (!sp) return t('The system proxy\'s state is shown once the server has answered.');
  const tidy = (s: string): string => s.trim().replace(/[.\s]+$/, '');
  if (!sp.supported) return tf('Not supported on this system: {reason}.', { reason: tidy(sp.reason) });
  let s: string;
  if (sp.error) s = tf('The system proxy could not be changed: {error}.', { error: tidy(sp.error) });
  else if (sp.enabled && sp.active) s = tf('On: this computer\'s proxy points at FormNET ({address}).', { address: sp.address });
  else if (sp.enabled) s = t('On, but this computer\'s proxy does not point at FormNET now: another program or the system settings changed it.');
  else if (sp.active) s = tf('Off, but this computer\'s proxy still points at FormNET ({address}).', { address: sp.address });
  else s = t('Off: this computer\'s proxy is as you set it.');
  if (sp.auto_config) s = `${s} ${t('This computer uses automatic proxy configuration, which the system prefers: programs that follow it may not reach FormNET.')}`;
  return s;
}

/** Preferences > Capture > System Proxy: the switch that points this computer's proxy at FormNET while it runs, what the
 *  system does now, the services changed (macOS), and how the old settings come back. Built once, patched by refresh(). */
export class SystemProxySection {
  readonly el = el('<div class="cv-panel-section cv-capture-sysproxy"></div>');
  private readonly toggle: Row<boolean>;
  private readonly box: HTMLInputElement;
  private readonly state = el('<p class="cv-note cv-sysproxy-state" role="status"></p>');
  private readonly servicesHead = el('<p class="cv-note cv-sysproxy-head"></p>');
  private readonly services = el('<ul class="cv-sysproxy-services" translate="no"></ul>');
  private busy = false;
  private last: SysProxyStatus | undefined;

  constructor(private readonly store: Store) {
    this.toggle = toggleRow('Point this computer\'s proxy at FormNET while it runs', (v) => this.set(v));
    this.box = q<HTMLInputElement>(this.toggle.el, 'input');
    const restore = el('<p class="cv-note"></p>');
    richText(restore, 'The previous settings come back when FormNET quits, and after a crash at its next start; {command} puts them back by hand.', { command: el('<code translate="no">formnet sysproxy restore</code>') });
    const paintHead = (): void => setText(this.servicesHead, t('Network services FormNET changed:'));
    paintHead();
    onLanguage(() => { paintHead(); this.paint(this.last); });
    this.el.append(this.toggle.el, this.state, this.servicesHead, this.services, restore);
    this.refresh(store.get().status);
  }

  private set(on: boolean): void {
    if (this.busy) { this.box.checked = !on; return; }
    this.busy = true;
    api.setSysproxy(on)
      .then((sp) => {
        const status = this.store.get().status;
        if (status) this.store.update({ status: { ...status, sysproxy: sp } });
        else this.paint(sp);
        if (sp.error) toast(sysproxySentence(sp), 'error');
      })
      .catch((err: Error) => toast(err.message, 'error'))
      .finally(() => {
        this.busy = false;
        this.paint(this.store.get().status?.sysproxy ?? this.last);
        api.status().then((status) => this.store.update({ status })).catch(() => undefined);
      });
  }

  refresh(status: Status | null): void {
    this.paint(status?.sysproxy);
  }

  private paint(sp: SysProxyStatus | undefined): void {
    this.last = sp;
    if (!this.busy) {
      this.toggle.set(!!sp?.enabled);
      setDisabled(this.box, !sp || !sp.supported);
    }
    const text = sysproxySentence(sp);
    setText(this.state, text);
    setTitle(this.state, text);
    this.state.classList.toggle('is-error', !!sp?.error);
    const list = sp?.services ?? [];
    setHidden(this.servicesHead, list.length === 0);
    setHidden(this.services, list.length === 0);
    const off = t('off');
    patchList(this.services, list, {
      key: (s) => s.name,
      create: (s) => { const li = el('<li><span class="cv-sysproxy-name"></span><span class="cv-sysproxy-addr"></span></li>'); this.fill(li, s.name, s.web, s.secure, off); return li; },
      update: (li, s) => this.fill(li, s.name, s.web, s.secure, off),
    });
  }

  private fill(li: HTMLElement, name: string, web: string, secure: string, off: string): void {
    setText(q(li, '.cv-sysproxy-name'), name);
    setText(q(li, '.cv-sysproxy-addr'), tf('web {web} · secure {secure}', { web: web || off, secure: secure || off }));
  }
}
