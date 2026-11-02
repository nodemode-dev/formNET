import { readChoice, setTheme } from './themes';
import { isLang, type Lang } from './i18n/langs';

/** Preferences that change how formnet works, kept on this device under one key. Every one acts on something real:
 *  the Home screen at launch and after Clear Recording, whether the Flows table follows new flows, how many rows it
 *  draws, the interface's text size and motion, and the theme pair Match System Appearance switches between. Server
 *  settings (the proxy, the body limit, the machine sampling) are the server's: Preferences > Capture reads and writes
 *  them through the API, so they are not in this record. */
export interface Prefs {
  language: Lang;
  /** Home covers the views at launch while the recording is empty. */
  autoShowHome: boolean;
  /** Home again when Clear Recording empties it. */
  homeOnClear: boolean;
  /** New flows keep the table following (Live) when the app starts. */
  liveFollows: boolean;
  /** Rows the Flows table draws (the newest ones matching the filter). */
  maxRows: number;
  /** Clear Recording asks first. */
  confirmClear: boolean;
  showTooltips: boolean;
  animations: boolean;
  /** How long a notice stays, in seconds; 0 is automatic (errors longer). */
  noticeSeconds: number;
  uiFontSize: UiFontSize;
  themeAuto: boolean;
  themeLight: string;
  themeDark: string;
  favouriteThemes: string[];
  /** Preferences > Machine: the order the process list starts in. */
  machineSort: MachineSort;
  /** Each dialog's last control values (Dialog.remember). */
  dialogValues: Record<string, Record<string, DialogValue>>;
}

export type DialogValue = string | boolean;
export type MachineSort = 'connections' | 'bytes' | 'name';
export const MACHINE_SORTS: ReadonlyArray<readonly [MachineSort, string]> = [['connections', 'Most connections first'], ['bytes', 'Most bytes first'], ['name', 'By name']];
const isMachineSort = (v: unknown): v is MachineSort => MACHINE_SORTS.some(([k]) => k === v);
export type UiFontSize = 'small' | 'default' | 'large' | 'larger';
export const UI_FONT_SIZES: ReadonlyArray<readonly [UiFontSize, string]> = [['small', 'Small'], ['default', 'Default'], ['large', 'Large'], ['larger', 'Larger']];
const UI_FONT_SCALE: Record<UiFontSize, number> = { small: 0.92, default: 1, large: 1.1, larger: 1.2 };

export const DEFAULT_PREFS: Prefs = { language: 'en', autoShowHome: true, homeOnClear: false, liveFollows: true, maxRows: 500, confirmClear: true, showTooltips: true, animations: true, noticeSeconds: 0, uiFontSize: 'default', themeAuto: false, themeLight: 'light', themeDark: 'studio', favouriteThemes: [], machineSort: 'connections', dialogValues: {} };

/** Every key formnet writes to localStorage starts with this, so Reset All Preferences can find them. */
export const PREFIX = 'formnet.';
const KEY = `${PREFIX}prefs`;

const num = (v: unknown, lo: number, hi: number, d: number): number => (typeof v === 'number' && Number.isFinite(v) ? Math.min(hi, Math.max(lo, v)) : d);
const flag = (v: unknown, d: boolean): boolean => (typeof v === 'boolean' ? v : d);
const plain = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const isFontSize = (v: unknown): v is UiFontSize => typeof v === 'string' && v in UI_FONT_SCALE;

function cleanDialogValues(v: unknown): Record<string, Record<string, DialogValue>> {
  const out: Record<string, Record<string, DialogValue>> = {};
  if (!plain(v)) return out;
  for (const [d, vals] of Object.entries(v).slice(0, 200)) {
    if (!plain(vals)) continue;
    const row: Record<string, DialogValue> = {};
    for (const [k, x] of Object.entries(vals).slice(0, 80)) if ((typeof x === 'string' && x.length <= 2000) || typeof x === 'boolean') row[k.slice(0, 120)] = x;
    out[d.slice(0, 120)] = row;
  }
  return out;
}

/** The system's language when it is one formnet has; English otherwise. */
function systemLang(): Lang {
  const tags = navigator.languages?.length ? navigator.languages : [navigator.language];
  for (const tag of tags) {
    if (isLang(tag)) return tag;
    const base = tag.split('-')[0];
    if (isLang(base)) return base;
  }
  return 'en';
}

/** Nothing stored is trusted: a missing, wrongly typed or out-of-range value falls back to the default or is clamped. */
export function loadPrefs(): Prefs {
  try {
    const r = JSON.parse(localStorage.getItem(KEY) ?? '{}') as Partial<Record<keyof Prefs, unknown>>;
    const d = DEFAULT_PREFS;
    return {
      language: isLang(r.language) ? r.language : systemLang(),
      autoShowHome: flag(r.autoShowHome, d.autoShowHome),
      homeOnClear: flag(r.homeOnClear, d.homeOnClear),
      liveFollows: flag(r.liveFollows, d.liveFollows),
      maxRows: Math.round(num(r.maxRows, 100, 5000, d.maxRows)),
      confirmClear: flag(r.confirmClear, d.confirmClear),
      showTooltips: flag(r.showTooltips, d.showTooltips),
      animations: flag(r.animations, d.animations),
      noticeSeconds: Math.round(num(r.noticeSeconds, 0, 30, d.noticeSeconds)),
      uiFontSize: isFontSize(r.uiFontSize) ? r.uiFontSize : d.uiFontSize,
      themeAuto: flag(r.themeAuto, d.themeAuto),
      themeLight: typeof r.themeLight === 'string' ? r.themeLight : d.themeLight,
      themeDark: typeof r.themeDark === 'string' ? r.themeDark : d.themeDark,
      favouriteThemes: Array.isArray(r.favouriteThemes) ? r.favouriteThemes.filter((x): x is string => typeof x === 'string').slice(0, 60) : [],
      machineSort: isMachineSort(r.machineSort) ? r.machineSort : d.machineSort,
      dialogValues: cleanDialogValues(r.dialogValues),
    };
  } catch { return { ...DEFAULT_PREFS }; }
}

