---
name: find-formnet
description: "Finds the running FormNET: whether it is up, its API and proxy addresses, its version and its data folder. Use first in any FormNET task, after a port moved, or when a tool says FormNET is not running."
---

# Find FormNET

## Purpose

Establish that FormNET runs and where, without guessing ports. Hands back the API address, the proxy address, the root certificate path and the data folder for the later steps.

## Preconditions

None. This runs first.

## Procedure

1. **With the MCP tools:** call `proxy_status`. A good answer carries `addr` (the proxy), `listening`, `intercept_mode`, `ca_path` and `formnet_version`. An answer that says FormNET is not running means exactly that; go to step 4.
2. **From a shell:** read `endpoints.json` in the data folder (`~/formnet` unless `FORMNET_DATA` is set). It names `addr` (API and web client) and `proxy_addr`. Never assume 8760 and 8761: they are preferences, and FormNET moves to a free loopback port when another program holds one, saying so in its start notices.
3. **Confirm it is live:** `curl -s --noproxy '*' http://[addr]/healthz` needs no token and answers `{status, product, version, proxy_addr, proxy_listening}`. A file with no answer behind it was left by a crash.
4. **Not running:** ask the person to open the FormNET app or run `formnet serve`. Do not start it yourself unless asked. When it starts it prints the address with the token on stderr; `formnet url` prints the same later. Both contain the token: use them to open the page on this computer, never paste them anywhere.
5. **Wrong data folder:** if the person runs FormNET with `FORMNET_DATA` set, every command and the MCP server need the same value, or they read another folder's files.

## Gate

The proxy address and the API address are known from `proxy_status`, `endpoints.json` or `/healthz`, and `/healthz` (or a tool call) answered.

## Hands back

`addr`, `proxy_addr`, `ca_path`, the version and the data folder.

## Owned resources

- `references/api-and-files.md` the files in the data folder and every route.
