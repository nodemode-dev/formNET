import type { Store } from '../state';
import { Dialog, confirmAction } from './dialogs';
import { el, q, setClass, setHidden, setText, setValue } from './dom';
import { icon } from './icons';
import { languages, onLanguage, setLanguage, t, type Lang } from './i18n';
import { checkRow, labelText, selectRow, sliderRow, tf } from './panels/controls';
import { AgentsSection } from './panels/agents';
import { HttpsSection, PassthroughSection, ProxySection, RootSection } from './panels/capture';
import { StorageSection } from './panels/storage';
import { BreakpointSettingsSection, RulesSection } from './panels/rules';
import { ThrottleSection } from './panels/throttle';
import { DevicesSection } from './panels/devices';
import type { RuleActionType } from '../types-traffic';
import { SystemProxySection } from './panels/system-proxy';
import { MACHINE_SORTS, PREFIX, UI_FONT_SIZES, UI_SCALES, loadPrefs, savePrefs, setUiScale, uiScaleChoice, type MachineSort, type Prefs, type UiFontSize } from './prefs';
import { STORAGE_KEY, THEMES, readChoice, setAccent, setIntensity, setTheme, themeSwatchesHtml } from './themes';

/** Edit > Preferences (Mod+,): the app builds one at start and shows it from the settings command. */
export interface SettingsDeps { store: Store; onPrefs: (p: Prefs) => void }

/** [command id suffix, menu label, section name]; the section name is the English identity (data-section). */
export const PREF_PAGES: ReadonlyArray<readonly [string, string, string]> = [
  ['general', 'General...', 'General'],
  ['interface', 'Interface...', 'Interface'],
  ['capture', 'Capture...', 'Capture'],
  ['machine', 'Machine...', 'Machine'],
  ['rules', 'Rules...', 'Rules'],
  ['throttle', 'Throttle...', 'Throttle'],
  ['devices', 'Other Devices...', 'Other Devices'],
  ['agents', 'Agents and Scripts...', 'Agents and Scripts'],
];

const ORDER = PREF_PAGES.map(([, , name]) => name);
const RAIL_ICONS: Record<string, string> = { General: 'pfGeneral', Interface: 'pfInterface', Capture: 'pfPerformance', Machine: 'pfScratch', Rules: 'rules', Throttle: 'throttle', 'Other Devices': 'devices', 'Agents and Scripts': 'pfPlugins' };
const RAIL_KEY = `${PREFIX}prefsRailCollapsed`;

/** Preferences: a floating rail of section tabs beside one scrolling pane of foldable section cards, a wire from the
 *  active tab to the pane, and Done. Every row is built once here and patched in place by show() and sync(); every
 *  change goes through change(), which saves and tells the app. */
