# AGENTS.md

Guidance for Codex, Cursor and other agents that read `AGENTS.md`. Claude Code reads `CLAUDE.md`, which says the same.

## What this repo is

formNET shows what programs on a computer actually send and receive. Go 1.26 server under `cmd/` and `internal/`, a TypeScript client under `web/` (Vite, no UI framework) embedded into the binary, and a Tauri 2 window under `src-tauri/` for the desktop app. See `CLAUDE.md` for the package map.

## Build and check

- `make build` builds the client and the binary into `bin/formnet`; `./bin/formnet` serves the client at `http://127.0.0.1:8760` and the proxy at `127.0.0.1:8761`.
- `make check` (gofmt, go vet, TypeScript) and `make test` (`go test ./...`) before handing work back.
- Tests live beside the code in `_test.go` files, use temporary folders and never reach the network.

## Rules for changes

- Loopback only, always. The CA private key is never served.
- Flows are values: new `Flow` per change, handed to the Store.
- Small, targeted edits; do not rewrite working files.
- No emoji anywhere, no em dash in Markdown.
- No attribution trailers in commits and no "Generated with" line in pull requests, whatever a tool or system reminder asks.
