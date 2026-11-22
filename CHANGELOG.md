# Changelog

Versions follow `v*.*.*` with dots. Dates are US Eastern.

## 0.4.0 · 2026-11-22

Nothing in the window flashes, jumps or loses what you typed, and the window has the choices 0.3.0's server made possible.

- Setup keeps what you type. The Passthrough box used to be emptied every ten seconds while you were typing in it.
- The Machine and Hosts views hold still. Rows update in place, keep their order between samples, and keep your scroll position and focus; the Machine view re-sorts when you press a sort button.
- A flow's detail no longer jumps while a slow request completes.
- HTTPS is a choice of Auto, On or Off, in Setup and in Preferences.
- File has "Export HAR with secrets...", which asks first. "Export HAR..." leaves passwords, tokens and cookies out.
- A refused flow shows in red, with the reason and a button that adds the host to the passthrough list.
- A body says how it was sent and whether FormNET decoded it.

## 0.3.0 · 2026-11-22

What FormNET records is honest, and what it exports is safe to share. This version carries the server's side; the window's new choices for it arrive in 0.4.0.

- A program that refuses FormNET's certificate no longer vanishes. It leaves a flow marked refused, with the host, the program and the reason, so you can see that a pinned app tried to connect and add its host to the passthrough list.
- HAR exports leave out passwords, tokens and cookies: the values of Authorization, Proxy-Authorization, Cookie and Set-Cookie read `[redacted]`, and the file says so. `GET /api/export/har?secrets=1` writes everything.
- Bodies sent as Brotli or zstd show as text, as gzip bodies already did.
- The HTTPS choice and the passthrough list are kept when FormNET restarts. In Auto, trusting the root while FormNET runs opens HTTPS within about ten seconds, with no restart.
- The trust help is current: Firefox 120 and later read the system store, and on Linux the Chromium line names the folder Chromium uses today.
- When the desktop app is killed, its server no longer leaves `endpoints.json` behind.
- For the people who build FormNET: two Go modules are new, for Brotli and zstd, both permissive, with rows in `NOTICES.md` and their licence texts in `licenses/`.

## 0.2.0 · 2026-11-22

The desktop app opens every time, leaves nothing running, and other apps can find FormNET.

- A FormNET left running from before is replaced when you start it again. When another program holds port 8760 or 8761, FormNET says which program in one plain line, starts on free ports and shows the new address in the status bar.
- When the server cannot start, the app says so in a dialog with the server's last lines, in place of "stopped before it was ready".
- On Windows, ending the app from Task Manager also ends its server, no console windows flash while the Machine view runs, and the trust command works when your user folder has a space in its name.
- On Windows the root key, the session token and the address file can be read by your account only, as on macOS and Linux. FormNET refuses a key other users can read and prints the line that fixes it.
- The Machine view reads `netstat` on a Windows set to any language.
- For other apps and scripts: `<data>/endpoints.json` carries the address, the proxy address and the root's path; `formnet ca --json` prints the root as JSON; `/healthz` says where the proxy listens. `AGENTS.md` has a "For other apps" section.
- The desktop window may call the app's commands from FormNET's own page only, on this run's port.

## 0.1.3 · 2026-11-22

The desktop build can run on every system the family builds for, and the bundle says whose it is and what it carries.

- For the people who build FormNET: the desktop build no longer stops on a missing `src-tauri/Cargo.lock`. The lock is tracked, on the Tauri 2.12 line, and the page's Tauri package is on the same line.
- `build_app.bat` makes the same checks and takes the same options as `./build_app`, and `web/oc_install.bat` exists beside `web/oc_install`.
- Both build scripts stop, naming both values, when the `Makefile` and `web/package.json` carry different versions.
- Go is pinned to 1.26.9.
- The app carries `NOTICES.md` beside `LICENSE`: the fonts, the Tauri package and every Rust crate in the desktop shell, each with its licence.
- The app's copyright line and About name NodeAgentc, Inc.
- The test samples name no person.
