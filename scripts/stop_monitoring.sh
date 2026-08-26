#!/bin/bash
# Stop Prometheus + Grafana monitoring stack.

set -e

for svc in goexchange-prometheus goexchange-grafana; do
  if docker ps --format "{{.Names}}" | grep -q "^$svc$"; then
    echo "Stopping $svc..."
    docker stop $svc
    docker rm $svc
  else
    echo "$svc not running"
  fi
done

echo "Done. Data volumes preserved (prometheus-data, grafana-data)"
