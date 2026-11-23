# HASHOUT_LOG, desktop shell, formnet

The window program in `src-tauri/` and its build scripts (`build_app`, `build_app.bat`, `tools/build_desktop`), ported from form.md's at v0.47.6 on 2026-10-30. Source files stay comment-free; what they would have said is here. Paths are relative to the repo root.

Current build version: v0.1.0
State: UNCOMPILED (still true after the 2026-11-02 family port below). The port was made on a machine without Rust. Nothing under `src-tauri/` has been through `cargo build` or `tauri build`; the first formcompiler desktop job (or the first `./build_app` on a machine with rustup) is its first compile. Expect only mechanical slips (a renamed identifier, a changed signature), since every change below is a rename or a deletion from working form.md code.

## What is the same as form.md

- `main.rs`: the server as a sidecar (`binaries/formnet-server-<triple>`), the ready line on stdout, the splash window until it arrives, the main window at 90% of the primary monitor (never under 980x640), loopback-only navigation with everything else opened in the system browser, downloads into the Downloads folder with a numbered name and revealed when done, the stop sequence (drop the child handle so stdin closes, wait up to 8 s for Terminated, then kill by pid), single instance, the failure dialogs.
- `tools/build_desktop` apart from names.
- (Until 2026-11-02 also: `menu.rs`, `build.rs`, `capabilities/default.json`, the splash markup and `Cargo.lock`; see the family port below.)

## What differs, beyond renaming form.md to formnet

