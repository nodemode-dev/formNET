---
name: read-flows
description: "Reads what FormNET recorded: lists flows with filters, pages through new traffic with a seq cursor, and reads one exchange's headers and bounded bodies through the MCP tools or the token-authenticated API. Use when the person asks what a program sent, what came back, or why a request failed."
---

# Read flows

## Purpose

Answer a question about recorded traffic with the fewest calls and the least data in context. Hands back the flows that answer it, by id, with the relevant lines quoted and credentials left out.

## Preconditions

Traffic was routed (`route-traffic`). For HTTPS contents, the flows are of kind `http`, not `tunnel`.

## Procedure

1. **Narrow first.** Call `list_flows` with the tightest filter the question allows: `host` (exact or `*.suffix`), `domain`, `kind`, `client`, `q` (text in URL, method, status, process or content type) and `pending`. `limit` defaults to 25 and returns the newest matches. `list_hosts` gives the overview when the host is not known yet.
2. **Poll, do not re-read.** Each answer has `last_seq`. Pass it as `since` to get only what arrived after it, for example while the person reproduces a bug.
3. **Open one exchange.** `get_flow` with the id returns headers, timing, TLS and the bodies. Bodies default to 8192 bytes each (`max_body_bytes` up to 65536); `truncated` says when more exists, and `capture_truncated` when FormNET kept only a prefix. Set `include_bodies` false to read headers only.
4. **Credentials are already hidden.** Authorization, Proxy-Authorization, Cookie and Set-Cookie read `[redacted]` in every MCP answer. Query strings and bodies are as captured and may hold keys or personal data: quote only what the answer needs.
5. **Through the API instead:** `GET /api/flows` with the same filters and `GET /api/flows/[id]`, with the bearer token from the data folder. The API returns credential headers as captured, so filter them out before quoting anything.
6. **Read the kind before concluding.** `tunnel` means FormNET only counted the bytes; `refused` means the program rejected FormNET's certificate (`pinned-programs`); an empty `error` with status 0 on a pending flow means it is still in progress.

## Gate

Every claim in the answer points at a flow id, and no credential or token appears in it.

## Hands back

The flow ids, the facts read from them, and any follow-up filter for the next poll.

## Owned resources

- `references/mcp-tools.md` parameters and classes of `list_flows`, `get_flow` and `list_hosts`.
- `references/api-and-files.md` the routes and the kinds of flow.
