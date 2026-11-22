import { trafficApi } from '../../api-traffic';
import type { Store } from '../../state';
import type { Status } from '../../types';
import { trafficOf, type ThrottleConfig, type ThrottleStatus } from '../../types-traffic';
import { el, q, setDisabled, setValue } from '../dom';
import { onLanguage, t } from '../i18n';
import { tf, toggleRow, type Row } from './controls';
import { labelled, linesOf, markField, rereadStatus, SayLine, trNote } from './traffic-common';

/** The presets in the order offered, with the names a person reads. */
export const THROTTLE_PRESETS: ReadonlyArray<readonly [string, string]> = [['3g', '3G'], ['4g', '4G'], ['slow-wifi', 'Slow Wi-Fi'], ['custom', 'Custom']];

/** The name a person reads for a preset. */
export const presetName = (id: string): string => t(THROTTLE_PRESETS.find(([k]) => k === id)?.[1] ?? id);

/** Preferences > Throttle: the switch, the network to imitate, the numbers of a custom one, and the hosts it covers.
 *  Every change is sent at once (the host list with Apply) and the status bar's chip follows. */
export class ThrottleSection {
  readonly el = el('<div class="cv-panel-section fn-throttle"></div>');
  private readonly toggle: Row<boolean>;
  private readonly box: HTMLInputElement;
  private readonly preset = el<HTMLSelectElement>('<select class="cv-input"></select>');
  private readonly down = el<HTMLInputElement>('<input class="cv-input" type="number" min="0" max="10000000" step="1">');
  private readonly up = el<HTMLInputElement>('<input class="cv-input" type="number" min="0" max="10000000" step="1">');
  private readonly latency = el<HTMLInputElement>('<input class="cv-input" type="number" min="0" max="10000" step="1">');
  private readonly hosts = el<HTMLTextAreaElement>('<textarea class="cv-input" rows="3" spellcheck="false" dir="ltr"></textarea>');
  private readonly apply = el<HTMLButtonElement>('<button type="button" class="cv-btn cv-btn-ghost"></button>');
  private readonly say = new SayLine();
  private last: ThrottleStatus | null = null;
  private edited = false;

  constructor(private readonly store: Store) {
    this.toggle = toggleRow('Slow the proxy\'s traffic', (on) => this.send({ enabled: on }, on ? 'Throttling.' : 'Not throttling.'));
    this.box = q<HTMLInputElement>(this.toggle.el, 'input');
    for (const [id, name] of THROTTLE_PRESETS) this.preset.add(new Option(t(name), id));
    this.preset.addEventListener('change', () => {
      const p = this.preset.value;
      this.send(p === 'custom' ? { preset: p, down_kbps: this.num(this.down), up_kbps: this.num(this.up), latency_ms: this.num(this.latency) } : { preset: p }, 'Saved.');
    });
    const sendNumbers = (): void => { if (this.preset.value === 'custom') this.send({ preset: 'custom', down_kbps: this.num(this.down), up_kbps: this.num(this.up), latency_ms: this.num(this.latency) }, 'Saved.'); };
    for (const n of [this.down, this.up, this.latency]) {
      n.addEventListener('change', sendNumbers);
      n.addEventListener('keydown', (e) => { if (e.key === 'Enter') { e.preventDefault(); n.blur(); } });
    }
    this.hosts.addEventListener('input', () => { this.edited = true; });
    this.apply.addEventListener('click', () => this.send({ hosts: linesOf(this.hosts) }, 'Applied.', () => { this.edited = false; }));
    const label = (): void => {
      this.hosts.setAttribute('aria-label', t('Throttled hosts'));
      this.hosts.placeholder = t('empty: every host; one host per line, *.suffix allowed');
      this.apply.textContent = t('Apply');
      this.paint(this.last);
    };
    const cols = el('<div class="cv-prefs-cols fn-throttle-nums"></div>');
    cols.append(labelled('Down (kbit/s)', this.down), labelled('Up (kbit/s)', this.up), labelled('Latency (ms)', this.latency));
    const bar = el('<div class="cv-improve"></div>');
    bar.append(this.apply);
    this.el.append(this.toggle.el, labelled('Network', this.preset), cols,
      trNote('A preset fills the numbers; Custom takes the ones you type. 0 is no limit that way. The rate is shared by every throttled connection, as one slow link would be, and the latency is added once per request and once per new tunnel.'),
      labelled('Hosts', this.hosts, 'cv-field fn-throttle-hosts'), bar, this.say.el,
      trNote('The page and the API never pass through the proxy and are never slowed; WebSocket messages are not throttled. The setting is kept when FormNET restarts.'));
    label();
    onLanguage(label);
    this.refresh(store.get().status);
  }

  refresh(status: Status | null): void {
    const th = trafficOf(status).throttle;
    if (th) this.paint(th);
    else if (!this.last) this.paint(null);
  }

  private num(box: HTMLInputElement): number {
    const v = box.valueAsNumber;
    return Number.isFinite(v) ? Math.max(0, Math.round(v)) : 0;
  }

  private paint(st: ThrottleStatus | null): void {
    this.last = st;
    setDisabled(this.box, !st);
    setDisabled(this.preset, !st);
    setDisabled(this.apply, !st);
    if (!st) return;
    this.toggle.set(st.enabled);
    const presets = st.presets ?? {};
    THROTTLE_PRESETS.forEach(([id, name], i) => {
      const o = this.preset.options[i], p = presets[id];
      const text = id === 'custom' ? t('Custom: the numbers below') : p ? tf('{name}: {down} kbit/s down, {up} kbit/s up, {latency} ms', { name: t(name), down: String(p.down_kbps), up: String(p.up_kbps), latency: String(p.latency_ms) }) : t(name);
      if (o && o.text !== text) o.text = text;
    });
    if (this.preset.value !== st.preset && document.activeElement !== this.preset) this.preset.value = st.preset;
    const custom = st.preset === 'custom';
    for (const [box, v] of [[this.down, st.down_kbps], [this.up, st.up_kbps], [this.latency, st.latency_ms]] as const) {
      setDisabled(box, !custom);
      setValue(box, String(v));
    }
    if (!this.edited) setValue(this.hosts, (st.hosts ?? []).join('\n'));
  }

  private send(change: Partial<ThrottleConfig>, done: string, after?: () => void): void {
    this.say.clear();
    trafficApi.setThrottle(change)
      .then((st) => { after?.(); this.paint(st); this.say.ok(done); rereadStatus(this.store); })
      .catch((err: unknown) => {
        this.say.fail(err);
        markField(err, { preset: this.preset, down_kbps: this.down, up_kbps: this.up, latency_ms: this.latency, hosts: this.hosts });
        this.paint(this.last);
      });
  }
}