export class SettingsDialog extends Dialog {
  private readonly grid = el('<div class="nm-theme-grid" role="group" aria-label="Theme"></div>');
  private readonly accent = el<HTMLInputElement>('<input class="cv-input" type="color" aria-label="Accent color">');
  private readonly nav = el('<div class="cv-prefs-nav" role="tablist"></div>');
  private readonly sections = new Map<string, HTMLElement>();
  private pagesEl: HTMLElement | null = null;
  private shell: { frame: HTMLElement; rail: HTMLElement; port: HTMLElement; shade: SVGPathElement; line: SVGPathElement } | null = null;
  private current = '';
  private wireRaf = 0;
  private spyRaf = 0;
  private spyMuted = 0;
  private wireD = '';
  private portY = Number.NaN;
  private prefs: Prefs = loadPrefs();
  private readonly homeRow = checkRow('Show Home screen at launch', (v) => this.change({ autoShowHome: v }));
  private readonly homeClearRow = checkRow('Show Home screen when the recording is cleared', (v) => this.change({ homeOnClear: v }));
  private readonly liveRow = checkRow('New flows keep the table following when FormNET starts', (v) => this.change({ liveFollows: v }));
  private readonly rowsRow = sliderRow('Rows drawn in the Flows table', 100, 5000, 100, (v) => this.change({ maxRows: v }), { def: 500 });
  private readonly confirmClearRow = checkRow('Ask before Clear Recording', (v) => this.change({ confirmClear: v }));
  private readonly intensityRow = sliderRow('Accent intensity (%)', 0, 150, 5, (v) => setIntensity(v / 100), { def: 100 });
  private readonly themeAutoRow = checkRow('Match System Appearance', (v) => this.change({ themeAuto: v }));
  private readonly themeLightRow = selectRow('Light theme', Object.entries(THEMES).filter(([, th]) => th.light).map(([k, th]) => [k, th.label] as const), (v) => this.change({ themeLight: v }));
  private readonly themeDarkRow = selectRow('Dark theme', Object.entries(THEMES).filter(([, th]) => !th.light).map(([k, th]) => [k, th.label] as const), (v) => this.change({ themeDark: v }));
  private readonly languageRow = selectRow('Language', languages(), (v) => { this.change({ language: v as Lang }); void setLanguage(v as Lang); });
  private readonly fontSizeRow = selectRow('Interface Text Size', UI_FONT_SIZES, (v) => this.change({ uiFontSize: v as UiFontSize }));
  private readonly scaleRow = selectRow('Interface Scale', UI_SCALES, (v) => setUiScale(v));
  private readonly animationsRow = checkRow('Enable animations', (v) => this.change({ animations: v }));
  private readonly noticeSecondsRow = sliderRow('Notice stays (seconds, 0 is automatic)', 0, 30, 1, (v) => this.change({ noticeSeconds: v }), { def: 0 });
  private readonly tooltipsRow = checkRow('Show Tool Tips', (v) => this.change({ showTooltips: v }));
  private readonly machineSortRow = selectRow('Process list order', MACHINE_SORTS, (v) => this.change({ machineSort: v as MachineSort }));
  private readonly storage = new StorageSection();
  private readonly https: HttpsSection;
  private readonly root: RootSection;
  private readonly passthrough: PassthroughSection;
  private readonly proxy: ProxySection;
  private readonly sysproxy: SystemProxySection;
  private readonly agents = new AgentsSection();
  private readonly rules: RulesSection;
  private readonly bpSettings = new BreakpointSettingsSection();
  private readonly throttle: ThrottleSection;
  private readonly devices: DevicesSection;

