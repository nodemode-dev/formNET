#!/usr/bin/env python3
"""Collect the licence texts of the Rust crates the desktop shell links into licenses/.

Run from the repository root after src-tauri/Cargo.lock changes, on macOS, Linux or Windows:

    python3 tools/collect_licenses.py            (Windows: py tools\\collect_licenses.py)

With cargo-about installed (cargo install cargo-about --locked --features cli) it writes
licenses/rust-src-tauri.html from src-tauri/about.toml and src-tauri/about.hbs. Without it, it
copies each shipped crate's licence files from the cargo registry into licenses/rust/<crate>-<version>/
(run `cargo fetch --locked` in src-tauri first). A crate whose package has no licence file gets a
NOTE.txt naming the licence its Cargo.toml declares, and the standard text of that licence is in
licenses/rust/standard/. Shipped means: reached from formnet-desktop through normal dependencies on
one of the five release targets, proc-macro crates and build dependencies left out.
"""
import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SHELL = ROOT / "src-tauri"
OUT = ROOT / "licenses"
TARGETS = ["aarch64-apple-darwin", "x86_64-apple-darwin", "x86_64-pc-windows-msvc",
           "x86_64-unknown-linux-gnu", "aarch64-unknown-linux-gnu"]
LICENSE_NAME = re.compile(r"^(un)?(licen[cs]e|licen[cs]es|copying|notice|copyright)([-_. ].*)?$", re.I)
STANDARD = OUT / "rust" / "standard"


def metadata(target):
    args = ["cargo", "metadata", "--locked", "--format-version", "1", "--manifest-path", str(SHELL / "Cargo.toml")]
    if target:
        args += ["--filter-platform", target]
    return json.loads(subprocess.run(args, check=True, capture_output=True, text=True).stdout)


def shipped():
    found = {}
    for target in TARGETS:
        meta = metadata(target)
        packages = {p["id"]: p for p in meta["packages"]}
        nodes = {n["id"]: n for n in meta["resolve"]["nodes"]}
        todo, seen = [meta["resolve"]["root"]], set()
        while todo:
            pid = todo.pop()
            if pid in seen:
                continue
            seen.add(pid)
            pkg = packages[pid]
            if any("proc-macro" in t["kind"] for t in pkg["targets"]):
                continue
            if pkg.get("source"):
                found.setdefault((pkg["name"], pkg["version"]), (pkg, set()))[1].add(target)
            for dep in nodes[pid]["deps"]:
                if any(k["kind"] is None for k in dep["dep_kinds"]):
                    todo.append(dep["pkg"])
    return found


def copy_files(src_dir, dest):
    copied = 0
    for p in sorted(src_dir.iterdir()):
        if not LICENSE_NAME.match(p.name):
            continue
        files = sorted(x for x in p.rglob("*") if x.is_file()) if p.is_dir() else [p]
        for f in files:
            target = dest / f.relative_to(src_dir)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(f, target)
            copied += 1
    return copied


def with_about():
    if subprocess.run(["cargo", "about", "--version"], capture_output=True).returncode != 0:
        return False
    subprocess.run(["cargo", "about", "generate", "--config", str(SHELL / "about.toml"),
                    "--manifest-path", str(SHELL / "Cargo.toml"), str(SHELL / "about.hbs"),
                    "-o", str(OUT / "rust-src-tauri.html")], check=True)
    shutil.rmtree(OUT / "rust", ignore_errors=True)
    print("rust: wrote licenses/rust-src-tauri.html with cargo-about")
    return True


def main():
    if "--registry" not in sys.argv[1:] and with_about():
        return
    (OUT / "rust-src-tauri.html").unlink(missing_ok=True)
    for old in (OUT / "rust").glob("*"):
        if old != STANDARD:
            shutil.rmtree(old) if old.is_dir() else old.unlink()
    crates = shipped()
    missing = []
    for (name, version), (pkg, _) in sorted(crates.items()):
        dest = OUT / "rust" / f"{name}-{version}"
        if copy_files(Path(pkg["manifest_path"]).parent, dest):
            continue
        missing.append(f"{name} {version} ({pkg.get('license') or 'no licence declared'})")
        dest.mkdir(parents=True, exist_ok=True)
        (dest / "NOTE.txt").write_text(
            f"{name} {version} declares the licence {pkg.get('license') or '(none)'} in its Cargo.toml and its "
            "package holds no licence file. The standard text of each licence named is in licenses/rust/standard/; "
            f"its source is {pkg.get('repository') or 'https://crates.io/crates/' + name}.\n", encoding="utf-8")
    print("rust: crates shipped", len(crates), "with a licence file", len(crates) - len(missing),
          "without one", len(missing))
    for line in missing:
        print("  no licence file:", line)


if __name__ == "__main__":
    main()
