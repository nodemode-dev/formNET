import { dataApi } from '../../api-data';
import type { Store } from '../../state';
import type { Status } from '../../types';
import { dataOf, type UpstreamChange, type UpstreamStatus } from '../../types-data';
import { dateTime } from '../../format';
import { el, q, setClass, setDisabled, setText, setTitle, setValue } from '../dom';
import { onLanguage, t } from '../i18n';
import { tf, toggleRow, type Row } from './controls';
import { labelled, linesOf, markField, rereadStatus, SayLine, trNote } from './traffic-common';

/** The state line: whole sentences from what the server says about the upstream. */
export function upstreamSentence(u: UpstreamStatus | null): string {
  if (!u) return t('The upstream proxy\'s state is shown once the server has answered.');
  if (u.error) return tf('Last failure, {time}: {error}', { time: dateTime(u.error_at), error: u.error });
  if (u.active) return tf('Every connection leaves through {url}, except the hosts listed under Bypass and this computer\'s own addresses.', { url: u.url });
  if (u.enabled && !u.url) return t('On, but no address is set: connections go out directly.');
  return t('Off: connections go out directly.');
}

/** Preferences > Capture > Upstream Proxy: send FormNET's own connections through another proxy (a corporate proxy, a
 *  local agent, a SOCKS5 tunnel). The switch is sent at once; the address, bypass list and username with Apply; the
 *  password is written only (the field says when one is set, and Clear removes it). Built once; refresh() patches it. */
export class UpstreamSection {
  readonly el = el('<div class="cv-panel-section fn-upstream"></div>');
  private readonly toggle: Row<boolean>;
  private readonly box: HTMLInputElement;
  private readonly url = el<HTMLInputElement>('<input class="cv-input" type="text" spellcheck="false" autocomplete="off" dir="ltr" inputmode="url">');
  private readonly bypass = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="3" spellcheck="false" dir="ltr"></textarea>');
  private readonly user = el<HTMLInputElement>('<input class="cv-input" type="text" spellcheck="false" autocomplete="off" dir="ltr">');
  private readonly pass = el<HTMLInputElement>('<input class="cv-input" type="password" autocomplete="new-password" dir="ltr">');
  private readonly clearPass = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly apply = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly state = el('<p class="cv-note fn-up-state" role="status"></p>');
  private readonly env = el('<p class="cv-note fn-up-env"></p>');
  private readonly say = new SayLine();
  private last: UpstreamStatus | null = null;
  private edited = false;

  constructor(private readonly store: Store) {
    this.toggle = toggleRow('Send FormNET\'s connections through another proxy', (on) => this.send({ enabled: on }, on ? 'On.' : 'Off.'));
    this.box = q<HTMLInputElement>(this.toggle.el, 'input');
    for (const n of [this.url, this.bypass, this.user]) n.addEventListener('input', () => { this.edited = true; });
    this.pass.addEventListener('input', () => { this.edited = true; });
    for (const n of [this.url, this.user, this.pass]) n.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); this.applyNow(); } });
    this.apply.addEventListener('click', () => this.applyNow());
    this.clearPass.addEventListener('click', () => this.send({ password: '' }, 'The password was removed.', () => { this.pass.value = ''; }));
    const passRow = el('<span class="cv-row-actions"></span>');
    passRow.append(this.pass, this.clearPass);
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.apply);
    this.el.append(this.toggle.el, this.state,
      labelled('Address', this.url),
      trNote('http://host:port, https://host:port (TLS to the proxy) or socks5://host:port, with the port. A user name and password written in the address move to the fields below.'),
      labelled('Bypass', this.bypass, 'cv-field fn-up-bypass'),
      labelled('User name', this.user),
      labelled('Password', passRow),
      bar, this.say.el, this.env,
      trNote('Loopback addresses and localhost always go directly, and the host each connection is for is still checked here. The password is kept in the data folder\'s settings file, readable only by you, like the root certificate\'s key.'));
    const words = (): void => {
      this.url.placeholder = t('http://proxy.example.com:3128');
      this.url.setAttribute('aria-label', t('Upstream proxy address'));
      this.bypass.placeholder = t('one host per line: *.corp.example, 10.0.0.0/8');
      this.bypass.setAttribute('aria-label', t('Hosts that go directly'));
      this.user.setAttribute('aria-label', t('User name'));
      this.pass.setAttribute('aria-label', t('Password'));
      setText(this.apply, t('Apply'));
      setText(this.clearPass, t('Clear'));
      this.paint(this.last);
    };
    words();
    onLanguage(words);
    this.refresh(store.get().status);
    dataApi.upstream().then((u) => this.paint(u)).catch(() => undefined);
  }

  refresh(status: Status | null): void {
    const u = dataOf(status).upstream;
    if (u) this.paint(u);
    else if (!this.last) this.paint(null);
  }

  private paint(u: UpstreamStatus | null): void {
    this.last = u;
    for (const n of [this.box, this.url, this.user, this.pass, this.apply] as const) setDisabled(n, !u);
    setDisabled(this.bypass as unknown as HTMLInputElement, !u);
    setDisabled(this.clearPass, !u?.has_password);
    const text = upstreamSentence(u);
    setText(this.state, text);
    setTitle(this.state, text);
    setClass(this.state, 'is-error', !!u?.error);
    setClass(this.state, 'is-on', !!u?.active && !u.error);
    setText(this.env, u?.from_env ? t('FORMNET_UPSTREAM_PROXY set this run\'s upstream when FormNET started. A change here applies on top of it for this run and is kept for the next start.') : '');
    if (!u) return;
    this.toggle.set(u.enabled);
    this.pass.placeholder = u.has_password ? t('set (type to replace)') : t('none');
    if (this.edited) return;
    setValue(this.url, u.url);
    setValue(this.bypass, (u.bypass ?? []).join('\n'));
    setValue(this.user, u.username);
  }

  private applyNow(): void {
    const change: UpstreamChange = { url: this.url.value.trim(), bypass: linesOf(this.bypass), username: this.user.value.trim() };
    if (this.pass.value) change.password = this.pass.value;
    this.send(change, 'Applied.', () => { this.edited = false; this.pass.value = ''; });
  }

  private send(change: UpstreamChange, done: string, after?: () => void): void {
    this.say.clear();
    dataApi.setUpstream(change)
      .then((u) => { after?.(); this.paint(u); this.say.ok(done); rereadStatus(this.store); })
      .catch((err: unknown) => {
        this.say.fail(err);
        markField(err, { url: this.url, bypass: this.bypass, username: this.user, password: this.pass });
        this.paint(this.last);
      });
  }
}
