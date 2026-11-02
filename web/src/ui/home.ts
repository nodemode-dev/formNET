import { bytes } from '../format.ts';
import type { State, Store } from '../state.ts';
import type { HostSummary, Process } from '../types.ts';
import { copyText } from '../dom.ts';
import { el, patchList, q, setClass, setHidden, setText } from './dom';
import { icon } from './icons';
import { isMac } from './dom';

export interface HomeDeps {
  store: Store;
  autoShow: () => boolean;
  afterClear: () => boolean;
}

type Page = 'start';

/** The Home screen, as the family's (formvis home.ts): a navigation column beside a Start page, over the views, under
 *  the title bar. formNET has views instead of documents, so flows stand in for them: Home shows at launch while the
 *  recording is empty (Preferences > General), again after Clear Recording when that choice is on, and whenever the
 *  brand mark is clicked; the first flow to arrive, any view command, Back or Esc puts it away. Built once; every
 *  later change patches text, attributes and the two keyed lists. */
export class HomeScreen {
  readonly root = el('<section class="cv-home" aria-label="Home" hidden></section>');
  private forced = false;
  private dismissed = false;
  private hadFlows = false;
  private lastCount = 0;
  private readonly back: HTMLElement;
  private readonly hostList: HTMLElement;
  private readonly hostEmpty: HTMLElement;
  private readonly procList: HTMLElement;
  private readonly procEmpty: HTMLElement;
  private readonly addr: HTMLElement;
  private readonly copyBtn: HTMLButtonElement;
  private copyTimer = 0;
  private page: Page = 'start';

