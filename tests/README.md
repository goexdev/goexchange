# UI Integration Tests

Automated UI tests for goexchange web frontend.

## Purpose

These tests catch integration bugs that pure API tests miss. Example: the
single Cancel button bug (M6.54) where frontend `cancelOrder()` didn't pass
the `pair` query param, causing a silent 400 error.

## Test Files

| File | What it tests |
|---|---|
| `test_cancel_order.py` | Single + Cancel All buttons (regression for M6.54) |
| `test_login_2fa.py` | Login flow with and without 2FA |
| `test_navigation.py` | All main pages load without errors |
| `test_settings.py` | 2FA enable flow + Notification Preferences UI |
| `helpers.py` | Shared test utilities (API client, browser, assertions) |

## Running Tests

```bash
# Run all tests
./run_tests.sh

# Run specific test
./run_tests.sh test_cancel_order
./run_tests.sh test_login_2fa
./run_tests.sh test_navigation
./run_tests.sh test_settings
```

## Prerequisites

1. Backend services running (api, matcher, scheduler)
2. Web deployed or accessible at $WEB_BASE
3. Python playwright installed: `pip install playwright`
4. Browser binaries: `playwright install chromium`

## Environment Variables

| Var | Default | Purpose |
|---|---|---|
| `API_BASE` | http://localhost:8099/api/v1 | Backend API URL |
| `WEB_BASE` | https://goexchange.top | Frontend URL |

## Adding New Tests

1. Create `test_<feature>.py` with tests as functions
2. Use `helpers.py` for shared setup
3. Use `TestRunner` for reporting
4. Add to test list in `run_tests.sh` if needed

## Test Output

Tests produce:
- Console output with PASS/FAIL per test
- Screenshots saved to `screenshots/` directory
- Exit code 0 = all pass, 1 = any failure

## CI Integration

```yaml
# Example GitHub Actions step
- name: Run UI Tests
  run: ./tests/run_tests.sh
  env:
    API_BASE: http://localhost:8099/api/v1
    WEB_BASE: http://web:3000
```
