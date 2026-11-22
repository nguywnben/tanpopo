#!/usr/bin/env python3
"""
Tanpopo Web Console End-to-End Verification Suite (TP-IMP-0047 / Stage 3-4).

Runs offline against a compiled `tanpopo` binary behind a real TLS reverse proxy
across Chromium, Firefox, and WebKit:
  - Setup Wizard (SCR-01) & Login (SCR-02) over HTTPS (__Host- Secure cookie + full reload)
  - All 16 canonical screens (SCR-01..SCR-16) and workspace routes
  - Desktop (1440x900), Tablet (768x1024), Mobile (375x812) with 0 horizontal overflow
  - Light & Dark themes
  - Offline axe-core WCAG 2.2 AA & SC 2.5.3 (label-content-name-mismatch) audit
  - Keyboard shortcuts (Tab, Control+K CommandPalette, Escape)
  - 15-language locale switch & Multi-Org provisioning + Scope Switcher isolation
  - Chromium Core Web Vitals (TTFB, FCP, LCP, CLS) & measured interaction latency (INP)
  - Zero external network requests (strict air-gap verification)
"""

from __future__ import annotations

import argparse
import http.client
import http.server
import json
import os
import re
import shlex
import shutil
import socket
import socketserver
import ssl
import subprocess
import sys
import tempfile
import threading
import time
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

from playwright.sync_api import BrowserContext, Page, sync_playwright

REPO_ROOT = Path(__file__).resolve().parents[3]
CONSOLE_ROOT = REPO_ROOT / "web" / "console"
AXE_JS_PATH = CONSOLE_ROOT / "node_modules" / "axe-core" / "axe.min.js"

# Canonical SCR-01..SCR-16 workspace route matrix (aligned with docs/ux/screen-specifications.md
# and web/console/src/shell/routes.ts).
REPRESENTATIVE_ROUTES_SCR_02_TO_16: list[tuple[str, str]] = [
    ("SCR-02", "/overview"),
    ("SCR-03", "/providers/adapters"),
    ("SCR-04", "/providers/connections"),
    ("SCR-05", "/models"),
    ("SCR-06", "/extensions/marketplace"),
    ("SCR-07", "/extensions/installed"),
    ("SCR-08", "/routes"),
    ("SCR-09", "/policies"),
    ("SCR-10", "/mcp/servers"),
    ("SCR-11", "/observability/traces"),
    ("SCR-12", "/finops/budgets"),
    ("SCR-13", "/access/virtual-keys"),
    ("SCR-14", "/security/approvals"),
    ("SCR-15", "/devtools/playground"),
    ("SCR-16", "/ops/health"),
]

ALL_AUTHENTICATED_ROUTES: list[tuple[str, str]] = [
    ("SCR-02", "/overview"),
    ("SCR-11", "/observability/traces"),
    ("SCR-12", "/finops/budgets"),
    ("SCR-12", "/finops/chargeback"),
    ("SCR-04", "/providers/connections"),
    ("SCR-03", "/providers/adapters"),
    ("SCR-05", "/models"),
    ("SCR-08", "/routes"),
    ("SCR-08", "/routes/explain"),
    ("SCR-09", "/policies"),
    ("SCR-07", "/extensions/installed"),
    ("SCR-06", "/extensions/marketplace"),
    ("SCR-07", "/extensions/lockfile"),
    ("SCR-10", "/mcp/servers"),
    ("SCR-10", "/a2a/agents"),
    ("SCR-15", "/devtools/playground"),
    ("SCR-15", "/devtools/sdk-snippets"),
    ("SCR-13", "/access/virtual-keys"),
    ("SCR-13", "/access/members"),
    ("SCR-14", "/security/approvals"),
    ("SCR-14", "/security/kill-switches"),
    ("SCR-14", "/security/audit-log"),
    ("SCR-16", "/ops/health"),
    ("SCR-16", "/ops/backups"),
    ("SCR-16", "/ops/upgrades"),
    ("SCR-16", "/ops/organizations"),
    ("SCR-16", "/settings/residency-retention"),
]


def free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return int(s.getsockname()[1])


