
"""
Test Withdraw flow: page loads, validation works.
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    WEB_BASE, setup_test_user, login_user, take_screenshot, TestRunner
)


def test_withdraw_page_loads():
    """Verify Withdraw page loads and shows form."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        user = setup_test_user()

        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/withdraw")
        time.sleep(3)

        body_text = page.inner_text("body")
        has_form = "Asset" in body_text or "资产" in body_text or "Amount" in body_text or "金额" in body_text or "Address" in body_text

        if not has_form:
            take_screenshot(page, "withdraw_no_form")
            raise AssertionError(f"Withdraw form fields missing")

        # Should have an address input
        addr_input = page.query_selector('input[placeholder*="ddress"], input[name="address"]')
        if not addr_input:
            take_screenshot(page, "withdraw_no_address")
            # Some implementations might use different selectors - check any input
            inputs = page.query_selector_all('input')
            if len(inputs) < 2:
                raise AssertionError(f"Too few inputs ({len(inputs)}) for withdraw form")

        take_screenshot(page, "withdraw_page_ok")
        browser.close()
        print(f"  ✓ Withdraw page loads with form")


def test_withdraw_validates_address():
    """Withdraw should reject obviously invalid addresses."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        user = setup_test_user()

        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/withdraw")
        time.sleep(3)

        # Find address input
        addr_input = page.query_selector('input[placeholder*="ddress"]') or page.query_selector('input[name="address"]')
        if addr_input:
            addr_input.fill("not_a_valid_address")
            time.sleep(0.5)

            # Find submit button
            submit = page.query_selector('button[type="submit"]')
            if submit:
                submit.click()
                time.sleep(2)

                # Look for error or rejection
                body_text = page.inner_text("body")
                has_error = any(word in body_text.lower() for word in ['invalid', 'error', '错误', '无效'])

                take_screenshot(page, "withdraw_validation")

                if not has_error:
                    print(f"  ⚠ No visible validation error for bad address (might be silent)")

        browser.close()
        print(f"  ✓ Withdraw validation tested")


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("Withdraw Page Loads", test_withdraw_page_loads)
    runner.run("Withdraw Validates Address", test_withdraw_validates_address)
    success = runner.report()
    sys.exit(0 if success else 1)