  constructor(host: HTMLElement, private readonly d: HomeDeps) {
    const mod = isMac ? '⌘' : 'Ctrl';
    const navItem = (v: Page, ic: string, label: string): string => `<button class="cv-home-nav-item" type="button" data-v="${v}">${icon(ic, 16)}<span>${label}</span></button>`;
    const cmdItem = (cmd: string, ic: string, label: string): string => `<button class="cv-home-nav-item" type="button" data-cmd="${cmd}">${icon(ic, 16)}<span>${label}</span></button>`;
    const key = (k: string): string => `<kbd class="cv-key">${mod}</kbd><kbd class="cv-key">${k}</kbd>`;
    const chips = ['HTTP', 'HTTPS', 'HTTP/2', 'WebSocket', 'CONNECT', 'HAR'].map((f) => `<span class="cv-home-format">${f}</span>`).join('');
    this.root.innerHTML = `<nav class="cv-home-nav" aria-label="Home">
      <div class="cv-home-brand">formNET <span>by nodemode.ai</span></div>
      ${navItem('start', 'home', 'Start')}
      <div class="cv-home-nav-sep"></div>
      <div class="cv-home-nav-title">Views</div>
      ${cmdItem('view-flows', 'flows', 'Flows')}${cmdItem('view-hosts', 'globe', 'Hosts')}${cmdItem('view-machine', 'chip', 'Machine')}${cmdItem('view-setup', 'wrench', 'Setup')}
      <div class="cv-home-nav-foot"><button class="cv-home-nav-item cv-home-back" type="button" data-back hidden>${icon('chevronLeft', 16)}<span>Back to Flows</span></button>${cmdItem('settings', 'settings', 'Preferences')}${cmdItem('help', 'info', 'Help')}</div>
    </nav>
    <div class="cv-home-main">
      <div class="cv-home-narrow">${navItem('start', 'home', 'Start')}${cmdItem('view-flows', 'flows', 'Flows')}${cmdItem('view-hosts', 'globe', 'Hosts')}${cmdItem('view-machine', 'chip', 'Machine')}${cmdItem('view-setup', 'wrench', 'Setup')}</div>
      <div class="cv-home-page" data-page="start">
        <div class="cv-home-head"><div><h1 class="cv-home-title"></h1><p class="cv-home-sub">Point a browser or a program at the proxy and watch what it actually sends.</p></div></div>
        <div class="cv-home-hero">
          <span class="cv-home-hero-icon">${icon('network', 26)}</span>
          <div class="cv-home-hero-text"><div class="cv-home-hero-title">Start from a program</div>
            <div class="cv-home-hero-sub">Set the proxy to <code class="cv-home-addr"></code> in a browser, a terminal or an app. HTTPS is opened once this computer trusts formNET's root; until then it is tunnelled and counted by host and bytes.</div>
            <div class="cv-home-formats">${chips}</div></div>
          <div class="cv-home-hero-actions">
            <button class="cv-home-action is-primary" type="button" data-cmd="view-flows">${icon('flows', 16)}<span class="cv-home-label">Open Flows</span>${key('1')}</button>
            <button class="cv-home-action" type="button" data-copy>${icon('copy', 16)}<span class="cv-home-label">Copy proxy address</span></button>
            <button class="cv-home-action is-ghost" type="button" data-cmd="help-trust-root">${icon('shield', 16)}<span class="cv-home-label">Trust the root certificate...</span></button>
          </div>
        </div>
        <section class="cv-home-section"><div class="cv-home-section-head"><h2 class="cv-home-section-title">Top hosts</h2><button class="cv-btn cv-btn-ghost" type="button" data-cmd="view-hosts">See all</button></div><div class="cv-home-cards" data-list="hosts"></div><p class="cv-home-empty" data-empty="hosts">Hosts appear here as programs talk through the proxy.</p></section>
        <section class="cv-home-section"><div class="cv-home-section-head"><h2 class="cv-home-section-title">Busiest processes</h2><button class="cv-btn cv-btn-ghost" type="button" data-cmd="view-machine">See all</button></div><div class="cv-home-cards" data-list="procs"></div><p class="cv-home-empty" data-empty="procs">Processes with connections appear here once the machine has been sampled.</p></section>
      </div>
    </div>`;
    this.back = q(this.root, '[data-back]');
    this.hostList = q(this.root, '[data-list="hosts"]');
    this.hostEmpty = q(this.root, '[data-empty="hosts"]');
    this.procList = q(this.root, '[data-list="procs"]');
    this.procEmpty = q(this.root, '[data-empty="procs"]');
    this.addr = q(this.root, '.cv-home-addr');
    this.copyBtn = q<HTMLButtonElement>(this.root, '[data-copy]');
    this.root.addEventListener('click', (e) => {
      const t = e.target as HTMLElement;
      if (t.closest('[data-back]')) { this.dismiss(); return; }
      if (t.closest('[data-copy]')) { void this.copyAddress(); return; }
      const v = (t.closest('[data-v]') as HTMLElement | null)?.dataset.v as Page | undefined;
      if (v) { this.show(v); return; }
      const card = t.closest<HTMLElement>('[data-host]');
      if (card?.dataset.host) { this.d.store.update({ hostFilter: card.dataset.host, view: 'flows' }); this.dismiss(); }
    });
    host.appendChild(this.root);
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !this.root.hidden && !e.defaultPrevented) { e.preventDefault(); this.dismiss(); } });
    d.store.subscribe((s, prev) => {
      if (s.flows.size !== prev.flows.size) this.sync();
      if (!this.root.hidden) this.fill(s, prev);
    });
    this.paint();
    this.sync();
  }

  private async copyAddress(): Promise<void> {
    const p = this.d.store.get().status?.proxy;
    const ok = p ? await copyText(`http://${p.addr}`) : false;
    const label = q(this.copyBtn, '.cv-home-label');
    setText(label, ok ? 'Copied' : 'Copy failed');
    window.clearTimeout(this.copyTimer);
    this.copyTimer = window.setTimeout(() => setText(label, 'Copy proxy address'), 1200);
  }

  private paint(): void {
    for (const n of Array.from(this.root.querySelectorAll<HTMLElement>('.cv-home-page'))) setHidden(n, n.dataset.page !== this.page);
    for (const n of Array.from(this.root.querySelectorAll<HTMLElement>('.cv-home-nav-item[data-v]'))) setClass(n, 'is-active', n.dataset.v === this.page);
    const h = new Date().getHours();
    setText(this.root.querySelector('[data-page="start"] .cv-home-title'), h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening');
  }

  /** The proxy address and the two lists, from the state: patched by key, so a card that stays keeps its node. */
  private fill(s: State, prev?: State): void {
    setText(this.addr, s.status?.proxy.addr ?? '127.0.0.1:8761');
    if (!prev || s.hosts !== prev.hosts) {
      const hosts = [...s.hosts].sort((a, b) => b.bytes_sent + b.bytes_received - (a.bytes_sent + a.bytes_received)).slice(0, 8);
      patchList<HostSummary>(this.hostList, hosts, {
        key: (x) => x.host,
        create: (x) => { const c = el('<button class="cv-home-card" type="button"><span class="cv-home-card-name cv-host-name"></span><span class="cv-home-card-meta"></span></button>'); c.dataset.host = x.host; this.fillHost(c, x); return c; },
        update: (c, x) => this.fillHost(c, x),
      });
      setHidden(this.hostEmpty, hosts.length > 0);
    }
    if (!prev || s.machine !== prev.machine) {
      const procs = [...(s.machine?.processes ?? [])].sort((a, b) => b.bytes_in + b.bytes_out - (a.bytes_in + a.bytes_out) || b.connections - a.connections).slice(0, 8);
      patchList<Process>(this.procList, procs, {
        key: (p) => String(p.pid),
        create: (p) => { const c = el('<div class="cv-home-card is-static"><span class="cv-home-card-name cv-proc-name"></span><span class="cv-home-card-meta"></span></div>'); this.fillProc(c, p); return c; },
        update: (c, p) => this.fillProc(c, p),
      });
      setHidden(this.procEmpty, procs.length > 0);
    }
  }

  private fillHost(c: HTMLElement, x: HostSummary): void {
    setText(q(c, '.cv-home-card-name'), x.host);
    setText(q(c, '.cv-home-card-meta'), `${x.flows} flows · ${bytes(x.bytes_sent + x.bytes_received)}`);
    c.title = `Show the flows of ${x.host}`;
  }

  private fillProc(c: HTMLElement, p: Process): void {
    setText(q(c, '.cv-home-card-name'), p.name);
    const moved = p.bytes_in + p.bytes_out;
    setText(q(c, '.cv-home-card-meta'), `${p.connections} connections${moved ? ` · ${bytes(moved)}` : ''}`);
    c.title = `pid ${p.pid}${p.path ? `, ${p.path}` : ''}`;
  }

  get isShown(): boolean { return !this.root.hidden; }

  /** The brand mark: Home over whatever view is open. */
  show(page?: Page): void {
    if (page && page !== this.page) { this.page = page; this.paint(); }
    this.forced = true;
    this.dismissed = false;
    this.sync();
  }

  /** A view command, Back or Esc: Home goes away until a flow count of zero returns or the mark is clicked again. */
  dismiss(): void {
    this.forced = false;
    this.dismissed = true;
    this.sync();
  }

  sync(): void {
    const s = this.d.store.get(), n = s.flows.size;
    if (n > this.lastCount) this.forced = false;
    if (n === 0 && this.lastCount > 0) this.dismissed = false;
    this.lastCount = n;
    if (n > 0) this.hadFlows = true;
    const on = this.forced || (!this.dismissed && n === 0 && (this.hadFlows ? this.d.afterClear() : this.d.autoShow()));
    if (on && this.root.hidden) { this.paint(); this.fill(s); }
    setHidden(this.root, !on);
    setHidden(this.back, n === 0 && !this.forced);
  }
}
