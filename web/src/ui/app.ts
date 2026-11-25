import type { Store, View } from '../state.ts';
import { startFlowQuery } from '../filters.ts';
import { connectLive } from '../live.ts';
import { flowDetail } from './flow-detail.ts';
import { flowsView } from './flows-view.ts';
import { hostsView } from './hosts-view.ts';
import { machineView } from './machine-view.ts';
import { setupView } from './setup-view.ts';
import { applyUiZoom, registerCommands } from './app-commands';
import { menus } from './app-menus';
import { Dock } from '../dock/dock';
import { CaptionButtons } from './caption-buttons';
import { comboOf } from './commands';
import { anyDialogOpen } from './dialogs';
import { formnetDock } from './dock-adapter';
import { mountOnce, q, revealApp, setClass, setDisabled, setTitle } from './dom';
import { HomeScreen } from './home';
import { icon } from './icons';
import { onLanguage, startPageTranslator, t } from './i18n';
import { MenuBar } from './menu';
import { NativeMenus, wantsNativeMenus } from './native-menu';
import { installNumberFields } from './number-fields';
import { OptionsBar, viewTab } from './options-bar';
import { UI_SCALE_EVENT, applyUiPrefs, loadPrefs, type Prefs } from './prefs';
import { SettingsDialog } from './settings-dialog';
import { StatusBar } from './status';
import { TitleTools } from './titlebar-tools';
import { toast } from './toast';

const VIEWS: ReadonlyArray<readonly [View, string]> = [['flows', 'Flows'], ['hosts', 'Hosts'], ['machine', 'Machine'], ['setup', 'Setup']];

const shell = (): string => `<div class="cv-app">
<header class="cv-titlebar">
<nav class="cv-menubar" aria-label="${t('Main menu')}"></nav>
<div class="cv-tb-doc"></div>
<div class="cv-tb-app">FormNET 2026</div>
<div class="cv-zoom"><button class="cv-tt-btn" type="button" data-cmd="settings">${icon('settings', 16)}</button></div>
</header>
<div class="cv-main">
<div class="cv-doc-tabs"><div class="cv-brand">${icon('network', 18)}</div><div class="cv-tabs"><div class="cv-tab-strip" role="tablist"></div></div></div>
<div class="cv-options"></div>
<main class="cv-stage"></main>
</div>
<footer class="cv-status"></footer>
</div>`;

/** Builds the window once around the store: the family's shell (title bar, menus, view tabs, options bar, status bar,
 *  Home) with FormNET's views on the stage, then connects the live data and reveals the page. */
