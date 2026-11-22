import { bytes, ago } from '../format.ts';
import type { State, Store, View } from '../state.ts';
import { visibleFlows } from '../state.ts';
import { q, setClass, setText, setTitle } from './dom';
import { onLanguage, t, tn } from './i18n';

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
  private infoKind: InfoKind = 'flows';
  private errText = '';
  private errTimer = 0;
  private last: State;

  constructor(root: HTMLElement, private readonly store: Store) {
    root.insertAdjacentHTML('beforeend',
      '<span class="cv-st-engine"></span><span class="cv-st-proxy"></span><span class="cv-st-ca"></span>' +
      '<button class="cv-st-info" type="button"></button><span class="cv-st-hint"></span>');
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
