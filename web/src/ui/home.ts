import { bytes } from '../format.ts';
import type { State, Store } from '../state.ts';
import type { HostSummary, Process } from '../types.ts';
import { copyText } from '../dom.ts';
import { brandMark } from './brand';
import { el, isMac, patchList, q, setClass, setHidden, setText, setTitle } from './dom';
import { icon } from './icons';
import { onLanguage, t, tn } from './i18n';
import { richText, tf } from './panels/controls';
import { setupGuide, type SetupGuide } from './setup-view';

export interface HomeDeps {
  store: Store;
  autoShow: () => boolean;
  afterClear: () => boolean;
}

type Page = 'start' | 'hosts' | 'processes' | 'setup';
type ListId = 'start-hosts' | 'start-procs' | 'hosts' | 'procs';

const LISTS: Record<Page, readonly ListId[]> = { start: ['start-hosts', 'start-procs'], hosts: ['hosts'], processes: ['procs'], setup: [] };
const byBytes = (a: HostSummary, b: HostSummary): number => b.bytes_sent + b.bytes_received - (a.bytes_sent + a.bytes_received);
const busiest = (a: Process, b: Process): number => b.bytes_in + b.bytes_out - (a.bytes_in + a.bytes_out) || b.connections - a.connections;
/** A count for the nav's fixed slot: four digits, then thousands. */
const compact = (n: number): string => (n < 10000 ? String(n) : `${Math.floor(n / 1000)}k`);

/** The Home screen, on the family's structure (formvis home.ts): a navigation column (the brand; Start, Hosts,
 *  Processes and Setup with their counts; the views; Preferences and Help at its foot) beside a main area with one page
 *  per nav item. FormNET has views instead of documents, so flows stand in for them: Home shows at launch while the
 *  recording is empty (Preferences > General), again after Clear Recording when that choice is on, and whenever the
 *  brand mark is clicked; the first flow to arrive, any view command, Back or Esc puts it away. Every page is built once;
 *  switching pages toggles them, and every later change patches text, attributes and the keyed lists. */
export class HomeScreen {
  readonly root = el('<section class="cv-home" aria-label="Home" hidden></section>');
  private forced = false;
  private dismissed = false;
  private hadFlows = false;
  private lastCount = 0;
  private readonly back: HTMLElement;
  private readonly addr: HTMLElement;
  private readonly copyBtn: HTMLButtonElement;
  private readonly guide: SetupGuide;
  private readonly orders = new Map<ListId, string[]>();
  private copyTimer = 0;
  private page: Page = 'start';

