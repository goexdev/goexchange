#!/bin/bash
################################################################################
# GoExchange - Fresh Deploy Script
# Version: 2.1 (2026-08-25)
#
# 用法:
#   DOMAIN=goexchange.top bash deploy-fresh.sh
#
# WARNING: 这会清除所有数据库数据并重新部署
################################################################################

set -e
set -o pipefail

# ============================================================================
# Config
# ============================================================================
REPO_DIR="/root/goexchange"
GO_BIN="/usr/local/go/bin/go"
DOMAIN="${DOMAIN:-goexchange.top}"

# Database (Docker)
DB_CONTAINER="goexchange-postgres"
DB_NAME="exchange"
DB_USER="exchange"
DB_PASS="exchange"

# Vault
VAULT_TOKEN="dev-root-token"
VAULT_ADDR="http://127.0.0.1:8200"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() { echo -e "${BLUE}[$(date +%H:%M:%S)]${NC} $1"; }
ok() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
err() { echo -e "${RED}[ERR]${NC} $1"; exit 1; }

# ============================================================================
# Step 0: Confirm
# ============================================================================
warn "================================================="
warn "  WARNING: This will DELETE ALL DATA and redeploy"
warn "================================================="
warn "  Database: ${DB_NAME} (all tables)"
warn "  Vault: all secrets"
warn "  Binaries: api, matcher, scheduler"
warn "  Web: /var/www/html/"
warn ""
warn "  Domain: ${DOMAIN}"
warn "================================================="
echo ""
read -p "Type 'YES' to continue: " CONFIRM
[ "$CONFIRM" = "YES" ] || err "Aborted"

# ============================================================================
# Step 1: Stop goexchange services
# ============================================================================
log "Step 1: Stopping goexchange services..."
systemctl stop goexchange-api goexchange-matcher goexchange-scheduler 2>/dev/null || true
sleep 2
ok "Services stopped"

# ============================================================================
# Step 2: Reset PostgreSQL database
# ============================================================================
log "Step 2: Resetting PostgreSQL database..."

# Check Docker container exists
if ! docker ps --format '{{.Names}}' | grep -q "^${DB_CONTAINER}$"; then
    err "Docker container ${DB_CONTAINER} not found. Start it first."
fi

# Drop and recreate database
# Force close any active connections to the DB
docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='${DB_NAME}' AND pid<>pg_backend_pid();" >/dev/null 2>&1 || true

# Drop database (suppress expected errors)
set +e
docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -c "DROP DATABASE IF EXISTS ${DB_NAME};" 2>&1
docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -c "DROP USER IF EXISTS ${DB_USER};" 2>&1
set -e

