
"""
Test that all main pages load without errors.
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    WEB_BASE, setup_test_user, login_user,
    reset_2fa, take_screenshot, TestRunner, assert_no_console_errors
)


PAGES = [
    ("/", "home"),
    ("/markets", "markets"),
    ("/trade/BTC_USDT", "trade"),
    ("/wallet", "wallet"),
    ("/orders", "orders"),
    ("/notifications", "notifications"),
    ("/settings", "settings"),
    ("/api-keys", "apikeys"),
]


def test_all_pages_load():
    """Navigate to all pages and verify no console errors."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        user = setup_test_user()
        reset_2fa(user["email"])

        errors = []

        def handle_console(msg):
            if msg.type == "error":
                errors.append(f"[{msg.type}] {msg.text}")

        page.on("console", handle_console)

        # Login
        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])

        for url, name in PAGES:
            print(f"  Testing /{name}...")
            errors.clear()

            page.goto(f"{WEB_BASE}{url}")
            time.sleep(2)

            # Page should have content (not just blank)
            body_text = page.inner_text("body")
            if len(body_text) < 50:
                take_screenshot(page, f"{name}_empty")
                raise AssertionError(f"Page /{name} appears blank (only {len(body_text)} chars)")

            assert_no_console_errors(errors, name)
            print(f"    ✓ /{name} loaded OK ({len(body_text)} chars)")

        browser.close()


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("All Pages Load", test_all_pages_load)
    success = runner.report()
    sys.exit(0 if success else 1)
