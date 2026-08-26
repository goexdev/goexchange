
"""
CRITICAL TEST: Single cancel button must work (regression test for M6.54)

This test was added to prevent the bug where:
- Frontend cancelOrder() didn't pass pair query param
- Backend returned 400 'missing pair query param'
- Error was silently swallowed
- User saw no feedback
"""
import sys
import time
from pathlib import Path
sys.path.insert(0, str(Path(__file__).parent))

from playwright.sync_api import sync_playwright
from helpers import (
    API_BASE, WEB_BASE, setup_test_user, login_user,
    place_order, list_orders, cancel_order, take_screenshot,
    reset_2fa, TestRunner
)


def test_single_cancel_button():
    """Verify clicking individual Cancel button works in Trade page."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        # Setup user with balance
        user = setup_test_user()
        reset_2fa(user["email"])

        # Place order via API
        order = place_order(user["token"], "BTC_USDT", "BUY", "29000", "0.01")
        order_id = order["order_id"]
        print(f"  Placed order {order_id[:8]}...")

        # Navigate to Trade page
        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/trade/BTC/USDT")
        time.sleep(3)

        # Scroll to Orders panel
        page.evaluate("window.scrollTo(0, document.body.scrollHeight)")
        time.sleep(1)

        # Find individual Cancel button (text == "Cancel", not "Cancel All")
        cancel_buttons = page.query_selector_all('button')

        individual_cancel = None
        for btn in cancel_buttons:
            text = btn.text_content().strip() if btn.text_content() else ""
            if text == "Cancel":
                individual_cancel = btn
                break

        if not individual_cancel:
            take_screenshot(page, "cancel_button_missing")
            raise AssertionError("Could not find individual Cancel button on Trade page")

        # Setup dialog handler to accept confirm
        page.on("dialog", lambda d: d.accept())

        # Click it
        individual_cancel.click()
        time.sleep(2)

        # Verify order is cancelled via API
        orders = list_orders(user["token"])
        target_order = next((o for o in orders if o["id"] == order_id), None)
        if not target_order:
            raise AssertionError(f"Order {order_id} not found after cancel")

        if target_order["status"] != "CANCELLED":
            take_screenshot(page, "cancel_not_worked")
            raise AssertionError(
                f"Order status should be CANCELLED but is {target_order['status']}"
            )

        take_screenshot(page, "cancel_success")
        browser.close()
        print(f"  ✓ Order {order_id[:8]} cancelled successfully via UI button")


def test_cancel_all_button():
    """Verify Cancel All button works."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1400, "height": 900})
        page = context.new_page()

        user = setup_test_user()
        reset_2fa(user["email"])

        # Place 3 orders
        placed = []
        for i in range(3):
            order = place_order(user["token"], "BTC_USDT", "BUY", str(29000 + i * 100), "0.01")
            placed.append(order["order_id"])
        print(f"  Placed {len(placed)} orders")

        page.goto(f"{WEB_BASE}/login")
        time.sleep(1)
        login_user(page, user["token"])
        page.goto(f"{WEB_BASE}/trade/BTC/USDT")
        time.sleep(3)

        page.evaluate("window.scrollTo(0, document.body.scrollHeight)")
        time.sleep(1)

        # Find Cancel All button
        cancel_all_btn = None
        for btn in page.query_selector_all('button'):
            text = btn.text_content().strip() if btn.text_content() else ""
            if "Cancel All" in text or "cancel all" in text.lower():
                cancel_all_btn = btn
                break

        if not cancel_all_btn:
            take_screenshot(page, "cancel_all_missing")
            raise AssertionError("Cancel All button not found")

        page.on("dialog", lambda d: d.accept())
        cancel_all_btn.click()
        time.sleep(2)

        # Verify all cancelled
        orders = list_orders(user["token"], status="OPEN")
        for oid in placed:
            still_open = any(o["id"] == oid for o in orders)
            if still_open:
                raise AssertionError(f"Order {oid[:8]} still OPEN after Cancel All")

        browser.close()
        print(f"  ✓ All {len(placed)} orders cancelled via Cancel All button")


if __name__ == "__main__":
    runner = TestRunner()
    runner.run("Single Cancel Button", test_single_cancel_button)
    runner.run("Cancel All Button", test_cancel_all_button)
    success = runner.report()
    sys.exit(0 if success else 1)
