#!/bin/bash
# Install goexchange systemd units
# Usage: sudo ./install-systemd.sh [--start]

set -e

UNIT_DIR="/etc/systemd/system"
SOURCE_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVICES=("goexchange-api" "goexchange-matcher" "goexchange-scheduler")

echo "==> Installing goexchange systemd units..."

# Copy unit files
for svc in "${SERVICES[@]}"; do
    if [ -f "$SOURCE_DIR/${svc}.service" ]; then
        cp "$SOURCE_DIR/${svc}.service" "$UNIT_DIR/${svc}.service"
        echo "  Installed $UNIT_DIR/${svc}.service"
    else
        echo "  WARNING: $SOURCE_DIR/${svc}.service not found"
    fi
done

# Copy target
if [ -f "$SOURCE_DIR/goexchange.target" ]; then
    cp "$SOURCE_DIR/goexchange.target" "$UNIT_DIR/goexchange.target"
    echo "  Installed $UNIT_DIR/goexchange.target"
fi

# Reload systemd
systemctl daemon-reload
echo "==> Reloaded systemd daemon"

# Enable target (starts all 3 services on boot)
systemctl enable goexchange.target 2>&1 | head -3
echo "  Enabled goexchange.target"

# Enable each service individually too (for individual start/stop)
for svc in "${SERVICES[@]}"; do
    systemctl enable "$svc.service" 2>&1 | head -1
done

# Optionally start now
if [ "$1" == "--start" ]; then
    echo
    echo "==> Starting services..."
    # Start each individually (target doesn't auto-start sub-services)
    for svc in "${SERVICES[@]}"; do
        systemctl start "$svc.service"
        echo "  Started $svc"
    done
    sleep 5
    echo
    echo "==> Service status:"
    for svc in "${SERVICES[@]}"; do
        echo "--- $svc ---"
        systemctl is-active "$svc.service"
    done
fi

echo
echo "==> Done. Useful commands:"
echo "  Start all:    systemctl start goexchange.target    (or each: goexchange-api/matcher/scheduler)"
echo "  Stop all:     systemctl stop goexchange-api goexchange-matcher goexchange-scheduler"
echo "  Status one:   systemctl status goexchange-api"
echo "  Logs:         journalctl -u goexchange-api -f"
echo "  Restart one:  systemctl restart goexchange-api"
echo "  Logs all:     journalctl -u 'goexchange-*' -f"
echo "  Auto-restart: enabled via Restart=always (crash → restart in 3-5s)"
