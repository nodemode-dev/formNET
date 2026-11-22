---
name: machine-table
description: "Reads FormNET's machine table: every connection this computer holds and the process that owns it, bounded and filtered by process, pid or state. Use when a program ignores the proxy, to find which process listens on a port, or to see everything that talks to the network."
---

# Read the machine table

## Purpose

See connections the proxy never sees: programs that ignore proxy settings, local servers, UDP, and anything else with a socket. Hands back the processes and connections that answer the question.

## Preconditions

None for `formnet snapshot`, which reads the table once on its own. The tool and the API need a running FormNET.

## Procedure

1. **With the MCP tools:** `machine_snapshot` with `process` (part of the name), `pid` or `state` (`ESTABLISHED`, `LISTEN`, ...). `limit` bounds both lists (default 50); `truncated` says when more matched. `total_processes` and `total_connections` give the size of the whole table.
2. **From a shell:** `formnet snapshot` prints the table once as JSON, or `GET /api/machine` with the token returns the latest one the running FormNET took (every 2 seconds by default).
3. **Read it with care.** Remote addresses are IPs; FormNET looks up names only when `FORMNET_REVERSE_DNS=1` is set, and `remote_host_verified` says whether the name maps back. `source` names the tool the platform provided; `supported` false or an `error` means the table is partial on this system.
4. **Connect it to flows.** A flow's `client_pid` and `client_process` come from this table, so a process seen here can be found in `list_flows` with `q` set to its name.

## Gate

The answer names the processes or connections by pid and address, with the snapshot time.

## Hands back

The matching rows and the `taken` time.

## Owned resources

- `references/mcp-tools.md` the `machine_snapshot` parameters.
