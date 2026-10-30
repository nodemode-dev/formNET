"""Window smoke for formnet: the built binary serves the client, the three
views switch, the status strip shows the proxy, and a request sent through
the proxy appears in the Flows table with its detail pane.

Run from the repo root after `make build`:
    python -m pytest web/tests/window -q
"""

from __future__ import annotations

import os
import secrets
import socket
import subprocess
import tempfile
import time
import json
import urllib.error
import urllib.request
from pathlib import Path

import pytest
from playwright.sync_api import Page, sync_playwright

ROOT = Path(__file__).resolve().parents[3]
BINARY = ROOT / ("bin/formnet.exe" if os.name == "nt" else "bin/formnet")


def free_port() -> int:
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


def wait_for(url: str, timeout: float = 15.0) -> None:
    deadline = time.time() + timeout
    last = "no attempt"
    while time.time() < deadline:
        try:
            with urllib.request.urlopen(url, timeout=1) as res:
                if res.status == 200:
                    return
                last = f"status {res.status}"
        except (urllib.error.URLError, ConnectionError, OSError) as err:
            last = str(err)
        time.sleep(0.2)
    raise RuntimeError(f"{url} did not come up: {last}")


@pytest.fixture(scope="module")
def server():
    if not BINARY.exists():
        pytest.skip(f"{BINARY} is missing; run `make build` first")
    port, proxy_port = free_port(), free_port()
    token = secrets.token_hex(16)
    with tempfile.TemporaryDirectory(prefix="formnet-window-") as data:
        env = {
            **os.environ,
            "FORMNET_DATA": data,
            "FORMNET_ADDR": f"127.0.0.1:{port}",
            "FORMNET_PROXY_ADDR": f"127.0.0.1:{proxy_port}",
            "FORMNET_TOKEN": token,
        }
        proc = subprocess.Popen(
            [str(BINARY), "serve"],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
        try:
            try:
                wait_for(f"http://127.0.0.1:{port}/healthz")
            except RuntimeError as err:
                if proc.poll() is not None:
                    _, stderr = proc.communicate(timeout=2)
                    raise RuntimeError(f"formnet exited with {proc.returncode}: {stderr}") from err
                raise
            yield {"url": f"http://127.0.0.1:{port}", "proxy": f"http://127.0.0.1:{proxy_port}", "token": token}
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()


@pytest.fixture(scope="module")
def page(server):
    with sync_playwright() as pw:
        browser = pw.chromium.launch()
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        pg = context.new_page()
        errors: list[str] = []
        pg.on("console", lambda msg: errors.append(msg.text) if msg.type == "error" else None)
        pg.on("pageerror", lambda err: errors.append(str(err)))
        # The page is opened once with the token; the server sets the session
        # cookie and redirects to /.
        pg.goto(f"{server['url']}/?token={server['token']}")
        pg.wait_for_selector(".topbar")
        yield pg, errors
        browser.close()


def send_through_proxy(proxy: str, url: str) -> int:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({"http": proxy}))
    with opener.open(url, timeout=5) as res:
        return int(res.status)


@pytest.fixture(scope="session")
def target():
    """A throwaway local HTTP server to send through the proxy.

    formnet refuses requests to its own addresses, so the smoke needs a
    different loopback listener as the destination.
    """
    import http.server
    import threading

    class Quiet(http.server.BaseHTTPRequestHandler):
        def do_GET(self):  # noqa: N802 (stdlib name)
            body = b'{"hello":"formnet"}'
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *args):  # silence
            return

    httpd = http.server.HTTPServer(("127.0.0.1", 0), Quiet)
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{httpd.server_address[1]}"
    finally:
        httpd.shutdown()


def test_session_redirect_and_cookie_required(page: tuple[Page, list[str]], server) -> None:
    pg, _ = page
    assert pg.url == server["url"] + "/", f"landed on {pg.url}, not /"
    assert "token" not in pg.url
    # A client without the cookie (a plain urllib request) gets 401 JSON.
    try:
        urllib.request.urlopen(server["url"] + "/api/status", timeout=5)
        raise AssertionError("no-cookie /api/status answered 200")
    except urllib.error.HTTPError as err:
        assert err.code == 401, f"no-cookie /api/status answered {err.code}"
        assert "error" in json.loads(err.read().decode())


def test_title_and_views(page: tuple[Page, list[str]]) -> None:
    pg, _ = page
    assert pg.title() == "formnet"
    for name in ("Hosts", "Machine", "Setup", "Flows"):
        pg.get_by_role("tab", name=name, exact=True).click()
        panel = pg.locator(f".view-{name.lower()}")
        assert panel.is_visible(), f"{name} view did not show"


def test_setup_shows_proxy_address(page: tuple[Page, list[str]], server) -> None:
    pg, _ = page
    pg.get_by_role("tab", name="Setup", exact=True).click()
    addr = server["proxy"].removeprefix("http://")
    pg.wait_for_selector(f".view-setup code.big:has-text('{addr}')", timeout=10000)
    assert pg.locator(".view-setup").get_by_text("Root certificate").is_visible()


def test_proxied_request_appears_with_detail(page: tuple[Page, list[str]], server, target) -> None:
    pg, errors = page
    pg.get_by_role("tab", name="Flows", exact=True).click()
    status = send_through_proxy(server["proxy"], target + "/hello")
    assert status == 200
    row = pg.locator("table.flows tbody tr", has_text="127.0.0.1").first
    row.wait_for(timeout=5000)
    assert "/hello" in row.inner_text()
    row.click()
    pane = pg.locator("aside.detail")
    pane.wait_for(state="visible", timeout=5000)
    pg.locator(".subtab", has_text="Request").click()
    pg.wait_for_selector("aside.detail dl.headers dt", timeout=5000)
    assert pane.locator("dl.headers dt").count() >= 1
    pg.keyboard.press("Escape")
    pane.wait_for(state="hidden", timeout=2000)
    assert errors == [], f"console errors: {errors}"
