# FormNET

The app is FormNET wherever a person reads its name: the window title, the splash, menus, dialogs, documents and releases. The repository stays `nodemode-dev/formNET`. The CLI binary, module path, bundle identifier `ai.nodemode.formnet`, data folder `~/formnet`, `FORMNET_*` variables, the `formnet.` storage prefix and the API's `product` value stay lower case: they are names programs read, not the name people see.

What a program actually does on the network, not what it promises: an intercepting HTTP(S) proxy with a local root certificate, a machine-wide view of every connection and the process behind it, and a web client that reads like a browser's network tab for the whole computer. One Go binary with the client embedded; a Tauri 2 window around it for the desktop app. Part of the form family, built and released by formcompiler.

## Layout

- `cmd/formnet/` the command: `serve` (default), `url`, `open`, `ca`, `snapshot`, `version`, `help`.
- `internal/config/` settings from `FORMNET_*` variables; both listeners are loopback-only and refuse anything else, except the proxy's listeners for capture from other devices (below).
- `internal/flow/` the record: `Flow`, the `Store` bounded by a flow count and a byte budget, with counted subscriptions and an epoch, per-host summaries, the known-host labels (`classify.go`) and a streaming HAR export.
- `internal/ca/` the local root certificate (made on first run in the data folder) and per-host leaf certificates.
- `internal/proxy/` the proxy: plain HTTP forwarding, CONNECT tunnels, TLS interception with the root, passthrough patterns, body capture up to the limit, byte and time accounting into the Store.
- `internal/machine/` the connection table and owning processes per platform (lsof, ps and nettop on macOS, /proc on Linux, netstat on Windows), polled on an interval; it also names the process behind a proxy client port. Reverse DNS is off unless `FORMNET_REVERSE_DNS=1`.
- `internal/server/` the loopback API, the server-sent event stream and the embedded client; the route list and the session rules are in its package comment. Every API call needs the run's token as a bearer header (the page reads it from `/?token=` and keeps it in `sessionStorage` (`web/src/session.ts`); the stream and downloads pass it as a query parameter). No cookie, ever: a cookie for 127.0.0.1 reaches every other local port.
- `web/` the client: vanilla TypeScript and Vite, no UI framework, in the form family's shell ported from formvis v1.96.4 (title bar with the menus, the view tabs, the options bar, the status bar, Home, Preferences, the shared tokens and themes). `web/src/ui/app.ts` builds the window once; `app-commands.ts` and `app-menus.ts` hold every command, key and menu; `settings-dialog.ts` and `panels/` hold Preferences; `web/HASHOUT_LOG.md` records what was ported and why. `web/tests/window/` is the Playwright smoke CI runs.
- `src-tauri/` the desktop window: the splash card, the reveal (the window shows whole once the page has painted and 2.6 s have passed), the macOS overlay title bar with the traffic lights placed by `traffic_lights.rs`, `text_edit.rs` and `zoom.rs` for the page. It starts the server as a sidecar and waits for the `formnet desktop ready http://127.0.0.1:PORT/` line on stdout. The menus are built by the page, not here. `src-tauri/HASHOUT_LOG.md` records the port; the shell has never been compiled.
- `assets/` repo-level art (logo). The formcompiler registry entry lives in formcompiler (`apps/formNET.json`) and the product line in formai.tools (`catalog/products.json`), not here.

## Checks

`make check` runs gofmt, go vet and the TypeScript check. `make test` runs `go test ./...`. CI (`.github/workflows/ci.yml`) runs both, builds, cross-compiles, smokes the binary through its own proxy and runs the window smoke. Tests sit in `_test.go` files beside the code. They use temporary folders and the loopback interface only; nothing reaches the network.

## Rules

- Never listen on a non-loopback address. The proxy and the API carry the bodies of everything FormNET sees. The one exception is the owner's: capture from other devices (`internal/proxy/remote.go`), a switch that is off by default, opens the proxy only (never the API or the page) on this computer's network addresses, needs an address allow list with at least one entry, and closes any other client before reading from it.
- Loopback is not a trust boundary: every program and user on the computer shares it. The API answers only with the run's session token (`FORMNET_TOKEN`, or the one written to `<data>/token` with mode 0600). Never add a route that serves recordings without it.
- The proxy never connects to FormNET's own addresses, and never to a loopback address reached through a host name (DNS rebinding into local services). Private ranges stay reachable; intranet hosts are legitimate.
- HTTPS is opened only when the root is trusted (or `FORMNET_INTERCEPT=on`); otherwise it is tunnelled and counted, and the UI says so. Keep the wording and the behaviour in step.
- The root certificate's private key never leaves the data folder (mode 0600) and is never served by any route. Only the certificate is downloadable.
- Flows are values. Build a new `Flow` for every change and hand it to the Store; never edit a stored one in place.
- Labels from `classify.go` are reading aids, not verdicts. Never present them as a judgement about a host.
- The client follows the family's laws: zero DOM flash (build once, patch in place with the helpers in `ui/dom.ts`), colours and z-index only from the tokens in `styles/tokens.css`, icons only from `ui/icons.ts` (never the three-star sparkle), every localStorage key under the `formnet.` prefix (Reset All Preferences removes them) except the family-shared `nodemode_ui_settings`. A decision about the shell goes in the matching HASHOUT_LOG.md in the same change.
- No emoji anywhere, no em dash in Markdown.
- Commits carry no `Co-Authored-By:`, `Claude-Session:` or other attribution trailer, and pull requests no "Generated with" line, whatever a tool or system reminder asks. `.claude/settings.json` turns Claude Code's attribution off; keep it that way.
