# HASHOUT_LOG, desktop shell, formnet

The window program in `src-tauri/` and its build scripts (`build_app`, `build_app.bat`, `tools/build_desktop`), ported from form.md's at v0.47.6 on 2026-10-30. Source files stay comment-free; what they would have said is here. Paths are relative to the repo root.

Current build version: v0.1.0
State: UNCOMPILED. The port was made on a machine without Rust. Nothing under `src-tauri/` has been through `cargo build` or `tauri build`; the first formcompiler desktop job (or the first `./build_app` on a machine with rustup) is its first compile. Expect only mechanical slips (a renamed identifier, a changed signature), since every change below is a rename or a deletion from working form.md code.

## What is the same as form.md

- `main.rs`: the server as a sidecar (`binaries/formnet-server-<triple>`), the ready line on stdout, the splash window until it arrives, the main window at 90% of the primary monitor (never under 980x640), loopback-only navigation with everything else opened in the system browser, downloads into the Downloads folder with a numbered name and revealed when done, the stop sequence (drop the child handle so stdin closes, wait up to 8 s for Terminated, then kill by pid), single instance, the failure dialogs.
- `menu.rs`: a macOS menubar only, items with id `cmd:<command>` calling a client hook, zoom 67% to 175%, reload.
- `build.rs`, `capabilities/default.json` (core:default, no remote IPC), the splash markup, `Cargo.lock` apart from the root package, `tools/build_desktop` apart from names.

## What differs, beyond renaming form.md to formnet

- The per-launch key is kept, renamed. form.md generates 32 random bytes as hex per launch (`FORMMD_DESKTOP_KEY`) and opens `/desktop/enter?key=...`; formnet does the same with `desktop_token()`, passes it to the sidecar as `FORMNET_TOKEN` and opens `http://127.0.0.1:PORT/?token=<hex>`. The server exchanges the token for a cookie and redirects to `/`, so no other local program that finds the port can read the recording. `getrandom = "0.3"` stays in `Cargo.toml` and in the root package's dependency list in `Cargo.lock`, exactly as in form.md.
- Environment for the sidecar: `FORMNET_DESKTOP=1` (the server stops when its stdin closes), `FORMNET_TOKEN`, `FORMNET_DATA` (the user's value if set, else `~/formnet`), `FORMNET_ADDR=127.0.0.1:8760`. Ready line: `formnet desktop ready http://127.0.0.1:PORT/` (the trailing slash is trimmed before the port is parsed, as before).
- Navigation: only `is_local` urls (http or ws on 127.0.0.1 or localhost at the server's port) and `about:` stay in the window. form.md also allowed top-level `blob:` and `data:` navigation; nothing in the formnet client needs it, so those are gone and such a navigation is refused (and, being neither http, https nor mailto, not opened outside either).
- Downloads: only a download whose url `is_local` is accepted (the HAR export and `ca.pem` come from the server); any other url returns false from the Requested event, which cancels it. form.md accepted every download.
- The "server stopped" dialog says the recording is gone, because formnet keeps flows in memory only.
- Menu: File has Export HAR (CmdOrCtrl+S), Download Root Certificate, Clear Recording (CmdOrCtrl+K); Edit has Filter (CmdOrCtrl+F); View has Flows, Hosts, Machine (CmdOrCtrl+1..3) and Toggle Dark Mode; Help has Proxy Setup, Trust the Root Certificate and formnet on GitHub (opens the repository instead of form.md's `/notices` page). The client hook is `window.__formnetRun(command)`; the command ids are `flows.exportHar`, `ca.download`, `flows.clear`, `flows.filter`, `view.flows`, `view.hosts`, `view.machine`, `shell.theme.toggle`, `help.proxySetup`, `help.trustRoot`. Until the client defines the hook the items do nothing, by design (`window.__formnetRun && ...`).
- `tauri.conf.json`: product formnet, main binary `formnet-desktop`, identifier `ai.nodemode.formnet`, version 0.1.0, category DeveloperTool, deb section devel, resources only `../LICENSE` (no NOTICES files: formnet has no `tools/notices` yet), splash accent `#f97316`.
- `entitlements.plist`: no microphone; `network.client` and `network.server` instead, since the hardened-runtime app hosts a loopback proxy and client. `Info.plist`: only `NSAllowsLocalNetworking`; no microphone usage string.
- Icons: `icons/` made by `tauri icon` from `assets/formnet_logo.svg` (a node with three arcs of traffic) rendered to 1024 px with `qlmanage`; only the six files form.md keeps (32, 128, 128@2x, icon.png, icns, ico); the splash icon is a copy of `128x128@2x.png`.
- Build scripts: no NOTICES step and no wiki check; binary `bin/formnet`, ldflags on `github.com/nodemode-dev/formnet/internal/app.Version`, version from `web/package.json`; the closing line says recordings stay in memory and the root certificate lives in `~/formnet`.
- `deploy/formcompiler.formnet.json` is the registry entry for formcompiler (passes `scripts/registry.py check`), `deploy/formai-tools.product.json` the products.json entry for formai.tools; both are copied into those repositories by hand when the app is registered.
