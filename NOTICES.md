# Third-party notices

FormNET is Copyright 2026 NodeAgentc, Inc. and licensed under the Apache License 2.0 (see `LICENSE`). It distributes the following third-party works.

## The page (web client)

Built by Vite into `web/dist`, embedded in the formnet server and shown in the desktop window. Versions from `web/package-lock.json`.

| Package | Version | Licence | Source | Notes |
| --- | --- | --- | --- | --- |
| @fontsource/inter (Inter typeface) | 5.3.0 | OFL-1.1 | https://github.com/fontsource/font-files (fonts/google/inter) | font files for weights 400, 500 and 600 |
| @fontsource/jetbrains-mono (JetBrains Mono typeface) | 5.3.0 | OFL-1.1 | https://github.com/fontsource/font-files (fonts/google/jetbrains-mono) | font files for weights 400 and 700 |
| @tauri-apps/api | 2.12.5 | Apache-2.0 OR MIT | https://github.com/tauri-apps/tauri | the page's bridge to the desktop shell |
| @xterm/xterm | 6.0.0 | MIT | https://github.com/xtermjs/xterm.js | the terminal dock's emulator and its stylesheet; Copyright (c) 2017-2019 The xterm.js authors, (c) 2014-2016 SourceLair Private Company, (c) 2012-2013 Christopher Jeffrey |
| @xterm/addon-fit | 0.11.0 | MIT | https://github.com/xtermjs/xterm.js (addons/addon-fit) | sizes the terminal dock's emulator to its panel; Copyright (c) 2019 The xterm.js authors |

The SIL Open Font License 1.1 text is at https://openfontlicense.org.

## The server's Go modules

Linked into the `formnet` binary to decode `br` and `zstd` bodies for display and to run the terminal dock, pure Go (built with `CGO_ENABLED=0`). Versions from `go.mod`; licences as their `LICENSE` files in the module say. Only the packages listed in Notes are linked: the rest of `klauspost/compress` (gzhttp under Apache-2.0, the s2 command helpers under MIT, snappy under BSD-3-Clause) is not in the binary. `github.com/xyproto/randomstring` v1.0.5 (BSD-3-Clause, Copyright 2023 Alexander F. Rødseth) appears in `go.sum` only as a test dependency of the brotli module and is not linked. The full licence texts of the linked packages are in `licenses/`, copied unchanged from the modules, and travel with the desktop app.

| Module | Version | Licence | Source | Notes |
| --- | --- | --- | --- | --- |
| github.com/andybalholm/brotli | v1.3.0 | MIT | https://github.com/andybalholm/brotli | packages brotli and brotli/matchfinder; Copyright (c) 2009, 2010, 2013-2016 by the Brotli Authors |
| github.com/klauspost/compress | v1.20.5 | BSD-3-Clause | https://github.com/klauspost/compress | packages zstd, huff0, fse, internal/cpuinfo, internal/le, internal/regmask and the module root; Copyright (c) 2012 The Go Authors, Copyright (c) 2019 Klaus Post |
| github.com/klauspost/compress (zstd/internal/xxhash) | v1.20.5 | MIT | https://github.com/klauspost/compress | its own LICENSE.txt; Copyright (c) 2016 Caleb Spare |
| github.com/klauspost/compress (internal/snapref) | v1.20.5 | BSD-3-Clause | https://github.com/klauspost/compress | its own LICENSE; Copyright (c) 2011 The Snappy-Go Authors |
| github.com/coder/websocket | v1.8.15 | ISC | https://github.com/coder/websocket | the terminal dock's WebSocket; packages websocket, internal/bpool, internal/errd and internal/util, on every system; Copyright (c) 2025 Coder; `licenses/coder-websocket-LICENSE.txt` |
| github.com/creack/pty | v1.1.24 | MIT | https://github.com/creack/pty | the terminal dock's PTY on macOS and Linux (not linked on Windows); Copyright (c) 2011 Keith Rarick; `licenses/creack-pty-LICENSE.txt` |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause | https://go.googlesource.com/sys | package unix on macOS and Linux (the terminal's process session), package windows on Windows (ConPTY and the job object); Copyright 2009 The Go Authors, with Google's additional patent grant; `licenses/golang-x-sys-LICENSE.txt` and `licenses/golang-x-sys-PATENTS.txt` |

