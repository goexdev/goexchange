#!/bin/bash
# UI Integration Test Runner for goexchange
#
# Runs all UI tests sequentially and reports results.
# Each test is independent - uses fresh browser session.
#
# Usage:
#   ./run_tests.sh [test_name]
#
# Examples:
#   ./run_tests.sh              # Run all
#   ./run_tests.sh test_cancel  # Run cancel tests only

set -e
cd "$(dirname "$0")"

# Activate venv if available
if [ -d "/usr/local/lib/hermes-agent/venv" ]; then
    source /usr/local/lib/hermes-agent/venv/bin/activate
fi

# Check prerequisites
echo "Checking prerequisites..."
python3 -c "import playwright" 2>/dev/null || {
    echo "Installing playwright..."
    pip install playwright 2>&1 | tail -2
}

python3 -c "from playwright.sync_api import sync_playwright" 2>/dev/null || {
    echo "Installing playwright python module..."
    pip install playwright 2>&1 | tail -2
}

# Check if API is reachable
API_URL="${API_BASE:-http://localhost:8099/api/v1}"
echo ""
echo "API: $API_URL"
curl -sf "http://localhost:8099/healthz" > /dev/null || {
    echo "ERROR: API not reachable. Start services first."
    exit 1
}

# Run tests
TESTS="${1:-all}"
echo ""
echo "Running tests: $TESTS"
echo "================================"

EXIT_CODE=0

if [ "$TESTS" = "all" ]; then
    for test in test_*.py; do
        echo ""
        echo "### $test ###"
        python3 "$test" || EXIT_CODE=1
    done
else
    for test in $TESTS; do
        if [ -f "${test}.py" ]; then
            echo ""
            echo "### $test ###"
            python3 "${test}.py" || EXIT_CODE=1
        else
            echo "Test not found: $test"
            EXIT_CODE=1
        fi
    done
fi

echo ""
echo "================================"
if [ $EXIT_CODE -eq 0 ]; then
    echo "All tests PASSED"
else
    echo "Some tests FAILED"
fi

exit $EXIT_CODE