# Recreate user (skip if already exists)
USER_EXISTS=$(docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='${DB_USER}'" 2>/dev/null)
if [ "$USER_EXISTS" != "1" ]; then
    docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -c "CREATE USER ${DB_USER} WITH PASSWORD '${DB_PASS}' SUPERUSER CREATEDB;" 2>&1
fi

# Create database (idempotent)
DB_EXISTS=$(docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='${DB_NAME}'" 2>/dev/null)
if [ "$DB_EXISTS" != "1" ]; then
    docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d postgres -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" 2>&1
fi

ok "Database reset"

# ============================================================================
# Step 3: Reset Vault
# ============================================================================
log "Step 3: Resetting Vault..."

# Kill existing vault
pkill -f "vault server" 2>/dev/null || true
sleep 3

# Start fresh vault in dev mode
nohup vault server -dev \
    -dev-root-token-id="${VAULT_TOKEN}" \
    -dev-listen-address="127.0.0.1:8200" \
    > /var/log/vault.log 2>&1 &

# Wait for vault to be ready
for i in {1..15}; do
    if curl -sf http://127.0.0.1:8200/v1/sys/health >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

export VAULT_ADDR="${VAULT_ADDR}"
export VAULT_TOKEN="${VAULT_TOKEN}"

# Seed secrets
curl -sf -X POST -H "X-Vault-Token: ${VAULT_TOKEN}" \
    -d "{\"data\":{\"connection_string\":\"postgres://${DB_USER}:${DB_PASS}@localhost:5432/${DB_NAME}?sslmode=disable\"}}" \
    ${VAULT_ADDR}/v1/secret/data/db/postgres >/dev/null

JWT_SECRET=$(openssl rand -hex 32)
curl -sf -X POST -H "X-Vault-Token: ${VAULT_TOKEN}" \
    -d "{\"data\":{\"secret\":\"${JWT_SECRET}\"}}" \
    ${VAULT_ADDR}/v1/secret/data/auth/jwt >/dev/null

ok "Vault reset and seeded"

# ============================================================================
# Step 4: Pull latest code
# ============================================================================
log "Step 4: Pulling latest code..."
cd "${REPO_DIR}"
git fetch origin main
git reset --hard origin/main
LATEST=$(git log --oneline -1)
ok "Latest: ${LATEST}"

# ============================================================================
# Step 5: Apply migrations
# ============================================================================
log "Step 5: Applying database migrations..."
cd "${REPO_DIR}"
shopt -s nullglob
for migration in migrations/*.up.sql; do
    log "  -> $(basename "$migration")"
    docker exec -i ${DB_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} < "$migration" 2>&1 | \
        grep -E "ERROR|NOTICE|CREATE|ALTER|INSERT" | head -3 || true
done
shopt -u nullglob

# Mark migrations as applied so binaries don't try to re-apply
docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -c "
    INSERT INTO schema_migrations (version, dirty) VALUES
        (1, false), (2, false), (3, false), (4, false), (5, false),
        (6, false), (7, false), (8, false), (9, false), (10, false),
        (11, false), (12, false), (13, false), (14, false), (15, false),
        (16, false), (17, false), (18, false), (19, false), (20, false),
        (21, false)
    ON CONFLICT (version) DO UPDATE SET dirty = false;
" 2>&1 | tail -2

ok "Migrations applied and marked as done"

# ============================================================================
# Step 6: Build backend
# ============================================================================
log "Step 6: Building backend..."
cd "${REPO_DIR}"
${GO_BIN} build -o bin/api ./cmd/api
${GO_BIN} build -o bin/matcher ./cmd/matcher
${GO_BIN} build -o bin/scheduler ./cmd/scheduler
cp config.yaml bin/
ok "Backend built"

# ============================================================================
# Step 7: Build and deploy frontend
# ============================================================================
log "Step 7: Building and deploying frontend..."
cd "${REPO_DIR}/web"

if [ ! -d "node_modules" ]; then
    log "  Installing npm dependencies..."
    npm install --silent
fi

VITE_DOMAIN="${DOMAIN}" npm run build 2>&1 | tail -3

# Deploy to nginx
rm -rf /var/www/html/assets
rm -f /var/www/html/index.html
cp -r dist/* /var/www/html/
ok "Frontend deployed"

# ============================================================================
# Step 8: Start services
# ============================================================================
log "Step 8: Starting goexchange services..."

# Stop any leftover node dev server (port 3000)
pkill -f "vite\|node.*dev" 2>/dev/null || true

# Restart goexchange services
systemctl start goexchange-matcher
sleep 3
systemctl start goexchange-api
sleep 3
systemctl start goexchange-scheduler
ok "Services started"

# ============================================================================
# Step 9: Verify
# ============================================================================
log "Step 9: Verifying deployment..."
sleep 5

# Check services
for svc in goexchange-api goexchange-matcher goexchange-scheduler; do
    if systemctl is-active --quiet "$svc"; then
        ok "$svc running"
    else
        err "$svc failed"
        journalctl -u "$svc" --no-pager | tail -10
    fi
done

# Check API
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8099/api/v1/markets 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "200" ]; then
    ok "API responding (200)"
else
    warn "API HTTP $HTTP_CODE"
fi

# Check web
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" http://localhost 2>/dev/null || echo "000")
[ "$HTTP_CODE" = "301" ] && HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" https://localhost 2>/dev/null || echo "000")
if [ "$HTTP_CODE" = "200" ]; then
    ok "Web responding (200)"
else
    warn "Web HTTP $HTTP_CODE"
fi

# ============================================================================
# Summary
# ============================================================================
ok "================================================="
ok "  DEPLOYMENT COMPLETE"
ok "================================================="
echo ""
echo "📊 Database state:"
docker exec ${DB_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -c "
    SELECT
        (SELECT COUNT(*) FROM users)        AS users,
        (SELECT COUNT(*) FROM trading_pairs) AS pairs,
        (SELECT COUNT(*) FROM orders)        AS orders,
        (SELECT COUNT(*) FROM balances)      AS balances
" 2>/dev/null

echo ""
echo "🌐 Access URLs:"
echo "   Frontend: https://${DOMAIN}"
echo "   API:      https://${DOMAIN}/api/v1/markets"
echo ""
echo "📝 Next: Register a new admin user:"
echo "   curl -X POST http://localhost:8099/api/v1/users/register \\"
echo "        -H 'Content-Type: application/json' \\"
echo "        -d '{\"email\":\"you@example.com\",\"password\":\"YourPass123!\",\"role\":\"admin\"}'"
echo ""
ok "Done!"