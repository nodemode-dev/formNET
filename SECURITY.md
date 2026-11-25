# Security policy

## Supported versions

Security fixes go into the newest release. Please update before reporting, and check whether the problem is still there.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Anything older | No |

## Reporting a vulnerability

**Do not open a public issue for a security problem.** Use GitHub's private reporting instead: on the repository's **Security** tab, choose **Report a vulnerability**. Include what you found, the steps to reproduce it, the version and platform, and what an attacker could do with it.

You can expect an acknowledgement within a week. We will keep you informed while it is fixed and credit you in the release notes, unless you would rather not be named.

## What FormNET keeps, and where

Everything lives in the data folder, `~/formnet` on macOS and Linux and `%USERPROFILE%\formnet` on Windows, unless `FORMNET_DATA` names another. On macOS and Linux FormNET makes the folder with mode 0700, so only your user can open it; on Windows it sits in your user folder and keeps that folder's access list. Files marked "yours alone" are written with mode 0600 on macOS and Linux, and on Windows with an access list that names your account only, set when the file is created.

| File | What it holds | Who can read it |
| --- | --- | --- |
| `ca.pem` | The root certificate FormNET made on its first run. Public: it is what you install to trust FormNET. | Anyone you give it to |
| `ca-key.pem` | The root's private key. With it, anyone could open HTTPS for every site on any computer that trusts this root. It never leaves the folder: no route serves it, and `http://formnet.cert/` serves the certificate only. | Yours alone; FormNET refuses to start when other users can read it, and prints the command that fixes it |
| `token` | This run's session token. Every API call needs it. | Yours alone |
| `endpoints.json` | Where the running FormNET listens, its pid, version and the root's path. Never the token. Removed when FormNET stops. | Yours alone |
| `settings.json` | The HTTPS choice, the passthrough list, the system proxy choice, the throttle, capture from other devices, the breakpoint settings and the upstream proxy, including its password if you set one. | Yours alone |
| `rules.json` | Your rules. | Yours alone |
| `sysproxy.json` | While FormNET points the system proxy at itself: the settings it replaced, so they can be put back after a crash. | Yours alone |
| `sessions/*.formnet` | Sessions you saved: every flow with its bodies and WebSocket messages, exactly as captured, passwords, tokens and cookies included. | Yours alone |
| `exports/*.har` | HAR files an agent exported through `formnet mcp`. Passwords, tokens and cookies are left out unless you approved including them. | Yours alone |

Recordings live in memory and are gone when FormNET stops, unless you save a session or export them. A HAR export from the window leaves out the values of `Authorization`, `Proxy-Authorization`, `Cookie` and `Set-Cookie` unless you choose "Export HAR with secrets...".

The root key and the upstream proxy's password are kept in files only your user can read, not in the system's password store (the Keychain, the Windows Credential Manager). The root key is used on every HTTPS handshake FormNET opens, and the password on every request it sends upstream, so they must be read without a prompt; the data folder already holds the root key, which is the more sensitive of the two, so a password store would not make the folder safer. The cost: a backup of the data folder carries both. Keep backups of it private, or leave the folder out of them.

## What FormNET listens on

- **The API and the page** answer on `127.0.0.1` only, always. Everything beyond the page's own files and `/healthz` (which says only that FormNET is up, its version and where its proxy is) needs this run's token, and no cookie is ever set. Requests from another site are refused, and so are writes that do not come from FormNET's own page.
- **The proxy** listens on `127.0.0.1` only, unless you turn on capture from other devices (off by default). That switch also opens the proxy, and only the proxy, on this computer's network addresses, for the addresses you allow: it cannot turn on with an empty allow list, and any other client is closed before anything is read from it. A device on that list may not reach this computer's loopback addresses through FormNET.
- **The terminal panel** opens a shell only when FormNET's own page asks with the token; its socket refuses any other origin.
- **`formnet mcp`** speaks only on its standard input and output, to the agent program that started it, and reads the running FormNET through the same token-guarded API.

## What counts

FormNET sees everything that passes through its proxy, so the things that matter most are the ways a recording, the root key or the token could reach someone else:

- Anything that makes the API, the page or the terminal reachable from another computer, or from a web page on another origin.
- Anything that serves or leaks `ca-key.pem`, the token, a recording or a session without the person's action.
- The proxy connecting to FormNET's own addresses, or to a loopback service through a host name.
- A rule, a map local or a session file writing or reading outside where it should.
- An agent changing state through `formnet mcp` without the approval its tool asks for.
- The system proxy left pointing at a FormNET that is no longer running.

Bugs that need someone to already control your account, or to run a modified build of FormNET, are ordinary bugs: please open an issue for those.