## The desktop shell's crates (src-tauri)

Every Rust crate linked into `formnet-desktop` for macOS (aarch64-apple-darwin, x86_64-apple-darwin), Windows (x86_64-pc-windows-msvc) or Linux (x86_64-unknown-linux-gnu, aarch64-unknown-linux-gnu), resolved from `src-tauri/Cargo.lock`: the normal dependency tree with default features, without build dependencies and proc-macro crates, which run at compile time and are not part of the program. Licence and source are the `license` and `repository` fields of `cargo metadata --format-version 1`. Where a crate offers a choice of licences, the recipient may use any one of them.

`option-ext` 0.2.0 is used unmodified under the Mozilla Public License 2.0; its source code is available from https://crates.io/crates/option-ext/0.2.0 and from its repository listed below, and the MPL-2.0 text is at https://mozilla.org/MPL/2.0/. The `icu_*`, `litemap`, `potential_utf`, `tinystr`, `writeable`, `yoke`, `zerofrom`, `zerotrie`, `zerovec` and `unicode-ident` crates include Unicode data under the Unicode License v3 (https://www.unicode.org/license.txt).

The system web views (WKWebView on macOS, Microsoft Edge WebView2 on Windows, WebKitGTK on Linux) are parts of the operating system or installed separately; they are not distributed with FormNET.

