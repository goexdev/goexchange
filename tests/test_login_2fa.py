
"""
Test login flow with 2FA integration (M6.45 regression test).
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    WEB_BASE, setup_test_user, reset_2fa, gen_totp,
    take_screenshot, TestRunner
)
import requests
from helpers import API_BASE


def enable_2fa_api(token: str) -> str:
    """Enable 2FA via API. Returns the secret for later use."""
    h = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}

    # Setup
    r = requests.post(f"{API_BASE}/users/me/2fa/setup", headers=h)
    secret = r.json()["secret"]

    # Enable with TOTP code
    code = gen_totp(secret)
    r = requests.post(f"{API_BASE}/users/me/2fa/enable", headers=h,
                      json={"code": code})
    if r.status_code != 200:
        raise RuntimeError(f"Failed to enable 2FA: {r.text}")
    return secret


def test_login_with_2fa():
    """Verify login flow requires 2FA when enabled."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        # Setup user and enable 2FA
        user = setup_test_user()
        reset_2fa(user["email"])
        secret = enable_2fa_api(user["token"])
        print(f"  2FA enabled for {user['email']}")

        # Now test UI login
        page.goto(f"{WEB_BASE}/login")
        time.sleep(2)

        # Fill login form
        email_input = page.query_selector('input[type="email"]') or page.query_selector('input[name="email"]')
        password_input = page.query_selector('input[type="password"]')

        if email_input and password_input:
            email_input.fill(user["email"])
            password_input.fill(user["password"])
            time.sleep(0.5)

            # Submit
            submit_btn = page.query_selector('button[type="submit"]')
            if submit_btn:
                submit_btn.click()
            time.sleep(2)

            # Should now show 2FA code input
            # Look for new input that appeared (likely for 6-digit code)
            all_inputs = page.query_selector_all('input')
            twofa_input = None
            for inp in all_inputs:
                ph = inp.get_attribute("placeholder") or ""
                maxlen = inp.get_attribute("maxlength") or "999"
                # 2FA input is usually 6 chars, placeholder mentions code/verification
                if "6" in maxlen or "code" in ph.lower() or "2fa" in ph.lower():
                    twofa_input = inp
                    break

            if twofa_input:
                # Generate current TOTP code
                code = gen_totp(secret)
                twofa_input.fill(code)
                time.sleep(2)
                take_screenshot(page, "2fa_login_complete")
                print(f"  ✓ 2FA login completed via UI")
            else:
                take_screenshot(page, "2fa_input_missing")
                raise AssertionError("2FA input field did not appear after password submit")
        else:
            raise AssertionError("Login form fields not found")

        browser.close()


def test_login_form_exists():
    """Verify login form has email + password fields."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        page.goto(f"{WEB_BASE}/login")
        time.sleep(2)

        # Check for required fields
        email_input = page.query_selector('input[type="email"]')
        password_input = page.query_selector('input[type="password"]')

        if not email_input:
            raise AssertionError("Email input field missing")
        if not password_input:
            raise AssertionError("Password input field missing")

        # Check for submit button (any button)
        buttons = page.query_selector_all('button')
        submit = None
        for btn in buttons:
            t = (btn.text_content() or "").lower()
            if "sign" in t or "log" in t or "auth" in t:
                submit = btn
                break

        if not submit:
            take_screenshot(page, "login_no_submit")
            raise AssertionError("No login submit button found")

        take_screenshot(page, "login_form_ok")
        browser.close()
        print(f"  ✓ Login form has email + password + submit button")


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("Login Form Exists", test_login_form_exists)
    runner.run("Login With 2FA", test_login_with_2fa)
    success = runner.report()
    sys.exit(0 if success else 1)
