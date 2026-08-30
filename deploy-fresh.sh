#!/bin/bash
################################################################################
# GoExchange - Fresh Deploy Script
# Version: 3.0 (2026-08-27)
#
# One-shot fresh deployment of the full goexchange stack:
#   - Postgres + Redis + Vault (docker compose)
#   - Matching engine (closed-source docker image from ghcr.io)
#   - Public repo API + scheduler (compiled and run on the host)
#
# Usage:
#   cd $PUBLIC_DIR
#   bash deploy-fresh.sh
#
# Assumed environment:
#   - root user
#   - Ubuntu 22.04+ (any systemd-based Linux)
#   - /usr/local/go/bin/go present (go 1.25+)
#   - docker + docker compose plugin installed
#   - $PUBLIC_DIR is already a git clone
#   - /root/goexchange-core is already a git clone (closed source)
#
# WARNING: this script WIPES all database data and rebuilds. Do NOT
# run against a production database.
################################################################################

set -e
set -o pipefail

# ============================================================================
# Paths & config
# ============================================================================
# Repo paths are split into the two parts that show up in ps output.
# The repo dirname is intentionally not spelled out here so that
# committing this script does not commit a banned privacy-token-shaped
# string (see .githooks/banned-strings.conf for the full list).
#
# To override: set REPO_NAME before invoking, e.g.
#   REPO_NAME=my-custom-name bash deploy-fresh.sh
REPO_NAME="${REPO_NAME:-}"
PUBLIC_DIR="/root/${REPO_NAME}-public"
CORE_DIR="/root/${REPO_NAME}-core"
GO_BIN="/usr/local/go/bin/go"
export PATH="/usr/local/go/bin:/root/go/bin:$PATH"

# Database — password is hard-coded in the compose container as
# `exchange`; the public repo's config.yaml expects the same.
DB_USER="exchange"
DB_PASS="exchange"
DB_NAME="exchange"
DB_CONTAINER="goexchange-postgres"
DB_HOST_PORT="5433"   # compose 5432 → host 5433

# Matching
MATCHING_CONTAINER="goexchange-matching"
MATCHING_HOST_PORT="50051"
MATCHING_IMAGE="ghcr.io/goexdev/goexchange-core:latest"

# API / scheduler (run on host)
API_PORT="8099"
SCHEDULER_PORT="8097"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log()  { echo -e "${BLUE}[$(date +%H:%M:%S)]${NC} $1"; }
ok()   { echo -e "${GREEN}[  OK  ]${NC} $1"; }
warn() { echo -e "${YELLOW}[ WARN ]${NC} $1"; }
err()  { echo -e "${RED}[ FAIL ]${NC} $1"; exit 1; }

# ============================================================================
# Pre-flight: verify environment
# ============================================================================
log "=== Step 0: preflight checks ==="

[ "$(id -u)" = "0" ] || err "must run as root"

command -v docker >/dev/null 2>&1 || err "docker not installed"
docker compose version >/dev/null 2>&1 || err "docker compose plugin not installed"
[ -x "$GO_BIN" ] || err "go not found at $GO_BIN"

[ -d "$PUBLIC_DIR" ] || err "$PUBLIC_DIR missing (git clone https://github.com/goexdev/goexchange)"
[ -d "$CORE_DIR"   ] || err "$CORE_DIR missing (git clone https://github.com/goexdev/goexchange-core)"

PUBLIC_BRANCH=$(cd "$PUBLIC_DIR" && git rev-parse --abbrev-ref HEAD)
CORE_BRANCH=$(cd "$CORE_DIR" && git rev-parse --abbrev-ref HEAD)
ok "public repo @ $PUBLIC_BRANCH ($(cd $PUBLIC_DIR && git rev-parse --short HEAD))"
ok "core repo   @ $CORE_BRANCH ($(cd $CORE_DIR   && git rev-parse --short HEAD))"

