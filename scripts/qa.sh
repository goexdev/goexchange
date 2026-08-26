#!/bin/bash
# QA Script - runs all tests and generates a comprehensive report
# Usage:
#   ./scripts/qa.sh              # Full QA (manual)
#   ./scripts/qa.sh --auto       # Auto mode (no colors, exits non-zero on fail)
#   ./scripts/qa.sh --fast       # Fast mode (skip E2E)
#   ./scripts/qa.sh --notify     # Send notification to Telegram on failure

set -e
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$( cd "$SCRIPT_DIR/.." && pwd )"
REPORT_DIR="$ROOT_DIR/reports/qa"
TIMESTAMP=$(date +"%Y%m%d_%H%M%S")
REPORT_FILE="$REPORT_DIR/qa-report-$TIMESTAMP.txt"
mkdir -p "$REPORT_DIR"

# Mode flags
AUTO_MODE=false
FAST_MODE=false
NOTIFY_MODE=false
for arg in "$@"; do
    case $arg in
        --auto) AUTO_MODE=true ;;
        --fast) FAST_MODE=true ;;
        --notify) NOTIFY_MODE=true ;;
    esac
done

# Colors (disabled in auto mode)
if [ "$AUTO_MODE" = true ]; then
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
else
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
fi

PASS=0
FAIL=0
WARN=0
RESULTS=()

print_header() {
    if [ "$AUTO_MODE" = false ]; then
        echo ""
        echo "=========================================="
        echo "$1"
        echo "=========================================="
    else
        echo "[$(date +%H:%M:%S)] $1"
    fi
}

run_check() {
    local name="$1"
    local cmd="$2"
    print_header "$name"
    if eval "$cmd" > /tmp/qa_output.log 2>&1; then
        echo -e "${GREEN}PASS${NC} $name"
        RESULTS+=("PASS: $name")
        PASS=$((PASS+1))
    else
        echo -e "${RED}FAIL${NC} $name"
        echo "  Output (last 5 lines):"
        tail -5 /tmp/qa_output.log | sed 's/^/    /'
        RESULTS+=("FAIL: $name")
        FAIL=$((FAIL+1))
    fi
}

send_telegram_notification() {
    local msg="$1"
    # Use hermes send (uses gateway credentials automatically)
    echo "$msg" | hermes send -t telegram 2>/dev/null || return
}

# 1. Health check
run_check "Service health check" "curl -k -s https://pow.credit/healthz | grep -q ok"

# 2. Public status endpoint
run_check "Public status endpoint" "curl -k -s https://pow.credit/api/v1/status | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d[\"status\"] in [\"operational\", \"degraded\"]'"

# 3. Backend Go tests
print_header "Backend Go tests"
cd "$ROOT_DIR"
if /usr/local/go/bin/go test ./internal/... > /tmp/qa_go.log 2>&1; then
    echo -e "${GREEN}PASS${NC} Backend Go tests"
    PASS=$((PASS+1))
    RESULTS+=("PASS: Backend Go tests")
else
    echo -e "${RED}FAIL${NC} Backend Go tests"
    tail -10 /tmp/qa_go.log | sed 's/^/    /'
    FAIL=$((FAIL+1))
    RESULTS+=("FAIL: Backend Go tests")
fi

# 4. Frontend unit tests
print_header "Frontend unit tests (Vitest)"
cd "$ROOT_DIR/web"
if ./node_modules/.bin/vitest run > /tmp/qa_vitest.log 2>&1; then
    echo -e "${GREEN}PASS${NC} Frontend Vitest"
    PASS=$((PASS+1))
    RESULTS+=("PASS: Frontend Vitest")
else
    echo -e "${RED}FAIL${NC} Frontend Vitest"
    tail -10 /tmp/qa_vitest.log | sed 's/^/    /'
    FAIL=$((FAIL+1))
    RESULTS+=("FAIL: Frontend Vitest")
fi

# 5. E2E tests (skip in --fast mode)
if [ "$FAST_MODE" = false ]; then
    print_header "E2E tests (Playwright)"
    if [ -d "node_modules/@playwright/test" ]; then
        if ./node_modules/.bin/playwright test > /tmp/qa_e2e.log 2>&1; then
            E2E_RESULT="PASS"
            PASS=$((PASS+1))
            RESULTS+=("PASS: E2E tests")
        else
            E2E_RESULT="FAIL"
            FAIL=$((FAIL+1))
            RESULTS+=("FAIL: E2E tests")
        fi
        echo "$E2E_RESULT E2E tests"
        echo "  Report: $ROOT_DIR/web/e2e/report"
    else
        echo -e "${YELLOW}SKIP${NC} Playwright not installed (run: cd web && npm install)"
        WARN=$((WARN+1))
    fi
fi

# 6. TypeScript build check
print_header "TypeScript build"
cd "$ROOT_DIR/web"
if npx tsc --noEmit > /tmp/qa_tsc.log 2>&1; then
    echo -e "${GREEN}PASS${NC} TypeScript no errors"
    PASS=$((PASS+1))
    RESULTS+=("PASS: TypeScript")
else
    echo -e "${YELLOW}WARN${NC} TypeScript errors (non-blocking)"
    WARN=$((WARN+1))
fi

# 7. Critical endpoints check
print_header "API endpoints"
endpoints=("/api/v1/markets" "/api/v1/markets/BTC/USDT/orderbook" "/api/v1/markets/BTC/USDT/ticker")
for ep in "${endpoints[@]}"; do
    code=$(curl -k -s -o /dev/null -w "%{http_code}" "https://pow.credit$ep")
    if [ "$code" = "200" ]; then
        echo -e "  ${GREEN}OK${NC} $ep"
    else
        echo -e "  ${RED}FAIL${NC} $ep (HTTP $code)"
    fi
done

# Generate report
{
    echo "QA Report - $TIMESTAMP"
    echo "=========================="
    echo "Mode: $([ "$AUTO_MODE" = true ] && echo "auto" || echo "manual")"
    echo ""
    for r in "${RESULTS[@]}"; do
        echo "  $r"
    done
    echo ""
    echo "Summary: $PASS passed, $FAIL failed, $WARN warnings"
} > "$REPORT_FILE"

echo ""
echo "============================================"
echo "QA Summary: $PASS passed, $FAIL failed, $WARN warnings"
echo "============================================"
echo "Report saved to: $REPORT_FILE"

# Send notification on failure
if [ $FAIL -gt 0 ] && [ "$NOTIFY_MODE" = true ]; then
    send_telegram_notification "QA FAILED: $FAIL failures, $PASS passes, $WARN warnings. Report: $REPORT_FILE"
fi

# Send periodic health report in auto mode
if [ "$AUTO_MODE" = true ] && [ $FAIL -eq 0 ]; then
    send_telegram_notification "QA OK: All $PASS checks passed"
fi

if [ $FAIL -gt 0 ]; then
    exit 1
fi
exit 0