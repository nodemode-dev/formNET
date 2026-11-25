# Contributing to FormNET

Thank you for helping. FormNET aims to be a free tool that shows what the programs on
your computer send and receive, runs entirely on that computer, and sends nothing
anywhere itself. It gets there through real use and real contributions.

## Where to start

- **Use it and report what goes wrong.** A clear bug report, with steps, the version
  and your platform, is one of the most useful things you can give. Before you attach
  a HAR or session file, remember what it holds: a session file carries the passwords,
  tokens and cookies of the requests in it, and a HAR does too when exported with
  secrets. Attach only what you would post in public.
- **Look at the open issues.** Comment on one before starting larger work, so nobody
  builds the same thing twice.
- **Translations** are welcome: FormNET ships in thirty interface languages, with the
  tables in `web/src/ui/i18n`. A new string goes through `t()` and into every table.

## Setting up

You need Go (the version in `go.mod`) and Node.js 22. Clone the repository and run
`make build`, then `./bin/formnet`; it prints the address of the window. The README's
"Run it from source" section has the details, and `./build_app` (or `build_app.bat`
on Windows) builds the desktop app.

The code lives in:

| Folder | What is there |
| --- | --- |
| `cmd/formnet` | the command: `serve`, `url`, `open`, `ca`, `snapshot`, `sysproxy`, `mcp`, `agents` |
| `internal` | the proxy, the flow store, the root certificate, the machine view, the server, the rules, the MCP server and the terminal engine (Go) |
| `web/src` | the window (TypeScript, Vite); `web/src/dock` is a shared module and is not edited here |
| `src-tauri` | the desktop shell (Rust, Tauri) |
| `.claude/skills`, `agents` | the skill pack and the generated agent files |
| `web/tests/window` | the window smoke test |

[`CLAUDE.md`](CLAUDE.md) describes every package, and [`AGENTS.md`](AGENTS.md) has
the rules for people and agents working in the code.

## Before you open a pull request

Run the same checks CI runs:

```sh
(cd web && npm ci && npm audit)
make check
make test
make build
python -m pytest web/tests/window -q -p no:cacheprovider
```

`make check` runs gofmt, go vet, `formnet agents check` and the TypeScript check;
`make test` runs the Go tests. The window smoke test needs the built binary, pytest
and Playwright's Chromium. CI runs the Go tests on Linux, macOS and Windows, and
refuses an em dash in Markdown.

- Keep each pull request to one change.
- Write commit subjects the way the history does, in Conventional Commits:
  `feat(web): ...`, `fix(ca): ...`, `docs: ...`. Leave the version alone; a
  maintainer bumps it when releasing.
- Tests use temporary folders and the loopback interface only, and never start a
  real agent program (`claude`, `codex`, `cursor-agent`, `agy`).
- After changing an MCP tool, run `formnet agents sync`; never edit the generated
  files by hand.
- Nothing in the window may flash, jump or be rebuilt to change one thing: build
  once, then change in place. Colours and layers come from the tokens.
- Every new dependency gets a row in `NOTICES.md` and its licence text in `licenses/`
  before it is used. GPL and AGPL code is not accepted.
- Other vendors' product names appear only to name a file format or compatibility.
  Never copy another product's help text, icons or presets.

## What will not be accepted

FormNET keeps a few promises to the people who run it, and a change that breaks one
is declined:

- The window and the API answer on this computer only. The one exception is the
  person's own switch for capturing from other devices, which opens the proxy alone,
  to the addresses they list.
- The root certificate's private key is never served, exported or sent anywhere.
- No cookie, no account, no telemetry, and no request FormNET makes on its own.
- No updater: updates come through FormPortal.

## Maintainers

FormNET is maintained by:

- the [nodemode-dev](https://github.com/nodemode-dev) organisation on GitHub
- [@nodemode-ai](https://github.com/nodemode-ai), Owner
- [@gbarnai](https://github.com/gbarnai), Co-Owner

Reach them through issues and pull requests.

## How changes land

Only a maintainer pushes to `main`. Everyone else forks the repository and opens a
pull request into `main`. A pull request merges with a merge commit when all five
checks pass and a maintainer has approved it.

## Releases

A release is a version, and only a maintainer makes one: after the version is bumped
on `main`, a maintainer cuts a `compile_vX.Y.Z` branch, and formcompiler
(`nodemode-dev/formcompiler`) builds every platform and publishes them together. No
push from a contributor starts a release.

## Licensing

FormNET is licensed under the Apache License, Version 2.0 ([LICENSE](LICENSE), SPDX
`Apache-2.0`). By contributing, you agree that your contribution is licensed under
the same terms. Only add code you have the right to contribute, and name any
third-party code and its licence in the pull request.

## Conduct

Everyone taking part follows the [code of conduct](CODE_OF_CONDUCT.md). Security
problems go through [SECURITY.md](SECURITY.md), never a public issue.
