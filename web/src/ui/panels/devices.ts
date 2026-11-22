import { trafficApi } from '../../api-traffic';
import type { Store } from '../../state';
import type { Status } from '../../types';
import { trafficOf, type RemoteStatus } from '../../types-traffic';
import { el, q, setClass, setDisabled, setText, setTitle, setValue } from '../dom';
import { onLanguage, t, tn } from '../i18n';
import { tf, toggleRow, type Row } from './controls';
import { labelled, linesOf, markField, rereadStatus, SayLine, trNote } from './traffic-common';

const CERT_URL = 'http://formnet.cert/';

/** The steps for each phone system, whole sentences with {address}, {port} and {url} filled in. */
const STEPS: ReadonlyArray<readonly [string, readonly string[]]> = [
  ['iPhone and iPad', [
    'Join the same Wi-Fi network as this computer.',
    'In Settings > Wi-Fi, tap the (i) beside the network, then Configure Proxy > Manual. Server: {address}, Port: {port}. Save.',
    'In Safari, open {url} and download the profile it offers.',
    'In Settings > General > VPN & Device Management, open the downloaded profile and install it.',
    'In Settings > General > About > Certificate Trust Settings, turn on full trust for the FormNET root.',
  ]],
  ['Android', [
    'Join the same Wi-Fi network as this computer.',
    'In Settings > Network & internet > Wi-Fi, open the network, edit it, and set Proxy to Manual. Host name: {address}, Port: {port}. Save.',
    'In Chrome, open {url} and download ca.cer.',
    'In Settings > Security > More security settings > Encryption & credentials > Install a certificate > CA certificate, choose the downloaded file.',
    'Chrome trusts the certificate from then on; most other apps trust only the certificates the system came with, so their HTTPS stays tunnelled and only counted.',
  ]],
  ['Another computer', [
    'Set its HTTP and HTTPS proxy to {address}, port {port}.',
    'Open {url} in its browser, download ca.pem, and add it to that computer\'s trusted root certificates.',
  ]],
];

/** Preferences > Other Devices: the switch that lets a phone or another computer on this network use the proxy, the
 *  allow list it needs first, what to type into the other device, how to trust the root there, and the connections
 *  refused from outside the list. Off unless turned on here or by FORMNET_REMOTE_CAPTURE. */
export class DevicesSection {
  readonly el = el('<div class="cv-panel-section fn-devices"></div>');
  private readonly toggle: Row<boolean>;
  private readonly box: HTMLInputElement;
  private readonly state = el('<p class="cv-note fn-dev-state" role="status"></p>');
  private readonly allow = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="3" spellcheck="false" dir="ltr"></textarea>');
  private readonly apply = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly addr = el<HTMLInputElement>('<input class="cv-input" type="text" readonly spellcheck="false" dir="ltr">');
  private readonly port = el<HTMLInputElement>('<input class="cv-input" type="text" readonly spellcheck="false" dir="ltr">');
  private readonly cert = el<HTMLInputElement>('<input class="cv-input" type="text" readonly spellcheck="false" dir="ltr">');
  private readonly refused = el('<p class="cv-note fn-dev-refused" role="status"></p>');
  private readonly steps: Array<{ li: HTMLElement; english: string }> = [];
  private readonly say = new SayLine();
  private last: RemoteStatus | null = null;
  private edited = false;
  private busy = false;