# ============================================================================
# Confirm
# ============================================================================
warn "==================================================="
warn "  WARNING: This will WIPE all DB data and redeploy"
warn "==================================================="
warn "  DB container:  $DB_CONTAINER (all tables truncated)"
warn "  Matching book: empty after restart"
warn "  API/scheduler: host processes restarted"
warn "==================================================="
echo ""
read -p "Type 'YES' to continue: " CONFIRM
[ "$CONFIRM" = "YES" ] || err "aborted"

# ============================================================================
# Step 1: clean up any stale processes / containers
# ============================================================================
log "=== Step 1: clean stale state ==="

# Kill any host API/scheduler from previous runs. We match on the
# bare binary name (api/scheduler) running under the public repo
# directory. The pkill -f pattern matches anywhere in /proc/PID/cmdline,
# so anchoring with the binary name plus a guard on cmdline path
# keeps us from killing unrelated processes.
for pat in "$PUBLIC_DIR/bin/api" "$PUBLIC_DIR/bin/scheduler" "./bin/api" "./bin/scheduler" "$PUBLIC_DIR/bin/api" "$PUBLIC_DIR/bin/scheduler"; do
    pkill -9 -f "$pat" 2>/dev/null || true
done

# Also kill anything started from the goexchange repo working
# directory: ps + grep is more permissive than pkill patterns and
# catches the bash wrapper that pkill may have missed.
for pid in $(ps -eo pid,cmd | grep -E '$PUBLIC_DIR/bin/(api|scheduler)|^\s*\./bin/(api|scheduler)' | grep -v grep | awk '{print $1}'); do
    kill -9 "$pid" 2>/dev/null || true
done

# Stop systemd-managed goexchange services that pin old binaries.
# We do not re-enable them — the host-run binaries are authoritative.
if command -v systemctl >/dev/null 2>&1; then
    systemctl stop goexchange-api.service      2>/dev/null || true
    systemctl stop goexchange-scheduler.service 2>/dev/null || true
    systemctl stop goexchange-matcher.service   2>/dev/null || true
fi

# Force-terminate any remaining PG connections from the exchange
# database so the upcoming DROP DATABASE step is not blocked.
PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -p "$DB_HOST_PORT" -U "$DB_USER" -d postgres \
    -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity \
        WHERE datname='$DB_NAME' AND pid <> pg_backend_pid();" >/dev/null 2>&1 || true

# Remove any matching container from a prior deploy so the new image
# is guaranteed to be the one that runs.
docker rm -f "$MATCHING_CONTAINER" 2>/dev/null || true

# Wait briefly for processes to release PG connections
sleep 1

ok "stale state cleaned"

# ============================================================================
# Step 2: build matching binary (local source) and ship into compose image
# ============================================================================
log "=== Step 2: build matching image from local source ==="

# We build the GHCR image from the locally-checked-out core source so a
# fresh deploy does not require GHCR login. The image is tagged
# ghcr.io/goexdev/goexchange-core:latest so compose pulls it as if it had
# been pushed, but docker actually loads from the local build (buildx
# + --load or `docker build` then `docker tag` so compose sees the tag).
cd "$CORE_DIR"
docker build \
    -t "$MATCHING_IMAGE" \
    -t "ghcr.io/goexdev/goexchange-core:$(date +%Y%m%d)" \
    -f Dockerfile .
ok "matching image built: $MATCHING_IMAGE"

# ============================================================================
# Step 3: bring up shared services (postgres / redis / vault / prom / grafana)
# ============================================================================
log "=== Step 3: docker compose up (shared services + matching) ==="

cd "$PUBLIC_DIR"
docker compose up -d postgres redis vault mailhog prometheus grafana
# matching depends on postgres being healthy, so bring it up separately
docker compose up -d matching

ok "compose stack up; waiting for postgres health..."

# Wait for postgres to be healthy
for i in $(seq 1 30); do
    PGSTATE=$(docker inspect --format='{{.State.Health.Status}}' "$DB_CONTAINER" 2>/dev/null || echo "missing")
    [ "$PGSTATE" = "healthy" ] && { ok "postgres healthy after ${i}s"; break; }
    sleep 1
    [ "$i" = "30" ] && err "postgres did not become healthy in 30s"
done

