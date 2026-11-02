# HASHOUT_LOG, web client, formnet

The window's page in `web/`, ported to the form family's shell on 2026-11-02 from formvis v1.96.4 (the three maps in the formai folder: splash and boot, Home and the bars, Preferences). Source files carry short doc comments only; the reasoning is here. Paths are relative to `web/`.

## What was copied from formvis as it is

`src/styles/tokens.css` (the NodeMode token sheet; formnet's own block holds the four row heights and the detail pane width), `fonts.css`, `scrollbars.css`, `light-mode.css` (first line), `src/ui/dom.ts`, `icons.ts` (plus eight formnet icons: flows, globe, chip, wrench, network, copy, shield, home), `themes.ts` (skin key `formnet_user_skin`, style id `fn-skin-css`), `commands.ts`, `menu.ts`, `native-menu.ts` (app menu "formnet", key `formnet.menusInWindow`), `caption-buttons.ts` (key `formnet.customTitleBar`), `toast.ts`, `i18n.ts` and `i18n/langs.ts` (see below), `dialogs.ts`, `dialog-history.ts`, `dropdown.ts`, `number-fields.ts`, `panels/controls.ts` (the Preferences port's notes are at the end). `public/theme-boot.js` is formvis's inline theme-boot script as a file: the server's CSP allows no inline script, so `index.html` loads it with a plain script element before the stylesheets; it still runs before the body exists. The CSS for the title bar, menus, dropdowns, toasts, caption buttons, tab row, options bar, status bar and Home is copied rule by rule into `src/styles/shell.css`; the views' old stylesheets (base, layout, tables, detail) are rewritten on the tokens in `src/styles/views.css`. Vite's `assetsInlineLimit` is 0: the CSP allows fonts from the server only, never `data:` URIs, so every font file stays a file.

## What differs from formvis, and why

- No documents. formnet shows views (Flows, Hosts, Machine, Setup), so the family's document tab row carries the view tabs (`.cv-tab`, role tab, the names the smoke test clicks), the options bar carries the Flows filter, Live, Clear, Export HAR and the counts, and the window title is always "formnet". `.cv-tb-doc` (shown only under the system menus on macOS) reads the proxy address: it is what the window "has open".
- No workspaces, no lock, no terminal dock: `titlebar-tools.ts` keeps the theme toggle and the command search only. No rail, no panels, no options-bar overflow.
- i18n ships no tables yet: `LOADERS` is empty, the Language picker lists English alone, and `t()`, `onLanguage()` and the page translator are in place so the tables can be ported without touching the callers.
- The old `desktop.ts` hook (`window.__formnetRun`) and the Rust-built menu are gone: menus come from the page (`app-menus.ts`), built by `MenuBar` in the window and by `NativeMenus` into the macOS menu bar, from one `Commands` registry (`app-commands.ts`).
- The old `.notice` bar is gone: an error from the data layer is a toast and sits in the status bar's hint slot for six seconds (`store.notice` is read once and reset).

## Home (home.ts)

Flows stand in for documents. `on = forced || (!dismissed && flows === 0 && (hadFlows ? prefs.homeOnClear : prefs.autoShowHome))`. The first flow to arrive clears `forced`; any `view-*` command, the Back button or Esc sets `dismissed`; a return to zero flows (Clear Recording) clears `dismissed` so `homeOnClear` decides; the brand mark (role button, Enter and Space) calls `show()`. Start page: greeting, hero with the proxy address, the chips, Open Flows (Mod 1), Copy proxy address (label flips to Copied for 1.2 s), Trust the root certificate; then Top hosts (8 by bytes, keyed cards, a click filters Flows to the host) and Busiest processes (8 by bytes then connections). Both lists are `patchList`ed; the hero and the lists reread the state only while Home is shown.

## Status bar (status.ts)

