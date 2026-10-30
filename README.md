<h1 align="center">formnet</h1>

<p align="center"><b>What a website, an app or the whole machine actually does on the network: who it calls, how often, how much it sends, and what is inside.</b></p>

<p align="center">
  <img src="https://img.shields.io/badge/version-v0.1.0-f97316" alt="version v0.1.0" />
  <img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+" />
  <img src="https://img.shields.io/badge/Node.js-22%2B-339933?logo=nodedotjs&logoColor=white" alt="Node.js 22+" />
  <img src="https://img.shields.io/badge/license-Apache--2.0-555555" alt="license Apache-2.0" />
</p>

formnet is the network tab for the whole computer. A browser's developer tools show what one page requests; Fiddler and Charles show what one program sends through a proxy. formnet does both and adds the layer under them: every connection the machine holds, which process holds it and how many bytes have moved, so the question "what does this thing really do" has one answer in one window.

It ships as one static Go binary with its web client embedded, and as a desktop app (a window around that same binary). Nothing leaves the computer: both listeners bind the loopback interface only, and the recordings stay in memory until you export them.

## What it shows

- **Flows**: every HTTP request and response that goes through the proxy, with headers, bodies (up to a limit), sizes, timings and the TLS that was negotiated. Opaque tunnels (hosts you choose not to open, or everything while the root is untrusted) are still counted by host and bytes.
- **Hosts**: per-host totals (requests, bytes each way, errors, first and last seen), grouped by domain, with a label for widely documented third parties (analytics, advertising, CDN, error reporting) as a reading aid.
- **Machine**: the computer's connection table, refreshed every two seconds: each process, its remote endpoints, states and, where the platform reports it, bytes in and out. Flows through the proxy are labelled with the process that made them.
- **Export**: HAR 1.2 of any filtered view, which browsers and other tools open.

## Quick start

```shell
./build_app --server-only     # or: make build
./bin/formnet                 # client at http://127.0.0.1:8760, proxy at 127.0.0.1:8761
```

Then point a program at the proxy:

```shell
curl -x http://127.0.0.1:8761 http://example.com/                                # plain HTTP, recorded in full
curl -x http://127.0.0.1:8761 --cacert ~/formnet/ca.pem https://example.com/     # HTTPS, opened with the local root
HTTPS_PROXY=http://127.0.0.1:8761 HTTP_PROXY=http://127.0.0.1:8761 some-program
```

To open HTTPS from programs that use the system trust store (browsers, most apps), make the computer trust formnet's root once:

```shell
./bin/formnet ca trust        # prints the command for this OS; nothing is installed without you running it
```

The root is made on this computer on first run and its private key never leaves `~/formnet`. Programs that pin their certificates will refuse the opened connection; add their hosts to the passthrough list (in the client, or `FORMNET_PASSTHROUGH=*.example.com`) and formnet will tunnel them and count bytes only.

The machine view needs no setup and no privileges; it reads the same table `lsof`, `/proc` or `netstat` would show you.

## Settings

| Variable | Default | What |
| --- | --- | --- |
| `FORMNET_DATA` | `~/formnet` | data folder: the root certificate and key |
| `FORMNET_ADDR` | `127.0.0.1:8760` | client and API (loopback only) |
| `FORMNET_PROXY_ADDR` | `127.0.0.1:8761` | proxy (loopback only) |
| `FORMNET_BODY_LIMIT` | `262144` | bytes of each body kept |
| `FORMNET_FLOW_LIMIT` | `5000` | flows kept before the oldest are dropped |
| `FORMNET_PASSTHROUGH` | | hosts never opened, comma-separated, `*.suffix` allowed |
| `FORMNET_MACHINE_INTERVAL` | `2` | seconds between machine snapshots |

## Development

```shell
make check    # gofmt, go vet, TypeScript
make test     # go test ./...
cd web && npm run dev   # client on :5176 proxied to a running formnet
```

The desktop app is built by `./build_app` (macOS, Linux) or `build_app.bat` (Windows) and, for releases, by formcompiler.

## Licence

Apache-2.0. See `LICENSE`.
