#!/bin/bash
# Uninstall goexchange systemd units
# Usage: sudo ./uninstall-systemd.sh

set -e

UNIT_DIR="/etc/systemd/system"
SERVICES=("goexchange-api" "goexchange-matcher" "goexchange-scheduler")

echo "==> Uninstalling goexchange systemd units..."

# Stop services first
for svc in "${SERVICES[@]}"; do
    if systemctl is-active "$svc.service" 2>/dev/null; then
        systemctl stop "$svc.service"
        echo "  Stopped $svc"
    fi
done

# Disable
for svc in "${SERVICES[@]}"; do
    if systemctl is-enabled "$svc.service" 2>/dev/null; then
        systemctl disable "$svc.service"
        echo "  Disabled $svc"
    fi
done

# Remove unit files
for svc in "${SERVICES[@]}"; do
    rm -f "$UNIT_DIR/${svc}.service"
    echo "  Removed $UNIT_DIR/${svc}.service"
done

# Remove target
rm -f "$UNIT_DIR/goexchange.target"

# Reload
systemctl daemon-reload
systemctl reset-failed

echo
echo "==> Done."