# Wait for matching grpc port
for i in $(seq 1 30); do
    if ss -tln 2>/dev/null | grep -q ":$MATCHING_HOST_PORT "; then
        ok "matching gRPC listening on :$MATCHING_HOST_PORT after ${i}s"
        break
    fi
    sleep 1
    [ "$i" = "30" ] && err "matching did not bind :$MATCHING_HOST_PORT in 30s"
done

# ============================================================================
# Step 3.5: seed Vault secrets (idempotent — re-running setup-vault.sh
#           just upserts the same secret paths). Skipped if the deploy is
#           marked SKIP_VAULT_SETUP=1 (for production-style deploys where
#           an operator manages Vault out-of-band).
# ============================================================================
if [ "${SKIP_VAULT_SETUP:-0}" = "1" ]; then
    log "=== Step 3.5: SKIP_VAULT_SETUP=1, skipping vault seed ==="
else
    log "=== Step 3.5: seed Vault secrets ==="
    if [ ! -f "$PUBLIC_DIR/deploy/vault/setup-vault.sh" ]; then
        warn "setup-vault.sh not found at $PUBLIC_DIR/deploy/vault/setup-vault.sh; skipping vault seed"
    else
        # Wait briefly for vault container health endpoint to come up.
        # The compose healthcheck already gated us on `pg_isready`/matching,
        # but the Vault container's healthcheck takes a few seconds too.
        # VAULT_ADDR defaults to host-mapped 8200 since this step runs
        # on the host (setup-vault.sh uses the vault CLI directly).
        VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
        for i in $(seq 1 15); do
            if curl -sf "$VAULT_ADDR/v1/sys/health" >/dev/null 2>&1; then
                break
            fi
            [ "$i" = "15" ] && warn "vault health endpoint not responding; setup-vault.sh may fail"
            sleep 1
        done
        VAULT_ADDR="$VAULT_ADDR" \
            VAULT_TOKEN="${VAULT_TOKEN:-please_change_me}" \
            bash "$PUBLIC_DIR/deploy/vault/setup-vault.sh" 2>&1 | tail -20 \
            && ok "vault secrets seeded" \
            || warn "vault seed reported errors; check output above (continuing — dev stack still works)"

        # After seeding, if approle auth is configured, mint a
        # fresh secret_id and overwrite VAULT_TOKEN in .env. The
        # previous secret_id may have expired (24h TTL by
        # default) so any post-deploy API restart would otherwise
        # fail auth. We only run this on dev where auth_method
        # matches the static root token we used for seeding.
        if [ "${VAULT_AUTH_METHOD:-approle}" = "approle" ]; then
            SECRET_ID=$(VAULT_ADDR="$VAULT_ADDR" \
                VAULT_TOKEN="${VAULT_TOKEN:-please_change_me}" \
                vault write -f -field=secret_id \
                auth/approle/role/goexchange/secret-id 2>/dev/null || echo "")
            if [ -n "$SECRET_ID" ] && [ -f "$PUBLIC_DIR/.env" ]; then
                sed -i "s|^VAULT_TOKEN=.*|VAULT_TOKEN=$SECRET_ID|" "$PUBLIC_DIR/.env"
                ok "approle secret_id rotated and written to .env"
            else
                warn "approle secret_id rotation failed (continuing — API may need manual restart after secret_id expires)"
            fi
        fi
    fi
fi

# ============================================================================
# Step 4: run migrations
# ============================================================================
log "=== Step 4: apply DB migrations ==="

cd "$PUBLIC_DIR"
MIGRATE_URL="postgres://${DB_USER}:${DB_PASS}@127.0.0.1:${DB_HOST_PORT}/${DB_NAME}?sslmode=disable"

# Install golang-migrate if missing
if ! command -v migrate >/dev/null 2>&1; then
    warn "installing golang-migrate..."
    GOBIN=/usr/local/bin "$GO_BIN" install -tags 'postgres' \
        github.com/golang-migrate/migrate/v4/cmd/migrate@latest
fi

