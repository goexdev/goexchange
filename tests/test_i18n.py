
"""
Test i18n: verify translations actually work (no literal keys shown).
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    WEB_BASE, take_screenshot, TestRunner
)


# Patterns that indicate a literal i18n key (not translated)
LITERAL_KEY_PATTERNS = [
    'auth.login.signIn',
    'auth.login.signUp',
    'common.loading',
    'common.cancel',
    'home.',
    'trade.',
    'wallet.',
    'settings.',
]


def has_literal_key(text: str) -> list:
    """Return list of literal keys found in text."""
    return [p for p in LITERAL_KEY_PATTERNS if p in text]


def test_login_no_literal_keys():
    """Login page should not show literal i18n keys."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        page.goto(f"{WEB_BASE}/login")
        time.sleep(3)

        body_text = page.inner_text("body")
        literals = has_literal_key(body_text)

        if literals:
            take_screenshot(page, "login_literal_keys")
            raise AssertionError(f"Login shows literal i18n keys: {literals}")

        # Verify "Sign in" or translation is visible
        buttons = page.query_selector_all('button')
        login_btn_text = None
        for btn in buttons:
            t = (btn.text_content() or "").strip()
            if t and 'sign' in t.lower():
                login_btn_text = t
                break

        if not login_btn_text:
            raise AssertionError("No login button found")
        if 'sign in' not in login_btn_text.lower() and '登录' not in login_btn_text:
            raise AssertionError(f"Login button text unexpected: '{login_btn_text}'")

        take_screenshot(page, "login_i18n_ok")
        browser.close()
        print(f"  ✓ Login page has no literal keys, button='{login_btn_text}'")


def test_home_no_literal_keys():
    """Home page should not show literal i18n keys."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        page.goto(f"{WEB_BASE}/home")
        time.sleep(3)

        body_text = page.inner_text("body")
        literals = has_literal_key(body_text)

        if literals:
            take_screenshot(page, "home_literal_keys")
            raise AssertionError(f"Home shows literal i18n keys: {literals}")

        take_screenshot(page, "home_i18n_ok")
        browser.close()
        print(f"  ✓ Home page has no literal keys")


def test_markets_no_literal_keys():
    """Markets page should not show literal i18n keys."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        page.goto(f"{WEB_BASE}/markets")
        time.sleep(3)

        body_text = page.inner_text("body")
        literals = has_literal_key(body_text)

        if literals:
            take_screenshot(page, "markets_literal_keys")
            raise AssertionError(f"Markets shows literal i18n keys: {literals}")

        take_screenshot(page, "markets_i18n_ok")
        browser.close()
        print(f"  ✓ Markets page has no literal keys")


def test_trade_no_literal_keys():
    """Trade page should not show literal i18n keys."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        page.goto(f"{WEB_BASE}/trade/BTC/USDT")
        time.sleep(3)

        body_text = page.inner_text("body")
        literals = has_literal_key(body_text)

        if literals:
            take_screenshot(page, "trade_literal_keys")
            raise AssertionError(f"Trade shows literal i18n keys: {literals}")

        take_screenshot(page, "trade_i18n_ok")
        browser.close()
        print(f"  ✓ Trade page has no literal keys")


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("Login i18n", test_login_no_literal_keys)
    runner.run("Home i18n", test_home_no_literal_keys)
    runner.run("Markets i18n", test_markets_no_literal_keys)
    runner.run("Trade i18n", test_trade_no_literal_keys)
    success = runner.report()
    sys.exit(0 if success else 1)