  constructor(private readonly store: Store) {
    this.toggle = toggleRow('Let other devices on this network use the proxy', (on) => this.send(on ? { enabled: true, allow: linesOf(this.allow) } : { enabled: false }, on ? 'Other devices can use the proxy.' : 'Only this computer can use the proxy.'));
    this.box = q<HTMLInputElement>(this.toggle.el, 'input');
    this.allow.addEventListener('input', () => { this.edited = true; this.paintSwitch(); });
    this.apply.addEventListener('click', () => this.send({ allow: linesOf(this.allow) }, 'Applied.', () => { this.edited = false; }));
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.apply);
    const guide = el('<div class="fn-dev-guide"></div>');
    for (const [system, lines] of STEPS) {
      const ol = el('<ol class="fn-dev-steps"></ol>');
      for (const english of lines) { const li = el('<li></li>'); this.steps.push({ li, english }); ol.append(li); }
      guide.append(trNote(system, 'cv-note-head'), ol);
    }
    const cols = el('<div class="cv-prefs-cols"></div>');
    cols.append(labelled('Proxy server', this.addr), labelled('Port', this.port));
    this.el.append(this.toggle.el, this.state,
      labelled('Allowed addresses', this.allow, 'cv-field fn-dev-allow'), bar, this.say.el,
      trNote('One address (192.168.1.23) or range (192.168.1.0/24) per line; only those devices may connect, and a range that holds every address is refused. Narrowing the list closes the connections of addresses no longer on it. Another device never reaches this computer\'s own local services through the proxy, and FormNET\'s page and API are never offered to it.'),
      trNote('On the other device', 'cv-note-head'), cols, labelled('Certificate page', this.cert), this.refused, guide,
      trNote('When you are done, set the other device\'s proxy back to Off: while FormNET is closed, or this switch is off, that device cannot reach the network through this computer.'));
    const label = (): void => {
      this.allow.setAttribute('aria-label', t('Allowed addresses'));
      this.allow.placeholder = t('one address or range per line');
      this.addr.setAttribute('aria-label', t('Proxy server'));
      this.port.setAttribute('aria-label', t('Port'));
      this.cert.setAttribute('aria-label', t('Certificate page'));
      this.apply.textContent = t('Apply');
      this.paint(this.last);
    };
    label();
    onLanguage(label);
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    const r = trafficOf(status).remote;
    if (r) this.paint(r);
    else if (!this.last) this.paint(null);
  }

  private paintSwitch(): void {
    const st = this.last;
    const canOn = !!st && (st.enabled || linesOf(this.allow).length > 0);
    if (!this.busy) setDisabled(this.box, !canOn);
    const why = t('Add at least one address or range to the list below to turn this on: FormNET never lets another device in without one.');
    setTitle(this.toggle.el, canOn ? '' : why);
    if (st && !st.enabled && !st.error) this.writeState(st, canOn);
  }

  private writeState(st: RemoteStatus | null, canOn: boolean): void {
    let text: string;
    if (!st) text = t('The state of capture from other devices is shown once the server has answered.');
    else if (st.error) text = st.error;
    else if (st.enabled) text = (st.listening ?? []).length ? tf('On: the proxy listens for other devices on {addresses}.', { addresses: (st.listening ?? []).join(', ') }) : t('On, but no network address of this computer was found yet: it is looked for again every 10 seconds.');
    else text = canOn ? t('Off: only this computer can use the proxy.') : t('Off. Add at least one address or range to the list below to turn this on: FormNET never lets another device in without one.');
    if (st?.from_env) text = `${text} ${t('This run\'s setting came from FORMNET_REMOTE_CAPTURE or FORMNET_REMOTE_ALLOW, which win again at the next start.')}`;
    setText(this.state, text);
    setTitle(this.state, text);
    setClass(this.state, 'is-error', !!st?.error);
    setClass(this.state, 'is-on', !!st?.enabled && !st.error);
  }

  private paint(st: RemoteStatus | null): void {
    this.last = st;
    setDisabled(this.apply, !st);
    if (st) {
      if (!this.busy) this.toggle.set(st.enabled);
      if (!this.edited) setValue(this.allow, (st.allow ?? []).join('\n'));
    }
    const addresses = st?.addresses ?? [];
    setValue(this.addr, addresses.length ? addresses.join(', ') : st ? t('No network address found') : '');
    setValue(this.port, st ? String(st.port) : '');
    setValue(this.cert, st ? st.cert_url || CERT_URL : '');
    const values = { address: addresses[0] ?? t('this computer\'s address'), port: st ? String(st.port) : '8080', url: st?.cert_url || CERT_URL };
    for (const s of this.steps) setText(s.li, tf(s.english, values));
    const n = st?.refused ?? 0;
    const refused = !st ? '' : n === 0 ? t('No connection from outside the list has been refused.') : `${tn(n, '{n} connection from outside the list was refused.', '{n} connections from outside the list were refused.')}${st.last_refused ? ` ${tf('The last came from {address}.', { address: st.last_refused })}` : ''}`;
    setText(this.refused, refused);
    setClass(this.refused, 'is-warn', n > 0);
    this.writeState(st, !!st && (st.enabled || linesOf(this.allow).length > 0));
    this.paintSwitch();
  }

  private send(change: { enabled?: boolean; allow?: string[] }, done: string, after?: () => void): void {
    if (this.busy) return;
    this.busy = true;
    this.say.clear();
    trafficApi.setRemote(change)
      .then((st) => { after?.(); this.busy = false; this.paint(st); this.say.ok(done); rereadStatus(this.store); })
      .catch((err: unknown) => {
        this.busy = false;
        this.say.fail(err);
        markField(err, { allow: this.allow });
        this.paint(this.last);
      });
  }
}
