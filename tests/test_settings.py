
"""
Test Settings page: Notification Preferences UI and 2FA section.
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    WEB_BASE, setup_test_user, login_user,
    reset_2fa, reset_notif_prefs, take_screenshot,
    TestRunner
)


def test_settings_page_loads():
    """Verify Settings page loads with notification preferences visible."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 1500})
        page = context.new_page()

        user = setup_test_user()
        reset_2fa(user["email"])
        reset_notif_prefs(user["email"])

        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/settings")
        time.sleep(3)

        page.evaluate("window.scrollTo(0, document.body.scrollHeight)")
        time.sleep(1)

        body_text = page.inner_text("body")

        # Check for key phrases (English or localized)
        has_notif_pref = any(phrase in body_text for phrase in [
            "Notification Preferences", "通知偏好",
            "Two-Factor", "2FA",
        ])
        if not has_notif_pref:
            take_screenshot(page, "settings_no_notif")
            raise AssertionError("Notification preferences section not found")

        # Find toggle buttons (the relative-positioned buttons we use)
        toggles = page.query_selector_all('button.relative')
        if len(toggles) < 4:
            take_screenshot(page, "settings_no_toggles")
            raise AssertionError(f"Expected at least 4 toggles, found {len(toggles)}")

        # Find at least one Critical or Recommended label
        has_critical_label = any(phrase in body_text for phrase in [
            "Critical", "Recommended", "关键", "推荐"
        ])
        if not has_critical_label:
            take_screenshot(page, "settings_no_critical_label")
            raise AssertionError("Critical/Recommended label not found")

        take_screenshot(page, "settings_success")
        browser.close()
        print(f"  ✓ Settings page shows notification preferences with {len(toggles)} toggles")


def test_2fa_section_visible():
    """Verify 2FA section is visible in Settings."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 1500})
        page = context.new_page()

        user = setup_test_user()
        reset_2fa(user["email"])

        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/settings")
        time.sleep(3)

        body_text = page.inner_text("body")
        has_2fa = any(phrase in body_text for phrase in [
            "Two-Factor", "2FA", "Authentication"
        ])

        if not has_2fa:
            take_screenshot(page, "no_2fa_section")
            raise AssertionError("2FA section not visible")

        # Check for Enable 2FA button (when 2FA is off)
        if "Enable 2FA" in body_text or "启用 2FA" in body_text:
            print(f"  ✓ Enable 2FA button visible")
        elif "Disable 2FA" in body_text or "禁用 2FA" in body_text:
            print(f"  ✓ 2FA enabled - Disable button visible")
        else:
            raise AssertionError("Neither Enable nor Disable 2FA button found")

        take_screenshot(page, "2fa_section_visible")
        browser.close()
        print(f"  ✓ 2FA section visible")


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("Settings Page Loads", test_settings_page_loads)
    runner.run("2FA Section Visible", test_2fa_section_visible)
    success = runner.report()
    sys.exit(0 if success else 1)