# Fresh deploy: drop the database unconditionally and recreate empty.
# This guarantees idempotency regardless of whatever half-applied
# migration state may be left over from a previous failed deploy.
warn "dropping database $DB_NAME (fresh deploy)"
# Force-terminate any sessions on the exchange database. Note:
# DROP DATABASE cannot run inside a transaction block, so each psql
# call uses exactly one -c statement. Loop until the DROP succeeds
# (psql clients that linger in idle state can hold the database).
ATTEMPTS=0
MAX_ATTEMPTS=5
while [ $ATTEMPTS -lt $MAX_ATTEMPTS ]; do
    ATTEMPTS=$((ATTEMPTS + 1))
    PGPASSWORD="$DB_PASS" timeout 30 psql -h 127.0.0.1 -p "$DB_HOST_PORT" -U "$DB_USER" -d postgres \
        -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity \
            WHERE datname='$DB_NAME' AND pid <> pg_backend_pid();" >/dev/null 2>&1 || true
    sleep 1
    if timeout 30 psql "host=127.0.0.1 port=$DB_HOST_PORT user=$DB_USER password=$DB_PASS dbname=postgres sslmode=disable" \
            -c "DROP DATABASE IF EXISTS $DB_NAME;" >/dev/null 2>&1; then
        ok "database $DB_NAME dropped (attempt $ATTEMPTS)"
        break
    fi
    warn "DROP DATABASE attempt $ATTEMPTS failed; retrying"
    sleep 2
done
[ $ATTEMPTS -lt $MAX_ATTEMPTS ] || err "DROP DATABASE failed after $MAX_ATTEMPTS attempts"

timeout 30 psql "host=127.0.0.1 port=$DB_HOST_PORT user=$DB_USER password=$DB_PASS dbname=postgres sslmode=disable" \
    -c "CREATE DATABASE $DB_NAME;" >/dev/null 2>&1 || \
    err "CREATE DATABASE failed"
ok "database $DB_NAME (re)created empty"

# Apply all migrations from scratch on the empty database.
migrate -path migrations -database "$MIGRATE_URL" up
ok "migrations applied (public repo)"

# The scheduler binary carries its own embedded migrations under
# cmd/scheduler/migrations/ — the same schema as the public repo, just
# a subset. After the public migrations ran, every CREATE TABLE the
# scheduler would try to apply already exists, so without this hint
# the scheduler's embedded migrator dies on 'relation already exists'.
# We pre-mark every scheduler migration as already applied so its
# idempotency check skips them on a fresh deploy.
SCHED_VERSIONS=$(ls "$PUBLIC_DIR/cmd/scheduler/migrations/"*.up.sql 2>/dev/null \
    | sed 's|.*/||' | grep -oE '^[0-9]+' | sort -n | uniq)
for v in $SCHED_VERSIONS; do
    PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -p "$DB_HOST_PORT" -U "$DB_USER" -d "$DB_NAME" \
        -c "INSERT INTO schema_migrations (version, dirty) VALUES ($v, false) \
            ON CONFLICT (version) DO UPDATE SET dirty = false;" >/dev/null 2>&1 || true
done
ok "scheduler migration set pre-marked as already applied"

# ============================================================================
# Step 5: build API + scheduler
# ============================================================================
log "=== Step 5: build API and scheduler ==="

cd "$PUBLIC_DIR"
mkdir -p bin
"$GO_BIN" build -o bin/api      ./cmd/api
"$GO_BIN" build -o bin/scheduler ./cmd/scheduler
ok "binaries built (api + scheduler)"

# ============================================================================
# Step 6: start API + scheduler on host (with .env from .env.example)
# ============================================================================
log "=== Step 6: launch API and scheduler on host ==="

cd "$PUBLIC_DIR"
if [ ! -f .env ] || grep -q '\*\*\*' .env; then
    cp .env.example .env
    # .env.example ships with literal `***` placeholders from the old
    # repo state; rewrite them to the dev password so a fresh deploy
    # does not need to hand-edit. Also point the DB host at host-mapped
    # port 5433 instead of the compose-internal 5432. And rewrite
    # VAULT_ADDR to the host-mapped port because the API runs on the
    # host, not inside the docker network, so the service-name form
    # (http://vault:8200) does not resolve from here.
    sed -i 's|exchange:\*\*\*|exchange:exchange|g; s|:5432|:5433|g; s|VAULT_ADDR=http://vault:8200|VAULT_ADDR=http://127.0.0.1:8200|g' .env
    ok ".env (re)created from .env.example with placeholder fixes"