- The per-launch key is kept, renamed. form.md generates 32 random bytes as hex per launch (`FORMMD_DESKTOP_KEY`) and opens `/desktop/enter?key=...`; formnet does the same with `desktop_token()`, passes it to the sidecar as `FORMNET_TOKEN` and opens `http://127.0.0.1:PORT/?token=<hex>`. The server exchanges the token for a cookie and redirects to `/`, so no other local program that finds the port can read the recording. `getrandom = "0.3"` stays in `Cargo.toml` and in the root package's dependency list in `Cargo.lock`, exactly as in form.md.
- Environment for the sidecar: `FORMNET_DESKTOP=1` (the server stops when its stdin closes), `FORMNET_TOKEN`, `FORMNET_DATA` (the user's value if set, else `~/formnet`), `FORMNET_ADDR=127.0.0.1:8760`. Ready line: `formnet desktop ready http://127.0.0.1:PORT/` (the trailing slash is trimmed before the port is parsed, as before).
- Navigation: only `is_local` urls (http or ws on 127.0.0.1 or localhost at the server's port) and `about:` stay in the window. form.md also allowed top-level `blob:` and `data:` navigation; nothing in the formnet client needs it, so those are gone and such a navigation is refused (and, being neither http, https nor mailto, not opened outside either).
- Downloads: only a download whose url `is_local` is accepted (the HAR export and `ca.pem` come from the server); any other url returns false from the Requested event, which cancels it. form.md accepted every download.
- The "server stopped" dialog says the recording is gone, because formnet keeps flows in memory only.
- Menu: File has Export HAR (CmdOrCtrl+S), Download Root Certificate, Clear Recording (CmdOrCtrl+K); Edit has Filter (CmdOrCtrl+F); View has Flows, Hosts, Machine (CmdOrCtrl+1..3) and Toggle Dark Mode; Help has Proxy Setup, Trust the Root Certificate and formnet on GitHub (opens the repository instead of form.md's `/notices` page). The client hook is `window.__formnetRun(command)`; the command ids are `flows.exportHar`, `ca.download`, `flows.clear`, `flows.filter`, `view.flows`, `view.hosts`, `view.machine`, `shell.theme.toggle`, `help.proxySetup`, `help.trustRoot`. Until the client defines the hook the items do nothing, by design (`window.__formnetRun && ...`).
- `tauri.conf.json`: product formnet, main binary `formnet-desktop`, identifier `ai.nodemode.formnet`, version 0.1.0, category DeveloperTool, deb section devel, resources `../LICENSE` and, since 2026-11-22, `../NOTICES.md` (see the notices entry below), splash accent `#f97316`.
- `entitlements.plist`: no microphone; `network.client` and `network.server` instead, since the hardened-runtime app hosts a loopback proxy and client. `Info.plist`: only `NSAllowsLocalNetworking`; no microphone usage string.
- Icons: `icons/` made by `tauri icon` from `assets/formnet_logo.svg` (a node with three arcs of traffic) rendered to 1024 px with `qlmanage`; only the six files form.md keeps (32, 128, 128@2x, icon.png, icns, ico); the splash icon is a copy of `128x128@2x.png`.
- Build scripts: no NOTICES step and no wiki check; binary `bin/formnet`, ldflags on `github.com/nodemode-dev/formnet/internal/app.Version`, version from `web/package.json`; the closing line says recordings stay in memory and the root certificate lives in `~/formnet`.
- `deploy/formcompiler.formnet.json` is the registry entry for formcompiler (passes `scripts/registry.py check`), `deploy/formai-tools.product.json` the products.json entry for formai.tools; both are copied into those repositories by hand when the app is registered.

## 2026-11-02: the family's shell, from formvis v1.96.4

The three formvis code maps (splash and boot, home/menu bar/title bar/status bar, Preferences) describe how every form app opens and looks. The window side of that is ported here, mechanically, from formvis `src-tauri/`; the page side lives in `web/` (its own log, `web/HASHOUT_LOG.md`). Still UNCOMPILED: this Mac has no Rust, so the files were copied and renamed by hand and re-read for imports, feature gates and braces; the first `cargo build` (formcompiler, or `./build_app` on a machine with rustup) is their first compile.

### What came from formvis

- `src/reveal.rs`, unchanged apart from the splash hook's name: `mark_launch` first in `main()`, `round_splash` first in setup (the splash window is made non-opaque with a clear background and its content layer masked to the card's 16 px radius, then shown), `BUILD_VISIBLE` (false on macOS) and `BACKGROUND` (#0a0a0a, the page's critical style) on the main window builder, `conceal` right after `build()` (macOS: on screen at alpha 0 and ignoring the mouse, so the page keeps rendering; elsewhere hidden), `app_ready` the command the page calls once its first content has painted, `show_main` waiting out `SPLASH_MIN` 2.6 s from launch, asking the splash to finish its bar (`window.formnetSplashDone()`, 220 ms), showing and focusing the window, placing the traffic lights and closing the splash; `FALLBACK` 20 s shows the window anyway if the page never says it is ready.
- `src/traffic_lights.rs`, unchanged: macOS only, the lights centred in the page's 38 px title row (`ROW` 38, `LEFT` 14), placed after build, on page load Finished, on Resized, Focused, ThemeChanged and ScaleFactorChanged (`on_window_event`), and by the `set_window_title` command, which the page calls when the title changes.
- `src/text_edit.rs`, unchanged: macOS only; while a text field has focus the page sends `undo`, `redo`, `cut`, `copy`, `paste` or `select-all` here instead of running its own command, and the standard edit action goes to the first responder, so the menu bar's Cmd+Z/X/C/V/A keys edit the field.
- `src/zoom.rs`, formnet's own small version of formvis's `window_state::ui_zoom`: `ui_zoom` without a factor returns the web view's zoom, with one clamps it to 0.5 to 3.0, sets it and keeps it in the managed `Zoom(Mutex<f64>)`; the page keeps the chosen scale on disk and asks again at the next start.
- `main.rs`: the main window gets `.visible(reveal::BUILD_VISIBLE)`, `.background_color(reveal::BACKGROUND)`, on macOS `.title_bar_style(TitleBarStyle::Overlay).hidden_title(true)` (the page's title row is the window's title row, as in formvis), `.on_page_load` placing the lights, then `reveal::conceal` and `traffic_lights::place` after `build()`. The splash is no longer closed when the window opens: `show_main` closes it once the page is ready and 2.6 s have passed. `close_splash` stays for the failure paths. The builder gains `.on_window_event` (the lights) and `.invoke_handler(generate_handler![traffic_lights::set_window_title, reveal::app_ready, text_edit::text_edit, zoom::ui_zoom])`.
- `splash/index.html`: formvis's `splash.html` with formnet's mark (the same inline SVG as `web/src/ui/brand.ts`, ids prefixed `splash-`), "Starting formnet", the bar in the mark's colours (`#FFAE2B`, `#F97316`, `#FF4F7A`), 2.6 s fill, 0.2 s finish, the `#0a0a0a` card at 16 px radius on a transparent page, `window.formnetSplashDone`.
- `tauri.conf.json`: the splash window as formvis's (360 by 200, no decorations, centred, transparent, no shadow, starts hidden; the `backgroundColor` is gone, the card paints itself) and `app.macOSPrivateApi: true` (transparency needs it). `Cargo.toml`: tauri feature `macos-private-api`; macOS-only `objc2` 0.6, `objc2-app-kit` 0.3 (NSWindow, NSButton, NSControl, NSView, NSResponder, objc2-core-foundation, NSColor) and `objc2-foundation` 0.3 (NSString, NSGeometry), as formvis declares them.
- `build.rs`: `APP_COMMANDS = ["set_window_title", "app_ready", "text_edit", "ui_zoom"]` through `tauri_build::try_build(...app_manifest(...commands(APP_COMMANDS)))`, so each gets a generated `allow-<name>` permission. `capabilities/default.json`: windows splash and main, `remote.urls = ["http://127.0.0.1:*"]` (the page is served by the formnet server, a remote origin to Tauri, and could not call a command without it), `core:default`, the window permissions the page's caption buttons use on Windows and Linux (minimize, toggle-maximize, maximize, unmaximize, is-maximized, close, start-dragging, set-decorations, set-title), `core:event:default`, and the four `allow-*` command permissions.

### What left

- `src/menu.rs` and the `window.__formnetRun(command)` hook: the menus are built by the page from its own command registry with `@tauri-apps/api/menu` (`NativeMenus` on macOS, the in-window menu bar elsewhere), as every form app does, so the labels, shortcuts and enabled states are the page's and never differ from the in-window bar. `core:default` carries the menu permissions. The zoom items moved to the page too (View > Zoom In, Zoom Out, Actual Size call `ui_zoom`).
- `splash/icon.png`: the splash draws the mark inline.
- `Cargo.lock`: deleted. It was form.md's lock with the root package renamed, and it no longer matches `Cargo.toml` (new macOS dependencies, a new tauri feature). A stale lock fails a `--locked` build; a missing one is simply written by the first `cargo build`, and that generated file is the one to commit.

### The page contract (what `web/` calls)

| Invoke | Arguments | Returns | When |
| --- | --- | --- | --- |
| `app_ready` | none | nothing | once, from `revealApp()` one frame after `body.app-ready` |
| `set_window_title` | `{ title: string }` | nothing | whenever `document.title` changes (the lights are placed again) |
| `text_edit` | `{ action: 'undo' \| 'redo' \| 'cut' \| 'copy' \| 'paste' \| 'select-all' }` | nothing (Err for another action) | a menu Edit item while a text field has focus (macOS) |
| `ui_zoom` | `{ factor?: number }` | the zoom in effect (f64) | View > Zoom items and Preferences > Interface Scale; no factor reads it |

The splash hook is `window.formnetSplashDone`. The page finds the desktop by `'__TAURI_INTERNALS__' in window`. The formnet server's CSP already allows the IPC transport (`connect-src 'self' ipc: http://ipc.localhost`), without which no invoke would reach the shell from the remote page.

### To check at the first compile

- `tauri::TitleBarStyle` and `WebviewWindowBuilder::hidden_title` exist on the pinned tauri 2 (formvis builds against the same major).
- `traffic_lights::place(&window.as_ref().window())` after `build()`: `WebviewWindow: AsRef<Webview>` and `Webview::window()` as formvis uses them.
- `window.ns_window()` in `reveal.rs` and `traffic_lights.rs` needs no extra feature on tauri 2 (formvis compiles it with the same Cargo features).
- `zoom.rs`: `WebviewWindow::set_zoom` exists on tauri 2; the `State<Zoom>` argument is the managed `zoom::Zoom`.

## 2026-11-22 · Cargo.lock on the Tauri 2.12 line, and the first compile check

`Cargo.lock` is tracked again, written by `cargo generate-lockfile` (rustc and cargo 1.97.0). It resolves `tauri` 2.12.7, `tauri-build` 2.7.6, `tauri-runtime` 2.12.4, `tauri-runtime-wry` 2.12.5, `tauri-utils` 2.10.4, `wry` 0.57.3, `tao` 0.37.3, `tauri-plugin-shell` 2.6.0, `tauri-plugin-dialog` 2.8.4 (with `tauri-plugin-fs` 2.7.1), `tauri-plugin-opener` 2.7.3 and `tauri-plugin-single-instance` 2.5.5. No `--precise` hold was needed: the newest `tauri` 2.x is on 2.12. `web/package.json` moved `@tauri-apps/api` to 2.12.5 (the newest 2.12 patch), so the page's API, `@tauri-apps/cli` 2.12.1 and the crate are on one minor line, which the Tauri CLI requires and `tools/build_desktop` checks (the `X.Y` of `tauri --version` against the `tauri` entry in this lock). When the CLI moves to a new minor, move `@tauri-apps/api` with it and run `cargo update -p tauri` (or `--precise` to the same minor).

`cargo check --locked` passes with no warning from this crate for x86_64-unknown-linux-gnu, x86_64-pc-windows-msvc, aarch64-apple-darwin and x86_64-apple-darwin, from a Linux machine with an empty placeholder sidecar at `binaries/formnet-server-<triple>[.exe]`. Every item under "To check at the first compile" above holds: no source change was needed. Two notes for a cross check from Linux only: the macOS targets need `CC=clang`, because `objc2-exception-helper` builds a small Objective-C file and the default `cc` is GCC; the Windows target prints `GNU compiler is not supported for this target` from the resource step of `tauri-build`, which is about the Linux host's C compiler and does not appear on Windows. `cargo check` is not `cargo build` or `tauri build`: linking, signing and bundling are still first done by formcompiler or `./build_app`.

tauri-build writes `permissions/autogenerated/` (one file per command in `APP_COMMANDS`) at every build; `src-tauri/.gitignore` keeps it out of git, as formvis does.

## 2026-11-22 · Rust toolchain: `stable`, no rust-toolchain.toml

No `rust-toolchain.toml` is kept. formcompiler installs `stable` with `dtolnay/rust-toolchain` and adds each platform's targets to that toolchain (`apps/formNET.json`: `"rust": true`, no extra targets), and `tools/build_desktop` installs `stable` with rustup and, on Apple silicon, sets `RUSTUP_TOOLCHAIN=stable-aarch64-apple-darwin`. A file pinning `1.97.0` would make rustup fetch a second toolchain inside `src-tauri/` with none of those targets added, so a cross target installed for `stable` would be missing; the `rustc -V` that `tools/build_desktop` prints at the repo root would name a different compiler from the one cargo uses in `src-tauri/`; and on Apple silicon the environment variable overrides the file anyway. The lock and `rust-version = "1.77.2"` in `Cargo.toml` already fix what is built; the shell was checked with stable 1.97.0.

## 2026-11-22 · Splash mark is the app icon PNG

`splash/index.html` shows `splash/formNET-256.png`, the same bytes as `icons/128x128@2x.png`, so the splash carries exactly the v3 mark of the bundle icon (README overhaul, #4) instead of a hand-kept copy of the older inline SVG that could drift from it. The commented-out `<svg>` that stayed after the `<img>` is removed; the old inline mark is in git history, for example `git show 4909cb0:src-tauri/splash/index.html`.

## 2026-11-22 · NOTICES.md

`NOTICES.md` at the repo root lists every distributed third-party work: the fonts and `@tauri-apps/api` that ship in `web/dist` (versions from `web/package-lock.json`), and every Rust crate linked into `formnet-desktop` for macOS, Windows or Linux. `tauri.conf.json` bundles it beside `LICENSE` (`"../NOTICES.md": "NOTICES.md"`). All crate licences are permissive (MIT, Apache-2.0, BSD, Zlib, Unlicense, CC0, Unicode-3.0, and MPL-2.0 for `option-ext`); none is GPL, LGPL or AGPL. The family's own design tokens get no row.

The crate set is the normal dependency tree without proc-macro crates and build dependencies, which run at compile time and are not in the program. To refresh it after a lock change, in `src-tauri/`:

```
for t in x86_64-unknown-linux-gnu aarch64-unknown-linux-gnu x86_64-pc-windows-msvc aarch64-apple-darwin x86_64-apple-darwin; do
  cargo tree --locked -e normal,no-proc-macro --target $t --prefix none -f '{p}' | sed 's/ (.*//' | awk '{print $1" "$2}' | sort -u > /tmp/nt_$t.txt
done
cat /tmp/nt_*.txt | sort -u | grep -v '^formnet-desktop ' > /tmp/nt_all.txt
cargo metadata --locked --format-version 1 > /tmp/meta.json
```

then, for each `name version` in `/tmp/nt_all.txt`, take the package of that name and version from `/tmp/meta.json` and print `| name | version | license | platforms | repository |`, where platforms are the targets whose `/tmp/nt_<target>.txt` lists it (the two Apple triples as macOS, the two Linux triples as Linux), sorted by name. Replace the table in `NOTICES.md` with that output by hand, re-read any licence that is not MIT, Apache-2.0, BSD, ISC, Zlib, Unicode or MPL-2.0, and update the version of `@tauri-apps/api` and the fonts from `web/package-lock.json`. The throwaway printer used on 2026-11-22 was a few lines of Python and is not kept in the repository.
