# Changelog

Versions follow `v*.*.*` with dots. Dates are US Eastern.

## 0.7.0 · 2026-11-22

No setup by hand, and any request can be sent again.

- On macOS and Windows FormNET can point the computer's proxy at itself while it runs (Preferences > Capture > System Proxy, off by default), so browsers and most apps show up in Flows with nothing to configure. Your previous proxy settings come back when FormNET quits, and at the next start if it ever crashed; `formnet sysproxy restore` puts them back by hand.
- Resend any captured request, or edit its method, address, headers and body first. The new request is recorded as its own flow and links back to the original.
- Setup shows ready-to-copy lines that send curl, Node.js, Python, Java, Go and git through FormNET, for sh and PowerShell, with this run's real address and certificate.
- The new labels read in English in the other languages until a later version translates them.

## 0.6.0 · 2026-11-22

Agents can use FormNET, and FormNET has the family's terminal panel.

- `formnet mcp`: an MCP server for agents such as Claude Code. It reads the running FormNET and starts nothing. Ten tools: six read flows, hosts, the machine table, the proxy and the root certificate; one exports a HAR into FormNET's own exports folder; three change things (clear the recording, the HTTPS choice, the passthrough list) and are marked so an agent asks you first. Passwords, tokens and cookies read `[redacted]` in everything an agent sees.
- A skill pack, `formnet-inspector`, teaches agents to point programs at the proxy, trust the root, read flows and export HAR.
- The terminal panel (View > Terminal, Cmd+J or Ctrl+J), shared with FormVis and FormDeck. It finds Claude Code, Codex, Cursor and Antigravity if you have them.
- Preferences > Agents and Scripts can send the traffic of new terminal tabs to Flows, so an agent you start there shows every request it makes.
- For the people who build FormNET: three Go modules (coder/websocket, creack/pty, golang.org/x/sys) and two web packages (xterm), all permissive, listed in `NOTICES.md`. `make check` also runs `formnet agents check`.

## 0.5.0 · 2026-11-22

FormNET looks and starts like the rest of the family, in its own colour, and speaks every language the family speaks.

- FormNET's own accent, a soft violet, in place of the family's orange. If you never chose an accent you get the new one from the first frame; an accent you chose is kept. The logo and the splash are unchanged.
- The 29 family themes, AMOLED Black included.
- The desktop app opens with no white or unpainted frame on macOS and Linux: the window stays unseen until its page has painted, then appears as the splash goes. On Windows the window may show briefly in the theme's colour behind the splash, never white. The window's address no longer carries the session token.
- Home has four pages, Start, Hosts, Processes and Setup, with live counts beside each, and stays open while traffic arrives if you opened it.
- Setup shows where things stand and what to copy; every setting is in Preferences, and each block on Setup has a button that opens the right panel. Preferences > Capture now holds the root certificate's state, its trust command and its download.
- Preferences has the family's floating tab rail and the line from the chosen tab to the dialog. The session token in Agents and Scripts is hidden until you choose Show.
- Buttons, tabs and segmented controls are the family's, in every view.
- Every language the family ships: 30 in all, switched in Preferences without a reload. Arabic reads right to left, while paths, addresses and sizes still read left to right. Dates, times and numbers follow the language.
- Flows past 500 rows no longer moves under you: rows you are reading stay put, and a note says how many newer flows are waiting.
- The name you read is FormNET everywhere: menus, About, dialogs, the trust help and the installer.

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
