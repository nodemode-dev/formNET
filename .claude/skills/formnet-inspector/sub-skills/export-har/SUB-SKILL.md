---
name: export-har
description: "Exports recorded flows as a HAR 1.2 file into FormNET's exports folder, redacted by default, and keeps it on the machine. Use when the person wants a recording to open in browser developer tools or another HAR viewer, or asks to save or share a capture."
---

# Export a HAR file

## Purpose

Write the flows the person wants into one file they can open locally. Hands back the file path and what the file does and does not hold.

## Preconditions

The flows exist (`read-flows`). Tunnels and refused flows have no HTTP shape and are not exported.

## Procedure

1. **Choose the flows** with the same filters as `list_flows` (`host`, `domain`, `kind`, `client`, `q`, `since`, `pending`) and an optional `limit` for the newest matches.
2. **Choose a plain file name**, such as `checkout-bug.har`: letters, digits, dot, dash and underscore, starting with a letter or digit. `.har` is added when missing. A path, a separator or `..` is refused, and so is a name that already exists: exports never replace a file.
3. **Call `export_har`.** It writes only into `[data]/exports`, a folder private to the person, and returns `path`, `bytes` and `secrets_included`.
4. **Secrets stay out by default.** The values of Authorization, Proxy-Authorization, Cookie and Set-Cookie read `[redacted]`. Query strings and request bodies are as captured. `include_secrets` true writes every header as captured: ask the person first, say that the file then holds live credentials, and suggest deleting it when done.
5. **Keep it on this computer.** Tell the person where the file is. Do not attach it to an issue, upload it, paste it or copy it elsewhere. If they want to share it with someone, say what it contains and let them decide and do it themselves.
6. **From a shell instead:** `GET /api/export/har` with the same filters and the bearer token streams the document; add `secrets=1` only with the person's yes.

## Gate

The file exists under `[data]/exports`, and the answer says whether it includes secrets.

## Hands back

The path, the size, and the redaction state.

## Owned resources

- `references/mcp-tools.md` the `export_har` parameters.
