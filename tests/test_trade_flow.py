
"""
Test full trade flow: place order, verify in list, cancel.
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    WEB_BASE, setup_test_user, login_user, place_order,
    list_orders, cancel_order, take_screenshot, TestRunner
)


def test_order_appears_in_panel():
    """After placing order, it should appear in Trade page Orders panel."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        user = setup_test_user()
        order = place_order(user["token"], "BTC_USDT", "BUY", "29500", "0.005")
        order_id = order["order_id"]

        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/trade/BTC/USDT")
        time.sleep(3)

        page.evaluate("window.scrollTo(0, document.body.scrollHeight)")
        time.sleep(1)

        body_text = page.inner_text("body")

        # Verify price is visible
        if "29500" not in body_text:
            take_screenshot(page, "order_price_not_shown")
            raise AssertionError(f"Order price 29500 not visible in Orders panel")

        # Verify Cancel button exists
        cancel_found = False
        for btn in page.query_selector_all('button'):
            text = btn.text_content().strip() if btn.text_content() else ""
            if text == "Cancel":
                cancel_found = True
                break

        if not cancel_found:
            take_screenshot(page, "no_cancel_button")
            raise AssertionError("No individual Cancel button found")

        # Cleanup
        cancel_order(user["token"], order_id, "BTC_USDT")

        take_screenshot(page, "order_in_panel_ok")
        browser.close()
        print(f"  ✓ Order appears in panel with Cancel button")


def test_balance_updates_after_cancel():
    """After cancel, balance should be refunded (M6.40 regression)."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        user = setup_test_user()

        # Get initial balance
        import requests
        from helpers import API_BASE
        h = {"Authorization": f"Bearer {user['token']}"}
        r = requests.get(f"{API_BASE}/wallets", headers=h)
        initial_usdt = float([w for w in r.json() if w['asset'] == 'USDT'][0]['available'])
        initial_frozen = float([w for w in r.json() if w['asset'] == 'USDT'][0]['frozen'])

        # Place order (0.005 BTC @ 29500 = 147.5 USDT frozen)
        order = place_order(user["token"], "BTC_USDT", "BUY", "29500", "0.005")
        order_id = order["order_id"]

        # Check frozen increased
        r = requests.get(f"{API_BASE}/wallets", headers=h)
        frozen_after_place = float([w for w in r.json() if w['asset'] == 'USDT'][0]['frozen'])

        if frozen_after_place <= initial_frozen:
            raise AssertionError(f"Balance frozen should increase: {initial_frozen} -> {frozen_after_place}")

        # Navigate and cancel via UI
        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/trade/BTC/USDT")
        time.sleep(3)
        page.evaluate("window.scrollTo(0, document.body.scrollHeight)")
        time.sleep(1)

        page.on("dialog", lambda d: d.accept())
        for btn in page.query_selector_all('button'):
            if btn.text_content().strip() == "Cancel":
                btn.click()
                break
        time.sleep(2)

        # Check frozen is back to initial
        r = requests.get(f"{API_BASE}/wallets", headers=h)
        final_frozen = float([w for w in r.json() if w['asset'] == 'USDT'][0]['frozen'])

        if abs(final_frozen - initial_frozen) > 0.01:
            take_screenshot(page, "balance_not_refunded")
            raise AssertionError(
                f"Balance not refunded: initial={initial_frozen}, after_place={frozen_after_place}, final={final_frozen}"
            )

        browser.close()
        print(f"  ✓ Balance refunded after cancel: {initial_frozen} -> {frozen_after_place} -> {final_frozen}")


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("Order in Panel", test_order_appears_in_panel)
    runner.run("Balance Refund on Cancel", test_balance_updates_after_cancel)
    success = runner.report()
    sys.exit(0 if success else 1)