  constructor(private readonly deps: SettingsDeps) {
    super('Preferences', 620);
    this.https = new HttpsSection(deps.store);
    this.root = new RootSection(deps.store);
    this.passthrough = new PassthroughSection(deps.store);
    this.proxy = new ProxySection(deps.store);
    this.sysproxy = new SystemProxySection(deps.store);
    this.rules = new RulesSection(deps.store);
    this.throttle = new ThrottleSection(deps.store);
    this.devices = new DevicesSection(deps.store);
    const swatches = document.createElement('template');
    swatches.innerHTML = themeSwatchesHtml();
    this.grid.append(swatches.content);
    const themes = el('<div class="cv-prefs-themes"></div>');
    themes.appendChild(this.grid);
    const pages = el('<div class="cv-prefs-pages"></div>');
    const grp = (label: string, ...content: HTMLElement[]): HTMLElement => {
      const g = el('<section class="cv-prefs-group"><h3 class="cv-prefs-label"></h3></section>');
      const h = q<HTMLElement>(g, '.cv-prefs-label');
      setText(h, t(label));
      onLanguage(() => setText(h, t(label)));
      g.append(...content);
      return g;
    };
    const nt = (text: string): HTMLElement => { const p = el('<p class="cv-note"></p>'); setText(p, t(text)); onLanguage(() => setText(p, t(text))); return p; };
    const cols = (...content: HTMLElement[]): HTMLElement => { const c = el('<div class="cv-prefs-cols"></div>'); c.append(...content); return c; };
    const add = (name: string, ...content: HTMLElement[]): void => {
      const tab = el<HTMLButtonElement>(`<button class="cv-prefs-railtab" type="button" role="tab"><span class="cv-prefs-tabicon">${icon(RAIL_ICONS[name] ?? 'pfGeneral', 18)}</span><span class="cv-prefs-tablabel"></span></button>`);
      tab.dataset.section = name;
      const label = q<HTMLElement>(tab, '.cv-prefs-tablabel');
      const paint = (): void => { setText(label, t(name)); tab.title = t(name); };
      paint();
      onLanguage(paint);
      tab.addEventListener('click', () => this.showSection(name, 'smooth'));
      this.nav.appendChild(tab);
      const page = el(`<section class="cv-prefs-page cv-prefs-sec" role="tabpanel"><button class="cv-prefs-sechead" type="button" aria-expanded="true"><span class="cv-prefs-secicon">${icon(RAIL_ICONS[name] ?? 'pfGeneral', 16)}</span><span class="cv-prefs-seclabel"></span><span class="cv-prefs-chev">${icon('chevronDown', 16)}</span></button><div class="cv-prefs-secbody"></div></section>`);
      page.dataset.section = name;
      const head = q<HTMLButtonElement>(page, '.cv-prefs-sechead'), headLabel = q<HTMLElement>(page, '.cv-prefs-seclabel');
      const paintHead = (): void => setText(headLabel, t(name));
      paintHead();
      onLanguage(paintHead);
      head.addEventListener('click', () => this.foldSection(name));
      q<HTMLElement>(page, '.cv-prefs-secbody').append(...content);
      pages.appendChild(page);
      this.sections.set(name, page);
    };
    const accentRow = el('<label class="cv-field"><span></span></label>');
    labelText(q(accentRow, 'span'), 'Accent');
    accentRow.appendChild(this.accent);
    const reset = el<HTMLButtonElement>('<button class="cv-btn cv-btn-danger" type="button"></button>');
    setText(reset, t('Reset All Preferences...'));
    reset.addEventListener('click', () => {
      void confirmAction('Reset all preferences?', 'Every preference of this window goes back to how FormNET comes; the recording, the root certificate and the server\'s settings are kept. FormNET reloads.', 'Reset', true).then((ok) => {
        if (!ok) return;
        for (const k of Object.keys(localStorage)) if (k.startsWith(PREFIX) || k === STORAGE_KEY) localStorage.removeItem(k);
        location.reload();
      });
    });
    add('General',
      grp('Startup', this.homeRow.el, this.homeClearRow.el, nt('The Home screen covers the views. At launch it shows while the recording is empty and leaves when the first flow arrives or a view is chosen; with the second choice on it comes back when Clear Recording empties it. The brand mark at the top left opens it at any time, and Home opened that way stays while flows arrive; Esc leaves it.')),
      grp('Behavior', this.liveRow.el, this.rowsRow.el, this.confirmClearRow.el, nt('Live keeps the Flows table following new flows; paused, the rows stay put while flows keep arriving. The table draws the newest rows that match the filter, up to the number chosen; the export always takes every matching flow.')),
      grp('Settings Storage', this.storage.el),
      grp('Reset', reset));
    add('Interface',
      grp('Theme', themes, nt('Right-click a theme to favorite it. Theme and accent are shared with NodeMode on this device.'), this.themeAutoRow.el, cols(this.themeLightRow.el, this.themeDarkRow.el), nt('Match System Appearance switches between the light and dark theme chosen here when the appearance of the computer changes; a theme picked by hand stays until the next change of a preference or of the appearance.'), accentRow, this.intensityRow.el, nt('Intensity sets how strongly the accent tints what it marks; 100% is the design\'s.')),
      grp('Language, Text and Scale', this.languageRow.el, nt('The language of the whole interface. Host names, paths, headers and bodies stay as the programs sent them.'), cols(this.fontSizeRow.el, this.scaleRow.el), nt('Text size scales the interface\'s text; scale zooms the whole window.')),
      grp('Motion and Notices', this.animationsRow.el, this.noticeSecondsRow.el, this.tooltipsRow.el));
    add('Capture',
      grp('System Proxy', this.sysproxy.el),
      grp('HTTPS', this.https.el),
      grp('Root Certificate', this.root.el),
      grp('Passthrough', this.passthrough.el),
      grp('Proxy', this.proxy.el));
    add('Machine',
      grp('Sampling', nt('The connection table is read every FORMNET_MACHINE_INTERVAL (2 s unless set), with the tools the system already has: lsof, ps and nettop on macOS, /proc on Linux, netstat on Windows. No privileges are needed. Reverse DNS is off unless FORMNET_REVERSE_DNS=1, and a name is shown only when it resolves back to the address.')),
      grp('Display', this.machineSortRow.el, nt('The order the process list opens in; the buttons above the list change it for the session.')));
    add('Rules', grp('Rules', this.rules.el), grp('Breakpoints', this.bpSettings.el));
    add('Throttle', grp('Throttle', this.throttle.el));
    add('Other Devices', grp('Capture from Other Devices', this.devices.el));
    add('Agents and Scripts', grp('API', this.agents.el));
    for (const name of ORDER) {
      const tab = this.nav.querySelector<HTMLElement>(`[data-section="${name}"]`), card = this.sections.get(name);
      if (tab?.parentElement) tab.parentElement.appendChild(tab);
      if (card) pages.appendChild(card);
    }
    const wrap = el('<div class="cv-prefs"></div>');
    wrap.append(pages);
    this.body.appendChild(wrap);
    this.pagesEl = pages;
    const frame = q<HTMLElement>(this.scrim, '.cv-dialog');
    frame.classList.add('cv-prefs-dlg');
    q<HTMLElement>(this.scrim, '.cv-dialog-head').prepend(el(`<span class="cv-prefs-headicon">${icon('settings', 16)}</span>`));
    let collapsed = false;
    try { collapsed = localStorage.getItem(RAIL_KEY) === '1'; } catch { collapsed = false; }
    const fold = el<HTMLButtonElement>(`<button class="cv-prefs-fold" type="button">${icon('pfFold', 16)}</button>`);
    const paintRail = (): void => {
      setClass(frame, 'is-rail-collapsed', collapsed);
      fold.title = t(collapsed ? 'Expand tabs' : 'Collapse tabs');
      fold.setAttribute('aria-label', fold.title);
      fold.setAttribute('aria-expanded', String(!collapsed));
    };
    fold.addEventListener('click', () => {
      collapsed = !collapsed;
      try { localStorage.setItem(RAIL_KEY, collapsed ? '1' : '0'); } catch { paintRail(); }
      paintRail();
      this.queueWire();
    });
    paintRail();
    onLanguage(paintRail);
    const rail = el('<div class="cv-prefs-rail"></div>');
    rail.append(this.nav, fold);
    const wire = el<HTMLElement>('<svg class="cv-prefs-wire" aria-hidden="true"><path class="cv-prefs-wire-shade"></path><path class="cv-prefs-wire-line"></path></svg>') as unknown as SVGSVGElement;
    const port = el('<span class="cv-prefs-port" aria-hidden="true"></span>');
    frame.append(rail, wire, port);
    this.shell = { frame, rail, port, shade: q<SVGPathElement>(wire, '.cv-prefs-wire-shade'), line: q<SVGPathElement>(wire, '.cv-prefs-wire-line') };
    pages.addEventListener('scroll', () => this.queueSpy(), { passive: true });
    new ResizeObserver(() => this.queueWire()).observe(frame);
    window.addEventListener('resize', () => this.queueWire());
    this.button('Done', 'primary', () => this.close());
    this.onClose = () => this.agents.mask();
    this.grid.addEventListener('contextmenu', (e) => {
      const id = (e.target as HTMLElement).closest<HTMLElement>('[data-theme-id]')?.dataset.themeId;
      if (!id) return;
      e.preventDefault();
      const cur = this.prefs.favouriteThemes;
      this.change({ favouriteThemes: cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id] });
      this.sync();
    });
    this.grid.addEventListener('click', (e) => {
      const b = (e.target as HTMLElement).closest<HTMLElement>('[data-theme-id]');
      if (!b?.dataset.themeId) return;
      setTheme(b.dataset.themeId);
      this.sync();
    });
    this.accent.addEventListener('input', () => { setAccent(this.accent.value); this.sync(); });
    document.addEventListener('nm:ui-settings-changed', () => { if (this.isOpen) this.sync(); });
    deps.store.subscribe((s, prev) => { if (s.status !== prev.status) this.refreshPanels(); });
    onLanguage(() => { this.refreshPanels(); this.sync(); this.queueWire(); });
    this.showSection('General');
  }

  show(section?: string): void {
    this.prefs = loadPrefs();
    this.homeRow.set(this.prefs.autoShowHome);
    this.homeClearRow.set(this.prefs.homeOnClear);
    this.liveRow.set(this.prefs.liveFollows);
    this.rowsRow.set(this.prefs.maxRows);
    this.confirmClearRow.set(this.prefs.confirmClear);
    this.themeAutoRow.set(this.prefs.themeAuto);
    this.themeLightRow.set(this.prefs.themeLight);
    this.themeDarkRow.set(this.prefs.themeDark);
    this.languageRow.set(this.prefs.language);
    this.fontSizeRow.set(this.prefs.uiFontSize);
    this.scaleRow.set(uiScaleChoice());
    this.animationsRow.set(this.prefs.animations);
    this.noticeSecondsRow.set(this.prefs.noticeSeconds);
    this.tooltipsRow.set(this.prefs.showTooltips);
    this.machineSortRow.set(this.prefs.machineSort);
    this.intensityRow.set(Math.round((readChoice().intensity ?? 1) * 100));
    if (!this.isOpen) this.agents.mask();
    this.refreshPanels();
    if (section && this.sections.has(section)) this.showSection(section);
    this.sync();
    this.open();
    if (section && this.sections.has(section)) this.scrollToSection(section, 'auto');
    this.queueWire();
  }

  private refreshPanels(): void {
    const status = this.deps.store.get().status;
    this.storage.refresh(status);
    this.https.refresh(status);
    this.root.refresh(status);
    this.passthrough.refresh(status);
    this.proxy.refresh(status);
    this.sysproxy.refresh(status);
    this.agents.refresh(status);
    this.rules.refresh(status);
    this.bpSettings.refresh(status);
    this.throttle.refresh(status);
    this.devices.refresh(status);
  }

  /** Preferences at Rules, the list showing only rules of `type` (every rule when null): Tools > Rewrite, Map Local... */
  showRules(type: RuleActionType | null): void {
    this.rules.showType(type);
    this.show('Rules');
  }

  private showSection(name: string, scroll: ScrollBehavior | null = null): void {
    if (!this.sections.has(name)) return;
    this.markSection(name);
    if (scroll) this.scrollToSection(name, scroll);
  }

  private markSection(name: string): void {
    if (this.current === name) return;
    this.current = name;
    for (const [n, page] of this.sections) setClass(page, 'is-current', n === name);
    for (const tab of Array.from(this.nav.children) as HTMLElement[]) { const on = tab.dataset.section === name; setClass(tab, 'is-active', on); tab.setAttribute('aria-selected', String(on)); }
    this.queueWire();
  }

  private scrollToSection(name: string, behavior: ScrollBehavior): void {
    const sec = this.sections.get(name), pages = this.pagesEl;
    if (!sec || !pages) return;
    if (sec.classList.contains('is-folded')) this.foldSection(name);
    const top = sec.getBoundingClientRect().top - pages.getBoundingClientRect().top + pages.scrollTop - 12;
    this.spyMuted = performance.now() + 700;
    pages.scrollTo({ top, behavior });
  }

  private foldSection(name: string): void {
    const sec = this.sections.get(name);
    if (!sec) return;
    const folded = !sec.classList.contains('is-folded');
    setClass(sec, 'is-folded', folded);
    setHidden(q(sec, '.cv-prefs-secbody'), folded);
    q(sec, '.cv-prefs-sechead').setAttribute('aria-expanded', String(!folded));
    this.queueWire();
    this.queueSpy();
  }

  private queueSpy(): void {
    if (this.spyRaf) return;
    this.spyRaf = requestAnimationFrame(() => {
      this.spyRaf = 0;
      const pages = this.pagesEl;
      if (!pages || !pages.clientHeight || performance.now() < this.spyMuted) return;
      const edge = pages.getBoundingClientRect().top + 24;
      const names = Array.from(this.sections.keys());
      let best = '', bestD = Infinity;
      if (pages.scrollHeight > pages.clientHeight + 4 && pages.scrollTop + pages.clientHeight >= pages.scrollHeight - 2) best = names[names.length - 1] ?? '';
      else for (const [n, sec] of this.sections) {
        const top = sec.getBoundingClientRect().top, d = top <= edge ? edge - top : top - edge + 100000;
        if (d < bestD) { bestD = d; best = n; }
      }
      if (best) this.markSection(best);
    });
  }

  private queueWire(): void {
    if (this.wireRaf) return;
    this.wireRaf = requestAnimationFrame(() => { this.wireRaf = 0; this.drawWire(); });
  }

  private drawWire(): void {
    const s = this.shell;
    if (!s || this.scrim.hidden) return;
    const F = s.frame.getBoundingClientRect();
    const tab = this.nav.querySelector<HTMLElement>('.cv-prefs-railtab.is-active');
    const head = s.frame.querySelector<HTMLElement>('.cv-dialog-head');
    if (!F.width || !tab || !head) return;
    const N = this.nav.getBoundingClientRect();
    let T = tab.getBoundingClientRect();
    if (T.top < N.top || T.bottom > N.bottom) {
      this.nav.scrollTop += T.top < N.top ? T.top - N.top - 6 : T.bottom - N.bottom + 6;
      T = tab.getBoundingClientRect();
    }
    const R = s.rail.getBoundingClientRect(), H = head.getBoundingClientRect();
    // Left to right the rail floats left of the frame and the port sits on the frame's left edge; right to left both
    // are mirrored, so the wire runs from the tab's left edge rightwards to a port on the frame's right edge.
    const rtl = getComputedStyle(s.frame).direction === 'rtl', w = s.frame.clientWidth, x = rtl ? -1 : 1;
    const portY = Math.round(H.bottom - F.top), sx = Math.round(rtl ? T.left - F.left : T.right - F.left), ty = Math.round(T.top - F.top + T.height / 2);
    const gap = rtl ? R.left - F.right : R.right - F.left, trunk = Math.round((rtl ? w : 0) + gap * 0.55), end = rtl ? w + 7 : -7;
    const dy = portY - ty, r = 10, dir = dy > 0 ? 1 : -1, sweep = (dir > 0) === !rtl ? 1 : 0;
    const d = Math.abs(dy) < 2 * r ? `M ${sx} ${ty} H ${trunk} V ${portY} H ${end}` : `M ${sx} ${ty} H ${trunk - x * r} a ${r} ${r} 0 0 ${sweep} ${x * r} ${dir * r} V ${portY - dir * r} a ${r} ${r} 0 0 ${1 - sweep} ${x * r} ${dir * r} H ${end}`;
    if (d !== this.wireD) { this.wireD = d; s.shade.setAttribute('d', d); s.line.setAttribute('d', d); }
    if (portY !== this.portY) { this.portY = portY; s.port.style.transform = `translateY(${portY}px)`; }
  }

  private change(patch: Partial<Prefs>): void {
    const kept = loadPrefs();
    this.prefs = { ...this.prefs, ...patch, dialogValues: kept.dialogValues };
    savePrefs(this.prefs);
    this.deps.onPrefs(this.prefs);
  }

  private sync(): void {
    const c = readChoice();
    setValue(this.accent, c.accentColor);
    for (const b of Array.from(this.grid.querySelectorAll<HTMLElement>('[data-theme-id]'))) {
      const id = b.dataset.themeId ?? '';
      const active = id === c.theme;
      setClass(b, 'is-active', active);
      const fav = this.prefs.favouriteThemes.includes(id);
      setClass(b, 'is-fav', fav);
      const order = fav ? '-1' : '0';
      if (b.style.order !== order) b.style.order = order;
      const name = THEMES[id]?.label ?? id;
      const title = tf(fav ? '{name} (favorite, right-click to remove)' : '{name} (right-click to add to favorites)', { name: t(name) });
      if (b.title !== title) b.title = title;
      const border = active ? c.accentColor : THEMES[id]?.vars['--border-color'] ?? '';
      if (b.dataset.border !== border) { b.dataset.border = border; b.style.borderColor = border; }
    }
  }
}
