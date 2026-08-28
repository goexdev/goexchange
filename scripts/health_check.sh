#!/bin/bash
# Health check script for goexchange production deployment.
#
# Usage:
#   ./scripts/health_check.sh              # Check all components
#   ./scripts/health_check.sh --json       # JSON output
#
# Exit codes:
#   0 - All OK
#   1 - Critical failure
#   2 - Warning

set -e

JSON=false
for arg in "$@"; do
  case $arg in
    --json|-j) JSON=true ;;
  esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

ok_count=0
warn_count=0
fail_count=0

check() {
  local name="$1"
  local cmd="$2"
  local critical="${3:-true}"

  if $JSON; then
    if eval "$cmd" >/dev/null 2>&1; then
      echo "  \"$name\": \"ok\""
      ok_count=$((ok_count+1))
    else
      if [ "$critical" = "true" ]; then
        echo "  \"$name\": \"fail\""
        fail_count=$((fail_count+1))
      else
        echo "  \"$name\": \"warn\""
        warn_count=$((warn_count+1))
      fi
    fi
  else
    if eval "$cmd" >/dev/null 2>&1; then
      echo -e "  ${GREEN}OK${NC} $name"
      ok_count=$((ok_count+1))
    else
      if [ "$critical" = "true" ]; then
        echo -e "  ${RED}FAIL${NC} $name"
        fail_count=$((fail_count+1))
      else
        echo -e "  ${YELLOW}WARN${NC} $name"
        warn_count=$((warn_count+1))
      fi
    fi
  fi
}

if ! $JSON; then
  echo "=== goexchange Health Check ==="
  echo "Time: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo
  echo "--- Services ---"
fi

# Check systemd services
check "goexchange-api active" "systemctl is-active --quiet goexchange-api"
check "goexchange-matcher active" "systemctl is-active --quiet goexchange-matcher"
check "goexchange-scheduler active" "systemctl is-active --quiet goexchange-scheduler"

if ! $JSON; then
  echo
  echo "--- Network ---"
fi

# Check ports
check "API port 8099 listening" "ss -tln | grep -q ':8099 '"
check "Matcher port 8098 listening" "ss -tln | grep -q ':8098 '"
check "Scheduler port 8097 listening" "ss -tln | grep -q ':8097 '"
check "Postgres port 5433 listening" "ss -tln | grep -q ':5433 '"
check "Vault port 8200 listening" "ss -tln | grep -q ':8200 '"

# Check HTTP endpoints
check "API healthz 200" "curl -s -o /dev/null -w '%{http_code}' http://localhost:8099/healthz | grep -q 200"
check "API markets endpoint 200" "curl -s -o /dev/null -w '%{http_code}' 'http://localhost:8099/api/v1/markets?enabled_only=true' | grep -q 200"

# Check HTTPS
check "HTTPS goexchange.top 200" "curl -k -s -o /dev/null -w '%{http_code}' https://goexchange.top/healthz | grep -q 200"
check "HTTPS markets endpoint 200" "curl -k -s -o /dev/null -w '%{http_code}' 'https://goexchange.top/api/v1/markets?enabled_only=true' | grep -q 200"

if ! $JSON; then
  echo
  echo "--- Database ---"
fi

# Check postgres
check "PostgreSQL responding" "PGPASSWORD=exchange psql -h localhost -p 5433 -U exchange -d exchange -c 'SELECT 1' -tA | grep -q '^1$'"

if ! $JSON; then
  echo
  echo "--- Vault ---"
fi

# Check vault
check "Vault healthy" "curl -s http://127.0.0.1:8200/v1/sys/health -o /dev/null -w '%{http_code}' | grep -q '200\|429\|472'"
check "Vault unsealed" "curl -s http://127.0.0.1:8200/v1/sys/health | grep -q '\"sealed\":false'"

if ! $JSON; then
  echo
  echo "--- Monitoring ---"
fi

# Check Prometheus + Grafana
check "Prometheus port 9090 listening" "ss -tln | grep -q ':9090 '"
check "Grafana port 3002 listening" "ss -tln | grep -q ':3002 '"
check "Prometheus targets UP" "curl -s http://localhost:9090/api/v1/targets | grep -q '\"health\":\"up\"'"
check "Grafana health 200" "curl -s http://localhost:3002/api/health | grep -q database"

if ! $JSON; then
  echo
  echo "--- Disk ---"
fi

# Check disk space
disk_pct=$(df / | tail -1 | awk '{print $5}' | tr -d %)
if [ "$disk_pct" -lt 80 ]; then
  if $JSON; then
    echo "  \"disk_usage\": \"${disk_pct}%\""
  else
    echo -e "  ${GREEN}OK${NC} Disk usage: ${disk_pct}%"
  fi
  ok_count=$((ok_count+1))
elif [ "$disk_pct" -lt 90 ]; then
  if $JSON; then
    echo "  \"disk_usage\": \"${disk_pct}%\""
  else
    echo -e "  ${YELLOW}WARN${NC} Disk usage: ${disk_pct}%"
  fi
  warn_count=$((warn_count+1))
else
  if $JSON; then
    echo "  \"disk_usage\": \"${disk_pct}%\""
  else
    echo -e "  ${RED}FAIL${NC} Disk usage: ${disk_pct}%"
  fi
  fail_count=$((fail_count+1))
fi

if ! $JSON; then
  echo
  echo "--- Summary ---"
  echo -e "  ${GREEN}OK: $ok_count${NC}, ${YELLOW}WARN: $warn_count${NC}, ${RED}FAIL: $fail_count${NC}"
fi

if [ $fail_count -gt 0 ]; then
  exit 1
elif [ $warn_count -gt 0 ]; then
  exit 2
else
  exit 0
fi