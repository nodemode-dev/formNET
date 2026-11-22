import type { MenuDef } from './menu';
import { PREF_PAGES } from './settings-dialog';

/** The menus, one list for the in-window bar and the macOS menu bar (native-menu.ts builds from the same ids). */
export const menus = (): MenuDef[] => [
  { label: 'File', items: ['export-har', 'export-har-secrets', 'download-ca', '-', 'clear-recording'] },
  { label: 'Edit', items: ['undo', 'redo', '-', 'cut', 'copy', 'paste', 'select-all', '-', 'find', '-', { label: 'Preferences', items: ['settings', '-', ...PREF_PAGES.map(([id]) => `prefs-${id}`)] }] },
  { label: 'View', items: ['view-flows', 'view-hosts', 'view-machine', 'view-setup', '-', 'home', '-', 'live', '-', 'terminal', 'terminal-new', '-', 'zoom-in', 'zoom-out', 'zoom-reset', '-', 'theme-toggle', '-', 'menus-in-window', 'custom-title-bar'] },
  { label: 'Help', items: ['help', 'help-trust-root', '-', 'github', 'about'] },
];
