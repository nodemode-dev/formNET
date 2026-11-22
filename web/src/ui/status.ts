import { breakpointWatch } from '../breakpoint-watch.ts';
import { bytes, ago } from '../format.ts';
import type { State, Store, View } from '../state.ts';
import { visibleFlows } from '../state.ts';
import { trafficOf } from '../types-traffic.ts';
import { q, setClass, setText, setTitle } from './dom';
import { onLanguage, t, tn } from './i18n';
import { presetName } from './panels/throttle';
import { tf } from './panels/controls';

/** The chips at the end of the bar, each with the command its click runs (registered by app-commands.ts). */
const CHIPS = [['paused', 'breakpoints-show'], ['rules', 'prefs-rules'], ['throttle', 'prefs-throttle'], ['devices', 'prefs-devices']] as const;
type Chip = (typeof CHIPS)[number][0];

const INFO_KEY = 'formnet.statusInfo';
const INFO_KINDS = ['flows', 'bytes', 'hosts', 'machine', 'sample'] as const;
type InfoKind = (typeof INFO_KINDS)[number];
const isDesktop = (): boolean => '__TAURI_INTERNALS__' in window;

/** What each view does with the pointer and the keys, shown in the hint slot. */
const HINTS: Record<View, string> = {
  flows: 'Click a row for details · Up and Down move · / filters · Esc closes the details',
  hosts: 'Click a host to filter Flows to it · labels are reading aids, not verdicts',
  machine: 'Every process with a connection · expand one for its connections · sampled every two seconds',
  setup: 'Point a program at the proxy · trust the root once to open HTTPS',
};

/** The bottom status bar, as the family's (formvis status.ts shape): built once, every slot patched in place at a
 *  fixed width so nothing reflows. Left to right: the server, the proxy, HTTPS opened or tunnelled, a readout the
 *  person cycles through, and the view's hint, which an error takes over for six seconds. */
export class StatusBar {
  private readonly engine: HTMLElement;
  private readonly proxy: HTMLElement;
  private readonly ca: HTMLElement;
  private readonly info: HTMLButtonElement;
  private readonly hint: HTMLElement;
  private readonly chips = new Map<Chip, HTMLButtonElement>();
  private infoKind: InfoKind = 'flows';
  private errText = '';
  private errTimer = 0;
  private last: State;

  constructor(root: HTMLElement, private readonly store: Store) {
    root.insertAdjacentHTML('beforeend',
      '<span class="cv-st-engine"></span><span class="cv-st-proxy"></span><span class="cv-st-ca"></span>' +
      '<button class="cv-st-info" type="button"></button><span class="cv-st-hint"></span>' +
      `<span class="cv-st-chips">${CHIPS.map(([k]) => `<button class="cv-st-chip cv-st-chip-${k} is-off" type="button" data-chip="${k}" tabindex="-1"></button>`).join('')}</span>`);
    for (const [k, cmd] of CHIPS) {
      const b = q<HTMLButtonElement>(root, `[data-chip="${k}"]`);
      this.chips.set(k, b);
      b.addEventListener('click', () => document.dispatchEvent(new CustomEvent('formnet:run', { detail: cmd })));
    }
    breakpointWatch.subscribe(() => this.syncChips(this.last));
    this.engine = q(root, '.cv-st-engine');
    this.proxy = q(root, '.cv-st-proxy');
    this.ca = q(root, '.cv-st-ca');
    this.info = q<HTMLButtonElement>(root, '.cv-st-info');
    this.hint = q(root, '.cv-st-hint');
    try { const k = localStorage.getItem(INFO_KEY) as InfoKind | null; if (k && INFO_KINDS.includes(k)) this.infoKind = k; } catch { /* default */ }
    this.info.addEventListener('click', (e) => {
      const n = INFO_KINDS.length;
      this.infoKind = INFO_KINDS[(INFO_KINDS.indexOf(this.infoKind) + (e.shiftKey ? n - 1 : 1)) % n];
      try { localStorage.setItem(INFO_KEY, this.infoKind); } catch { /* not kept */ }
      this.syncInfo(this.last);
    });
    this.last = store.get();
    store.subscribe((s) => { this.last = s; this.sync(s); });
    window.setInterval(() => { if (this.infoKind === 'sample') this.syncInfo(this.last); }, 5000);
    onLanguage(() => this.sync(this.last));
    this.sync(this.last);
  }

  /** An error sits in the hint slot in the danger colour for six seconds, then the view's hint comes back. */
  flashError(text: string): void {
    this.errText = text;
    this.syncHint(this.last);
    window.clearTimeout(this.errTimer);
    this.errTimer = window.setTimeout(() => { this.errText = ''; this.syncHint(this.last); }, 6000);
  }

  private sync(s: State): void {
    const st = s.status;
    const engine = !s.connected && !st ? t('Server offline') : st ? t(isDesktop() ? 'Server {version} · desktop' : 'Server {version}').replace('{version}', st.version) : t('Server');
    setText(this.engine, engine);
    setTitle(this.engine, s.connected ? t('Connected to the FormNET server') : t('Reconnecting to the FormNET server'));
    setClass(this.engine, 'is-off', !s.connected);
    const p = st?.proxy;
    setText(this.proxy, p ? t('Proxy {addr}').replace('{addr}', p.addr) : '');
    setTitle(this.proxy, p ? (p.running ? t('The proxy is listening') : p.error ? t('The proxy is not running: {error}').replace('{error}', p.error) : t('The proxy is not running')) : '');
    setClass(this.proxy, 'is-off', !!p && !p.running);
    setText(this.ca, p ? t(p.intercept ? 'HTTPS opened' : 'HTTPS tunnelled') : '');
    setTitle(this.ca, st ? t(st.ca.trusted ? 'The root is trusted on this computer' : 'The root is not trusted yet: HTTPS is counted, not read') : '');
    this.syncInfo(s);
    this.syncHint(s);
    this.syncChips(s);
  }