export function startApp(store: Store, onLost: () => void): void {
  const appHost = document.getElementById('app');
  if (!appHost) return;
  mountOnce(appHost, shell());
  const root = q(appHost, '.cv-app');
  const stage = q(root, '.cv-stage');
  const strip = q(root, '.cv-tab-strip');
  const tabs = VIEWS.map(([id, label]) => { const b = viewTab(id, label); strip.appendChild(b); return b; });
  strip.addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest<HTMLElement>('.cv-tab[data-view]');
    if (b?.dataset.view) commands.run(`view-${b.dataset.view}`);
  });
  const query = startFlowQuery(store, () => store.update((s) => ({ filterEpoch: s.filterEpoch + 1 })));
  const options = new OptionsBar(q(root, '.cv-options'), store, query);
  stage.append(flowsView(store), hostsView(store), machineView(store), setupView(store), flowDetail(store));
  const home = new HomeScreen(stage, { store, autoShow: () => loadPrefs().autoShowHome, afterClear: () => loadPrefs().homeOnClear });
  const brand = q(root, '.cv-brand');
  brand.setAttribute('role', 'button');
  brand.setAttribute('tabindex', '0');
  const paintBrand = (): void => { brand.setAttribute('aria-label', t('Home')); setTitle(brand, t('Home')); };
  paintBrand();
  onLanguage(paintBrand);
  brand.addEventListener('click', () => home.show());
  brand.addEventListener('keydown', (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); home.show(); } });
  const status = new StatusBar(q(root, '.cv-status'), store);
  const captionButtons = new CaptionButtons(q(root, '.cv-titlebar'), () => { try { return localStorage.getItem('formnet.customTitleBar') === '1'; } catch { return false; } });
  const onPrefs = (p: Prefs): void => { applyUiPrefs(p); store.update({ maxRows: p.maxRows, machineSort: p.machineSort }); home.sync(); };
  const settings = new SettingsDialog({ store, onPrefs });
  // The family's terminal dock: built once, hidden, its row in the grid above the status bar (styles/terminal.css).
  const dock: Dock<'antigravity'> = new Dock(formnetDock({ openSettings: () => settings.show('Agents and Scripts'), layerOpen: anyDialogOpen, changed: () => dock.refresh() }));
  dock.mount(root, q(root, '.cv-status'));
  const commands = registerCommands({ store, settings, home, focusFilter: () => options.focus(), captionButtons, dock });
  const titleTools = new TitleTools(q(root, '.cv-titlebar'), commands, () => void dock.toggleKind('shell'));
  const menubar = new MenuBar(q(root, '.cv-menubar'), commands, menus());
  let native: NativeMenus | null = null;
  if (wantsNativeMenus()) {
    const menu = new NativeMenus(commands, menus);
    native = menu;
    void menu.build().catch(() => undefined);
    store.subscribe(() => menu.refresh());
    onLanguage(() => { void menu.build().catch(() => undefined); });
  }
  dock.onTab = () => { titleTools.syncDock(dock.isShowing('shell')); native?.refresh(); };
  dock.onTab();
  // The proxy address is what the window "has open": it reads at the left of the title row where the menus are the system's.
  const doc = q(root, '.cv-tb-doc');
  const paintDoc = (): void => { const p = store.get().status?.proxy; const text = p ? t('Proxy {addr}').replace('{addr}', p.addr) : ''; if (doc.textContent !== text) doc.textContent = text; };
  // Every button[data-cmd] runs its command; a chrome pass keeps its disabled state and tooltip in step.
  let chromeFrame = 0;
  const syncChrome = (): void => {
    if (chromeFrame) return;
    chromeFrame = requestAnimationFrame(() => {
      chromeFrame = 0;
      for (const b of Array.from(root.querySelectorAll<HTMLButtonElement>('button[data-cmd]'))) {
        const cmd = commands.get(b.dataset.cmd ?? '');
        if (!cmd) continue;
        setDisabled(b, !commands.enabled(cmd));
        const label = t(commands.label(cmd)).replace(/\.\.\.$/, ''), keys = commands.keyLabel(cmd);
        setTitle(b, keys ? t('{label} ({keys})').replace('{label}', label).replace('{keys}', keys) : label);
        if (b.classList.contains('cv-tt-btn') && b.getAttribute('aria-label') !== label) b.setAttribute('aria-label', label);
      }
      for (const b of tabs) { const on = b.dataset.view === store.get().view; setClass(b, 'is-active', on); if (b.getAttribute('aria-selected') !== String(on)) b.setAttribute('aria-selected', String(on)); }
      paintDoc();
    });
  };
  store.subscribe(syncChrome);
  syncChrome();
  root.addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest<HTMLButtonElement>('button[data-cmd]');
    if (b && root.contains(b) && !b.disabled) commands.run(b.dataset.cmd ?? '');
  });
  // A dialog's button (Preferences > Capture) runs a command by name without reaching into the shell.
  document.addEventListener('formnet:run', (e) => { const id = (e as CustomEvent<string>).detail; if (typeof id === 'string') commands.run(id); });
  // A notice is an error from the data layer: a toast, and the status bar's hint slot for six seconds.
  store.subscribe((s, prev) => {
    if (s.notice && s.notice !== prev.notice) { toast(s.notice, 'error'); status.flashError(s.notice); store.update({ notice: '' }); }
  });
  window.addEventListener('keydown', (e) => {
    if (anyDialogOpen() || e.defaultPrevented) return;
    if (menubar.isOpen) { if (e.key === 'Escape') { e.preventDefault(); menubar.close(); } return; }
    const target = e.target instanceof Element ? e.target : null;
    const inField = !!target?.closest('input, textarea, select, [contenteditable="true"]');
    if (e.key === '/' && !inField && !e.ctrlKey && !e.metaKey && !e.altKey) { e.preventDefault(); commands.run('find'); return; }
    if (inField) return;
    const cmd = commands.forEvent(e);
    if (!cmd || /^(undo|redo|cut|copy|paste|select-all)$/.test(cmd.id)) return;
    e.preventDefault();
    if (!e.repeat || /^(zoom-in|zoom-out)$/.test(cmd.id)) commands.run(cmd.id);
  });
  void comboOf;
  installNumberFields();
  const prefs = loadPrefs();
  store.update({ maxRows: prefs.maxRows, machineSort: prefs.machineSort, live: prefs.liveFollows });
  applyUiPrefs(prefs);
  applyUiZoom();
  window.addEventListener(UI_SCALE_EVENT, applyUiZoom);
  document.title = 'FormNET';
  if ('__TAURI_INTERNALS__' in window) void import('@tauri-apps/api/core').then((m) => m.invoke('set_window_title', { title: 'FormNET' })).catch(() => undefined);
  connectLive(store, onLost);
  startPageTranslator();
  const fonts = 'fonts' in document ? document.fonts.ready.then(() => undefined) : Promise.resolve();
  void Promise.race([fonts, new Promise<void>((r) => setTimeout(r, 2500))]).then(() => revealApp());
}
