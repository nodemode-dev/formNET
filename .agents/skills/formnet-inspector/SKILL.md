---
name: formnet-inspector
description: "Drives FormNET, the local network inspector, to see what a program or agent actually sends and receives. Use whenever the person wants to route a program, script, Node or Go service or coding agent through the FormNET proxy, find the running FormNET, trust or untrust its root certificate, read recorded requests and responses through the API or the FormNET MCP tools, export a HAR file, read the machine's connection table, or fix a program that refuses the proxy because it pins certificates. Trigger on FormNET, formnet, HTTPS_PROXY, NODE_EXTRA_CA_CERTS, NODE_USE_ENV_PROXY, what is this app sending, record this traffic, intercept HTTPS locally, list_flows, export_har, formnet snapshot, endpoints.json, or a flow of kind refused. Not for packet capture with Wireshark or tcpdump, not for remote servers or production traffic, and not for changing FormNET's own source code, which the repository's agent instructions cover."
---

# FormNET inspector

FormNET records what programs on this computer actually put on the wire: an intercepting HTTP(S) proxy on loopback, a root certificate made on first run, and a table of every connection with the process that owns it. This pack is the method for using it well from an agent: find it, point a program at it, read what it saw, and hand the person an export, without ever moving the recording off the machine.

## Layout

```
formnet-inspector/
  SKILL.md                     you are here, the router
  references/
    mcp-tools.md               the MCP tools, generated from the server's own list
    api-and-files.md           endpoints.json, the token, /healthz and the API routes
    connecting-agents.md       adding `formnet mcp` to an MCP client
  sub-skills/
    find-formnet/              is it running, where, and which data folder
    route-traffic/             HTTPS_PROXY and friends, Node, Go, Python, curl
    trust-root/                trusting and untrusting the root, per system
    read-flows/                list, filter, page and read one exchange
    export-har/                a HAR file under exports, redacted by default
    machine-table/             every connection and its process
    pinned-programs/           refused flows and the passthrough list
  evals/evals.json             evaluations and trigger phrasings
```

Paths inside a sub-skill resolve against the pack root: `references/[file].md` means `formnet-inspector/references/[file].md`.

## Routing

| The request is about | Load |
| --- | --- |
| Whether FormNET runs, its address, its port moved, the data folder | `sub-skills/find-formnet/SUB-SKILL.md` |
| Sending a program, a test run or an agent through the proxy | `sub-skills/route-traffic/SUB-SKILL.md` |
| HTTPS shows only tunnels, certificate errors, removing the root | `sub-skills/trust-root/SUB-SKILL.md` |
| What a request carried, what came back, polling new traffic | `sub-skills/read-flows/SUB-SKILL.md` |
| A HAR file, sharing a recording with a teammate | `sub-skills/export-har/SUB-SKILL.md` |
| Programs that ignore the proxy, listening ports, which process talks | `sub-skills/machine-table/SUB-SKILL.md` |
| A flow of kind refused, an app that breaks only through the proxy | `sub-skills/pinned-programs/SUB-SKILL.md` |

A broad task ("record what this CLI sends to its API and tell me what it uploads") usually runs find, route, read in that order, and trust only when the flows come back as `tunnel`.

## Two ways in

1. **The MCP tools** when the client has `formnet mcp` configured: the tool names, classes and parameters are in `references/mcp-tools.md`. Start with `proxy_status`. Setup is in `references/connecting-agents.md`.
2. **The loopback API** with the run's token, from a shell: `references/api-and-files.md`. Same data, same filters.

The MCP server only reads the running FormNET. When FormNET is not running every tool says so; ask the person to open the FormNET app or run `formnet serve`. Do not start it yourself unless the person asks, and never start it a second time with another data folder to get around an error.

## Rules that always apply

- **The recording stays on this computer.** Flows hold other programs' credentials, cookies and payloads. Never upload a flow, a HAR file or a screenshot of either to a paste site, an issue tracker, a chat or any remote service, and never copy them to another machine. Quote the few lines the answer needs, with credentials left out.
- **The token stays on this computer too.** Use it only inside a local command (`$(cat ~/formnet/token)`), never print it in an answer, and never put it in a file outside the data folder. `formnet url` prints it; do not paste that output anywhere.
- **Ask before you change anything.** `clear_flows`, `set_interception` and `set_passthrough` change the recording or the proxy for every program on the machine. `export_har` with `include_secrets` writes credentials to disk. Each needs the person's yes first.
- **Trust commands come from FormNET.** Print them with `formnet ca trust` or the `ca_info` tool and show them to the person to run. Never write a trust command from memory and never run one yourself.
- **Labels are hints.** The category and owner FormNET shows for a host are reading aids, not a verdict about it.

## When a step fails

| Symptom | Recovery |
| --- | --- |
| A tool says FormNET is not running | Ask the person to start it, then call the tool again. |
| A tool says the token was refused | FormNET restarted with another data folder or `FORMNET_TOKEN`; the MCP server needs the same `FORMNET_DATA`. |
| An unknown tool name | Read `references/mcp-tools.md`; names are exact and lower case. |
| Flows stop half way through a task | Read `last_seq` from the last answer and continue with `since`; nothing is lost by asking again. |

To tell a skill problem from a server problem, call `proxy_status` directly with no other context. If that fails too, the fault is the MCP connection or FormNET, not this method.