  /** The chips keep their slots whether shown or not (an off chip is only invisible), so one appearing moves nothing. */
  private syncChips(s: State): void {
    const tr = trafficOf(s.status);
    const paused = breakpointWatch.pausedCount();
    const rules = tr.rules, th = tr.throttle, rm = tr.remote;
    const chip = (k: Chip, on: boolean, text: string, tip: string, warn = false): void => {
      const b = this.chips.get(k);
      if (!b) return;
      setClass(b, 'is-off', !on);
      setClass(b, 'is-warn', on && warn);
      setText(b, on ? text : '');
      setTitle(b, on ? tip : '');
      const tab = on ? '0' : '-1';
      if (b.getAttribute('tabindex') !== tab) b.setAttribute('tabindex', tab);
      if (on) b.setAttribute('aria-label', tip); else b.removeAttribute('aria-label');
    };
    chip('paused', paused > 0, tn(paused, '{n} paused', '{n} paused'), tn(paused, '{n} exchange is waiting at a breakpoint. Click to open the Breakpoints panel.', '{n} exchanges are waiting at breakpoints. Click to open the Breakpoints panel.'), true);
    chip('rules', !!rules?.enabled && rules.count > 0, tf('Rules ({n})', { n: String(rules?.count ?? 0) }), tn(rules?.count ?? 0, '{n} rule is in the list and rules apply. Click for Preferences > Rules.', '{n} rules are in the list and rules apply. Click for Preferences > Rules.'));
    chip('throttle', !!th?.enabled, presetName(th?.preset ?? ''), th ? tf('Throttled to {name}: {down} kbit/s down, {up} kbit/s up, {latency} ms. Click for Preferences > Throttle.', { name: presetName(th.preset), down: String(th.down_kbps), up: String(th.up_kbps), latency: String(th.latency_ms) }) : '');
    const listening = (rm?.listening ?? []).join(', ');
    chip('devices', !!rm?.enabled, t('Other devices'), rm?.error ? tf('Capture from other devices: {error}. Click for Preferences > Other Devices.', { error: rm.error }) : listening ? tf('Other devices can use the proxy at {addresses}. Click for Preferences > Other Devices.', { addresses: listening }) : t('Capture from other devices is on, but no network address was found yet. Click for Preferences > Other Devices.'), !!rm?.error);
  }

  /** The chosen readout, written into the one slot that is already there; a click picks the next (Shift the previous). */
  private syncInfo(s: State): void {
    const kind = this.infoKind;
    let text = '', tip = '';
    if (kind === 'flows') {
      const shown = visibleFlows(s).length;
      text = shown === s.flows.size ? tn(s.flows.size, '{n} flow', '{n} flows') : t('{shown} of {total} flows').replace('{shown}', String(shown)).replace('{total}', String(s.flows.size));
      tip = t('Flows kept, and how many the filter shows. Click for the next readout, Shift+click for the previous.');
    } else if (kind === 'bytes') {
      let sent = 0, got = 0;
      for (const f of s.flows.values()) { sent += f.bytes_sent; got += f.bytes_received; }
      text = t('{sent} sent · {received} received').replace('{sent}', bytes(sent)).replace('{received}', bytes(got));
      tip = t('Bytes through the proxy, in the flows kept. Click for the next readout, Shift+click for the previous.');
    } else if (kind === 'hosts') {
      text = tn(s.hosts.length, '{n} host', '{n} hosts');
      tip = t('Hosts seen through the proxy. Click for the next readout, Shift+click for the previous.');
    } else if (kind === 'machine') {
      const m = s.machine;
      text = m ? t('{processes} · {connections}').replace('{processes}', tn((m.processes ?? []).length, '{n} process', '{n} processes')).replace('{connections}', tn((m.connections ?? []).length, '{n} connection', '{n} connections')) : t('No sample yet');
      tip = t('The last sample of the machine\'s connection table. Click for the next readout, Shift+click for the previous.');
    } else {
      const m = s.machine;
      text = m?.taken ? t('Sampled {age}').replace('{age}', ago(m.taken)) : t('No sample yet');
      tip = t('When the machine was last sampled. Click for the next readout, Shift+click for the previous.');
    }
    setText(this.info, text);
    setTitle(this.info, tip);
  }

  private syncHint(s: State): void {
    setClass(this.hint, 'is-error', !!this.errText);
    if (this.errText) { setText(this.hint, this.errText); setTitle(this.hint, this.errText); return; }
    const notice = (s.status?.notices ?? []).join(' ');
    setClass(this.hint, 'is-error', !!notice);
    if (notice) { setText(this.hint, notice); setTitle(this.hint, notice); return; }
    const text = t(HINTS[s.view]);
    setText(this.hint, text);
    setTitle(this.hint, text);
  }
}