  constructor(host: HTMLElement, private readonly d: HomeDeps) {
    const mod = isMac ? '⌘' : 'Ctrl';
    const count = '<span class="cv-home-count">0</span>';
    const navItem = (v: Page, ic: string, label: string, counted = false): string => `<button class="cv-home-nav-item" type="button" data-v="${v}">${icon(ic, 16)}<span>${t(label)}</span>${counted ? count : ''}</button>`;
    const cmdItem = (cmd: string, ic: string, label: string, counted = false): string => `<button class="cv-home-nav-item" type="button" data-cmd="${cmd}">${icon(ic, 16)}<span>${t(label)}</span>${counted ? count : ''}</button>`;
    const pages = (): string => `${navItem('start', 'home', 'Start')}${navItem('hosts', 'globe', 'Hosts', true)}${navItem('processes', 'chip', 'Processes', true)}${navItem('setup', 'wrench', 'Setup')}`;
    const key = (k: string): string => `<kbd class="cv-key">${mod}</kbd><kbd class="cv-key">${k}</kbd>`;
    const chips = ['HTTP', 'HTTPS', 'HTTP/2', 'WebSocket', 'CONNECT', 'HAR'].map((f) => `<span class="cv-home-format">${f}</span>`).join('');
    const head = (title: string, sub: string, action = ''): string => `<div class="cv-home-head"><div><h1 class="cv-home-title">${t(title)}</h1><p class="cv-home-sub">${t(sub)}</p></div>${action ? `<div class="cv-home-head-actions">${action}</div>` : ''}</div>`;
    const slot = (list: ListId, empty: string): string => `<div class="cv-home-slot${list.startsWith('start-') ? ' is-short' : ''}"><div class="cv-home-cards" data-list="${list}"></div><p class="cv-home-empty" data-empty="${list}">${t(empty)}</p></div>`;
    const hostsEmpty = 'Hosts appear here as programs talk through the proxy.', procsEmpty = 'Processes with connections appear here once the machine has been sampled.';
    this.root.innerHTML = `<nav class="cv-home-nav" aria-label="${t('Home')}">
      <div class="cv-home-brand">${brandMark(20, 'FormNET')}FormNET <span translate="no">by nodemode.ai</span></div>
      ${pages()}
      <div class="cv-home-nav-sep"></div>
      <div class="cv-home-nav-title">${t('Views')}</div>
      ${cmdItem('view-flows', 'flows', 'Flows', true)}${cmdItem('view-machine', 'chip', 'Machine')}
      <div class="cv-home-nav-foot"><button class="cv-home-nav-item cv-home-back is-off" type="button" data-back>${icon('chevronLeft', 16)}<span>${t('Back to Flows')}</span></button>${cmdItem('settings', 'settings', 'Preferences')}${cmdItem('help', 'info', 'Help')}</div>
    </nav>
    <div class="cv-home-main">
      <div class="cv-home-narrow">${pages()}${cmdItem('view-flows', 'flows', 'Flows')}</div>
      <div class="cv-home-page" data-page="start">
        <div class="cv-home-head"><div><h1 class="cv-home-title"></h1><p class="cv-home-sub">${t('Point a browser or a program at the proxy and watch what it actually sends.')}</p></div></div>
        <div class="cv-home-hero">
          <span class="cv-home-hero-icon">${icon('network', 26)}</span>
          <div class="cv-home-hero-text"><div class="cv-home-hero-title">${t('Start from a program')}</div>
            <div class="cv-home-hero-sub"><code class="cv-home-addr" data-no-i18n></code></div>
            <div class="cv-home-formats">${chips}</div></div>
          <div class="cv-home-hero-actions">
            <button class="cv-home-action is-primary" type="button" data-cmd="view-flows">${icon('flows', 16)}<span class="cv-home-label">${t('Open Flows')}</span>${key('1')}</button>
            <button class="cv-home-action" type="button" data-copy>${icon('copy', 16)}<span class="cv-home-label">${t('Copy proxy address')}</span></button>
            <button class="cv-home-action is-ghost" type="button" data-cmd="prefs-capture">${icon('shield', 16)}<span class="cv-home-label">${t('Trust the root certificate...')}</span></button>
          </div>
        </div>
        <section class="cv-home-section"><div class="cv-home-section-head"><h2 class="cv-home-section-title">${t('Top hosts')}</h2><button class="cv-btn cv-btn-ghost" type="button" data-v="hosts">${t('See all')}</button></div>${slot('start-hosts', hostsEmpty)}</section>
        <section class="cv-home-section"><div class="cv-home-section-head"><h2 class="cv-home-section-title">${t('Busiest processes')}</h2><button class="cv-btn cv-btn-ghost" type="button" data-v="processes">${t('See all')}</button></div>${slot('start-procs', procsEmpty)}</section>
      </div>
      <div class="cv-home-page" data-page="hosts" hidden>${head('Hosts', 'Every host programs reached through the proxy, busiest first when this page opens; a host keeps its place while it stays. Click one to see its flows.', `<button class="cv-btn" type="button" data-cmd="view-hosts">${icon('globe', 14)}${t('Hosts view')}</button>`)}${slot('hosts', hostsEmpty)}</div>
      <div class="cv-home-page" data-page="processes" hidden>${head('Processes', 'Processes with open connections in the last machine sample, busiest first when this page opens.', `<button class="cv-btn" type="button" data-cmd="view-machine">${icon('chip', 14)}${t('Machine view')}</button>`)}${slot('procs', procsEmpty)}</div>
      <div class="cv-home-page" data-page="setup" hidden>${head('Setup', 'The proxy\'s address, what happens to HTTPS and why, and how to point a program at it. Each block\'s button opens the settings in Preferences.')}<div class="cv-home-setup"></div></div>
    </div>`;
    this.back = q(this.root, '[data-back]');
    this.addr = q(this.root, '.cv-home-addr');
    richText(q(this.root, '.cv-home-hero-sub'), 'Set the proxy to {address} in a browser, a terminal or an app. HTTPS is opened once this computer trusts FormNET\'s root; until then it is tunnelled and counted by host and bytes.', { address: this.addr });
    this.copyBtn = q<HTMLButtonElement>(this.root, '[data-copy]');
    this.guide = setupGuide();
    q(this.root, '.cv-home-setup').appendChild(this.guide.el);
    this.root.addEventListener('click', (e) => {
      const target = e.target as HTMLElement;
      if (target.closest('[data-back]')) { this.dismiss(); return; }
      if (target.closest('[data-copy]')) { void this.copyAddress(); return; }
      const v = (target.closest('[data-v]') as HTMLElement | null)?.dataset.v as Page | undefined;
      if (v) { this.show(v); return; }
      const card = target.closest<HTMLElement>('[data-host]');
      if (card?.dataset.host) { this.d.store.update({ hostFilter: card.dataset.host, view: 'flows' }); this.dismiss(); }
    });
    host.appendChild(this.root);
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !this.root.hidden && !e.defaultPrevented) { e.preventDefault(); this.dismiss(); } });
    d.store.subscribe((s, prev) => {
      if (s.flows.size !== prev.flows.size) this.sync();
      this.counts(s);
      if (s.status !== prev.status) this.guide.patch(s.status);
      if (!this.root.hidden) this.fill(s, prev);
    });
    this.counts(d.store.get());
    this.guide.patch(d.store.get().status);
    onLanguage(() => { this.paint(); if (!this.root.hidden) this.fill(d.store.get()); });
    this.paint();
    this.sync();
  }

  private async copyAddress(): Promise<void> {
    const p = this.d.store.get().status?.proxy;
    const ok = p ? await copyText(`http://${p.addr}`) : false;
    const label = q(this.copyBtn, '.cv-home-label');
    setText(label, t(ok ? 'Copied' : 'Copy failed'));
    window.clearTimeout(this.copyTimer);
    this.copyTimer = window.setTimeout(() => setText(label, t('Copy proxy address')), 1200);
  }

  /** The chosen page shown, the others hidden, its nav item marked; attribute writes only. */
  private paint(): void {
    for (const n of Array.from(this.root.querySelectorAll<HTMLElement>('.cv-home-page'))) setHidden(n, n.dataset.page !== this.page);
    for (const n of Array.from(this.root.querySelectorAll<HTMLElement>('.cv-home-nav-item[data-v]'))) setClass(n, 'is-active', n.dataset.v === this.page);
    const h = new Date().getHours();
    setText(this.root.querySelector('[data-page="start"] .cv-home-title'), t(h < 12 ? 'Good morning' : h < 18 ? 'Good afternoon' : 'Good evening'));
  }

  /** The nav's counts, patched on every change whether Home shows or not: each sits in a fixed-width slot. */
  private counts(s: State): void {
    const put = (sel: string, n: number): void => { for (const c of Array.from(this.root.querySelectorAll(`${sel} .cv-home-count`))) setText(c, compact(n)); };
    put('[data-v="hosts"]', s.hosts.length);
    put('[data-v="processes"]', s.machine?.processes?.length ?? 0);
    put('[data-cmd="view-flows"]', s.flows.size);
  }

  /** The lists of the page shown take their order afresh (busiest first) the next time they fill. */
  private reorder(): void { for (const id of LISTS[this.page]) this.orders.delete(id); }

  /** The page shown, from the state: the proxy address and its lists, patched by key in the order they took when the
   *  page opened, so a card that stays keeps its node and its place; a new one joins at the end. */
  private fill(s: State, prev?: State): void {
    setText(this.addr, s.status?.proxy.addr ?? '127.0.0.1:8761');
    const lists = LISTS[this.page];
    if (!prev || s.hosts !== prev.hosts) for (const id of lists) if (id === 'start-hosts' || id === 'hosts') this.hostsInto(id, s, id === 'hosts' ? 0 : 8);
    if (!prev || s.machine !== prev.machine) for (const id of lists) if (id === 'start-procs' || id === 'procs') this.procsInto(id, s, id === 'procs' ? 0 : 8);
  }

  /** The keys in the order the list took when its page opened: a key that is gone keeps its card (marked gone) so
   *  nothing after it moves up; a new key joins at the end while there is room. An emptied list starts afresh. */
  private ordered<T>(id: ListId, sorted: T[], key: (x: T) => string, limit: number): Array<{ key: string; item: T | null }> {
    const now = new Map(sorted.map((x) => [key(x), x] as const));
    const keys = now.size ? [...(this.orders.get(id) ?? [])] : [];
    const known = new Set(keys);
    for (const k of now.keys()) if (!known.has(k) && (!limit || keys.length < limit)) { keys.push(k); known.add(k); }
    this.orders.set(id, keys);
    return keys.map((k) => ({ key: k, item: now.get(k) ?? null }));
  }

  private hostsInto(id: ListId, s: State, limit: number): void {
    const hosts = this.ordered(id, [...s.hosts].sort(byBytes), (x) => x.host, limit);
    patchList(q(this.root, `[data-list="${id}"]`), hosts, {
      key: (e) => e.key,
      create: (e) => { const c = el('<button class="cv-home-card" type="button"><span class="cv-home-card-name cv-host-name"></span><span class="cv-home-card-meta"></span></button>'); c.dataset.host = e.key; this.fillHost(c, e.key, e.item); return c; },
      update: (c, e) => this.fillHost(c, e.key, e.item),
    });
    setHidden(q(this.root, `[data-empty="${id}"]`), hosts.length > 0);
  }

  private procsInto(id: ListId, s: State, limit: number): void {
    const procs = this.ordered(id, [...(s.machine?.processes ?? [])].sort(busiest), (p) => String(p.pid), limit);
    patchList(q(this.root, `[data-list="${id}"]`), procs, {
      key: (e) => e.key,
      create: (e) => { const c = el('<div class="cv-home-card is-static"><span class="cv-home-card-name cv-proc-name"></span><span class="cv-home-card-meta"></span></div>'); this.fillProc(c, e.item); return c; },
      update: (c, e) => this.fillProc(c, e.item),
    });
    setHidden(q(this.root, `[data-empty="${id}"]`), procs.length > 0);
  }

  private fillHost(c: HTMLElement, host: string, x: HostSummary | null): void {
    setClass(c, 'is-gone', !x);
    setText(q(c, '.cv-home-card-name'), host);
    setText(q(c, '.cv-home-card-meta'), x ? `${tn(x.flows, '{n} flow', '{n} flows')} · ${bytes(x.bytes_sent + x.bytes_received)}` : t('No flows kept now'));
    setTitle(c, tf('Show the flows of {host}', { host }));
  }

  private fillProc(c: HTMLElement, p: Process | null): void {
    setClass(c, 'is-gone', !p);
    if (!p) { setText(q(c, '.cv-home-card-meta'), t('No connections now')); return; }
    setText(q(c, '.cv-home-card-name'), p.name);
    const moved = p.bytes_in + p.bytes_out;
    setText(q(c, '.cv-home-card-meta'), `${tn(p.connections, '{n} connection', '{n} connections')}${moved ? ` · ${bytes(moved)}` : ''}`);
    setTitle(c, p.path ? tf('pid {pid}, {path}', { pid: String(p.pid), path: p.path }) : tf('pid {pid}', { pid: String(p.pid) }));
  }

  get isShown(): boolean { return !this.root.hidden; }

  /** The brand mark: Home over whatever view is open, at `page` when given. Home opened this way stays while flows
   *  arrive, so its counts can be watched; only the Home shown over an empty recording leaves with the first flow. */
  show(page?: Page): void {
    if (page && page !== this.page) {
      this.page = page;
      this.reorder();
      this.paint();
      if (!this.root.hidden) this.fill(this.d.store.get());
    }
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
    if (n === 0 && this.lastCount > 0) this.dismissed = false;
    this.lastCount = n;
    if (n > 0) this.hadFlows = true;
    const on = this.forced || (!this.dismissed && n === 0 && (this.hadFlows ? this.d.afterClear() : this.d.autoShow()));
    if (on && this.root.hidden) { this.reorder(); this.paint(); this.fill(s); }
    setHidden(this.root, !on);
    setClass(this.back, 'is-off', n === 0 && !this.forced);
  }
}
