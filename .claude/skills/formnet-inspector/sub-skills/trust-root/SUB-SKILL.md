---
name: trust-root
description: "Explains trusting and untrusting FormNET's root certificate on macOS, Windows and Linux using only the commands FormNET prints. Use when HTTPS appears only as tunnels, a program reports an unknown certificate authority, or the person wants the root removed."
---

# Trust and untrust the root

## Purpose

Get the person to the exact command that makes this computer trust (or stop trusting) FormNET's root, and confirm the result. Hands back the trust state before and after.

## Preconditions

FormNET runs, or at least has made its root (`formnet ca` works without a running server).

## Procedure

1. **Read the state.** `ca_info` (MCP) or `formnet ca` prints the path, the SHA-256 fingerprint, the expiry and `trusted`. `formnet ca --json` gives the same as one object.
2. **Print the command, do not write one.** `formnet ca trust` prints what to run for this system and what it does; `ca_info` returns `trust_command`, `trust_hint` and `untrust_command`. Show them to the person exactly as printed. Never compose a `security`, `certutil` or `update-ca-certificates` line from memory, and never run one yourself: it changes the computer's trust for every program.
3. **Say what trust means.** While the root is trusted, programs that use the system store accept certificates FormNET makes for any host. The private key stays in the data folder, readable by the person only, and is never served.
4. **After the person runs it:** call `ca_info` again. With interception mode `auto` FormNET notices the change within about ten seconds and opens HTTPS for new connections without a restart; `proxy_status` shows `intercept` true.
5. **Browsers and runtimes with their own store.** The hint printed by `formnet ca trust` names the ones that need a second step on this system (for example Firefox, or the NSS database on Linux). Node needs `NODE_EXTRA_CA_CERTS` even with a trusted system store; see `route-traffic`.
6. **Untrust when done.** Show `untrust_command` from `ca_info` when the person finishes inspecting, and suggest `set_interception` with `off` if they keep FormNET running.

## Gate

`trusted` read back from `ca_info` or `formnet ca` matches what the person wanted.

## Hands back

The trust state before and after, and which command the person ran.

## Owned resources

- `references/mcp-tools.md` `ca_info`, `set_interception` and `proxy_status`.