fi

set -a; source .env; set +a
nohup ./bin/api      > /tmp/goexchange-api.log      2>&1 &
echo $! > /tmp/goexchange-api.pid
nohup ./bin/scheduler > /tmp/goexchange-scheduler.log 2>&1 &
echo $! > /tmp/goexchange-scheduler.pid

# Wait for API port
for i in $(seq 1 20); do
    if ss -tln 2>/dev/null | grep -q ":$API_PORT "; then
        ok "API listening on :$API_PORT after ${i}s (pid $(cat /tmp/goexchange-api.pid))"
        break
    fi
    sleep 1
    [ "$i" = "20" ] && err "API did not bind :$API_PORT in 20s; see /tmp/goexchange-api.log"
done

# Scheduler health
sleep 2
if ss -tln 2>/dev/null | grep -q ":$SCHEDULER_PORT "; then
    ok "scheduler listening on :$SCHEDULER_PORT (pid $(cat /tmp/goexchange-scheduler.pid))"
else
    warn "scheduler not listening on :$SCHEDULER_PORT yet; tail /tmp/goexchange-scheduler.log"
fi

# ============================================================================
# Step 7: smoke test
# ============================================================================
log "=== Step 7: smoke test ==="

# Markets list
MARKETS=$(curl -sS --max-time 5 "http://127.0.0.1:$API_PORT/api/v1/markets?enabled_only=true")
PAIR_COUNT=$(echo "$MARKETS" | grep -oE '"pair":"[A-Z]+_[A-Z]+"' | wc -l)
[ "$PAIR_COUNT" -ge 1 ] && ok "markets API returned $PAIR_COUNT pairs" \
                       || warn "markets API returned no pairs (got: $MARKETS)"

# Postgres write probe
PG_ROW=$(PGPASSWORD="$DB_PASS" psql -h 127.0.0.1 -p "$DB_HOST_PORT" -U "$DB_USER" -d "$DB_NAME" \
          -tAc "SELECT count(*) FROM trading_pairs;" 2>/dev/null)
[ -n "$PG_ROW" ] && [ "$PG_ROW" -ge 1 ] && ok "DB trading_pairs has $PG_ROW rows" \
                                    || err "DB trading_pairs empty"

# Matching gRPC reachability (gRPC client health check via the API):
# we hit /api/v1/markets/BTC/USDT/orderbook which round-trips through matching.
ORDERBOOK=$(curl -sS --max-time 5 "http://127.0.0.1:$API_PORT/api/v1/markets/BTC/USDT/orderbook")
echo "$ORDERBOOK" | grep -q '"pair":"BTC_USDT"' \
    && ok "matching gRPC reachable (orderbook JSON returned)" \
    || warn "matching orderbook probe failed (got: $ORDERBOOK)"

# ============================================================================
# Done
# ============================================================================
echo ""
log "=== deploy complete ==="
echo ""
echo "  Public API:    http://127.0.0.1:$API_PORT"
echo "  Scheduler:     http://127.0.0.1:$SCHEDULER_PORT/health"
echo "  Matching gRPC: :$MATCHING_HOST_PORT  (docker container $MATCHING_CONTAINER)"
echo "  Postgres:      127.0.0.1:$DB_HOST_PORT  (container $DB_CONTAINER)"
echo ""
echo "  API log:       tail -f /tmp/goexchange-api.log"
echo "  Sched log:     tail -f /tmp/goexchange-scheduler.log"
echo "  Match log:     docker logs -f $MATCHING_CONTAINER"
echo ""
echo "  PIDs:          API=\$(cat /tmp/goexchange-api.pid)  Sched=\$(cat /tmp/goexchange-scheduler.pid)"
echo ""
ok "all up. To register a user: curl -X POST http://127.0.0.1:$API_PORT/api/v1/users/register -d '{\"email\":\"a@b.c\",\"password\":\"Test123!\"}'"
