#!/bin/bash
# Start Prometheus + Grafana monitoring stack (Docker).

set -e

if ! command -v docker &> /dev/null; then
  echo "ERROR: docker not found" >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Start Prometheus (host network so it can reach goexchange-api)
if ! docker ps --format "{{.Names}}" | grep -q "^goexchange-prometheus$"; then
  echo "Starting Prometheus..."
  docker run -d \
    --name goexchange-prometheus \
    --restart=unless-stopped \
    --network=host \
    -v "$REPO_ROOT/deploy/monitoring/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro" \
    -v "$REPO_ROOT/deploy/monitoring/prometheus/rules:/etc/prometheus/rules:ro" \
    -v prometheus-data:/prometheus \
    prom/prometheus:latest \
    --config.file=/etc/prometheus/prometheus.yml \
    --storage.tsdb.path=/prometheus \
    --web.listen-address=:9090
  echo "  Started (http://localhost:9090)"
else
  echo "Prometheus already running"
fi

# Start Grafana (host network so it can reach Prometheus)
if ! docker ps --format "{{.Names}}" | grep -q "^goexchange-grafana$"; then
  echo "Starting Grafana..."
  docker run -d \
    --name goexchange-grafana \
    --restart=unless-stopped \
    --network=host \
    -v grafana-data:/var/lib/grafana \
    -v "$REPO_ROOT/deploy/monitoring/grafana/grafana.ini:/etc/grafana/grafana.ini:ro" \
    -e GF_SECURITY_ADMIN_PASSWORD=admin123 \
    -e GF_USERS_ALLOW_SIGN_UP=false \
    -e GF_SERVER_ROOT_URL=https://pow.credit/grafana/ \
    grafana/grafana:latest
  echo "  Started (http://localhost:3002 - host network)"
  echo "  Remote access: https://pow.credit/grafana/"
  echo "  Default login: admin / admin123"
else
  echo "Grafana already running"
fi

# Wait for Grafana to be ready and import dashboard
echo "Waiting for Grafana..."
for i in {1..30}; do
  if curl -k -s -u admin:admin123 http://localhost:3002/api/health | grep -q "ok"; then
    echo "  Grafana is ready"
    break
  fi
  sleep 2
done

# Import dashboard
DASHBOARD_FILE="$REPO_ROOT/deploy/grafana/goexchange-dashboard.json"
if [ -f "$DASHBOARD_FILE" ]; then
  python3 -c "
import json, requests, urllib3
urllib3.disable_warnings()
with open('$DASHBOARD_FILE') as f:
    dashboard = json.load(f)
r = requests.post(
    'http://localhost:3002/api/dashboards/import',
    auth=('admin', 'admin123'),
    json={'dashboard': dashboard, 'overwrite': True},
    verify=False
)
print(f'Dashboard import: {r.status_code}')" 2>/dev/null || echo "Dashboard import: python3 not available, skip"
fi

sleep 2
echo
echo "=== Status ==="
docker ps --format "table {{.Names}}	{{.Status}}	{{.Ports}}" | grep -E "goexchange-(prometheus|grafana)"
