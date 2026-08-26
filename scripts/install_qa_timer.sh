#!/bin/bash
# Install QA systemd timer
# Usage: ./scripts/install_qa_timer.sh

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Copy service and timer
cp "$SCRIPT_DIR/qa-qa.service" /etc/systemd/system/
cp "$SCRIPT_DIR/qa-qa.timer" /etc/systemd/system/

# Reload systemd
systemctl daemon-reload

# Enable and start timer
systemctl enable qa-qa.timer
systemctl start qa-qa.timer

echo "QA timer installed and started"
echo ""
echo "Status:"
systemctl status qa-qa.timer --no-pager
echo ""
echo "Next runs:"
systemctl list-timers qa-qa.timer --no-pager