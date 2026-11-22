import { api } from '../api.ts';
import type { FlowFilter } from '../api.ts';
import type { State, Store } from '../state.ts';
import { visibleFlows } from '../state.ts';
import { el, q, setClass, setHidden, setText } from './dom';
import { icon } from './icons';

/** The part of the page's filter the server's export honours: the text filter and the host chosen in Hosts. */
export function exportFilter(s: State): FlowFilter {
  return { ...(s.filter ? { q: s.filter } : {}), ...(s.hostFilter ? { host: s.hostFilter } : {}) };
}

const VIEW_NAMES: Record<State['view'], string> = { flows: 'Flows', hosts: 'Hosts', machine: 'Machine', setup: 'Setup' };

/** The options bar under the view tabs (the family's second row): the filter and its host chip, Live, Clear Recording,
 *  Export HAR and the counts. Built once; the state patches it. */
export class OptionsBar {
  readonly filter: HTMLInputElement;
  private readonly chip: HTMLButtonElement;
  private readonly live: HTMLButtonElement;
  private readonly exportLink: HTMLAnchorElement;
  private readonly counts: HTMLElement;
  private readonly title: HTMLElement;

  constructor(host: HTMLElement, store: Store) {
    host.insertAdjacentHTML('beforeend',
      '<span class="cv-opt-title">Flows</span>' +
      '<div class="cv-opt cv-opt-filter"><input class="cv-opt-input filter" type="search" placeholder="Filter flows (press / )" aria-label="Filter flows" autocomplete="off" spellcheck="false">' +
      `<button class="cv-opt-chip" type="button" title="Remove the host filter" hidden><span class="cv-opt-chip-text"></span>${icon('close', 12)}</button></div>` +
      '<span class="cv-sep"></span>' +
      '<button class="cv-btn cv-opt-live" type="button" data-cmd="live" aria-pressed="true"></button>' +
      '<button class="cv-btn" type="button" data-cmd="clear-recording">Clear</button>' +
      '<a class="cv-btn cv-opt-export" download="formnet.har" title="HAR of the flows matching the text and host filter, as the server applies them; passwords, tokens and cookies are left out">Export HAR</a>' +
      '<button class="cv-btn cv-btn-ghost" type="button" data-cmd="export-har-secrets">Export HAR with secrets...</button>' +
      '<span class="cv-opt-counts" aria-live="polite"></span>');
    this.filter = q<HTMLInputElement>(host, 'input.filter');
    this.chip = q<HTMLButtonElement>(host, '.cv-opt-chip');
    this.live = q<HTMLButtonElement>(host, '.cv-opt-live');
    this.exportLink = q<HTMLAnchorElement>(host, '.cv-opt-export');
    this.counts = q(host, '.cv-opt-counts');
    this.title = q(host, '.cv-opt-title');
    this.filter.addEventListener('input', () => store.update({ filter: this.filter.value }));
    this.chip.addEventListener('click', () => store.update({ hostFilter: '' }));
    const render = (s: State): void => {
      setText(this.title, VIEW_NAMES[s.view]);
      if (this.filter.value !== s.filter && document.activeElement !== this.filter) this.filter.value = s.filter;
      setText(q(this.chip, '.cv-opt-chip-text'), s.hostFilter ? `host: ${s.hostFilter}` : '');
      setHidden(this.chip, !s.hostFilter);
      setText(this.live, s.live ? 'Live' : 'Paused');
      setClass(this.live, 'is-on', s.live);
      if (this.live.getAttribute('aria-pressed') !== String(s.live)) this.live.setAttribute('aria-pressed', String(s.live));
      const href = api.harURL(exportFilter(s));
      if (this.exportLink.getAttribute('href') !== href) this.exportLink.href = href;
      const shown = visibleFlows(s).length;
      setText(this.counts, shown === s.flows.size ? `${s.flows.size} flows` : `${shown} of ${s.flows.size} flows`);
    };
    store.subscribe(render);
    render(store.get());
  }

  focus(): void { this.filter.focus(); this.filter.select(); }
}

export const viewTab = (id: string, label: string): HTMLElement => el(`<button class="cv-tab" type="button" role="tab" data-view="${id}"><span class="cv-tab-name">${label}</span></button>`);
