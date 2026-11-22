# Adding the FormNET MCP server to a client

`formnet mcp` is an MCP server on stdio. The client starts it as a child process; it reads `[data]/endpoints.json` and `[data]/token` on every tool call, talks to the running FormNET on loopback, and starts no other program. FormNET itself has to be running already (the app, or `formnet serve`).

It speaks protocol 2026-07-28 (per-request `_meta`, `server/discover`, no session) and answers the `initialize` handshake of 2025-11-25, 2025-06-18 and 2025-03-26 for clients that still send it.

## Configuration

The command is the `formnet` binary with the single argument `mcp`. Give the full path when `formnet` is not on the client's PATH. Set `FORMNET_DATA` only when FormNET runs with a data folder other than `~/formnet`.

A project `.mcp.json`, read by MCP clients that use that file:

```json
{
  "mcpServers": {
    "formnet": {
      "command": "formnet",
      "args": ["mcp"]
    }
  }
}
```

Codex, in `~/.codex/config.toml`:

```toml
[mcp_servers.formnet]
command = "formnet"
args = ["mcp"]
```

Cursor, in `.cursor/mcp.json` or the user-level file:

```json
{
  "mcpServers": {
    "formnet": { "command": "formnet", "args": ["mcp"] }
  }
}
```

With a different data folder, add `"env": {"FORMNET_DATA": "[path to the data folder]"}` beside `args` (in TOML, `env = { FORMNET_DATA = "[path]" }`).

## What to expect

- Read-only tools run without asking in most clients. `export_har` writes one new file under `[data]/exports`. `clear_flows`, `set_interception` and `set_passthrough` are marked destructive, so a client that honours annotations asks the person first.
- When FormNET is not running, every tool answers with an error that says to start it. Start it and call the tool again; nothing needs restarting on the client side.
- Nothing the server writes to stdout is anything but protocol messages. Its one start line and any warnings go to stderr.

## Checking the connection

Ask the client to call `proxy_status`. A good answer names the proxy address and the interception mode. If the client cannot list the tools at all, run `formnet mcp` in a terminal: it prints its start line on stderr and waits for input; close it with Ctrl-D (Ctrl-Z then Enter on Windows).
