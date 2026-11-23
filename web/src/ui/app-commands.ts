import { api } from '../api.ts';
import type { Store, View } from '../state.ts';
import { Commands } from './commands';
import { confirmAction } from './dialogs';
import { isMac } from './dom';
import { MENUS_IN_WINDOW_KEY, menusInWindow, wantsNativeMenus } from './native-menu';
import { wantsCustomTitleBar } from './caption-buttons';
import { exportFilter } from './options-bar';
import { UI_SCALES, autoUiScale, loadPrefs, setShellZoom, setUiScale, uiScaleChoice } from './prefs';
import { PREF_PAGES, type SettingsDialog } from './settings-dialog';
import { readChoice, setTheme } from './themes';
import { toast } from './toast';

export interface CommandDeps {
  store: Store;
  settings: SettingsDialog;
  home: { show(): void; dismiss(): void };
  focusFilter: () => void;
  captionButtons: { sync(): void };
}

const isTauri = (): boolean => '__TAURI_INTERNALS__' in window;
const store = (key: string, value: string): void => { try { localStorage.setItem(key, value); } catch { /* this session only */ } };

function download(href: string, name: string): void {
  const a = document.createElement('a');
  a.href = href;
  a.download = name;
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/** A text edit through the browser's own command; the macOS menu routes these to the shell's text_edit instead. */
const edit = (name: string) => (): void => { document.execCommand(name); };

const SCALES = UI_SCALES.map(([k]) => k).filter((k) => k !== 'auto').map(Number);
const uiZoom = (): number => { const v = uiScaleChoice(); return v === 'auto' ? autoUiScale() : Number(v); };
const cssZoom = (zoom: number): void => { const z = zoom === 1 ? '' : String(zoom); if (document.documentElement.style.zoom !== z) { document.documentElement.style.zoom = z; window.dispatchEvent(new Event('resize')); } };
let shellZoomed: Promise<void> = Promise.resolve();
/** Interface scale: CSS zoom in a browser; in the desktop app the web view's own zoom (the shell's ui_zoom). */
export function applyUiZoom(): void {
  if (!isTauri()) { cssZoom(uiZoom()); return; }
  shellZoomed = shellZoomed.then(async () => {
    try {
      const { invoke } = await import('@tauri-apps/api/core');
      const held = Number(await invoke('ui_zoom', { factor: null })) || 1;
      setShellZoom(held);
      const want = uiZoom();
      if (want !== held) setShellZoom(Number(await invoke('ui_zoom', { factor: want })) || want);
      cssZoom(1);
    } catch { setShellZoom(1); cssZoom(uiZoom()); }
  });
}
const stepZoom = (by: number): void => {
  const at = SCALES.indexOf(uiZoom()), next = SCALES[Math.max(0, Math.min(SCALES.length - 1, (at < 0 ? SCALES.indexOf(1) : at) + by))];
  setUiScale(String(next));
};

/** Every command the menus, the keys, the title bar and Home run, by id. */
export function registerCommands(d: CommandDeps): Commands {
  const c = new Commands();
  const { store: st } = d;
  const view = (id: View) => (): void => { st.update({ view: id }); d.home.dismiss(); };
  const setup = (card: string) => (): void => { st.update({ view: 'setup' }); d.home.dismiss(); requestAnimationFrame(() => document.getElementById(card)?.scrollIntoView({ block: 'start' })); };
  const clear = (): void => { api.clearFlows().catch((err: Error) => st.update({ notice: err.message })); };
  c.add(
    { id: 'export-har', label: 'Export HAR...', keys: ['Mod+S'], run: () => download(api.harURL(exportFilter(st.get())), 'formnet.har') },
    { id: 'download-ca', label: 'Download Root Certificate...', run: () => download(api.caURL(), 'formnet-ca.pem') },
    { id: 'clear-recording', label: 'Clear Recording', keys: ['Mod+K'], enabled: () => st.get().flows.size > 0, run: () => {
      if (!loadPrefs().confirmClear) { clear(); return; }
      void confirmAction('Clear the recording?', 'Every flow kept so far is dropped. Export a HAR first if you want to keep them.', 'Clear', true).then((ok) => { if (ok) clear(); });
    } },
    { id: 'undo', label: 'Undo', keys: ['Mod+Z'], run: edit('undo') },
    { id: 'redo', label: 'Redo', keys: ['Shift+Mod+Z'], run: edit('redo') },
    { id: 'cut', label: 'Cut', keys: ['Mod+X'], run: edit('cut') },
    { id: 'copy', label: 'Copy', keys: ['Mod+C'], run: edit('copy') },
    { id: 'paste', label: 'Paste', keys: ['Mod+V'], run: edit('paste') },
    { id: 'select-all', label: 'Select All', keys: ['Mod+A'], run: edit('selectAll') },
    { id: 'find', label: 'Filter...', keys: ['Mod+F'], run: () => { st.update({ view: 'flows' }); d.home.dismiss(); d.focusFilter(); } },
    { id: 'settings', label: 'Preferences...', keys: ['Mod+,'], run: () => d.settings.show() },
    ...PREF_PAGES.map(([id, label, section]) => ({ id: `prefs-${id}`, label, run: () => d.settings.show(section) })),
    { id: 'view-flows', label: 'Flows', keys: ['Mod+1'], run: view('flows') },
    { id: 'view-hosts', label: 'Hosts', keys: ['Mod+2'], run: view('hosts') },
    { id: 'view-machine', label: 'Machine', keys: ['Mod+3'], run: view('machine') },
    { id: 'view-setup', label: 'Setup', keys: ['Mod+4'], run: view('setup') },
    { id: 'home', label: 'Home', run: () => d.home.show() },
    { id: 'live', label: 'Live', checked: () => st.get().live, run: () => st.update((s) => ({ live: !s.live })) },
    { id: 'zoom-in', label: 'Zoom In', keys: ['Mod+='], enabled: () => uiZoom() < SCALES[SCALES.length - 1], run: () => stepZoom(1) },
    { id: 'zoom-out', label: 'Zoom Out', keys: ['Mod+-'], enabled: () => uiZoom() > SCALES[0], run: () => stepZoom(-1) },
    { id: 'zoom-reset', label: 'Actual Size', keys: ['Mod+0'], enabled: () => uiZoom() !== 1, run: () => setUiScale('1') },
    { id: 'theme-toggle', label: 'Light or Dark', keys: ['Shift+Mod+L'], run: () => setTheme(readChoice().theme === 'light' ? 'studio' : 'light') },
    { id: 'menus-in-window', label: 'Menus in Window', enabled: () => isTauri() && isMac, checked: menusInWindow, run: () => {
      const on = !menusInWindow();
      store(MENUS_IN_WINDOW_KEY, on ? '1' : '0');
      toast(on ? 'The menus stay in the window from the next start.' : 'The menus go to the menu bar from the next start.');
    } },
    { id: 'custom-title-bar', label: 'Custom Title Bar', enabled: wantsCustomTitleBar, checked: () => { try { return localStorage.getItem('formnet.customTitleBar') === '1'; } catch { return false; } }, run: () => {
      let on = false;
      try { on = localStorage.getItem('formnet.customTitleBar') !== '1'; } catch { on = true; }
      store('formnet.customTitleBar', on ? '1' : '0');
      d.captionButtons.sync();
    } },
    { id: 'help', label: 'Proxy Setup...', keys: ['F1'], run: setup('proxy-card') },
    { id: 'help-trust-root', label: 'Trust the Root Certificate...', run: setup('ca-card') },
    { id: 'github', label: 'formNET on GitHub', run: () => { window.open('https://github.com/nodemode-dev/formNET', '_blank', 'noopener'); } },
    { id: 'about', label: 'About formNET', run: () => { const s = st.get().status; toast(`formNET ${s?.version ?? ''} by NodeAgentc, Inc.: what a website, an app or the whole machine actually does on the network.`); } },
  );
  void wantsNativeMenus;
  return c;
}
