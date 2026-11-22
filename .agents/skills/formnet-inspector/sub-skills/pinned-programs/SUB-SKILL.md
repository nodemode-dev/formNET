---
name: pinned-programs
description: "Diagnoses programs that refuse FormNET's certificate (flows of kind refused, usually certificate pinning) and puts their hosts on the passthrough list so they work through the proxy again, counted but not opened. Use when an app breaks only while routed through FormNET."
---

# Programs that pin certificates

## Purpose

Tell a pinned program apart from a broken setup, and fix it with the least change. Hands back the hosts added to passthrough and the proxy status read back.

## Preconditions

Interception is on (a refusal needs an opened connection). `find-formnet` and `read-flows` have run.

## Procedure

1. **Find the refusals.** `list_flows` with `kind` `refused`. Each row names the host, the program (`client_process`, `client_pid`) and an `error` such as `the client refused FormNET's certificate: unknown certificate authority`. Bursts are collapsed to one flow per program and host every 2 seconds.
2. **Tell pinning from missing trust.** If other programs' HTTPS to the same host opens fine and only this program refuses, it pins its certificates, or ships its own trust store. If every program refuses, the root is not trusted: go to `trust-root` instead.
3. **Ask, then add the hosts.** Read the current list with `proxy_status`, add the refused hosts (exact names, or a `*.suffix` pattern when the program uses many subdomains of one owner), and call `set_passthrough` with the whole new list. It changes the proxy for every program and is kept for the next start, so the person agrees first.
4. **Confirm.** The program works again, and its traffic to those hosts now shows as kind `tunnel`: hosts and bytes only, never contents. Say so plainly: FormNET cannot show what a pinned program sends.
5. **Undo later.** Remove the hosts with another `set_passthrough` when the person is done.

## Gate

The refused hosts are on the passthrough list read back from `proxy_status`, and the person knows those hosts are no longer opened.

## Hands back

The list before and after, and the program's new flow kind.

## Owned resources

- `references/mcp-tools.md` `set_passthrough` and `proxy_status`.
- `references/api-and-files.md` the kinds of flow.
