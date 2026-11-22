import type { FlowFilter } from '../api.ts';
import { dataApi, type FlowParams } from '../api-data.ts';
import { flowFilters, parseTyped, TYPED_SAMPLES, typedForms, type FlowQuery } from '../filters.ts';
import type { State, Store } from '../state.ts';
import { visibleFlows } from '../state.ts';
import { el, q, setClass, setHidden, setText, setTitle } from './dom';
import { FilterBar } from './flow-filters';
import { onLanguage, t, tn } from './i18n';
import { icon } from './icons';
import { SessionChip } from './session-dialog';

/** The part of the page's filter the server's export honours: the text filter and the host chosen in Hosts. */
export function exportFilter(s: State): FlowFilter {
  return { ...(s.filter ? { q: s.filter } : {}), ...(s.hostFilter ? { host: s.hostFilter } : {}) };
}

/** Every filter of the Flows list as the server's list and export routes take them. */
export const exportParams = (s: State): FlowParams => flowFilters.params(s.filter, s.hostFilter);

const VIEW_NAMES: Record<State['view'], string> = { flows: 'Flows', hosts: 'Hosts', machine: 'Machine', setup: 'Setup' };

/** The options bar under the view tabs (the family's second row): the text box with its body switch and host chip,
 *  the quick filter chips and More filters, Live, Clear Recording, Export HAR, the counts in a fixed slot, and last the
 *  open session. Built once; the state and the filters patch it. `query` is the Flows list's server query, whose answer
 *  says when a body search stopped early. */
export class OptionsBar {
  readonly filter: HTMLInputElement;
  readonly filters: FilterBar;
  private readonly chip: HTMLButtonElement;
  private readonly live: HTMLButtonElement;
  private readonly exportLink: HTMLAnchorElement;
  private readonly counts: HTMLElement;
  private readonly title: HTMLElement;

  constructor(host: HTMLElement, store: Store, query: FlowQuery | null = null) {
    host.insertAdjacentHTML('beforeend',
      `<span class="cv-opt-title">${t('Flows')}</span>` +
      `<div class="cv-opt cv-opt-filter"><input class="cv-opt-input filter" type="search" placeholder="${t('Filter flows (press / )')}" aria-label="${t('Filter flows')}" autocomplete="off" spellcheck="false">` +
      `<button class="cv-opt-chip" type="button" title="${t('Remove the host filter')}" hidden><span class="cv-opt-chip-text"></span>${icon('close', 12)}</button></div>` +
      '<span class="cv-sep"></span>' +
      '<button class="cv-btn cv-opt-live" type="button" data-cmd="live" aria-pressed="true"></button>' +
      `<button class="cv-btn" type="button" data-cmd="clear-recording">${t('Clear')}</button>` +
      `<a class="cv-btn cv-opt-export" download="formnet.har" title="${t('HAR of the flows matching the filters, as the server applies them; passwords, tokens and cookies are left out')}">${t('Export HAR')}</a>` +
      `<button class="cv-btn cv-btn-ghost" type="button" data-cmd="export-har-secrets">${t('Export HAR with secrets...')}</button>` +
      '<span class="fn-opt-end"><span class="cv-opt-counts" aria-live="polite"></span></span>');
    this.filter = q<HTMLInputElement>(host, 'input.filter');
    this.chip = q<HTMLButtonElement>(host, '.cv-opt-chip');
    this.live = q<HTMLButtonElement>(host, '.cv-opt-live');
    this.exportLink = q<HTMLAnchorElement>(host, '.cv-opt-export');
    this.counts = q(host, '.cv-opt-counts');
    this.title = q(host, '.cv-opt-title');
    this.filters = new FilterBar(store, query);
    this.filter.after(this.filters.bodyToggle, this.filters.cutMark);
    q(host, '.cv-opt-filter').after(this.filters.chips, this.filters.more);
    q(host, '.fn-opt-end').append(new SessionChip(store).el);
    this.filter.addEventListener('input', () => store.update({ filter: this.filter.value }));
    this.chip.addEventListener('click', () => store.update({ hostFilter: '' }));
    const words = (): void => {
      const body = flowFilters.get().body;
      const ph = t(body ? 'Find text in bodies (press / )' : 'Filter flows (press / )');
      if (this.filter.placeholder !== ph) this.filter.placeholder = ph;
      this.exportLink.title = t('HAR of the flows matching the filters, as the server applies them; passwords, tokens and cookies are left out');
      hint(store.get().filter);
    };
    const hint = (text: string): void => {
      const bad = parseTyped(text).invalid;
      if (bad.length) { if (this.filter.getAttribute('aria-invalid') !== 'true') this.filter.setAttribute('aria-invalid', 'true'); }
      else if (this.filter.hasAttribute('aria-invalid')) this.filter.removeAttribute('aria-invalid');
      setTitle(this.filter, bad.length
        ? t('{token} is not a filter FormNET can read. It takes these forms: {forms}.').replace('{token}', bad[0] ?? '').replace('{forms}', typedForms(bad[0] ?? ''))
        : t('Plain text, or filters such as {samples}. A - in front of a filter inverts it.').replace('{samples}', TYPED_SAMPLES));
    };
    words();
    onLanguage(words);
    flowFilters.subscribe(() => { words(); render(store.get()); });
    const render = (s: State): void => {
      setText(this.title, t(VIEW_NAMES[s.view]));
      hint(s.filter);
      if (this.filter.value !== s.filter && document.activeElement !== this.filter) this.filter.value = s.filter;
      setText(q(this.chip, '.cv-opt-chip-text'), s.hostFilter ? t('host: {host}').replace('{host}', s.hostFilter) : '');
      setHidden(this.chip, !s.hostFilter);
      setText(this.live, t(s.live ? 'Live' : 'Paused'));
      setClass(this.live, 'is-on', s.live);
      if (this.live.getAttribute('aria-pressed') !== String(s.live)) this.live.setAttribute('aria-pressed', String(s.live));
      const href = dataApi.harURL(exportParams(s));
      if (this.exportLink.getAttribute('href') !== href) this.exportLink.href = href;
      const shown = visibleFlows(s).length;
      setText(this.counts, shown === s.flows.size ? tn(s.flows.size, '{n} flow', '{n} flows') : t('{shown} of {total} flows').replace('{shown}', String(shown)).replace('{total}', String(s.flows.size)));
    };
    store.subscribe(render);
    render(store.get());
  }

  focus(): void { this.filter.focus(); this.filter.select(); }
}

export const viewTab = (id: string, label: string): HTMLElement => el(`<button class="cv-tab" type="button" role="tab" data-view="${id}"><span class="cv-tab-name">${t(label)}</span></button>`);
