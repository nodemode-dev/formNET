---
name: route-traffic
description: "Points a program, a test run or a coding agent at the FormNET proxy with the right environment for its runtime: HTTPS_PROXY and HTTP_PROXY, Node's NODE_USE_ENV_PROXY and NODE_EXTRA_CA_CERTS, Python and curl, and the Go loopback exception. Use when the person wants a program's traffic recorded."
---

# Route traffic through FormNET

## Purpose

Make a program send its HTTP(S) through the proxy and trust the root, so FormNET records it. Hands back the exact command or environment that was used and a first flow that proves it worked.

## Preconditions

`find-formnet` gave the proxy address and `ca_path`. For HTTPS contents (not just hosts and bytes), the root is trusted or interception is on; see `trust-root`.

## Procedure

1. **Set the proxy for the one program, not the whole shell profile:**
   `HTTPS_PROXY=http://[proxy_addr] HTTP_PROXY=http://[proxy_addr] [program]`. On Windows PowerShell set `$env:HTTPS_PROXY` and `$env:HTTP_PROXY` in that session only. Leave out of `NO_PROXY` every destination that should be recorded.
2. **Trust the root for that runtime** with `ca_path`:

   | Runtime | Proxy | Trust |
   | --- | --- | --- |
   | curl | `-x http://[proxy_addr]` | `--cacert [ca_path]` |
   | Node 24 or 22.21 and later | `NODE_USE_ENV_PROXY=1` (or `--use-env-proxy`) plus `HTTPS_PROXY` and `HTTP_PROXY` | `NODE_EXTRA_CA_CERTS=[ca_path]` |
   | Python requests | `HTTPS_PROXY`, `HTTP_PROXY` | `REQUESTS_CA_BUNDLE=[ca_path]` |
   | OpenSSL-based tools | `HTTPS_PROXY`, `HTTP_PROXY` | `SSL_CERT_FILE=[ca_path]` (replaces the bundle; prefer a system trust) |
   | Go (net/http default transport) | `HTTPS_PROXY`, `HTTP_PROXY` | the system store, after `trust-root` |

3. **Know the runtime's gaps:**
   - Node reads `NODE_USE_ENV_PROXY` and `NODE_EXTRA_CA_CERTS` only when the process starts; setting them inside a running program does nothing. A client created with its own `ca` option ignores `NODE_EXTRA_CA_CERTS`. Older Node versions ignore the proxy variables entirely unless the program uses a proxy agent.
   - Go's `ProxyFromEnvironment` never proxies requests to `localhost` or a loopback address, so a Go program talking to a local server is not recorded through the proxy. Use `machine-table` to see those connections.
   - FormNET itself refuses to connect to loopback addresses reached through a host name, and to its own ports.
4. **Pointing a coding agent at it:** start the agent from a shell with the same variables, so its API calls and its tool calls go through FormNET. Interception shows the agent's credentials in the recording: everything in the rules of the pack root applies. Start it the way the person normally does; this pack never starts an agent program itself.
5. **Prove it:** run one request and call `list_flows` with `limit` 5 (or `GET /api/flows?limit=5`). A new row with the program's `client_process` proves routing. A row of kind `tunnel` means routing works and the root is not trusted yet.

## Gate

A flow from the program is in the recording, with its kind stated.

## Hands back

The command or environment used, and the id and kind of the first flow.

## Owned resources

- `references/api-and-files.md` the kinds of flow and the routes.
- `references/mcp-tools.md` `list_flows` parameters.
