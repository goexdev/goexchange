#!/bin/bash
# Deploy web build to /var/www/html
# Usage: ./deploy-web.sh

set -e

DIST_DIR=/root/goexchange/web/dist
WEB_DIR=/var/www/html
ASSETS_DIR=$WEB_DIR/assets

if [ ! -d "$DIST_DIR" ]; then
    echo "Error: $DIST_DIR not found. Run 'npm run build' first."
    exit 1
fi

# Clean assets dir
# Clean assets dir - remove all chunks (language files + main bundle)
rm -f $ASSETS_DIR/*.js $ASSETS_DIR/*.css

# Copy new files
cp $DIST_DIR/index.html $WEB_DIR/
cp $DIST_DIR/assets/* $ASSETS_DIR/

# Verify
echo "=== Deployed files ==="
ls -la $ASSETS_DIR/

# Test
echo
echo "=== HTTP status ==="
HTML_STATUS=$(curl -sk -o /dev/null -w "%{http_code}" https://pow.credit/)
echo "HTML: $HTML_STATUS"
for f in $ASSETS_DIR/*; do
    NAME=$(basename $f)
    STATUS=$(curl -sk -o /dev/null -w "%{http_code}" "https://pow.credit/assets/$NAME")
    echo "$NAME: $STATUS"
done

echo
echo "Deployment complete."