export function savePrefs(p: Prefs): void {
  try { localStorage.setItem(KEY, JSON.stringify(p)); } catch { /* kept for this session only */ }
}

/** Changes some preferences and keeps them: what a dialog remembers goes through here. */
export function updatePrefs(patch: Partial<Prefs>): Prefs {
  const p = { ...loadPrefs(), ...patch };
  savePrefs(p);
  return p;
}

/** A value a dialog kept under `name` (see `Dialog.remember`). */
export const recalled = (dialog: string, name: string): DialogValue | undefined => loadPrefs().dialogValues[dialog]?.[name];

/** Keeps `value` for a dialog under `name`; nothing is written when it is already what is kept. */
export function remembered(dialog: string, name: string, value: DialogValue): void {
  const cur = loadPrefs().dialogValues;
  if (cur[dialog]?.[name] === value) return;
  updatePrefs({ dialogValues: { ...cur, [dialog]: { ...cur[dialog], [name]: value } } });
}

let sizedK = 0;
/** Scales the interface's font-size tokens (--fs-9 to --fs-18) to the chosen size. */
export function applyUiFontSize(p: Prefs): void {
  const k = UI_FONT_SCALE[p.uiFontSize], s = document.documentElement.style;
  for (const n of [9, 10, 11, 12, 13, 14, 16, 18]) { if (k === 1) s.removeProperty(`--fs-${n}`); else s.setProperty(`--fs-${n}`, `${Math.round(n * k * 10) / 10}px`); }
  if (k !== sizedK) { const first = sizedK === 0; sizedK = k; if (!first) document.dispatchEvent(new CustomEvent('nm:ui-settings-changed', { detail: { key: 'fontSize' } })); }
}

/** Interface scale: the whole window zoomed (CSS zoom on the root in the browser, the web view's own zoom in the
 *  desktop app), kept on this device; View > Zoom In and Zoom Out step through the same list. */
export const UI_SCALES: ReadonlyArray<readonly [string, string]> = [['auto', 'Auto (follow the display)'], ['0.8', '80%'], ['0.9', '90%'], ['1', '100%'], ['1.1', '110%'], ['1.25', '125%'], ['1.5', '150%'], ['1.75', '175%'], ['2', '200%']];
export const UI_SCALE_KEY = `${PREFIX}uiZoom`;
export const UI_SCALE_EVENT = 'formnet:ui-zoom';
export function uiScaleChoice(): string {
  try { const v = localStorage.getItem(UI_SCALE_KEY) ?? '1'; return UI_SCALES.some(([k]) => k === v) ? v : '1'; } catch { return '1'; }
}
let shellZoom = 1;
/** The zoom the desktop window already holds (its shell reports it): a zoomed web view reports a larger pixel ratio, which Auto divides out. */
export function setShellZoom(zoom: number): void { shellZoom = zoom > 0 ? zoom : 1; }
/** What Auto means: a display the system already scales is left at 100%; one reported at 1 that is 3000 pixels wide or more gets 150%, and from 3800 pixels 200%. */
export function autoUiScale(): number {
  const ratio = (window.devicePixelRatio || 1) / shellZoom, wide = screen.width || 0;
  if (ratio > 1.01) return 1;
  return wide >= 3800 ? 2 : wide >= 3000 ? 1.5 : 1;
}
export function setUiScale(v: string): void {
  try { localStorage.setItem(UI_SCALE_KEY, v); } catch { /* kept for this session only */ }
  window.dispatchEvent(new Event(UI_SCALE_EVENT));
}

let systemScheme: MediaQueryList | null = null;
let systemSchemeOn: (() => void) | null = null;
/** Interface > Match System Appearance: the light or dark theme of the pair is set now and again when the system's appearance changes. */
export function followSystemTheme(p: Prefs): void {
  if (typeof matchMedia !== 'function') return;
  if (!systemScheme) systemScheme = matchMedia('(prefers-color-scheme: dark)');
  if (systemSchemeOn) { systemScheme.removeEventListener('change', systemSchemeOn); systemSchemeOn = null; }
  if (!p.themeAuto) return;
  const pick = (): void => { const id = systemScheme?.matches ? p.themeDark : p.themeLight; if (readChoice().theme !== id) setTheme(id); };
  systemSchemeOn = pick;
  systemScheme.addEventListener('change', pick);
  pick();
}

/** Puts the interface-side preferences into effect; run once at start and after every change. */
export function applyUiPrefs(p: Prefs): void {
  applyUiFontSize(p);
  document.body.classList.toggle('animations-disabled', !p.animations);
  followSystemTheme(p);
}
