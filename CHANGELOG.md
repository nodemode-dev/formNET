# Changelog

Versions follow `v*.*.*` with dots. Dates are US Eastern.

## 0.1.3 · 2026-11-22

The desktop build can run on every system the family builds for, and the bundle says whose it is and what it carries.

- For the people who build FormNET: the desktop build no longer stops on a missing `src-tauri/Cargo.lock`. The lock is tracked, on the Tauri 2.12 line, and the page's Tauri package is on the same line.
- `build_app.bat` makes the same checks and takes the same options as `./build_app`, and `web/oc_install.bat` exists beside `web/oc_install`.
- Both build scripts stop, naming both values, when the `Makefile` and `web/package.json` carry different versions.
- Go is pinned to 1.26.9.
- The app carries `NOTICES.md` beside `LICENSE`: the fonts, the Tauri package and every Rust crate in the desktop shell, each with its licence.
- The app's copyright line and About name NodeAgentc, Inc.
- The test samples name no person.
