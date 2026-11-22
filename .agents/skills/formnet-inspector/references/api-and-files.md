# The API and the files

Everything below lives on this computer. The data folder is `~/formnet` unless `FORMNET_DATA` names another one (on Windows `%USERPROFILE%\formnet`).

## Files in the data folder

| File | What it holds | Who reads it |
| --- | --- | --- |
| `endpoints.json` | `product` (always `formnet`), `version`, `pid`, `addr` (API and web client, ip:port), `proxy_addr`, `ca_path`, `started` (UTC). Present while FormNET runs, removed on a clean stop. Never the token. | Any program that needs to find FormNET |
| `token` | This run's session token, one line. Readable by the person who started FormNET only. Changes on every start unless `FORMNET_TOKEN` is set. | Local commands, the MCP server |
| `ca.pem` | The root certificate, the public half. | Programs that must trust the proxy |
| `ca-key.pem` | The root's private key. Mode 0600 or a user-only access list. Never served, never read by the MCP server, never needed by an agent. | FormNET only |
| `settings.json` | Interception mode and the passthrough list chosen in the client or through the API. | FormNET at start |
| `exports/` | HAR files written by the `export_har` tool. | The person |

A crash can leave `endpoints.json` behind. Confirm with `/healthz` before trusting it.

## Routes

Every route but `/healthz` and the page needs `Authorization: Bearer [token]`. The `Host` header must be the loopback address, so call `127.0.0.1:[port]` directly and keep the call out of any proxy (`curl --noproxy '*'`).

| Route | Answer |
| --- | --- |
| `GET /healthz` | `{status, product, version, proxy_addr, proxy_listening}`; no token |
| `GET /api/status` | proxy, CA, flow counts, machine summary and start notices |
| `GET /api/flows` | flows without bodies; filters `host`, `domain`, `kind`, `client`, `q`, `since`, `pending`, `limit` |
| `GET /api/flows/[id]` | one flow with its captured bodies |
| `POST /api/flows/clear` | forget every flow (ask first) |
| `GET /api/hosts` | per-host totals, most traffic first |
| `GET /api/machine` | the latest connection table |
| `GET /api/proxy` | proxy status |
| `POST /api/proxy` | `{"intercept": "auto" or "on" or "off"}` and or `{"passthrough": [...]}` (ask first) |
| `GET /api/ca` | root certificate info and the trust and untrust commands |
| `GET /ca.pem` | the root certificate |
| `GET /api/export/har` | HAR 1.2, same filters as `/api/flows`; credentials redacted unless `secrets=1` |
| `GET /api/stream` | server-sent events: `flow.added`, `flow.updated`, `flow.cleared`, `machine.snapshot` |

## Calling it from a shell

macOS and Linux:

```bash
D="${FORMNET_DATA:-$HOME/formnet}"
A=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["addr"])' "$D/endpoints.json")
curl -s --noproxy '*' -H "Authorization: Bearer $(cat "$D/token")" "http://$A/api/flows?limit=20"
```

PowerShell 7 on Windows (`-NoProxy` needs PowerShell 7):

```powershell
$D = if ($env:FORMNET_DATA) { $env:FORMNET_DATA } else { "$env:USERPROFILE\formnet" }
$E = Get-Content "$D\endpoints.json" | ConvertFrom-Json
$T = (Get-Content "$D\token").Trim()
Invoke-RestMethod -Uri "http://$($E.addr)/api/flows?limit=20" -Headers @{ Authorization = "Bearer $T" } -NoProxy
```

The token appears only inside the command, never in its output. Do not echo it.

## Kinds of flow

| Kind | What FormNET saw |
| --- | --- |
| `http` | the request and response: headers, bodies up to the body limit, timing |
| `websocket` | the upgrade headers, then byte counts |
| `tunnel` | HTTPS passed through unopened: host, port and bytes only (the root is not trusted, interception is off, or the host is on the passthrough list) |
| `refused` | FormNET opened the connection and the program refused its certificate, usually because it pins certificates |
