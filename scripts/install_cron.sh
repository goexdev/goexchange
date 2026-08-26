#!/bin/bash
# Install QA cron job - runs every 6 hours
# Usage: ./scripts/install_cron.sh

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOT_DIR="$( cd "$SCRIPT_DIR/.." && pwd )"

# Add cron job: run QA every 6 hours, send notification on failure
CRON_LINE="0 */6 * * * cd $ROOT_DIR && /root/goexchange/scripts/qa.sh --auto --notify >> /var/log/qa-cron.log 2>&1"

# Check if already installed
if crontab -l 2>/dev/null | grep -q "qa.sh --auto"; then
    echo "QA cron job already installed"
    exit 0
fi

# Add to crontab
(crontab -l 2>/dev/null; echo "$CRON_LINE") | crontab -
echo "QA cron job installed: every 6 hours"
crontab -l | grep qa