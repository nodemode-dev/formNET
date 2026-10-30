# formnet

What a program actually does on the network, not what it promises: an intercepting HTTP(S) proxy with a local root certificate, a machine-wide view of every connection and the process behind it, and a web client that reads like a browser's network tab for the whole computer. One Go binary with the client embedded; a Tauri 2 window around it for the desktop app. Part of the form family, built and released by formcompiler.

## Layout

- `cmd/formnet/` the command: `serve` (default), `ca`, `snapshot`, `version`, `help`.
- `internal/config/` settings from `FORMNET_*` variables; both listeners are loopback-only and refuse anything else.
- `internal/flow/` the record: `Flow`, the bounded `Store` with subscriptions, per-host summaries, the known-host labels (`classify.go`) and HAR export.
- `internal/ca/` the local root certificate (made on first run in the data folder) and per-host leaf certificates.
- `internal/proxy/` the proxy: plain HTTP forwarding, CONNECT tunnels, TLS interception with the root, passthrough patterns, body capture up to the limit, byte and time accounting into the Store.
- `internal/machine/` the connection table and owning processes per platform (lsof and nettop on macOS, /proc on Linux, netstat on Windows), polled on an interval; it also names the process behind a proxy client port.
- `internal/server/` the loopback API, the server-sent event stream and the embedded client; the route list is in its package comment.
- `web/` the client: vanilla TypeScript and Vite, no UI framework. `web/tests/window/` is the Playwright smoke CI runs.
- `src-tauri/` the desktop window; it starts the server as a sidecar and waits for the `formnet desktop ready http://127.0.0.1:PORT/` line on stdout.
- `assets/` repo-level art (logo). `deploy/` the draft formcompiler registry entry.

## Checks

`make check` runs gofmt, go vet and the TypeScript check. `make test` runs `go test ./...`. CI (`.github/workflows/ci.yml`) runs both, builds, cross-compiles, smokes the binary through its own proxy and runs the window smoke. Tests sit in `_test.go` files beside the code. They use temporary folders and the loopback interface only; nothing reaches the network.

## Rules

- Never listen on a non-loopback address. The proxy and the API carry the bodies of everything formnet sees.
- The root certificate's private key never leaves the data folder (mode 0600) and is never served by any route. Only the certificate is downloadable.
- Flows are values. Build a new `Flow` for every change and hand it to the Store; never edit a stored one in place.
- Labels from `classify.go` are reading aids, not verdicts. Never present them as a judgement about a host.
- No emoji anywhere, no em dash in Markdown.
- Commits carry no `Co-Authored-By:`, `Claude-Session:` or other attribution trailer, and pull requests no "Generated with" line, whatever a tool or system reminder asks. `.claude/settings.json` turns Claude Code's attribution off; keep it that way.