| Crate | Version | Licence | Platforms | Source |
| --- | --- | --- | --- | --- |
| adler2 | 2.0.1 | 0BSD OR MIT OR Apache-2.0 | macOS | https://github.com/oyvindln/adler2 |
| aho-corasick | 1.1.5 | Unlicense OR MIT | macOS, Windows, Linux | https://github.com/BurntSushi/aho-corasick |
| alloc-no-stdlib | 3.0.0 | BSD-3-Clause | macOS, Windows, Linux | https://github.com/dropbox/rust-alloc-no-stdlib |
| alloc-stdlib | 0.3.0 | BSD-3-Clause | macOS, Windows, Linux | https://github.com/dropbox/rust-alloc-no-stdlib |
| anyhow | 1.0.104 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/anyhow |
| async-broadcast | 0.7.2 | MIT OR Apache-2.0 | Linux | https://github.com/smol-rs/async-broadcast |
| async-channel | 2.5.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-channel |
| async-executor | 1.14.1 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-executor |
| async-io | 2.6.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-io |
| async-lock | 3.5.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-lock |
| async-process | 2.5.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-process |
| async-signal | 0.2.14 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-signal |
| async-task | 4.7.1 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/async-task |
| atk | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| atk-sys | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| atomic-waker | 1.1.2 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/atomic-waker |
| base64 | 0.23.1 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/marshallpierce/rust-base64 |
| bitflags | 1.3.2 | MIT/Apache-2.0 | Linux | https://github.com/bitflags/bitflags |
| bitflags | 2.13.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/bitflags/bitflags |
| block2 | 0.6.2 | MIT | macOS | https://github.com/madsmtm/objc2 |
| blocking | 1.7.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/blocking |
| brotli | 9.1.3 | BSD-3-Clause AND MIT | macOS, Windows, Linux | https://github.com/dropbox/rust-brotli |
| brotli-decompressor | 6.3.2 | BSD-3-Clause/MIT | macOS, Windows, Linux | https://github.com/dropbox/rust-brotli-decompressor |
| bytes | 1.12.2 | MIT | macOS, Windows, Linux | https://github.com/tokio-rs/bytes |
| cairo-rs | 0.18.5 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| cairo-sys-rs | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| cfb | 0.14.0 | MIT | macOS, Windows, Linux | https://github.com/mdsteele/rust-cfb |
| cfg-if | 1.0.5 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/cfg-if |
| concurrent-queue | 2.5.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/concurrent-queue |
| cookie | 0.18.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/SergioBenitez/cookie-rs |
| core-foundation | 0.10.1 | MIT OR Apache-2.0 | macOS | https://github.com/servo/core-foundation-rs |
| core-foundation-sys | 0.8.7 | MIT OR Apache-2.0 | macOS | https://github.com/servo/core-foundation-rs |
| core-graphics | 0.25.0 | MIT OR Apache-2.0 | macOS | https://github.com/servo/core-foundation-rs |
| core-graphics-types | 0.2.0 | MIT OR Apache-2.0 | macOS | https://github.com/servo/core-foundation-rs |
| core_detect | 1.0.0 | MIT/Apache-2.0 | macOS, Windows, Linux | https://github.com/thomcc/core_detect |
| crc32fast | 1.5.5 | MIT OR Apache-2.0 | macOS | https://github.com/srijs/rust-crc32fast |
| crossbeam-channel | 0.5.17 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/crossbeam-rs/crossbeam |
| crossbeam-utils | 0.8.23 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/crossbeam-rs/crossbeam |
| ctor | 1.0.14 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/mmastrac/linktime |
| dbus | 0.9.12 | Apache-2.0/MIT | Linux | https://github.com/diwic/dbus-rs |
| deranged | 0.5.9 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/jhpratt/deranged |
| dirs | 7.0.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://codeberg.org/dirs/dirs-rs |
| dirs-sys | 0.5.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dirs-dev/dirs-sys-rs |
| dispatch2 | 0.3.1 | Zlib OR Apache-2.0 OR MIT | macOS | https://github.com/madsmtm/objc2 |
| dlopen2 | 0.8.2 | MIT | Linux | https://github.com/OpenByteDev/dlopen2 |
| dpi | 0.1.2 | Apache-2.0 AND MIT | macOS, Windows, Linux | https://github.com/rust-windowing/winit |
| dunce | 1.0.5 | CC0-1.0 OR MIT-0 OR Apache-2.0 | macOS, Windows, Linux | https://gitlab.com/kornelski/dunce |
| embed_plist | 1.2.2 | MIT OR Apache-2.0 | macOS | https://github.com/nvzqz/embed-plist-rs |
| encoding_rs | 0.8.44 | (Apache-2.0 OR MIT) AND BSD-3-Clause | macOS, Windows, Linux | https://github.com/hsivonen/encoding_rs |
| endi | 1.1.1 | MIT | Linux | https://github.com/zeenix/endi |
| enumflags2 | 0.7.12 | MIT OR Apache-2.0 | Linux | https://github.com/meithecatte/enumflags2 |
| equivalent | 1.0.3 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/indexmap-rs/equivalent |
| erased-serde | 0.4.11 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/erased-serde |
| errno | 0.3.15 | MIT OR Apache-2.0 | Linux | https://github.com/lambda-fairy/rust-errno |
| event-listener | 5.4.2 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/event-listener |
| event-listener-strategy | 0.5.4 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/event-listener-strategy |
| fastrand | 2.5.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/fastrand |
| fdeflate | 0.3.7 | MIT OR Apache-2.0 | macOS | https://github.com/image-rs/fdeflate |
| field-offset | 0.3.6 | MIT OR Apache-2.0 | Linux | https://github.com/Diggsey/rust-field-offset |
| flate2 | 1.1.11 | MIT OR Apache-2.0 | macOS | https://github.com/rust-lang/flate2-rs |
| fnv | 1.0.7 | Apache-2.0 / MIT | macOS, Windows, Linux | https://github.com/servo/rust-fnv |
| foreign-types | 0.5.0 | MIT/Apache-2.0 | macOS | https://github.com/sfackler/foreign-types |
| foreign-types-shared | 0.3.1 | MIT/Apache-2.0 | macOS | https://github.com/sfackler/foreign-types |
| form_urlencoded | 1.2.3 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/servo/rust-url |
| futures-channel | 0.3.34 | MIT OR Apache-2.0 | Linux | https://github.com/rust-lang/futures-rs |
| futures-core | 0.3.34 | MIT OR Apache-2.0 | Linux | https://github.com/rust-lang/futures-rs |
| futures-executor | 0.3.34 | MIT OR Apache-2.0 | Linux | https://github.com/rust-lang/futures-rs |
| futures-io | 0.3.34 | MIT OR Apache-2.0 | Linux | https://github.com/rust-lang/futures-rs |
| futures-lite | 2.6.1 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/futures-lite |
| futures-task | 0.3.34 | MIT OR Apache-2.0 | Linux | https://github.com/rust-lang/futures-rs |
| futures-util | 0.3.34 | MIT OR Apache-2.0 | Linux | https://github.com/rust-lang/futures-rs |
| gdk | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| gdk-pixbuf | 0.18.5 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| gdk-pixbuf-sys | 0.18.0 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| gdk-sys | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| gdkwayland-sys | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| gdkx11 | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| gdkx11-sys | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| getrandom | 0.3.4 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-random/getrandom |
| gio | 0.18.4 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| gio-sys | 0.18.1 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| glib | 0.18.5 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| glib-sys | 0.18.1 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| glob | 0.3.4 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/glob |
| gobject-sys | 0.18.0 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| gtk | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| gtk-sys | 0.18.2 | MIT | Linux | https://github.com/gtk-rs/gtk3-rs |
| hashbrown | 0.17.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/hashbrown |
| hex | 0.4.3 | MIT OR Apache-2.0 | Linux | https://github.com/KokaKiwi/rust-hex |
| http | 1.5.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/hyperium/http |
| icu_collections | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| icu_locale_core | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| icu_normalizer | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| icu_normalizer_data | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| icu_properties | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| icu_properties_data | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| icu_provider | 2.4.0 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| idna | 1.1.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/servo/rust-url/ |
| idna_adapter | 1.2.2 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/hsivonen/idna_adapter |
| indexmap | 2.14.2 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/indexmap-rs/indexmap |
| infer | 0.22.0 | MIT | macOS, Windows, Linux | https://github.com/bojand/infer |
| is-docker | 0.2.0 | MIT | Linux | https://github.com/TheLarkInn/is-docker |
| is-wsl | 0.4.0 | MIT | Linux | https://github.com/TheLarkInn/is-wsl |
| itoa | 1.0.19 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/itoa |
| javascriptcore-rs | 1.1.2 | MIT | Linux | https://github.com/tauri-apps/javascriptcore-rs |
| javascriptcore-rs-sys | 1.1.1 | MIT | Linux | https://github.com/tauri-apps/javascriptcore-rs |
| json-patch | 4.2.0 | MIT/Apache-2.0 | macOS, Windows, Linux | https://github.com/idubrov/json-patch |
| jsonptr | 0.7.1 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/chanced/jsonptr |
| keyboard-types | 0.8.3 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-windowing/keyboard-types |
| libc | 0.2.191 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/libc |
| libdbus-sys | 0.2.7 | Apache-2.0/MIT | Linux | https://github.com/diwic/dbus-rs |
| linux-raw-sys | 0.12.1 | Apache-2.0 WITH LLVM-exception OR Apache-2.0 OR MIT | Linux | https://github.com/sunfishcode/linux-raw-sys |
| litemap | 0.8.3 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| lock_api | 0.4.14 | MIT OR Apache-2.0 | Windows, Linux | https://github.com/Amanieu/parking_lot |
| log | 0.4.35 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/log |
| memchr | 2.8.3 | Unlicense OR MIT | macOS, Windows, Linux | https://github.com/BurntSushi/memchr |
| memoffset | 0.9.1 | MIT | Linux | https://github.com/Gilnaa/memoffset |
| mime | 0.3.18 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/hyperium/mime |
| miniz_oxide | 0.8.9 | MIT OR Zlib OR Apache-2.0 | macOS | https://github.com/Frommi/miniz_oxide/tree/master/miniz_oxide |
| miniz_oxide | 0.9.1 | MIT OR Zlib OR Apache-2.0 | macOS | https://github.com/Frommi/miniz_oxide/tree/master/miniz_oxide |
| mio | 1.2.5 | MIT | macOS, Windows, Linux | https://github.com/tokio-rs/mio |
| muda | 0.20.0 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/muda |
| num-conv | 0.2.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/jhpratt/num-conv |
| objc2 | 0.6.5 | MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-app-kit | 0.3.2 | Zlib OR Apache-2.0 OR MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-core-foundation | 0.3.2 | Zlib OR Apache-2.0 OR MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-encode | 4.1.0 | MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-exception-helper | 0.1.1 | Zlib OR Apache-2.0 OR MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-foundation | 0.3.2 | MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-quartz-core | 0.3.2 | Zlib OR Apache-2.0 OR MIT | macOS | https://github.com/madsmtm/objc2 |
| objc2-web-kit | 0.3.2 | Zlib OR Apache-2.0 OR MIT | macOS | https://github.com/madsmtm/objc2 |
| once_cell | 1.21.4 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/matklad/once_cell |
| open | 5.4.5 | MIT | macOS, Windows, Linux | https://github.com/Byron/open-rs |
| option-ext | 0.2.0 | MPL-2.0 | macOS, Windows, Linux | https://github.com/soc/option-ext.git |
| ordered-stream | 0.2.0 | MIT OR Apache-2.0 | Linux | https://github.com/danieldg/ordered-stream |
| os_pipe | 1.2.3 | MIT | macOS, Windows, Linux | https://github.com/oconnor663/os_pipe.rs |
| pango | 0.18.3 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| pango-sys | 0.18.0 | MIT | Linux | https://github.com/gtk-rs/gtk-rs-core |
| parking | 2.2.1 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/parking |
| parking_lot | 0.12.5 | MIT OR Apache-2.0 | Windows, Linux | https://github.com/Amanieu/parking_lot |
| parking_lot_core | 0.9.12 | MIT OR Apache-2.0 | Windows, Linux | https://github.com/Amanieu/parking_lot |
| percent-encoding | 2.3.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/servo/rust-url/ |
| phf | 0.13.1 | MIT | macOS, Windows, Linux | https://github.com/rust-phf/rust-phf |
| phf_shared | 0.13.1 | MIT | macOS, Windows, Linux | https://github.com/rust-phf/rust-phf |
| pin-project-lite | 0.2.18 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/taiki-e/pin-project-lite |
| piper | 0.2.5 | MIT OR Apache-2.0 | Linux | https://github.com/smol-rs/piper |
| plist | 1.10.4 | MIT | macOS, Windows, Linux | https://github.com/ebarnard/rust-plist/ |
| png | 0.18.1 | MIT OR Apache-2.0 | macOS | https://github.com/image-rs/image-png |
| polling | 3.11.0 | Apache-2.0 OR MIT | Linux | https://github.com/smol-rs/polling |
| potential_utf | 0.1.6 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| powerfmt | 0.2.1 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/jhpratt/powerfmt |
| proc-macro2 | 1.0.107 | MIT OR Apache-2.0 | Linux | https://github.com/dtolnay/proc-macro2 |
| quick-xml | 0.42.0 | MIT | macOS, Windows, Linux | https://github.com/tafia/quick-xml |
| quote | 1.0.47 | MIT OR Apache-2.0 | Linux | https://github.com/dtolnay/quote |
| raw-window-handle | 0.6.2 | MIT OR Apache-2.0 OR Zlib | macOS, Windows, Linux | https://github.com/rust-windowing/raw-window-handle |
| regex | 1.13.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/regex |
| regex-automata | 0.4.19 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/regex |
| regex-syntax | 0.8.12 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/regex |
| rfd | 0.17.2 | MIT | macOS, Windows, Linux | https://github.com/PolyMeilex/rfd |
| rustix | 1.1.6 | Apache-2.0 WITH LLVM-exception OR Apache-2.0 OR MIT | Linux | https://github.com/bytecodealliance/rustix |
| same-file | 1.0.6 | Unlicense/MIT | macOS, Windows, Linux | https://github.com/BurntSushi/same-file |
| scopeguard | 1.2.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/bluss/scopeguard |
| semver | 1.0.28 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/semver |
| serde | 1.0.229 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/serde-rs/serde |
| serde-untagged | 0.1.9 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/serde-untagged |
| serde_core | 1.0.229 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/serde-rs/serde |
| serde_json | 1.0.154 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/serde-rs/json |
| serde_spanned | 1.1.3 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/toml-rs/toml |
| serde_with | 3.25.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/jonasbb/serde_with/ |
| serialize-to-javascript | 0.1.2 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/chippers/serialize-to-javascript |
| shared_child | 1.1.2 | MIT | macOS, Windows, Linux | https://github.com/oconnor663/shared_child.rs |
| signal-hook-registry | 1.4.8 | MIT OR Apache-2.0 | Linux | https://github.com/vorner/signal-hook |
| simd-adler32 | 0.3.10 | MIT | macOS | https://github.com/mcountryman/simd-adler32 |
| simdutf8 | 0.1.5 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rusticstuff/simdutf8 |
| siphasher | 1.0.4 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/jedisct1/rust-siphash |
| slab | 0.4.12 | MIT | Linux | https://github.com/tokio-rs/slab |
| smallvec | 1.16.6 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/smallvec/smallvec |
| socket2 | 0.6.5 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/rust-lang/socket2 |
| softbuffer | 0.4.8 | MIT OR Apache-2.0 | Windows | https://github.com/rust-windowing/softbuffer |
| soup3 | 0.5.0 | MIT | Linux | https://gitlab.gnome.org/World/Rust/soup3-rs |
| soup3-sys | 0.5.0 | MIT | Linux | https://gitlab.gnome.org/World/Rust/soup3-rs |
| stable_deref_trait | 1.2.1 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/storyyeller/stable_deref_trait |
| syn | 3.0.10 | MIT OR Apache-2.0 | Linux | https://github.com/dtolnay/syn |
| tao | 0.37.3 | Apache-2.0 | macOS, Windows, Linux | https://github.com/tauri-apps/tao |
| tauri | 2.12.7 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/tauri |
| tauri-plugin-dialog | 2.8.4 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/plugins-workspace |
| tauri-plugin-fs | 2.7.1 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/plugins-workspace |
| tauri-plugin-opener | 2.7.3 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/plugins-workspace |
| tauri-plugin-shell | 2.6.0 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/plugins-workspace |
| tauri-plugin-single-instance | 2.5.5 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/plugins-workspace |
| tauri-runtime | 2.12.4 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/tauri |
| tauri-runtime-wry | 2.12.5 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/tauri |
| tauri-utils | 2.10.4 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/tauri |
| thiserror | 1.0.69 | MIT OR Apache-2.0 | Linux | https://github.com/dtolnay/thiserror |
| thiserror | 2.0.21 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/thiserror |
| time | 0.3.55 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/time-rs/time |
| time-core | 0.1.9 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/time-rs/time |
| tinystr | 0.8.5 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| tokio | 1.54.1 | MIT | macOS, Windows, Linux | https://github.com/tokio-rs/tokio |
| toml | 1.2.2+spec-1.1.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/toml-rs/toml |
| toml_datetime | 1.1.3+spec-1.1.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/toml-rs/toml |
| toml_parser | 1.1.6+spec-1.1.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/toml-rs/toml |
| toml_writer | 1.2.0+spec-1.1.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/toml-rs/toml |
| tracing | 0.1.44 | MIT | macOS, Windows, Linux | https://github.com/tokio-rs/tracing |
| tracing-core | 0.1.36 | MIT | macOS, Windows, Linux | https://github.com/tokio-rs/tracing |
| typeid | 1.0.3 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/dtolnay/typeid |
| unicode-ident | 1.0.27 | (MIT OR Apache-2.0) AND Unicode-3.0 | Linux | https://github.com/dtolnay/unicode-ident |
| unicode-segmentation | 1.13.3 | MIT OR Apache-2.0 | Windows | https://github.com/unicode-rs/unicode-segmentation |
| url | 2.5.9 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/servo/rust-url |
| urlpattern | 0.6.0 | MIT | macOS, Windows, Linux | https://github.com/denoland/rust-urlpattern |
| utf8_iter | 1.1.0 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/hsivonen/utf8_iter |
| utf_types | 1.0.0 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/hsivonen/utf_types |
| uuid | 1.29.3 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/uuid-rs/uuid |
| walkdir | 2.5.1 | Unlicense/MIT | macOS, Windows, Linux | https://github.com/BurntSushi/walkdir |
| web-time | 1.1.0 | MIT OR Apache-2.0 | macOS, Windows, Linux | https://github.com/daxpedda/web-time |
| webkit2gtk | 2.0.2 | MIT | Linux | https://github.com/tauri-apps/webkit2gtk-rs |
| webkit2gtk-sys | 2.0.2 | MIT | Linux | https://github.com/tauri-apps/webkit2gtk-rs |
| webview2-com | 0.39.1 | MIT | Windows | https://github.com/wravery/webview2-rs |
| webview2-com-sys | 0.39.1 | MIT | Windows | https://github.com/wravery/webview2-rs |
| winapi-util | 0.1.11 | Unlicense OR MIT | Windows | https://github.com/BurntSushi/winapi-util |
| window-vibrancy | 0.8.1 | Apache-2.0 OR MIT | macOS, Windows | https://github.com/tauri-apps/tauri-plugin-vibrancy |
| windows | 0.62.2 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-collections | 0.3.2 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-core | 0.62.2 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-future | 0.3.2 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-link | 0.2.1 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-numerics | 0.3.1 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-result | 0.4.1 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-strings | 0.5.1 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-sys | 0.60.2 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-sys | 0.61.2 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-targets | 0.53.5 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-threading | 0.2.1 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows-version | 0.1.7 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| windows_x86_64_msvc | 0.53.1 | MIT OR Apache-2.0 | Windows | https://github.com/microsoft/windows-rs |
| winnow | 1.0.5 | MIT | macOS, Windows, Linux | https://github.com/winnow-rs/winnow |
| writeable | 0.6.4 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| wry | 0.57.3 | Apache-2.0 OR MIT | macOS, Windows, Linux | https://github.com/tauri-apps/wry |
| x11 | 2.21.0 | MIT | Linux | https://github.com/AltF02/x11-rs.git |
| x11-dl | 2.21.0 | MIT | Linux | https://github.com/AltF02/x11-rs.git |
| yoke | 0.8.4 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| zbus | 5.19.0 | MIT | Linux | https://github.com/z-galaxy/zbus/ |
| zbus_names | 4.3.5 | MIT | Linux | https://github.com/z-galaxy/zbus/ |
| zcheapstr | 1.4.0 | MIT | Linux | https://github.com/z-galaxy/zcheapstr/ |
| zerofrom | 0.1.8 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| zerotrie | 0.2.5 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| zerovec | 0.11.9 | Unicode-3.0 | macOS, Windows, Linux | https://github.com/unicode-org/icu4x |
| zmij | 1.0.23 | MIT | macOS, Windows, Linux | https://github.com/dtolnay/zmij |
| zvariant | 5.15.1 | MIT | Linux | https://github.com/z-galaxy/zbus/ |
| zvariant_utils | 4.2.0 | MIT | Linux | https://github.com/z-galaxy/zbus/ |
