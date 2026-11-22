# HASHOUT_LOG, Go server, formnet

## 2026-11-22 · Port takeover order

8760 (API and web client) and 8761 (proxy) stay the preferred ports, whether they come from the defaults or from `FORMNET_ADDR` / `FORMNET_PROXY_ADDR`. At `serve` start both are bound first and only then read back, so there is no gap between picking a port and holding it; `proxy.Serve` takes the bound listener for this (`ListenAndServe` stays for the tests and wraps it).

When a bind fails, in this order:

1. Look for a leftover FormNET: `GET /healthz` on the taken API address and on the address the last run wrote to `endpoints.json`. Only an answer with `product: formnet` counts, and only when that FormNET holds a port this run wants (its API address, or the proxy address its health body names; an older body without `proxy_addr` counts for the default pair).
2. Ask it to stop with `POST /api/quit` and the last run's token, then retry both binds every 100 ms for up to 5 s. The old server closes its listeners at once on shutdown, so the ports come free well inside that.
3. If the quit is not accepted (an older build without the route, or a token that no longer matches), fall back to the pid in `endpoints.json`, but only when that pid is the process listening on the address right now and its program name starts with `formnet`. SIGTERM on Unix (the old run then shuts down cleanly), terminate on Windows. A process that did not identify as FormNET is never signalled.
4. Whatever is still taken: bind port 0 on the same loopback host, write one plain line to stderr naming the port and its holder (process name and pid from `machine.Listening` when the platform tells this user, "another program" otherwise, "another FormNET that would not stop" when step 2 and 3 failed), and put the same sentence into `/api/status` as `notices`. The status bar shows it in the hint slot. The desktop ready line, the Host guard and the proxy's refuse list all use the addresses actually bound.

Why move instead of fail: the desktop window starts FormNET as a sidecar and cannot ask the person to free a port; a FormNET on another port that says where it is beats no FormNET.

## 2026-11-22 · The quit route needs the token

`POST /api/quit` sits under `/api/`, so it needs the run's bearer token like every other route, and the write guard refuses it from a browser page on another origin. Without the token any program or any web page that can reach loopback could stop someone's recording. The new process reads `<data>/token` before it writes its own, so only someone who can read that file (the same user, mode 0600 or a user-only access list) can stop the old run. The handler answers 202 before it stops, so the caller knows the request was taken.

## 2026-11-22 · The endpoints file

`<data>/endpoints.json` is how other programs find FormNET after a move. Fields: `product`, `version`, `pid`, `addr`, `proxy_addr`, `ca_path`, `started` (UTC). Written atomically (a private temporary file renamed over the old one) once both listeners serve, readable by the user only, and removed on a clean exit. A stopping run removes it only while the file still names its own pid, so the run that replaced it keeps its file. It never holds the token or anything recorded. `formnet url` and `formnet open` read `addr` from it and fall back to the configured address when it is missing. `/healthz` now also says `proxy_addr` and `proxy_listening`, so a caller without the token can confirm the file and find the proxy.

## 2026-11-22 · Windows access lists through advapi32, not icacls

The root key, the token file and the endpoints file are created on Windows with a security descriptor that grants the current user full control and nothing else, with inheritance turned off (`D:P(A;;FA;;;<user SID>)`). `checkKeyMode` reads the key's access list and refuses it when an account other than the user, SYSTEM, the Administrators or the owner rights entry may read it, naming the accounts and giving the exact `icacls` line that fixes it (`/inheritance:r`, `/grant:r` for the user's SID, `/remove:g` for explicit entries).

Chosen over running `icacls.exe`: the descriptor is applied in the same `CreateFile` call that makes the file, so there is no moment when the key exists with the folder's inherited access; there is no child process (no window, no PATH or SystemRoot lookup, no timeout); and nothing depends on icacls output, which is localized and changes between Windows versions. The cost is a few `syscall` calls into advapi32 (`ConvertStringSecurityDescriptorToSecurityDescriptorW`, `GetNamedSecurityInfoW`, `ConvertSecurityDescriptorToStringSecurityDescriptorW`), still standard library only. Existing installs keep working: a key in the user profile inherits SYSTEM, Administrators and the user, which the check allows. The key stays a file only the user can read; it is not moved to a system password store.

## 2026-11-22 · Windows trust commands use double quotes

`certutil` lines printed for Windows wrap the path and the certificate name in double quotes: single quotes are not quotes in cmd.exe. A Windows path cannot hold a double quote. Unix lines keep POSIX single quoting.

## 2026-11-22 · No console windows from the machine view

`netstat` and `tasklist` run with `HideWindow` and `CREATE_NO_WINDOW`, so the desktop app no longer flashes a console every two seconds. The browser opener (`rundll32 url.dll,FileProtocolHandler`) is a GUI program, opens no console, and runs only from `formnet open` in a terminal, so it is left as it was.

## 2026-11-22 · netstat rows by column position

`netstat -ano` prints its TCP state in the system's language (`ABHÖREN`, `HERGESTELLT` in German). Rows are now read by position: protocol, local address, foreign address, then the state words, and the pid in the last column. Known English states map as before (`LISTENING` becomes `LISTEN`); an unknown word is `LISTEN` when the foreign address is `0.0.0.0:0`, `[::]:0` or `*:*`, and an established connection otherwise. UDP rows carry no state and stay `UDP`. Localized states that are neither (a localized `TIME_WAIT`) show as established, which keeps the row and its owner visible rather than dropping it.
