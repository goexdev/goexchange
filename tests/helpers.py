
"""
UI Test Helpers for goexchange.

Provides:
- API client for backend setup (create users, place orders)
- Browser session for UI tests
- Common assertions
- Test data cleanup

Usage:
    from helpers import login_user, place_order, run_test
"""
import os
import sys
import time
import json
import subprocess
from typing import Optional, Dict, Any
from playwright.sync_api import sync_playwright, Page, Browser, BrowserContext

# Config
API_BASE = os.getenv("API_BASE", "http://localhost:8099/api/v1")
HEALTH_URL = os.getenv("HEALTH_URL", "http://localhost:8099/healthz")
WEB_BASE = os.getenv("WEB_BASE", "https://pow.credit")
TEST_USER_EMAIL = "uitest@goexchange.local"
TEST_USER_PASSWORD = "uitest123!"


def setup_test_user() -> Dict[str, str]:
    """Create or reset test user with USDT balance. Returns credentials."""
    import requests

    # Try login first - handle both regular and 2FA cases
    try:
        r = requests.post(f"{API_BASE}/users/login",
                          json={"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD})
        if r.status_code == 200:
            data = r.json()
            if "token" in data:
                token = data["token"]
                # Reset 2FA just to be sure
                reset_2fa(TEST_USER_EMAIL)
                return {"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD, "token": token}
            elif data.get("requires_2fa"):
                # 2FA is enabled - reset it
                reset_2fa(TEST_USER_EMAIL)
                # Try again
                r = requests.post(f"{API_BASE}/users/login",
                                  json={"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD})
                if r.status_code == 200:
                    data = r.json()
                    if "token" in data:
                        return {"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD, "token": data["token"]}
    except Exception:
        pass

    # Register
    r = requests.post(f"{API_BASE}/users/register",
                      json={"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD})
    if r.status_code in [201, 409]:
        r = requests.post(f"{API_BASE}/users/login",
                          json={"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD})
        token = r.json()["token"]

        # Add USDT balance via direct DB
        subprocess.run([
            "docker", "exec", "-i", "goexchange-postgres", "psql",
            "-U", "exchange", "-d", "exchange", "-c",
            f"INSERT INTO balances (user_id, asset, available) VALUES ((SELECT id FROM users WHERE email = '{TEST_USER_EMAIL}'), 'USDT', '100000') ON CONFLICT DO NOTHING;"
        ], check=False)

        # Set KYC level high
        subprocess.run([
            "docker", "exec", "-i", "goexchange-postgres", "psql",
            "-U", "exchange", "-d", "exchange", "-c",
            f"UPDATE users SET kyc_level = 2, kyc_status = 'APPROVED', daily_withdraw_limit_usdt = 1000000 WHERE email = '{TEST_USER_EMAIL}';"
        ], check=False)

        # Reset 2FA (in case previous test left it on)
        reset_2fa(TEST_USER_EMAIL)
        # Reset notification prefs
        reset_notif_prefs(TEST_USER_EMAIL)

        return {"email": TEST_USER_EMAIL, "password": TEST_USER_PASSWORD, "token": token}

    raise RuntimeError(f"Failed to setup test user: {r.text}")


def login_user(page: Page, token: str = None) -> None:
    """Set up auth in browser via localStorage token injection."""
    if token is None:
        user = setup_test_user()
        token = user["token"]
    page.goto(f"{WEB_BASE}/")
    page.evaluate(f"() => localStorage.setItem('goexchange_token', '{token}')")


def place_order(token: str, pair: str = "BTC_USDT", side: str = "BUY",
                price: str = "29000", quantity: str = "0.01") -> Dict[str, Any]:
    """Place an order via API (faster than UI). Returns order info."""
    import requests
    h = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
    r = requests.post(f"{API_BASE}/orders", headers=h,
                      data=f'{{"pair":"{pair}","side":"{side}","price":"{price}","quantity":"{quantity}"}}')
    if r.status_code != 201:
        raise RuntimeError(f"Failed to place order: {r.status_code} {r.text}")
    return r.json()


def list_orders(token: str, status: str = None) -> list:
    """List user orders. Optionally filter by status."""
    import requests
    h = {"Authorization": f"Bearer {token}"}
    r = requests.get(f"{API_BASE}/orders", headers=h)
    orders = r.json().get("orders", [])
    if status:
        orders = [o for o in orders if o["status"] == status]
    return orders


def cancel_order(token: str, order_id: str, pair: str) -> bool:
    """Cancel a specific order. Returns True if successful."""
    import requests
    h = {"Authorization": f"Bearer {token}"}
    r = requests.delete(f"{API_BASE}/orders/{order_id}?pair={pair}", headers=h)
    return r.status_code == 200


def wait_for_rate_limit(seconds: int = 60):
    """Wait for login rate limit to reset (5/min/IP)."""
    print(f"  Waiting {seconds}s for rate limit...")
    time.sleep(seconds)


def gen_totp(secret: str) -> str:
    """Generate current TOTP code from base32 secret."""
    result = subprocess.run(
        ["python3", "/tmp/gen_totp.py", secret],
        capture_output=True, text=True
    )
    return result.stdout.strip()


def reset_2fa(email: str) -> None:
    """Remove 2FA for test user."""
    subprocess.run([
        "docker", "exec", "-i", "goexchange-postgres", "psql",
        "-U", "exchange", "-d", "exchange", "-c",
        f"DELETE FROM user_totp WHERE user_id = (SELECT id FROM users WHERE email = '{email}');"
    ], check=False)


def reset_notif_prefs(email: str) -> None:
    """Reset notification preferences."""
    subprocess.run([
        "docker", "exec", "-i", "goexchange-postgres", "psql",
        "-U", "exchange", "-d", "exchange", "-c",
        f"DELETE FROM user_notification_prefs WHERE user_id = (SELECT id FROM users WHERE email = '{email}');"
    ], check=False)


def take_screenshot(page: Page, name: str) -> str:
    """Save screenshot to tests/screenshots/. Returns path."""
    path = f"/root/goexchange/tests/screenshots/{name}.png"
    page.screenshot(path=path, full_page=False)
    return path


def assert_no_console_errors(errors: list, test_name: str) -> None:
    """Check no critical console errors occurred."""
    critical = [e for e in errors if "[error]" in e.lower() and "websocket" not in e.lower()]
    if critical:
        raise AssertionError(f"{test_name}: console errors: {critical}")


class TestResult:
    """Track test result for reporting."""
    def __init__(self, name: str):
        self.name = name
        self.passed = False
        self.error: Optional[str] = None
        self.duration: float = 0
        self.screenshots: list = []

    def __repr__(self):
        status = "✓" if self.passed else "✗"
        return f"  {status} {self.name} ({self.duration:.1f}s)"


class TestRunner:
    """Run multiple tests and collect results."""
    def __init__(self):
        self.results: list = []
        self.current: Optional[TestResult] = None

    def run(self, name: str, test_fn):
        """Run a single test with timing and error capture."""
        self.current = TestResult(name)
        start = time.time()
        print(f"\nRunning: {name}")
        try:
            test_fn()
            self.current.passed = True
            print(f"  ✓ PASS ({time.time() - start:.1f}s)")
        except Exception as e:
            self.current.passed = False
            self.current.error = str(e)
            print(f"  ✗ FAIL: {e}")
        finally:
            self.current.duration = time.time() - start
            self.results.append(self.current)

    def report(self) -> bool:
        """Print summary and return True if all passed."""
        passed = sum(1 for r in self.results if r.passed)
        total = len(self.results)
        print(f"\n{'='*60}")
        print(f"Results: {passed}/{total} passed")
        print('='*60)
        for r in self.results:
            print(r)
            if not r.passed and r.error:
                print(f"      Error: {r.error[:200]}")
        return passed == total