Slots, left to right, each at a fixed width so nothing reflows: `.cv-st-engine` "Server <version> · desktop" or "Server offline" (desktop when `__TAURI_INTERNALS__` exists), `.cv-st-proxy` "Proxy <addr>" (title: listening or not), `.cv-st-ca` "HTTPS opened" or "HTTPS tunnelled" (title: the root's trust), `.cv-st-info` a readout cycled by click (Shift reverses; kept under `formnet.statusInfo`): flows shown of kept, bytes sent and received, hosts, processes and connections of the last sample, sample age; `.cv-st-hint` the view's hint, or an error in the danger colour for six seconds.

## Commands and keys (app-commands.ts)

export-har Mod+S · download-ca · clear-recording Mod+K (asks first when `confirmClear`) · undo Mod+Z · redo Shift+Mod+Z · cut Mod+X · copy Mod+C · paste Mod+V · select-all Mod+A (the browser's own edit commands; the macOS menu sends them to the shell's text_edit while a field has focus) · find Mod+F · settings Mod+, · prefs-general, prefs-interface, prefs-capture, prefs-machine, prefs-agents (Edit > Preferences) · view-flows Mod+1 · view-hosts Mod+2 · view-machine Mod+3 · view-setup Mod+4 · home · live (checked) · zoom-in Mod+= · zoom-out Mod+- · zoom-reset Mod+0 (the UI_SCALES list; CSS zoom in a browser, the shell's `ui_zoom` in the desktop app) · theme-toggle Shift+Mod+L · menus-in-window (macOS desktop, next start) · custom-title-bar (Windows and Linux desktop) · help F1 (Setup, proxy card) · help-trust-root (Setup, root card) · github · about (a toast for now; an About dialog is a later port). The window keydown runs `commands.forEvent` unless a dialog or the menu bar is open or focus is in a field; `/` focuses the filter. The Preferences dialog runs commands through the document event `formnet:run` with the command id as detail.

## State additions (state.ts)

`maxRows` (the Flows table's row cap, from Preferences) and `machineSort` (the Machine view's order, from Preferences; its sort buttons write it back). `live` starts from `prefs.liveFollows`.

## Smoke (tests/window/test_window.py)

Six tests: the token leaves the address and the API needs it, a reload keeps the session (the status bar names the server), Home shows over an empty recording and goes away from its Flows item (and comes back from the brand mark, and leaves on Esc), the title and the four views, Setup shows the proxy address, a proxied request appears with its detail pane.

## Preferences port notes (from the PREFERENCES fork, 2026-11-02)

Ported from formvis v1.96.4 (FORMVIS_SETTINGS_MAP.md). The shape is the family's: `SettingsDialog extends Dialog`, the frame `.cv-prefs-dlg`, the floating rail `.cv-prefs-rail` (collapse kept under `formnet.prefsRailCollapsed`), one scrolling pane `.cv-prefs-pages` of foldable `.cv-prefs-sec` cards, the scroll spy, the SVG wire and port, Done. Every row is built once in the constructor; `show()` patches values, `sync()` the theme swatches; `change()` saves through `savePrefs` and calls `deps.onPrefs`.

Sections (data-section is the English name): General (pfGeneral): Startup, Behavior, Settings Storage (`panels/storage.ts`, the data folder read as the parent of `status.ca.path`), Reset (removes every key starting with `formnet.` and `nodemode_ui_settings`, then reloads). Interface (pfInterface): Theme, Language, Text and Scale, Motion and Notices. Capture (pfPerformance): HTTPS, Passthrough, Proxy (`panels/capture.ts`; the toggle and the list write through `api.updateProxy`, then read `api.status` into the store so every view follows). Machine (pfScratch): Sampling (read-only note), Display (`machineSort`). Agents and Scripts (pfPlugins): API (`panels/agents.ts`, with a copyable curl example).

Contract: `prefs.ts` adds `machineSort: MachineSort` ('connections' | 'bytes' | 'name', default 'connections', validated), exports `MachineSort`, `MACHINE_SORTS`; nothing renamed or removed. `settings-dialog.ts`: `SettingsDeps { store, onPrefs }`, `PREF_PAGES` (general, interface, capture, machine, agents), `SettingsDialog(deps)`, `show(section?)`, `isOpen` (from Dialog). The dialog subscribes to the store itself (a new `status` patches the Capture, Storage and Agents panels in place, open or not) and listens to `nm:ui-settings-changed` to re-sync the swatches while open. It expects `.cv-btn`, `.cv-btn-ghost`, `.cv-btn-danger`, `.cv-icon-btn`, `.cv-dd-*` and `.cv-menu*` from shell.css. Reset All Preferences removes `formnet.*` keys: every window key stays under that prefix (the token lives in sessionStorage under `formnet.token`, untouched since only localStorage is cleared). Literals kept (the family's documented exceptions): the scrim colour and blur, the wire shade (`#000` at 45 percent), the thumb shadows, and fixed control sizes (30px, 10 by 18 thumb, 12.5px and 11.5px text).

## 2026-11-02 · the app is formNET

The owner chose the repository's spelling, formNET, on 2026-11-02 (the family writes acronyms in capitals: formMCP, formDAM, formCAD). Every name a person sees changed: the title bar's `formNET 2026`, `document.title` and the desktop window title, the Home brand line, the brand mark's label, the About entry and toast, the macOS application menu, the status bar titles and the Preferences prose. Every name a program reads stayed `formnet`: the CLI words in `code` elements, the `formnet.` localStorage prefix, the `formnet:run` event, the `formnetSplashDone` hook, the identifier, the data folder and the ready line the desktop shell waits for. The window smoke asserts the new title and brand.
