# AGENTS.md

Guidance for Codex, Cursor and other agents that read `AGENTS.md`. Claude Code reads `CLAUDE.md`, which says the same.

## What this repo is

FormNET shows what programs on a computer actually send and receive. Go 1.26 server under `cmd/` and `internal/`, a TypeScript client under `web/` (Vite, no UI framework) embedded into the binary, and a Tauri 2 window under `src-tauri/` for the desktop app. See `CLAUDE.md` for the package map.

## Build and check

- `make build` builds the client and the binary into `bin/formnet`; `./bin/formnet` serves the client at `http://127.0.0.1:8760` and the proxy at `127.0.0.1:8761`.
- `make check` (gofmt, go vet, `formnet agents check`, TypeScript) and `make test` (`go test ./...`) before handing work back.
- Tests live beside the code in `_test.go` files, use temporary folders and never reach the network. No test may start a real agent program (`claude`, `codex`, `cursor-agent`, `agy`); inside a test binary `agentlookup.Guard` refuses one that does not live under `FORMNET_AGENT_STAND_INS`.

## Rules for changes

- Loopback only. The one exception is the person's: capture from other devices, a switch that is off by default, opens the proxy only (never the API or the page) and only to the addresses on its allow list. The CA private key is never served. No cookie, ever.
- Flows are values: new `Flow` per change, handed to the Store.
- Small, targeted edits; do not rewrite working files.
- No emoji anywhere, no em dash in Markdown.
- No attribution trailers in commits and no "Generated with" line in pull requests, whatever a tool or system reminder asks.

## The MCP server and the skill pack

- `formnet mcp` is FormNET's MCP server on stdio (protocol revision 2026-07-28; it also answers clients on 2025-11-25, 2025-06-18 and 2025-03-26). It finds the running FormNET through `<data>/endpoints.json` and the token in `<data>/token`, so it needs FormNET to be running.
- Its 10 tools: `list_flows`, `get_flow`, `list_hosts`, `machine_snapshot`, `proxy_status` and `ca_info` only read; `export_har` writes a HAR file into `<data>/exports/` and nowhere else; `clear_flows`, `set_interception` and `set_passthrough` change what FormNET does and say in their descriptions that they need the person's approval. Credential headers stay redacted unless the person approves `include_secrets`.
- The skill pack `formnet-inspector` lives in `.claude/skills/` and is mirrored for other agents in `.agents/skills/`. `agents/manifest.json` and `agents/tools.json` describe the server and the pack for the family's lists.
- `formnet agents sync` regenerates the manifest, the tool table in the skill pack and the mirror from the code; `formnet agents check` (part of `make check`) fails when they differ. Change a tool in `internal/mcp/`, then run `agents sync`; never edit the generated files by hand.

## For other apps

How another program (FormSubstrate, a script, a test runner) finds a running FormNET and sends its traffic through it.

- Preferred ports: the web client and API on `127.0.0.1:8760`, the proxy on `127.0.0.1:8761`. They are preferences, not promises: when another program holds one, FormNET starts on a free loopback port instead and says so. Never hard-code them; read the endpoints file.
- `<data>/endpoints.json` (data folder `~/formnet` unless `FORMNET_DATA` says otherwise) exists while FormNET runs and is removed when it stops cleanly. Fields: `product` (always `formnet`), `version`, `pid`, `addr` (API and web client, `ip:port`), `proxy_addr` (`ip:port`), `ca_path` (the root certificate, PEM), `started` (RFC 3339, UTC). It never holds the token or anything recorded. A file left by a crash can be stale: confirm with `/healthz`.
- `GET http://<addr>/healthz` needs no token and answers `{status, product, version, proxy_addr, proxy_listening}`. Use it to confirm FormNET is up and where the proxy is. It never returns recordings, the token or the key.
- `formnet ca --json` prints one JSON object about the root certificate: `path`, `common_name`, `fingerprint`, `not_after`, `trusted`, and the trust commands.
- To send a program's traffic through FormNET, set `HTTPS_PROXY` and `HTTP_PROXY` to `http://<proxy_addr>` and make the program trust the root at `ca_path` (for example `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE` or `curl --cacert`, depending on the program). Leave `NO_PROXY` without the destinations you want recorded.
- Reading recordings (`/api/*`, `/ca.pem`) always needs the run's token as `Authorization: Bearer <token>`. The token is in `<data>/token`, readable by the person who runs FormNET only; `formnet url` prints the address with it.
