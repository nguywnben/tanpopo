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
                })),
                incompleteMismatch: res.incomplete
                    .filter(i => i.id === 'label-content-name-mismatch')
                    .map(i => ({id: i.id, help: i.help, targets: i.nodes.slice(0, 3).map(n => n.target.join(' '))}))
            };
        }"""
    )
    blocking = [
        v for v in axe_result["violations"]
        if v.get("impact") in ("critical", "serious") or v.get("id") == "label-content-name-mismatch"
    ] + axe_result["incompleteMismatch"]
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

    # Step 1: On a clean database with zero active adapters, verify unavailable presets are hidden and activation guidance renders
    form.get_by_role("heading", name="Initial provider connection").wait_for(state="visible", timeout=5000)
    form.get_by_text("No provider adapters enabled yet").wait_for(state="visible", timeout=5000)
    if form.locator('select, [role="combobox"]').count() != 0:
        raise AssertionError("Setup Step 1 exposed unavailable adapter choices when available_adapters is empty")
    run_axe_and_layout_audit(page, "/setup (step 1)", "light", "desktop-1440")
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


def _select_combo_option(page: Page, scope_locator: Any, label_pattern: str, option_pattern: str | None = None) -> None:
    combo = scope_locator.get_by_role("combobox", name=re.compile(label_pattern, re.I)).first
    combo.click()
    if option_pattern is not None:
        page.get_by_role("option", name=re.compile(option_pattern, re.I)).first.click()
    else:
        page.locator('[role="option"]').first.click()


def verify_clean_onboarding_to_trace(page: Page, https_base: str, http_base: str, fake_base_url: str) -> dict[str, Any]:
    # 1. Fresh instance: Providers -> Adapters shows helpful empty state + Open Marketplace CTA
    page.goto(f"{https_base}/providers/adapters", wait_until="networkidle")
    page.get_by_role("button", name="Open Marketplace").click()
    page.wait_for_url("**/extensions/marketplace", timeout=10000)

    # 2. Marketplace renders bundled official packages -> Stage & Activate @tanpopo/openai-compatible
    mkt_row = page.locator("tr", has_text="@tanpopo/openai-compatible").first
    mkt_row.wait_for(state="visible", timeout=10000)
    mkt_row.get_by_role("button", name="Stage for review").click()
    approve_btn = page.get_by_role("button", name="Approve & activate")
    approve_btn.wait_for(state="visible", timeout=10000)
    consent_cb = page.get_by_role("checkbox", name=re.compile("explicitly consent", re.I)).first
    consent_cb.wait_for(state="visible", timeout=5000)
    if not consent_cb.is_checked():
        consent_cb.click()
    approve_btn.click()

    view_adp_btn = page.get_by_role("button", name="View Adapters")
    view_adp_btn.wait_for(state="visible", timeout=10000)
    view_adp_btn.click()
    page.wait_for_url("**/providers/adapters", timeout=10000)
    page.locator("tr", has_text="OpenAI-Compatible").first.wait_for(state="visible", timeout=10000)

    # 3. Create Provider Connection -> Test Connection (promote SM-02 to ACTIVE)
    page.goto(f"{https_base}/providers/connections", wait_until="networkidle")
    page.get_by_role("button", name="New connection").first.click()
    conn_form = page.locator("#tp-create-connection-form")
    conn_form.wait_for(state="visible", timeout=10000)
    conn_form.get_by_label("Connection name").fill("E2E Local Upstream")
    _select_combo_option(page, conn_form, "Provider adapter", "OpenAI-Compatible")
    conn_form.get_by_label("Endpoint URL").fill(fake_base_url)
    _select_combo_option(page, conn_form, "Billing mode", "Self-hosted infra")
    _select_combo_option(page, conn_form, "Data region", "On-prem")
    priv_cb = conn_form.get_by_role("checkbox", name=re.compile("Allow private", re.I))
    if not priv_cb.is_checked():
        priv_cb.click()
    conn_form.get_by_label("Allowed CIDRs").fill("127.0.0.1/32")
    page.get_by_role("button", name="Create connection").click()

    conn_row = page.locator("tr", has_text="E2E Local Upstream").first
    conn_row.wait_for(state="visible", timeout=10000)
    conn_row.get_by_role("button", name=re.compile("Actions", re.I)).click()
    page.get_by_role("menuitem", name="Probe connection").or_(
        page.get_by_role("button", name="Probe connection")
    ).or_(page.get_by_text("Probe connection")).first.click()
    conn_row.get_by_text(re.compile(r"^Active$", re.I)).wait_for(state="visible", timeout=10000)

    # 4. Register Model + Add Deployment
    page.goto(f"{https_base}/models", wait_until="networkidle")
    page.get_by_role("button", name="Register model").first.click()
    mod_form = page.locator("#tp-register-model-form")
    mod_form.wait_for(state="visible", timeout=10000)
    mod_form.get_by_label("Display name").fill("Llama 3.2 Local")
    mod_form.get_by_label("Canonical model slug").fill("llama3.2")
    mod_form.get_by_label(re.compile("Declared capabilities", re.I)).first.click()
    page.get_by_role("option", name=re.compile("^streaming$", re.I)).first.click()
    page.keyboard.press("Escape")
    page.get_by_role("button", name="Register model").last.click()
    mod_form.wait_for(state="hidden", timeout=10000)

    page.get_by_role("button", name="Add deployment").first.click()
    dep_form = page.locator("#tp-add-deployment-form")
    dep_form.wait_for(state="visible", timeout=10000)
    _select_combo_option(page, dep_form, "Logical model", "llama3.2")
    _select_combo_option(page, dep_form, "Provider connection", "E2E Local Upstream")
    dep_form.get_by_label("Upstream model name").fill("llama3.2")
    zero_cb = dep_form.get_by_role("checkbox", name=re.compile("Self-hosted zero-rate", re.I))
    if not zero_cb.is_checked():
        zero_cb.click()
    page.get_by_role("button", name="Save deployment").click()
    dep_form.wait_for(state="hidden", timeout=10000)
    page.locator("tr", has_text="llama3.2").first.wait_for(state="visible", timeout=10000)

    # 5. Create Route
    page.goto(f"{https_base}/routes", wait_until="networkidle")
    page.get_by_role("button", name="Create route").first.click()
    route_dlg = page.locator('dialog[open], [role="dialog"]').last
    route_dlg.wait_for(state="visible", timeout=10000)
    route_dlg.get_by_label("Route slug").fill("llama3.2")
    _select_combo_option(page, route_dlg, "Add deployment target", "llama3.2")
    route_dlg.get_by_role("button", name="Add target").click()
    route_dlg.get_by_role("button", name="Create route").last.click()
    page.locator("tr", has_text="llama3.2").first.wait_for(state="visible", timeout=10000)

    # 6. Mint Virtual Key
    page.goto(f"{https_base}/access/virtual-keys", wait_until="networkidle")
    page.get_by_role("button", name="Issue key").first.click()
    key_form = page.locator("#tp-issue-key-form")
    key_form.wait_for(state="visible", timeout=10000)
    key_form.get_by_label("Key name").fill("e2e-runtime-key")
    allow_all_cb = key_form.get_by_role("checkbox").first
    if not allow_all_cb.is_checked():
        allow_all_cb.click()
    page.locator('dialog[open]').last.locator('button[type="submit"]').click()
    secret_el = page.locator("pre, code").filter(has_text=re.compile(r"vk_[A-Za-z0-9_-]{16,}")).first
    secret_el.wait_for(state="visible", timeout=10000)
    m = re.search(r"vk_[A-Za-z0-9_-]{16,}", secret_el.inner_text())
    if not m:
        raise AssertionError(f"Minted Virtual Key secret missing from modal: {secret_el.inner_text()[:200]!r}")
    vk_token = m.group(0)
    page.get_by_role("button", name="I stored it — close & erase").click()

    # 7. Call Data Plane /v1/chat/completions -> 200 OK
    host_port = http_base.removeprefix("http://")
    host, port_str = host_port.split(":", 1)
    conn = http.client.HTTPConnection(host, int(port_str), timeout=10)
    try:
        conn.request(
            "POST",
            "/v1/chat/completions",
            body=json.dumps({"model": "llama3.2", "messages": [{"role": "user", "content": "hello"}]}),
            headers={"Content-Type": "application/json", "Authorization": f"Bearer {vk_token}"},
        )
        resp = conn.getresponse()
        raw_resp = resp.read().decode("utf-8", errors="replace")
        if resp.status != 200 or "Tanpopo E2E OK" not in raw_resp:
            raise AssertionError(f"POST /v1/chat/completions expected 200 Tanpopo E2E OK, got {resp.status}: {raw_resp[:200]}")
    finally:
        conn.close()

    # 8. Verify recorded trace appears on Traces screen
    page.goto(f"{https_base}/observability/traces", wait_until="networkidle")
    page.locator("tr", has_text="llama3.2").first.wait_for(state="visible", timeout=10000)

    return {
        "adapter_activated": "@tanpopo/openai-compatible",
        "connection": "E2E Local Upstream",
        "route_slug": "llama3.2",
        "chat_completions_status": 200,
        "trace_visible": True,
    }


def verify_round1_defects_e2e(page: Page, https_base: str) -> dict[str, Any]:
    results: dict[str, Any] = {}

    # 1. DEF-003, DEF-012, DEF-014, DEF-015: FinOps create budget & CSV/JSON export with group_by=model
    page.goto(f"{https_base}/finops/chargeback", wait_until="networkidle")
    page.get_by_role("combobox", name=re.compile("Group by", re.I)).first.click()
    page.get_by_role("option", name="Model").first.click()
    page.wait_for_timeout(300)
    with page.expect_download(timeout=10000) as csv_dl:
        page.get_by_role("button", name=re.compile("Export CSV", re.I)).first.click()
    csv_path = csv_dl.value.path()
    csv_text = Path(csv_path).read_text(encoding="utf-8") if csv_path else ""
    if "group_key" not in csv_text:
        raise AssertionError(f"DEF-014/015: Unexpected FinOps CSV export content: {csv_text!r}")

    with page.expect_download(timeout=10000) as json_dl:
        page.get_by_role("button", name=re.compile("Export JSON", re.I)).first.click()
    json_path = json_dl.value.path()
    json_raw = Path(json_path).read_text(encoding="utf-8") if json_path else "[]"
    json.loads(json_raw)

    page.get_by_role("button", name=re.compile("Create budget", re.I)).first.click()
    page.locator("dialog.astryx-drawer[open] button[form='tp-create-budget-form']").first.click()
    page.wait_for_timeout(300)
    page.goto(f"{https_base}/finops/budgets", wait_until="networkidle")
    page.wait_for_selector("text=MONTHLY", timeout=8000)
    results["finops_ok"] = True

    # 2. DEF-010: Access Virtual Key with A2A plane & /a2a/* route grant
    page.goto(f"{https_base}/access/virtual-keys", wait_until="networkidle")
    page.get_by_role("button", name=re.compile("Issue key", re.I)).first.click()
    issue_dlg = page.locator("dialog.astryx-drawer[open]").last
    issue_dlg.get_by_label(re.compile("Key name", re.I)).fill("e2e-a2a-key")
    issue_dlg.get_by_label(re.compile("Allowed planes", re.I)).first.click()
    page.get_by_role("option", name=re.compile("A2A", re.I)).first.click()
    page.keyboard.press("Escape")
    issue_dlg.get_by_label(re.compile("Allowed route patterns", re.I)).fill("/a2a/*")
    issue_dlg.get_by_role("button", name=re.compile("Create key", re.I)).click()
    page.get_by_role("button", name=re.compile("I stored it", re.I)).first.click()
    page.wait_for_selector("text=e2e-a2a-key", timeout=8000)
    results["vkey_a2a_ok"] = True

    # 3. DEF-008: Encrypted Backup availability matches host volatile-staging security gate
    page.goto(f"{https_base}/ops/backups", wait_until="networkidle")
    has_swap = False
    swaps_path = Path("/proc/swaps")
    if swaps_path.exists():
        swap_lines = [ln.strip() for ln in swaps_path.read_text(encoding="utf-8").splitlines() if ln.strip()]
        has_swap = len(swap_lines) > 1
    if not has_swap:
        if page.locator("text=Encrypted backups unavailable").count() > 0:
            raise AssertionError("DEF-008: Encrypted backups reported unavailable on swap-free Linux host")
        page.get_by_role("button", name=re.compile("Create encrypted backup", re.I)).first.click()
        page.wait_for_selector("text=Encrypted backup created", timeout=10000)
        results["backup_ok"] = "created"
    else:
        page.wait_for_selector("text=Encrypted backups require verified /dev/shm tmpfs staging with swap disabled", timeout=8000)
        btn = page.get_by_role("button", name=re.compile("Create encrypted backup", re.I)).first
        if not btn.is_disabled():
            raise AssertionError("DEF-008: Create backup button must be disabled when swap-free staging is unavailable")
        results["backup_ok"] = "honest_fail_closed_swap_active"

    return results


def verify_round2_defects_e2e(
    page: Page,
    https_base: str,
    browser: Any,
    external_requests: list[str],
    page_errors: list[str],
) -> dict[str, Any]:
    results: dict[str, Any] = {}

    # Restore primary organization in topbar Scope Switcher (previous test created & switched to Bravo Org)
    scope_btn = page.locator("button.tp-scope-switcher")
    if scope_btn.count() > 0 and "Bravo Org" in scope_btn.inner_text():
        scope_btn.click()
        opts = page.get_by_role("option")
        for idx in range(opts.count()):
            txt = opts.nth(idx).inner_text()
            if "Bravo Org" not in txt:
                opts.nth(idx).click()
                break
        page.wait_for_function("() => !document.querySelector('button.tp-scope-switcher')?.textContent?.includes('Bravo Org')", timeout=5000)

    # 1. DEF-021, DEF-022, DEF-023: Members load cleanly, enrollment activation logs in, role update revokes session (401 -> session_expired)
    page.goto(f"{https_base}/access/members", wait_until="networkidle")
    page.get_by_text("Organization members").first.wait_for(state="visible", timeout=8000)
    page.get_by_role("button", name=re.compile("Add member", re.I)).first.click()
    mem_form = page.locator("#tp-add-member-form")
    mem_form.wait_for(state="visible", timeout=8000)
    mem_form.get_by_label(re.compile("Member email", re.I)).fill("e2e-enroll@tanpopo.example")
    # Ensure DEVELOPER role is selected
    roles_combo = mem_form.get_by_role("combobox", name=re.compile("Assign roles|Select roles", re.I)).first
    roles_combo.click()
    dev_opt = page.get_by_role("option", name=re.compile("DEVELOPER", re.I)).first
    if dev_opt.get_attribute("aria-selected") != "true":
        dev_opt.click()
    page.keyboard.press("Escape")
    page.locator('dialog[open] button[form="tp-add-member-form"]').click()

    enroll_dlg = page.locator('dialog[open], [role="dialog"]').last
    enroll_dlg.wait_for(state="visible", timeout=8000)
    dlg_text = enroll_dlg.inner_text()
    tok_match = re.search(r"tpen_[A-Za-z0-9_-]{12,}", dlg_text)
    if not tok_match:
        raise AssertionError(f"DEF-021: one-time enrollment token missing in modal: {dlg_text[:300]!r}")
    enroll_token = tok_match.group(0)
    enroll_dlg.get_by_role("button", name=re.compile("^Done$", re.I)).click()

    # Open isolated incognito context for invited member enrollment
    enroll_console_errs: list[str] = []
    enroll_ctx = browser.new_context(viewport={"width": 1440, "height": 900}, ignore_https_errors=True, locale="en-US")
    attach_airgap_and_error_guards(enroll_ctx, external_requests, enroll_console_errs, page_errors)
    try:
        enroll_page = enroll_ctx.new_page()
        enroll_page.goto(f"{https_base}/login?enroll={enroll_token}", wait_until="networkidle")
        pw_inputs = enroll_page.locator('input[type="password"]')
        pw_inputs.nth(0).fill("TanpopoEnroll!Strong#2026")
        pw_inputs.nth(1).fill("TanpopoEnroll!Strong#2026")
        enroll_page.locator('button[type="submit"]').click()
        enroll_page.wait_for_url("**/overview", timeout=15000)
        enroll_page.locator("main#main-content h1").wait_for(state="visible", timeout=15000)
        results["enrollment_login_ok"] = True

        # Owner updates invited member's roles -> immediately revokes active member session
        page.goto(f"{https_base}/access/members", wait_until="networkidle")
        mem_row = page.locator("tr", has_text="e2e-enroll@tanpopo.example").first
        mem_row.wait_for(state="visible", timeout=8000)
        mem_row.get_by_text("Password set").first.wait_for(state="visible", timeout=5000)
        mem_row.get_by_role("button", name=re.compile("Actions for e2e-enroll@tanpopo.example", re.I)).click()
        page.get_by_role("menuitem", name=re.compile("Edit roles", re.I)).or_(
            page.get_by_text(re.compile("Edit roles", re.I))
        ).first.click()
        page.locator('dialog[open] button:has-text("Save roles")').first.click()
        page.get_by_text("Member roles updated.").first.wait_for(state="visible", timeout=8000)

        # Revoked member session triggers an in-app API request -> forced 401 redirect to /login?reason=session_expired
        enroll_page.get_by_role("link", name=re.compile("^Traces$", re.I)).first.click()
        enroll_page.wait_for_function(
            "() => window.location.pathname === '/login' && window.location.search.includes('reason=session_expired')",
            timeout=15000,
        )
        enroll_page.get_by_text(re.compile("Session expired", re.I)).first.wait_for(state="visible", timeout=8000)
        unexpected_enroll_errs = [e for e in enroll_console_errs if "401" not in e and "403" not in e]
        if unexpected_enroll_errs:
            raise AssertionError(f"DEF-022: Unexpected console errors during session expiry check: {unexpected_enroll_errs}")
        results["role_revoke_401_ok"] = True
    finally:
        enroll_ctx.close()

    # 2. DEF-034 & DEF-020: Seal signing secret -> Register webhook -> Disable -> Enable -> Delete
    page.goto(f"{https_base}/providers/connections", wait_until="networkidle")
    page.get_by_role("tab", name=re.compile("Vault Secrets", re.I)).first.click()
    page.get_by_role("button", name=re.compile("Seal secret", re.I)).first.click()
    sec_form = page.locator("#tp-seal-secret-form")
    sec_form.wait_for(state="visible", timeout=8000)
    sec_form.get_by_label(re.compile("Credential name", re.I)).fill("e2e-whk-sec")
    _select_combo_option(page, sec_form, "Secret kind", "Webhook HMAC Secret")
    sec_form.locator("input[type='password']").first.fill("whsec_e2e_def034_hmac_signing_key")
    page.locator('dialog[open] button[form="tp-seal-secret-form"]').click()
    page.locator("tr", has_text="e2e-whk-sec").first.wait_for(state="visible", timeout=8000)

    page.goto(f"{https_base}/settings/residency-retention", wait_until="networkidle")
    page.get_by_role("button", name=re.compile("Add webhook", re.I)).first.click()
    whk_form = page.locator("#tp-register-webhook-form")
    whk_form.wait_for(state="visible", timeout=8000)
    whk_form.get_by_label(re.compile("HTTPS receiver URL", re.I)).fill("https://93.184.216.34/e2e-def034")
    _select_combo_option(page, whk_form, "Signing credential", "e2e-whk-sec")
    page.locator('dialog[open] button[form="tp-register-webhook-form"]').click()
    whk_row = page.locator("tr", has_text="https://93.184.216.34").first
    whk_row.wait_for(state="visible", timeout=8000)

    # Disable -> verify Disabled -> Enable -> verify Enabled -> Delete
    whk_row.get_by_role("button", name=re.compile("Actions", re.I)).click()
    page.get_by_role("menuitem", name=re.compile("Disable webhook", re.I)).or_(page.get_by_text("Disable webhook")).first.click()
    whk_row.get_by_text(re.compile(r"^Disabled$", re.I)).wait_for(state="visible", timeout=8000)

    whk_row.get_by_role("button", name=re.compile("Actions", re.I)).click()
    page.get_by_role("menuitem", name=re.compile("Enable webhook", re.I)).or_(page.get_by_text("Enable webhook")).first.click()
    whk_row.get_by_text(re.compile(r"^Enabled$", re.I)).wait_for(state="visible", timeout=8000)

    whk_row.get_by_role("button", name=re.compile("Actions", re.I)).click()
    page.get_by_role("menuitem", name=re.compile("Delete webhook", re.I)).or_(page.get_by_text("Delete webhook")).first.click()
    del_dlg = page.locator('dialog[open], [role="alertdialog"], [role="dialog"]').last
    del_dlg.wait_for(state="visible", timeout=5000)
    del_dlg.get_by_role("button", name=re.compile("Delete webhook", re.I)).click()
    whk_row.wait_for(state="hidden", timeout=8000)
    results["webhook_lifecycle_ok"] = True

    # 3. DEF-039 & DEF-028: Policies effective table & Explain omitting forced openai_compatible
    page.goto(f"{https_base}/policies", wait_until="networkidle")
    page.get_by_text("Effective resolved policy").first.wait_for(state="visible", timeout=8000)
    results["policies_visible_ok"] = True

    page.goto(f"{https_base}/routes/explain", wait_until="networkidle")
    exp_combo = page.get_by_role("combobox", name=re.compile("Target model or route slug", re.I)).first
    exp_combo.click()
    exp_combo.fill("llama3.2")
    exp_combo.press("Tab")
    _select_combo_option(page, page.locator("main#main-content"), "Data residency", "On-prem")
    page.get_by_role("button", name=re.compile("Explain route", re.I)).first.click()
    page.get_by_text("Dry-run verified").first.wait_for(state="visible", timeout=8000)
    results["explain_ok"] = True

    # 4. DEF-017: Playground correlates response cleanly without stuck Trace pending
    page.goto(f"{https_base}/devtools/playground", wait_until="networkidle")
    page.wait_for_function(
        "() => { const el = document.querySelector('.tp-scope-switcher-env'); return el && el.textContent && !el.textContent.includes('No env'); }",
        timeout=8000,
    )
    pg_model = page.get_by_role("combobox", name=re.compile("Model or route slug", re.I)).first
    pg_model.locator("xpath=..").click()
    pg_model.fill("llama3.2")
    opt = page.get_by_role("option", name=re.compile("llama3\\.2", re.I)).first
    opt.wait_for(state="visible", timeout=5000)
    opt.click()
    page.get_by_label(re.compile("^Prompt$", re.I)).fill("Round 2 DEF-017 trace check")
    page.get_by_role("button", name=re.compile("^Send$", re.I)).click()
    try:
        page.get_by_text("Tanpopo E2E OK").first.wait_for(state="visible", timeout=15000)
    except Exception as exc:
        alerts = page.locator('[role="alert"], .astryx-banner').all_inner_texts()
        raise AssertionError(f"DEF-017: Playground reply missing; alerts={alerts!r}") from exc
    page.wait_for_function(
        """() => !document.body.innerText.includes('Trace metadata pending') || document.body.innerText.includes('req_')""",
        timeout=15000,
    )
    results["playground_trace_ok"] = True

    # 5. DEF-036: Marketplace reconciles installed @tanpopo/openai-compatible
    page.goto(f"{https_base}/extensions/marketplace", wait_until="networkidle")
    mkt_row = page.locator("tr", has_text="@tanpopo/openai-compatible").first
    mkt_row.wait_for(state="visible", timeout=8000)
    if mkt_row.get_by_role("button", name="Stage for review").count() > 0:
        raise AssertionError("DEF-036: Marketplace still shows Stage for review for already-installed @tanpopo/openai-compatible")
    mkt_row.get_by_text(re.compile(r"^Installed$", re.I)).wait_for(state="visible", timeout=5000)
    results["marketplace_reconciled_ok"] = True

    # 6. DEF-031: Mobile 375px Drawer CIDR validation renders inside drawer with 0 overflow
    page.set_viewport_size({"width": 375, "height": 812})
    try:
        page.goto(f"{https_base}/mcp/servers", wait_until="networkidle")
        page.get_by_role("button", name=re.compile("Register server", re.I)).first.click()
        mcp_form = page.locator("#tp-register-mcp-form")
        mcp_form.wait_for(state="visible", timeout=8000)
        mcp_form.get_by_label(re.compile("Server slug", re.I)).fill("e2e-bad-cidr")
        mcp_form.get_by_label(re.compile("Display name", re.I)).fill("E2E Bad CIDR")
        mcp_form.get_by_label(re.compile("Streamable HTTP endpoint URL|Endpoint URL", re.I)).fill("http://127.0.0.1:9000/mcp")
        priv_cb = mcp_form.locator('input[type="checkbox"]').first
        if not priv_cb.is_checked():
            priv_cb.click()
        mcp_form.get_by_label(re.compile("Allowed CIDRs", re.I)).fill("127.0.0.1/33")
        page.locator('dialog[open] button[form="tp-register-mcp-form"]').click()
        page.locator('dialog[open]').get_by_text(re.compile("explicit non-wildcard CIDRs", re.I)).first.wait_for(state="visible", timeout=5000)
        ovf = page.evaluate("() => document.documentElement.scrollWidth > document.documentElement.clientWidth")
        if ovf:
            raise AssertionError("DEF-031: Mobile 375px MCP drawer caused horizontal overflow")
        drawer_png = os.environ.get("TANPOPO_E2E_MOBILE_MCP_DRAWER_PNG", "").strip()
        if drawer_png:
            page.screenshot(path=drawer_png)
        page.locator('dialog[open] button:has-text("Cancel")').first.click()
        results["mobile_cidr_in_drawer_ok"] = True
    finally:
        page.set_viewport_size({"width": 1440, "height": 900})

    return results


def verify_round3_defects_e2e(
    page: Page,
    https_base: str,
    owner_email: str,
    owner_pw: str,
    shot_dir: Path | None = None,
) -> dict[str, Any]:
    results: dict[str, Any] = {}

    def save_shot(name: str) -> None:
        if shot_dir is not None:
            shot_dir.mkdir(parents=True, exist_ok=True)
            page.screenshot(path=str(shot_dir / name))

    # 1. DEF-042: Access -> Members omits OPERATOR from member role picker
    page.goto(f"{https_base}/access/members", wait_until="networkidle")
    page.get_by_role("button", name=re.compile("Add member", re.I)).first.click()
    mem_form = page.locator("#tp-add-member-form")
    mem_form.wait_for(state="visible", timeout=8000)
    role_combo = mem_form.get_by_role("combobox", name=re.compile("Assign roles|Select roles", re.I)).first
    role_combo.click()
    opt_texts = [t.strip() for t in page.locator('[role="option"]').all_inner_texts()]
    if any("operator" in txt.lower() for txt in opt_texts):
        raise AssertionError(f"DEF-042: Member invite role picker unexpectedly includes OPERATOR: {opt_texts}")
    page.keyboard.press("Escape")
    page.locator('dialog[open] button:has-text("Cancel")').first.click()
    results["access_no_operator_ok"] = True

    # 2. DEF-043 & DEF-044: Extensions provenance separation & Quarantine -> Uninstall workflow
    page.goto(f"{https_base}/extensions/marketplace", wait_until="networkidle")
    mkt_anth = page.locator("tr", has_text="@tanpopo/anthropic").first
    mkt_anth.wait_for(state="visible", timeout=8000)
    if not re.search(r"Signature verified \([a-f0-9]+\)", mkt_anth.inner_text(), re.I):
        raise AssertionError(f"DEF-043: Marketplace row missing verified signature fingerprint: {mkt_anth.inner_text()!r}")
    mkt_anth.get_by_role("button", name="Stage for review").click()
    rev_dlg = page.locator('dialog[open], [role="dialog"]').last
    rev_dlg.wait_for(state="visible", timeout=8000)
    rev_dlg.get_by_text(re.compile("Enterprise signature", re.I)).first.wait_for(state="visible", timeout=5000)
    page.wait_for_timeout(350)
    save_shot("e2e-round3-ext-review-1440.png")
    consent_cb = rev_dlg.get_by_role("checkbox", name=re.compile("explicitly consent", re.I)).first
    if consent_cb.count() > 0 and not consent_cb.is_checked():
        consent_cb.click()
    rev_dlg.get_by_role("button", name="Approve & activate").click()
    rev_dlg.wait_for(state="hidden", timeout=10000)

    page.goto(f"{https_base}/extensions/installed", wait_until="networkidle")
    anth_row = page.locator("tr", has_text="@tanpopo/anthropic").first
    anth_row.wait_for(state="visible", timeout=8000)
    save_shot("e2e-round3-ext-installed-1440.png")
    anth_row.get_by_role("button", name=re.compile("Actions", re.I)).click()
    if page.get_by_role("menuitem", name=re.compile("Uninstall package", re.I)).count() > 0:
        raise AssertionError("DEF-044: Active extension row unexpectedly offered direct Uninstall before Quarantine")
    page.get_by_role("menuitem", name=re.compile("Quarantine package", re.I)).or_(
        page.get_by_text("Quarantine package")
    ).first.click()
    q_dlg = page.locator('dialog[open], [role="alertdialog"], [role="dialog"]').last
    q_dlg.wait_for(state="visible", timeout=8000)
    q_inputs = q_dlg.locator("input")
    q_inputs.nth(0).fill("Round 3 E2E quarantine->uninstall verification")
    if q_inputs.count() > 1:
        q_inputs.nth(1).fill("@tanpopo/anthropic")
    q_dlg.get_by_role("button", name=re.compile("Confirm quarantine", re.I)).click()
    q_dlg.wait_for(state="hidden", timeout=10000)

    anth_row.get_by_text(re.compile("^Quarantined$", re.I)).first.wait_for(state="visible", timeout=8000)
    anth_row.get_by_role("button", name=re.compile("Actions", re.I)).click()
    page.get_by_role("menuitem", name=re.compile("Uninstall package", re.I)).or_(
        page.get_by_text("Uninstall package")
    ).first.click()
    u_dlg = page.locator('dialog[open], [role="alertdialog"], [role="dialog"]').last
    u_dlg.wait_for(state="visible", timeout=8000)
    u_dlg.locator("input").first.fill("Clean up quarantined adapter")
    u_dlg.get_by_role("button", name=re.compile("Confirm uninstall", re.I)).click()
    u_dlg.wait_for(state="hidden", timeout=10000)
    anth_row.wait_for(state="hidden", timeout=8000)
    results["extensions_provenance_and_sm01_ok"] = True

    # 3. DEF-050: Routing table header is "Target chain" and omits "(100%)" on PRIORITY_FAILOVER
    page.goto(f"{https_base}/routes", wait_until="networkidle")
    page.get_by_text("Target chain").first.wait_for(state="visible", timeout=8000)
    if page.get_by_text("Fallback chain").count() > 0:
        raise AssertionError("DEF-050: Stale 'Fallback chain' header still visible on RoutingPage")
    save_shot("e2e-round3-routes-1440.png")
    results["routing_target_chain_ok"] = True

    # 4. DEF-032 & DEF-035: Models short connection ID & empty-project isolation check
    page.goto(f"{https_base}/models", wait_until="networkidle")
    page.get_by_role("button", name="Add deployment").first.click()
    dep_form = page.locator("#tp-add-deployment-form")
    dep_form.wait_for(state="visible", timeout=8000)
    conn_combo = dep_form.get_by_role("combobox", name=re.compile("Provider connection", re.I)).first
    conn_combo.click()
    conn_opts = [t.strip() for t in page.locator('[role="option"]').all_inner_texts()]
    if not any(re.search(r"E2E Local Upstream \(.+ · [a-z0-9]{8}\)", opt) for opt in conn_opts):
        raise AssertionError(f"DEF-032: Connection picker missing short ID suffix: {conn_opts}")
    page.keyboard.press("Escape")
    page.locator('dialog[open] button:has-text("Cancel")').first.click()
    results["models_connection_short_id_ok"] = True

    # 5. DEF-046 & DEF-047: Playground prompt/scope reset & cross-tab logout propagation + re-login
    page.goto(f"{https_base}/devtools/playground", wait_until="networkidle")
    page.wait_for_function(
        "() => { const el = document.querySelector('.tp-scope-switcher-env'); return el && el.textContent && !el.textContent.includes('No env'); }",
        timeout=8000,
    )
    pg_model = page.get_by_role("combobox", name=re.compile("Model or route slug", re.I)).first
    pg_model.locator("xpath=..").click()
    pg_model.fill("llama3.2")
    pg_opt = page.get_by_role("option", name=re.compile("llama3\\.2", re.I)).first
    pg_opt.wait_for(state="visible", timeout=5000)
    pg_opt.click()
    pg_prompt = page.get_by_label(re.compile("^Prompt$", re.I)).first
    pg_prompt.fill("Round3 E2E persistence check")
    page.get_by_role("button", name=re.compile("^Send$", re.I)).first.click()
    page.get_by_text("Tanpopo E2E OK").first.wait_for(state="visible", timeout=15000)
    save_shot("e2e-round3-playground-1440.png")

    # Mobile 375x812 dark check + screenshot (switch theme at desktop viewport before collapsing topbar controls)
    set_theme_mode(page, "dark")
    page.set_viewport_size({"width": 375, "height": 812})
    page.wait_for_timeout(150)
    save_shot("e2e-round3-playground-mobile-375-dark.png")
    page.set_viewport_size({"width": 1440, "height": 900})
    set_theme_mode(page, "light")

    # Click Inspect trace -> verifies navigation to /observability/traces?request_id=... (not fallback /overview)!
    inspect_btn = page.get_by_role("button", name=re.compile("Inspect trace", re.I)).first
    inspect_btn.wait_for(state="visible", timeout=10000)
    inspect_btn.click()
    page.wait_for_url("**/observability/traces?request_id=*", timeout=10000)
    page.locator("main#main-content h1").get_by_text(re.compile("Request traces", re.I)).wait_for(state="visible", timeout=8000)
    results["playground_inspect_trace_route_ok"] = True

    # Return to Playground, send prompt, and switch env via Scope Switcher -> Playground must clear stale chat turns!
    page.goto(f"{https_base}/devtools/playground", wait_until="networkidle")
    page.wait_for_function(
        "() => { const el = document.querySelector('.tp-scope-switcher-env'); return el && el.textContent && !el.textContent.includes('No env'); }",
        timeout=8000,
    )
    pg_model2 = page.get_by_role("combobox", name=re.compile("Model or route slug", re.I)).first
    pg_model2.locator("xpath=..").click()
    pg_model2.fill("llama3.2")
    pg_opt2 = page.get_by_role("option", name=re.compile("llama3\\.2", re.I)).first
    pg_opt2.wait_for(state="visible", timeout=5000)
    pg_opt2.click()
    page.get_by_label(re.compile("^Prompt$", re.I)).first.fill("Round3 E2E scope reset check")
    page.get_by_role("button", name=re.compile("^Send$", re.I)).first.click()
    page.get_by_text("Tanpopo E2E OK").first.wait_for(state="visible", timeout=15000)

    scope_btn = page.locator("button.tp-scope-switcher").first
    scope_btn.click()
    no_env_opt = page.get_by_role("option", name=re.compile("No env|All env", re.I)).first
    if no_env_opt.count() > 0:
        no_env_opt.click()
        page.wait_for_timeout(200)
        if page.get_by_text("Tanpopo E2E OK").count() > 0:
            raise AssertionError("DEF-046: Playground kept stale response after scope switch")
    else:
        page.keyboard.press("Escape")
    results["playground_scope_reset_ok"] = True

    # Cross-tab logout propagation: tab2 logged into /overview -> sign out tab1 -> tab2 redirects to /login!
    tab2 = page.context.new_page()
    try:
        tab2.goto(f"{https_base}/overview", wait_until="networkidle")
        tab2.locator("main#main-content h1").wait_for(state="visible", timeout=8000)
        page.get_by_role("button", name=re.compile("^Sign out$", re.I)).first.click()
        tab2.wait_for_url("**/login**", timeout=10000)
        results["cross_tab_logout_ok"] = True
    finally:
        tab2.close()

    # Sign back in cleanly for remaining sweeps
    page.goto(f"{https_base}/login", wait_until="networkidle")
    page.locator('input[type="email"]').fill(owner_email)
    page.locator('input[type="password"]').fill(owner_pw)
    page.locator('button[type="submit"]').click()
    page.wait_for_url("**/overview", timeout=15000)
    page.locator("main#main-content h1").wait_for(state="visible", timeout=15000)
    return results


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
        if not env_srv.get("TANPOPO_RELEASE_ROOT_PUBKEYS"):
            rel_sig_file = tmpdir / "release-signatures.json"
            pub_res = subprocess.run(
                ["go", "run", "./web/console/e2e/signer", "--out", str(rel_sig_file)],
                cwd=str(REPO_ROOT),
                env=env_build,
                capture_output=True,
                text=True,
                check=True,
            )
            env_srv["TANPOPO_RELEASE_ROOT_PUBKEYS"] = pub_res.stdout.strip()
            env_srv["TANPOPO_RELEASE_SIGNATURES_PATH"] = str(rel_sig_file)

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

                        # Full clean onboarding: Marketplace activation -> Adapter -> Connection -> Model/Deploy/Route -> VK -> /v1/chat/completions -> Traces
                        print("[e2e] chromium phase=clean-onboarding-to-trace", flush=True)
                        b_summary["clean_onboarding_to_trace"] = verify_clean_onboarding_to_trace(
                            page, https_base, f"http://127.0.0.1:{http_port}", f"http://127.0.0.1:{fake_port}"
                        )

                        # Multi-Org, Keyboard Palette & Locale Switch (immediately post-login before long sweep)
                        print("[e2e] chromium phase=interactive-multiorg-i18n", flush=True)
                        b_summary["interactive"] = verify_keyboard_and_multi_org_and_i18n(page, https_base)

                        # Round 1 DEF workflows (FinOps Create/Export, Virtual Key A2A, Backups)
                        print("[e2e] chromium phase=round1-defects", flush=True)
                        b_summary["round1_defects"] = verify_round1_defects_e2e(page, https_base)

                        # Round 2 DEF workflows (Enrollment+401, Webhooks, Policies, Explain, Playground Trace, Marketplace, Mobile CIDR)
                        print("[e2e] chromium phase=round2-defects", flush=True)
                        b_summary["round2_defects"] = verify_round2_defects_e2e(
                            page, https_base, browser, report["external_requests"], report["page_errors"]
                        )

                        # Round 3 DEF workflows (Access no OPERATOR, Extensions provenance + SM-01, Routing Target chain, Models short IDs, Playground reset + cross-tab logout)
                        print("[e2e] chromium phase=round3-defects", flush=True)
                        raw_shot_dir = os.environ.get("TANPOPO_E2E_SCREENSHOT_DIR", "").strip()
                        b_summary["round3_defects"] = verify_round3_defects_e2e(
                            page, https_base, owner_email, owner_pw, Path(raw_shot_dir) if raw_shot_dir else None
                        )

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

                        if bname == "webkit":
                            page.goto(f"{https_base}/settings/residency-retention", wait_until="networkidle")
                            page.get_by_role("button", name=re.compile("Add webhook", re.I)).first.click()
                            page.wait_for_selector("dialog.astryx-drawer[open]", timeout=8000)
                            drawer_box = page.evaluate(
                                """() => {
                                    const dlg = document.querySelector('dialog.astryx-drawer[open]');
                                    if (!dlg) return null;
                                    const r = dlg.getBoundingClientRect();
                                    return {left: r.left, right: r.right, width: r.width, innerWidth: window.innerWidth};
                                }"""
                            )
                            if not drawer_box or drawer_box["left"] < 0 or drawer_box["right"] > 375 or drawer_box["width"] < 200:
                                raise AssertionError(f"DEF-009: WebKit 375px Drawer positioned outside viewport: {drawer_box}")
                            url_input = page.locator("dialog.astryx-drawer[open] input[type='url']").first
                            url_input.fill("https://hooks.example.com/tanpopo-webkit")
                            if url_input.input_value() != "https://hooks.example.com/tanpopo-webkit":
                                raise AssertionError("DEF-009: Unable to fill input inside WebKit 375px Drawer")
                            screenshot_path = os.environ.get("TANPOPO_E2E_WEBKIT_DRAWER_PNG", "").strip()
                            if screenshot_path:
                                page.screenshot(path=screenshot_path)
                            page.locator("dialog.astryx-drawer[open] button:has-text('Cancel')").first.click()
                            b_summary["webkit_375_drawer"] = drawer_box

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