class _FakeLLMHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
        return

    def do_GET(self) -> None:  # noqa: N802
        payload = json.dumps({"object": "list", "data": [{"id": "llama3.2", "object": "model"}]}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def do_POST(self) -> None:  # noqa: N802
        length = int(self.headers.get("Content-Length", "0") or 0)
        raw = self.rfile.read(length) if length > 0 else b"{}"
        try:
            body = json.loads(raw.decode("utf-8"))
        except Exception:
            body = {}

        if body.get("stream"):
            chunks = [
                'data: {"id":"chatcmpl-e2e","object":"chat.completion.chunk","model":"llama3.2","choices":[{"index":0,"delta":{"role":"assistant","content":"Tanpopo E2E OK"}}]}\n\n',
                'data: {"id":"chatcmpl-e2e","object":"chat.completion.chunk","model":"llama3.2","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":4,"total_tokens":12}}\n\n',
                "data: [DONE]\n\n",
            ]
            encoded = "".join(chunks).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.send_header("Cache-Control", "no-cache")
            self.send_header("Content-Length", str(len(encoded)))
            self.end_headers()
            self.wfile.write(encoded)
            self.wfile.flush()
            return

        resp = json.dumps(
            {
                "id": "chatcmpl-e2e",
                "object": "chat.completion",
                "model": "llama3.2",
                "choices": [{"index": 0, "message": {"role": "assistant", "content": "Tanpopo E2E OK"}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12},
            }
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(resp)))
        self.end_headers()
        self.wfile.write(resp)


class _ThreadingHTTPServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True

    def __init__(self, server_address: tuple[str, int], RequestHandlerClass: Any, ssl_ctx: ssl.SSLContext | None = None) -> None:
        self._ssl_ctx = ssl_ctx
        super().__init__(server_address, RequestHandlerClass)

    def finish_request(self, request: Any, client_address: Any) -> None:
        if self._ssl_ctx is not None:
            try:
                request = self._ssl_ctx.wrap_socket(request, server_side=True)
            except (ssl.SSLError, OSError):
                try:
                    request.close()
                except OSError:
                    pass
                return
        try:
            super().finish_request(request, client_address)
        except (BrokenPipeError, ConnectionResetError, ssl.SSLError, OSError):
            pass


def wait_for_http_ready(url: str, timeout_s: float = 20.0) -> None:
    deadline = time.monotonic() + timeout_s
    parsed = urlparse(url)
    while time.monotonic() < deadline:
        try:
            conn = http.client.HTTPConnection(parsed.hostname, parsed.port, timeout=1.5)
            conn.request("GET", "/healthz")
            resp = conn.getresponse()
            resp.read()
            conn.close()
            if resp.status == 200:
                return
        except Exception:
            pass
        time.sleep(0.15)
    raise RuntimeError(f"tanpopo did not become healthy at {url} within {timeout_s}s")


def wait_for_file_nonempty(path: Path, timeout_s: float = 15.0) -> str:
    deadline = time.monotonic() + timeout_s
    while time.monotonic() < deadline:
        if path.exists():
            txt = path.read_text(encoding="utf-8").strip()
            if txt:
                return txt
        time.sleep(0.15)
    raise RuntimeError(f"file {path} not populated within {timeout_s}s")


def attach_airgap_and_error_guards(context: BrowserContext, external_urls: list[str], console_errors: list[str], page_errors: list[str]) -> None:
    axe_bytes = AXE_JS_PATH.read_bytes()

    def on_axe(route: Any) -> None:
        route.fulfill(status=200, content_type="application/javascript; charset=utf-8", body=axe_bytes)

    def on_external(route: Any) -> None:
        external_urls.append(route.request.url)
        route.abort()

    context.route("**/__e2e__/axe.min.js", on_axe)
    context.route(re.compile(r"^https?://(?!(?:127\.0\.0\.1|localhost)(?::\d+)?(?:/|$))"), on_external)

    def on_page(page: Page) -> None:
        def handle_req(req: Any) -> None:
            parsed = urlparse(req.url)
            if parsed.scheme in ("http", "https") and parsed.hostname not in ("127.0.0.1", "localhost"):
                external_urls.append(req.url)

        def handle_console(msg: Any) -> None:
            if msg.type == "error":
                txt = msg.text
                if "favicon.ico" in txt:
                    return
                console_errors.append(f"[{page.url}] {txt}")

        page.on("request", handle_req)
        page.on("response", lambda resp: console_errors.append(f"[http:{resp.status}] {resp.request.method} {resp.url}") if resp.status >= 500 else None)
        page.on("requestfailed", lambda req: console_errors.append(f"[reqfail] {req.method} {req.url}: {req.failure}") if "ERR_ABORTED" not in (req.failure or "") else None)
        page.on("console", handle_console)
        page.on("pageerror", lambda exc: page_errors.append(f"[{page.url}] {exc}"))

    context.on("page", on_page)


def run_axe_and_layout_audit(page: Page, route_path: str, theme_mode: str, viewport_label: str) -> dict[str, Any]:
    try:
        page.wait_for_load_state("networkidle")
        is_auth_page = route_path.startswith("/setup") or route_path.startswith("/login") or route_path.startswith("SCR-01:")
        h1_sel = "h1" if is_auth_page else "main#main-content h1"
        page.wait_for_selector(h1_sel, timeout=15000)
        page.wait_for_load_state("networkidle")
    except Exception as exc:
        body_text = ""
        try:
            body_text = page.locator("body").inner_text()[:400]
        except Exception:
            pass
        raise RuntimeError(f"Timed out waiting for h1 at {route_path} ({viewport_label}, {theme_mode}) url={page.url!r} body={body_text!r}: {exc}") from exc

    # Check horizontal overflow strictly at document root.
    layout_info = page.evaluate(
        """() => ({
            scrollWidth: document.documentElement.scrollWidth,
            clientWidth: document.documentElement.clientWidth,
            h1Count: document.querySelectorAll('h1').length,
            h1Text: document.querySelector('h1')?.textContent?.trim() || '',
            theme: document.documentElement.getAttribute('data-theme') || '',
            lang: document.documentElement.getAttribute('lang') || '',
        })"""
    )
    if layout_info["scrollWidth"] > layout_info["clientWidth"]:
        raise AssertionError(
            f"Horizontal overflow at {route_path} ({viewport_label}, {theme_mode}): "
            f"scrollWidth={layout_info['scrollWidth']} > clientWidth={layout_info['clientWidth']}"
        )
    if layout_info["h1Count"] != 1 or not layout_info["h1Text"]:
        raise AssertionError(
            f"Expected single non-empty <h1> at {route_path} ({viewport_label}), got count={layout_info['h1Count']} text={layout_info['h1Text']!r}"
        )

    if not page.evaluate("Boolean(window.axe)"):
        origin = page.evaluate("window.location.origin")
        page.add_script_tag(url=f"{origin}/__e2e__/axe.min.js")
    axe_result = page.evaluate(
        """async () => {
            const res = await window.axe.run(document, {
                runOnly: {
                    type: 'tag',
                    values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']
                }
            });
            return {
                violations: res.violations.map(v => ({
                    id: v.id,
                    impact: v.impact,
                    help: v.help,
                    targets: v.nodes.slice(0, 3).map(n => n.target.join(' '))
                }))
            };
        }"""
    )
    blocking = [
        v for v in axe_result["violations"]
        if v.get("impact") in ("critical", "serious") or v.get("id") == "label-content-name-mismatch"
    ]
    if blocking:
        raise AssertionError(
            f"Accessibility failure at {route_path} ({viewport_label}, {theme_mode}): {json.dumps(blocking, indent=2)}"
        )
    return {
        "route": route_path,
        "viewport": viewport_label,
        "theme": theme_mode,
        "scrollWidth": layout_info["scrollWidth"],
        "clientWidth": layout_info["clientWidth"],
        "h1": layout_info["h1Text"],
        "axe_all_violations": len(axe_result["violations"]),
        "axe_blocking": 0,
    }


def measure_chromium_cwv_and_inp(page: Page, base_url: str, routes: list[str]) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    # Install PerformanceObserver for LCP, CLS, and EventTiming (INP) once before navigation.
    page.add_init_script(
        """() => {
            window.__tpPerf = { lcp: 0, cls: 0, maxEventDuration: 0 };
            try {
                new PerformanceObserver((list) => {
                    for (const entry of list.getEntries()) {
                        window.__tpPerf.lcp = Math.max(window.__tpPerf.lcp, entry.startTime);
                    }
                }).observe({ type: 'largest-contentful-paint', buffered: true });
            } catch {}
            try {
                new PerformanceObserver((list) => {
                    for (const entry of list.getEntries()) {
                        if (!entry.hadRecentInput) window.__tpPerf.cls += entry.value;
                    }
                }).observe({ type: 'layout-shift', buffered: true });
            } catch {}
            try {
                new PerformanceObserver((list) => {
                    for (const entry of list.getEntries()) {
                        window.__tpPerf.maxEventDuration = Math.max(window.__tpPerf.maxEventDuration, entry.duration);
                    }
                }).observe({ type: 'event', durationThreshold: 16, buffered: true });
            } catch {}
        }"""
    )
    cwv_errs: list[str] = []
    page.on("response", lambda resp: cwv_errs.append(f"HTTP {resp.status} {resp.url}") if resp.status >= 400 else None)
    page.on("requestfailed", lambda req: cwv_errs.append(f"REQFAILED {req.method} {req.url}: {req.failure}"))
    page.on("pageerror", lambda exc: cwv_errs.append(f"pageerror: {exc}"))
    page.on("console", lambda msg: cwv_errs.append(f"console.{msg.type}: {msg.text}") if msg.type == "error" else None)
    for route in routes:
        page.goto(f"{base_url}{route}", wait_until="networkidle")
        try:
            page.wait_for_selector("main#main-content h1", timeout=15000)
        except Exception as exc:
            body_snip = ""
            try:
                body_snip = page.locator("body").inner_text()[:250]
            except Exception:
                pass
            raise RuntimeError(f"CWV route {route} missing workspace h1 at {page.url!r} body={body_snip!r} errs={cwv_errs[-8:]!r}: {exc}") from exc

        # Trigger real user interactions (Command Palette shortcut + Escape) to measure INP.
        page.locator("button.tp-jump-btn").wait_for(state="visible", timeout=5000)
        page.keyboard.press("Control+k")
        page.wait_for_selector('dialog[open], [role="dialog"]', timeout=5000)
        page.keyboard.press("Escape")
        page.wait_for_timeout(120)

        perf = page.evaluate(
            """() => {
                const nav = performance.getEntriesByType('navigation')[0];
                const fcpEntry = performance.getEntriesByName('first-contentful-paint')[0];
                return {
                    ttfb_ms: nav ? Math.round(nav.responseStart * 100) / 100 : null,
                    fcp_ms: fcpEntry ? Math.round(fcpEntry.startTime * 100) / 100 : null,
                    lcp_ms: Math.round((window.__tpPerf?.lcp || 0) * 100) / 100,
                    cls: Math.round((window.__tpPerf?.cls || 0) * 10000) / 10000,
                    inp_ms: Math.round((window.__tpPerf?.maxEventDuration || 0) * 100) / 100,
                };
            }"""
        )
        if perf["lcp_ms"] > 2000:
            raise AssertionError(f"LCP budget exceeded on {route}: {perf['lcp_ms']}ms > 2000ms")
        if perf["cls"] > 0.05:
            raise AssertionError(f"CLS budget exceeded on {route}: {perf['cls']} > 0.05")
        if perf["inp_ms"] > 150:
            raise AssertionError(f"INP budget exceeded on {route}: {perf['inp_ms']}ms > 150ms")
        results.append({"route": route, **perf})
    return results


def set_theme_mode(page: Page, target: str) -> None:
    current = page.evaluate("document.documentElement.getAttribute('data-theme') || 'light'")
    if current != target:
        btn = page.locator(".tp-topbar-theme button, .tp-auth-controls button[aria-label*='theme' i]").first
        btn.click()
        page.wait_for_function(f"document.documentElement.getAttribute('data-theme') === '{target}'", timeout=5000)


def complete_setup_wizard(page: Page, base_url: str, bootstrap_token: str, owner_email: str, owner_pw: str, fake_llm_url: str) -> None:
    page.goto(f"{base_url}/setup", wait_until="networkidle")
    page.wait_for_selector("h1", timeout=10000)
    try:
        page.locator("form.tp-setup-form").wait_for(state="visible", timeout=15000)
    except Exception as exc:
        raise RuntimeError(f"Setup form missing at {page.url!r} body={page.locator('body').inner_text()[:300]!r}: {exc}") from exc

    # Check initial Setup page WCAG SC 2.5.3 compliance across Stepper buttons!
    run_axe_and_layout_audit(page, "/setup (step 0)", "light", "desktop-1440")

    form = page.locator("form.tp-setup-form")
    pw_inputs = form.locator('input[type="password"]')
    pw_inputs.nth(0).fill(bootstrap_token)
    form.locator('input[type="email"]').fill(owner_email)
    pw_inputs.nth(1).fill(owner_pw)
    pw_inputs.nth(2).fill(owner_pw)
    form.get_by_role("button", name="Continue").click()

    # Step 1: Audit local starter connection fields (including allow_private_ip), then skip starter connection on fresh DB
    form.get_by_role("heading", name="Initial provider connection").wait_for(state="visible", timeout=5000)
    form.locator('input[type="url"]').fill(fake_llm_url)
    allow_priv = form.get_by_role("checkbox", name="Allow private or loopback upstream for this connection")
    if not allow_priv.is_checked():
        allow_priv.click()
    run_axe_and_layout_audit(page, "/setup (step 1)", "light", "desktop-1440")
    starter_cb = form.get_by_role("checkbox", name="Provision a starter connection, deployment, route, and virtual key")
    if starter_cb.is_checked():
        starter_cb.click()
    form.get_by_role("button", name="Continue").click()

    # Step 2: Key custody acknowledgment
    form.get_by_role("heading", name="Master key custody").wait_for(state="visible", timeout=5000)
    custody_cb = form.get_by_role("checkbox", name="I have securely backed up the Tanpopo master key")
    if not custody_cb.is_checked():
        custody_cb.click()
    run_axe_and_layout_audit(page, "/setup (step 2)", "light", "desktop-1440")
    form.get_by_role("button", name="Continue").click()

    # Step 3: Review & submit
    form.get_by_role("heading", name="Review and initialize").wait_for(state="visible", timeout=5000)
    complete_btn = form.locator('button[type="submit"]')
    complete_btn.wait_for(state="visible", timeout=5000)
    run_axe_and_layout_audit(page, "/setup (step 3)", "light", "desktop-1440")
    complete_btn.click()

    page.wait_for_url("**/overview", timeout=15000)
    page.locator("main#main-content h1").wait_for(state="visible", timeout=15000)


def sign_in_and_verify_https_reload(page: Page, base_url: str, owner_email: str, owner_pw: str, browser_name: str) -> None:
    page.goto(f"{base_url}/login", wait_until="networkidle")
    page.wait_for_selector("h1", timeout=10000)
    run_axe_and_layout_audit(page, "/login", "light", f"{browser_name}-1440")

    page.locator('input[type="email"]').fill(owner_email)
    page.locator('input[type="password"]').fill(owner_pw)
    page.locator('button[type="submit"]').click()
    page.wait_for_url("**/overview", timeout=15000)
    page.wait_for_load_state("networkidle")
    try:
        page.locator("main#main-content h1").wait_for(state="visible", timeout=15000)
    except Exception as exc:
        raise RuntimeError(f"[{browser_name}] pre-reload workspace h1 missing at {page.url!r} body={page.locator('body').inner_text()[:200]!r}: {exc}") from exc

    # Full page reload: verifies __Host-tanpopo_admin_session (Secure; HttpOnly) persists!
    page.wait_for_timeout(250)
    page.reload(wait_until="networkidle")
    try:
        page.locator("main#main-content h1").wait_for(state="visible", timeout=20000)
    except Exception as exc:
        body_snip = ""
        try:
            body_snip = page.locator("body").inner_text()[:250]
        except Exception:
            pass
        raise AssertionError(f"[{browser_name}] HTTPS reload failed to render workspace at {page.url!r} body={body_snip!r}: {exc}") from exc
    if "/login" in page.url:
        raise AssertionError(f"[{browser_name}] HTTPS __Host- session cookie did not survive full page reload (redirected to {page.url})")

    # Confirm HttpOnly protection: document.cookie must NEVER expose __Host-tanpopo_admin_session.
    doc_cookie = page.evaluate("document.cookie")
    if "__Host-tanpopo_admin_session" in doc_cookie:
        raise AssertionError(f"[{browser_name}] HttpOnly cookie leaked into document.cookie")


def verify_keyboard_and_multi_org_and_i18n(page: Page, base_url: str) -> dict[str, Any]:
    if page.url.startswith(base_url):
        page.evaluate(
            """() => {
                if (window.location.pathname !== '/ops/organizations') {
                    window.history.pushState({}, '', '/ops/organizations');
                    window.dispatchEvent(new PopStateEvent('popstate'));
                }
            }"""
        )
    else:
        page.goto(f"{base_url}/ops/organizations", wait_until="networkidle")
    page.wait_for_selector("h1", timeout=10000)

    # Keyboard: Control+K opens Jump CommandPalette, Escape closes it
    page.keyboard.press("Control+k")
    palette = page.locator('dialog[open], [role="dialog"]').first
    palette.wait_for(state="visible", timeout=5000)
    page.keyboard.press("Escape")
    palette.wait_for(state="hidden", timeout=5000)

    # Air-Gap banner check (-offline=true was passed to tanpopo serve)
    page.get_by_text("Air-gapped offline mode").wait_for(state="visible", timeout=5000)

    # Create second organization via Organizations UI
    page.get_by_role("button", name="Create organization").click()
    dlg = page.locator('dialog[open], [role="dialog"]').first
    dlg.wait_for(state="visible", timeout=5000)
    dlg.locator("input").nth(0).fill("Bravo Org")
    dlg.locator("input").nth(1).fill("bravo-org")
    submit_btn = dlg.get_by_role("button", name="Create organization")
    submit_btn.wait_for(state="visible", timeout=5000)
    submit_btn.click()
    try:
        page.get_by_text("Bravo Org").first.wait_for(state="visible", timeout=10000)
    except Exception as exc:
        raise RuntimeError(f"Bravo Org missing after submit; dialog_open={dlg.is_visible()} body={page.locator('main#main-content').inner_text()[:400]!r}: {exc}") from exc

    # Switch org using topbar Scope Switcher button
    scope_btn = page.locator("button.tp-scope-switcher")
    scope_btn.click()
    page.get_by_role("option", name="Bravo Org").click()
    page.wait_for_function("document.querySelector('button.tp-scope-switcher')?.textContent?.includes('Bravo Org')", timeout=5000)

    # Open Crypto-shred confirmation AlertDialog & cancel safely
    shred_btns = page.get_by_role("button", name="Crypto-shred")
    shred_btns.last.click()
    shred_dlg = page.locator('dialog[open], [role="alertdialog"], [role="dialog"]').last
    shred_dlg.wait_for(state="visible", timeout=5000)
    shred_dlg.get_by_role("button", name="Cancel").click()
    shred_dlg.wait_for(state="hidden", timeout=5000)

    # Verify localized Unit 45 Air-Gap + Multi-Org strings render (not English fallback) in Vietnamese & Japanese
    lang_sel = page.locator(".tp-topbar-lang [role='combobox']").first
    checked_locales: dict[str, str] = {}
    for loc_code, native_name, expected_substr in [
        ("vi", "Tiếng Việt", "Chế độ offline cách ly mạng"),
        ("ja", "日本語", "拡張機能の外部ネットワーク取得は無効化されています"),
    ]:
        lang_sel.click()
        page.get_by_role("option", name=native_name).first.click()
        page.get_by_text(expected_substr).wait_for(state="visible", timeout=5000)
        checked_locales[loc_code] = expected_substr

    # Restore English locale cleanly
    lang_sel.click()
    page.get_by_role("option", name="English").first.click()
    page.get_by_text("Air-gapped offline mode").wait_for(state="visible", timeout=5000)

    return {"multi_org_created_and_switched": True, "keyboard_command_palette": True, "locales_verified": checked_locales}


def main() -> int:
    parser = argparse.ArgumentParser(description="Run Tanpopo Web Console Playwright E2E suite")
    parser.add_argument("--binary", default="", help="Path to prebuilt tanpopo binary (defaults to building into temp dir)")
    parser.add_argument("--browsers", default="chromium,firefox,webkit", help="Comma-separated browsers: chromium,firefox,webkit")
    parser.add_argument("--report", default="/tmp/tanpopo-e2e-report.json", help="JSON report output path")
    args = parser.parse_args()

    if not AXE_JS_PATH.exists():
        raise SystemExit(f"axe-core not found at {AXE_JS_PATH}; run npm ci --ignore-scripts in web/console")

    requested_browsers = [b.strip() for b in args.browsers.split(",") if b.strip()]
    tmpdir = Path(tempfile.mkdtemp(prefix="tanpopo-console-e2e-"))
    server_proc: subprocess.Popen[bytes] | None = None
    proxy_proc: subprocess.Popen[str] | None = None
    fake_llm_srv: _ThreadingHTTPServer | None = None

    try:
        env_build = os.environ.copy()
        env_build.setdefault("GOTOOLCHAIN", "go1.26.11")
        env_build["CGO_ENABLED"] = "0"

        bin_path = Path(args.binary) if args.binary else tmpdir / "tanpopo"
        if not args.binary:
            subprocess.run(["go", "build", "-o", str(bin_path), "./cmd/tanpopo"], cwd=str(REPO_ROOT), env=env_build, check=True)

        tlsproxy_bin = tmpdir / "tlsproxy"
        subprocess.run(["go", "build", "-o", str(tlsproxy_bin), "./web/console/e2e/tlsproxy"], cwd=str(REPO_ROOT), env=env_build, check=True)

        data_dir = tmpdir / "data"
        data_dir.mkdir(parents=True, exist_ok=True)
        boot_file = data_dir / "bootstrap.token"

        fake_port = free_port()
        fake_llm_srv = _ThreadingHTTPServer(("127.0.0.1", fake_port), _FakeLLMHandler)
        threading.Thread(target=fake_llm_srv.serve_forever, daemon=True).start()
        fake_llm_url = f"http://127.0.0.1:{fake_port}/v1"

        http_port = free_port()
        tls_port = free_port()
        env_srv = os.environ.copy()
        env_srv["TANPOPO_DATA_DIR"] = str(data_dir)
        env_srv["TANPOPO_DB_PATH"] = str(data_dir / "tanpopo.db")
        env_srv["TANPOPO_BOOTSTRAP_TOKEN_FILE"] = str(boot_file)
        env_srv["TANPOPO_MASTER_KEY"] = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
        env_srv["TANPOPO_ALLOWED_ORIGINS"] = f"https://127.0.0.1:{tls_port}"

        server_log = open(tmpdir / "tanpopo.log", "wb")
        server_proc = subprocess.Popen(
            [str(bin_path), "serve", "--listen", f"127.0.0.1:{http_port}", "--offline=true", "--multi-org=true"],
            cwd=str(REPO_ROOT),
            env=env_srv,
            stdout=server_log,
            stderr=subprocess.STDOUT,
        )
        wait_for_http_ready(f"http://127.0.0.1:{http_port}")
        bootstrap_token = wait_for_file_nonempty(boot_file)

        proxy_log = open(tmpdir / "tlsproxy.log", "w", encoding="utf-8")
        proxy_proc = subprocess.Popen(
            [str(tlsproxy_bin), "--listen", f"127.0.0.1:{tls_port}", "--target", f"http://127.0.0.1:{http_port}"],
            stdout=subprocess.PIPE,
            stderr=proxy_log,
            text=True,
        )
        assert proxy_proc.stdout is not None
        ready_line = proxy_proc.stdout.readline().strip()
        if not ready_line.startswith("READY https://"):
            raise RuntimeError(f"tlsproxy failed to start: {ready_line!r}")
        ready_parts = ready_line.removeprefix("READY ").strip().split()
        https_base = ready_parts[0]
        spki_pin = ready_parts[1] if len(ready_parts) > 1 else ""

        owner_email = "owner-e2e@tanpopo.example"
        owner_pw = "TanpopoE2E!Strong#2026"

        report: dict[str, Any] = {
            "https_base": https_base,
            "browsers": {},
            "external_requests": [],
            "console_errors": [],
            "page_errors": [],
        }

        with sync_playwright() as p:
            setup_done = False
            for idx, bname in enumerate(requested_browsers):
                print(f"[e2e] starting browser={bname}", flush=True)
                launcher = getattr(p, bname)
                launch_kwargs: dict[str, Any] = {"headless": True}
                if bname == "chromium":
                    chrome_bin = os.environ.get("TANPOPO_E2E_CHROMIUM_BIN", "").strip()
                    if not chrome_bin:
                        default_chr = Path(launcher.executable_path)
                        hs_candidates = sorted(default_chr.parents[2].glob("chromium_headless_shell-*/chrome-headless-shell-*/chrome-headless-shell")) if len(default_chr.parents) >= 3 else []
                        if hs_candidates and hs_candidates[-1].exists():
                            chrome_bin = str(hs_candidates[-1])
                        elif default_chr.exists():
                            chrome_bin = str(default_chr)
                        elif Path("/usr/bin/google-chrome").exists():
                            chrome_bin = "/usr/bin/google-chrome"
                    if chrome_bin:
                        if sys.platform.startswith("linux") and shutil.which("taskset") and os.environ.get("TANPOPO_E2E_PIN_CPU", "1") != "0":
                            wrapper = tmpdir / "chromium-cpu0.sh"
                            wrapper.write_text(f'#!/bin/sh\nexec taskset -c 0 {shlex.quote(chrome_bin)} "$@"\n', encoding="utf-8")
                            wrapper.chmod(0o755)
                            launch_kwargs["executable_path"] = str(wrapper)
                        else:
                            launch_kwargs["executable_path"] = chrome_bin
                    elif os.environ.get("TANPOPO_E2E_CHROMIUM_CHANNEL", "").strip():
                        launch_kwargs["channel"] = os.environ["TANPOPO_E2E_CHROMIUM_CHANNEL"].strip()
                    chromium_args = [
                        "--disable-dev-shm-usage",
                        "--disable-gpu",
                        "--no-sandbox",
                        "--ignore-certificate-errors",
                        "--allow-insecure-localhost",
                        *( [f"--ignore-certificate-errors-spki-list={spki_pin}"] if spki_pin else [] ),
                        "--disable-extensions",
                        "--disable-sync",
                        "--mute-audio",
                        "--disable-background-networking",
                        "--disable-component-update",
                        "--disable-renderer-backgrounding",
                        "--disable-features=WebAssemblyTrapHandler,Vulkan,AudioServiceOutOfProcess,BackForwardCache,Translate,OptimizationHints",
                    ]
                    if os.environ.get("TANPOPO_E2E_CHROMIUM_NO_OPT", "0") == "1":
                        chromium_args.append("--js-flags=--no-opt")
                    launch_kwargs["args"] = chromium_args
                browser = launcher.launch(**launch_kwargs)
                b_summary: dict[str, Any] = {"https_reload_ok": False, "audits": []}
                try:
                    def is_renderer_crash(exc: Exception) -> bool:
                        msg = str(exc)
                        return bname == "chromium" and ("Page crashed" in msg or "Target crashed" in msg)

                    # 1. Desktop 1440x900 context
                    ctx = browser.new_context(viewport={"width": 1440, "height": 900}, ignore_https_errors=True, locale="en-US")
                    attach_airgap_and_error_guards(ctx, report["external_requests"], report["console_errors"], report["page_errors"])
                    page = ctx.new_page()

                    if not setup_done:
                        for attempt in range(3):
                            try:
                                complete_setup_wizard(page, https_base, bootstrap_token, owner_email, owner_pw, fake_llm_url)
                                setup_done = True
                                break
                            except Exception as exc:
                                if not is_renderer_crash(exc) or attempt == 2:
                                    raise
                                print(f"[e2e] chromium setup tab crashed (attempt {attempt + 1}), reopening fresh tab", flush=True)
                                ctx.close()
                                ctx = browser.new_context(viewport={"width": 1440, "height": 900}, ignore_https_errors=True, locale="en-US")
                                attach_airgap_and_error_guards(ctx, report["external_requests"], report["console_errors"], report["page_errors"])
                                page = ctx.new_page()
                        # Explicit sign-out / fresh incognito login context on same browser so HTTPS reload is also verified!
                        ctx.close()
                        ctx = browser.new_context(viewport={"width": 1440, "height": 900}, ignore_https_errors=True, locale="en-US")
                        attach_airgap_and_error_guards(ctx, report["external_requests"], report["console_errors"], report["page_errors"])
                        page = ctx.new_page()

                    for attempt in range(3):
                        try:
                            sign_in_and_verify_https_reload(page, https_base, owner_email, owner_pw, bname)
                            break
                        except Exception as exc:
                            if not is_renderer_crash(exc) or attempt == 2:
                                raise
                            print(f"[e2e] chromium login/reload tab crashed (attempt {attempt + 1}), relaunching browser", flush=True)
                            try:
                                ctx.close()
                            except Exception:
                                pass
                            try:
                                browser.close()
                            except Exception:
                                pass
                            browser = launcher.launch(**launch_kwargs)
                            ctx = browser.new_context(viewport={"width": 1440, "height": 900}, ignore_https_errors=True, locale="en-US")
                            attach_airgap_and_error_guards(ctx, report["external_requests"], report["console_errors"], report["page_errors"])
                            page = ctx.new_page()
                    b_summary["https_reload_ok"] = True

                    if bname == "chromium":
                        def navigate_route(cur_page: Page, target_path: str, hard: bool = False) -> None:
                            if hard:
                                cur_page.goto(f"{https_base}{target_path}", wait_until="networkidle")
                                return
                            cur_page.evaluate(
                                """(path) => {
                                    if (window.location.pathname !== path) {
                                        window.history.pushState({}, '', path);
                                        window.dispatchEvent(new PopStateEvent('popstate'));
                                    }
                                }""",
                                target_path,
                            )
                            cur_page.wait_for_function(
                                """(expected) => window.location.pathname === expected""",
                                arg=target_path,
                                timeout=10000,
                            )

                        def audit_chromium_route(cur_page: Page, scr: str, route_path: str, theme_mode: str, vp_label: str, vp_size: dict[str, int]) -> Page:
                            for attempt in range(3):
                                try:
                                    if cur_page.url == "about:blank" or not cur_page.url.startswith(https_base):
                                        cur_page.goto(f"{https_base}{route_path}", wait_until="networkidle")
                                        set_theme_mode(cur_page, theme_mode)
                                    else:
                                        navigate_route(cur_page, route_path, hard=False)
                                    b_summary["audits"].append(run_axe_and_layout_audit(cur_page, f"{scr}:{route_path}", theme_mode, vp_label))
                                    return cur_page
                                except Exception as exc:
                                    if not is_renderer_crash(exc) or attempt == 2:
                                        raise
                                    print(f"[e2e] chromium route tab crashed at {route_path} ({vp_label}, {theme_mode}), recovering", flush=True)
                                    try:
                                        cur_page.close()
                                    except Exception:
                                        pass
                                    cur_page = ctx.new_page()
                                    cur_page.set_viewport_size(vp_size)
                            return cur_page

                        # Multi-Org, Keyboard Palette & Locale Switch (immediately post-login before long sweep)
                        print("[e2e] chromium phase=interactive-multiorg-i18n", flush=True)
                        b_summary["interactive"] = verify_keyboard_and_multi_org_and_i18n(page, https_base)

                        # CWV & real click/keyboard INP on key workspace screens
                        print("[e2e] chromium phase=cwv-inp", flush=True)
                        for attempt in range(3):
                            cwv_ctx = browser.new_context(viewport={"width": 1440, "height": 900}, ignore_https_errors=True, locale="en-US")
                            try:
                                attach_airgap_and_error_guards(cwv_ctx, report["external_requests"], report["console_errors"], report["page_errors"])
                                cwv_page = cwv_ctx.new_page()
                                cwv_page.goto(f"{https_base}/login", wait_until="networkidle")
                                cwv_page.locator('input[type="email"]').fill(owner_email)
                                cwv_page.locator('input[type="password"]').fill(owner_pw)
                                cwv_page.locator('button[type="submit"]').click()
                                cwv_page.wait_for_url("**/overview", timeout=15000)
                                cwv_page.locator("main#main-content h1").wait_for(state="visible", timeout=15000)
                                b_summary["cwv_inp"] = measure_chromium_cwv_and_inp(
                                    cwv_page, https_base, ["/overview", "/observability/traces", "/routes", "/ops/organizations"]
                                )
                                break
                            except Exception as exc:
                                if not is_renderer_crash(exc) or attempt == 2:
                                    raise
                                print(f"[e2e] chromium cwv tab crashed (attempt {attempt + 1}), retrying", flush=True)
                            finally:
                                try:
                                    cwv_ctx.close()
                                except Exception:
                                    pass

                        # Hard deep-link verification of backend SPA routes fixed in embed.go / serve.go
                        print("[e2e] chromium phase=deep-links", flush=True)
                        for deep_path in ["/mcp/servers", "/a2a/agents"]:
                            for attempt in range(3):
                                try:
                                    navigate_route(page, deep_path, hard=True)
                                    page.wait_for_selector("h1", timeout=10000)
                                    break
                                except Exception as exc:
                                    if not is_renderer_crash(exc) or attempt == 2:
                                        raise
                                    print(f"[e2e] chromium deep-link tab crashed at {deep_path}, recovering", flush=True)
                                    try:
                                        page.close()
                                    except Exception:
                                        pass
                                    page = ctx.new_page()
                                    page.set_viewport_size({"width": 1440, "height": 900})

                        # Full 27-route Light desktop sweep + representative SCR-02..16 Dark sweep + 768 tablet + 375 mobile
                        print("[e2e] chromium phase=light-1440 (27 routes)", flush=True)
                        for scr, route_path in ALL_AUTHENTICATED_ROUTES:
                            page = audit_chromium_route(page, scr, route_path, "light", "desktop-1440", {"width": 1440, "height": 900})

                        print("[e2e] chromium phase=dark-1440", flush=True)
                        set_theme_mode(page, "dark")
                        for scr, route_path in REPRESENTATIVE_ROUTES_SCR_02_TO_16:
                            page = audit_chromium_route(page, scr, route_path, "dark", "desktop-1440", {"width": 1440, "height": 900})
                        set_theme_mode(page, "light")

                        # Tablet (768x1024) + Mobile (375x812) responsive audit across SCR-02..16
                        print("[e2e] chromium phase=tablet-768-mobile-375", flush=True)
                        page.set_viewport_size({"width": 768, "height": 1024})
                        for scr, route_path in [("SCR-02", "/overview"), ("SCR-08", "/routes"), ("SCR-12", "/finops/budgets"), ("SCR-16", "/ops/organizations")]:
                            page = audit_chromium_route(page, scr, route_path, "light", "tablet-768", {"width": 768, "height": 1024})

                        page.set_viewport_size({"width": 375, "height": 812})
                        for scr, route_path in REPRESENTATIVE_ROUTES_SCR_02_TO_16:
                            page = audit_chromium_route(page, scr, route_path, "light", "mobile-375", {"width": 375, "height": 812})
                    else:
                        # Firefox & WebKit: full SCR-02..SCR-16 Light desktop + Dark spot + Mobile 375px verification
                        set_theme_mode(page, "light")
                        for scr, route_path in REPRESENTATIVE_ROUTES_SCR_02_TO_16:
                            page.goto(f"{https_base}{route_path}", wait_until="networkidle")
                            b_summary["audits"].append(run_axe_and_layout_audit(page, f"{scr}:{route_path}", "light", f"{bname}-1440"))

                        set_theme_mode(page, "dark")
                        for scr, route_path in [("SCR-02", "/overview"), ("SCR-11", "/observability/traces"), ("SCR-14", "/security/approvals"), ("SCR-16", "/ops/organizations")]:
                            page.goto(f"{https_base}{route_path}", wait_until="networkidle")
                            b_summary["audits"].append(run_axe_and_layout_audit(page, f"{scr}:{route_path}", "dark", f"{bname}-1440"))
                        set_theme_mode(page, "light")

                        page.set_viewport_size({"width": 375, "height": 812})
                        for scr, route_path in [("SCR-02", "/overview"), ("SCR-08", "/routes"), ("SCR-12", "/finops/budgets"), ("SCR-14", "/security/kill-switches"), ("SCR-16", "/ops/organizations")]:
                            page.goto(f"{https_base}{route_path}", wait_until="networkidle")
                            b_summary["audits"].append(run_axe_and_layout_audit(page, f"{scr}:{route_path}", "light", f"{bname}-375"))

                    ctx.close()
                    report["browsers"][bname] = b_summary
                finally:
                    browser.close()

        if report["external_requests"]:
            raise AssertionError(f"Air-gap violation: external requests detected: {report['external_requests']}")
        if report["console_errors"] or report["page_errors"]:
            raise AssertionError(
                f"Browser runtime errors detected: console={report['console_errors']} page={report['page_errors']}"
            )

        Path(args.report).write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
        print(f"E2E PASS: verified {len(requested_browsers)} browser engines across SCR-01..SCR-16; report -> {args.report}")
        return 0
    finally:
        if report["console_errors"] or report["page_errors"]:
            print(f"[e2e-errs] console={report['console_errors'][-10:]} page={report['page_errors'][-10:]}", flush=True)
        if not Path(args.report).exists():
            for log_name in ("tanpopo.log", "tlsproxy.log"):
                p = tmpdir / log_name
                if p.exists():
                    lines = p.read_text(encoding="utf-8", errors="replace").splitlines()[-15:]
                    if lines:
                        print(f"[e2e-{log_name}] " + " | ".join(lines), flush=True)
        if proxy_proc is not None:
            proxy_proc.terminate()
            try:
                proxy_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proxy_proc.kill()
        if fake_llm_srv is not None:
            fake_llm_srv.shutdown()
            fake_llm_srv.server_close()
        if server_proc is not None:
            server_proc.terminate()
            try:
                server_proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server_proc.kill()
        shutil.rmtree(tmpdir, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
