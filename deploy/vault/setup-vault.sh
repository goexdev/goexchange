#!/bin/bash
# setup-vault.sh - Initialize Vault with goexchange secrets
# Usage: VAULT_TOKEN=*** ./setup-vault.sh
#
# This script:
# 1. Enables KV v2 at "secret/"
# 2. Stores all goexchange secrets (DB, JWT, notifier, hot wallet keys)
# 3. Sets up policies for goexchange read-only access
# 4. Sets up AppRole auth (recommended for production)

set -e

VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}"
# Default token matches compose VAULT_DEV_ROOT_TOKEN_ID. In
# production, pass VAULT_TOKEN explicitly (or run after `vault login`).
VAULT_TOKEN="${VAULT_TOKEN:-please_change_me}"
export VAULT_ADDR VAULT_TOKEN

echo "==> Setting up goexchange secrets in Vault..."
echo "    Vault: $VAULT_ADDR"

# Check vault status
if ! vault status >/dev/null 2>&1; then
    echo "ERROR: Vault not reachable at $VAULT_ADDR"
    exit 1
fi

# Enable KV v2 if not already
vault secrets list 2>/dev/null | grep -q "^secret/" || {
    echo "==> Enabling KV v2 secrets engine at 'secret/'"
    vault secrets enable -path=secret kv-v2
}

# DB credentials (CHANGE THESE IN PRODUCTION!)
echo "==> Storing DB credentials..."
vault kv put secret/db/postgres \
    host="${DB_HOST:-127.0.0.1}" \
    port="${DB_PORT:-5433}" \
    user="${DB_USER:-exchange}" \
    password="${DB_PASSWORD:-exchange}" \
    database="${DB_DATABASE:-exchange}"

# JWT secret (CHANGE THIS IN PRODUCTION!)
echo "==> Storing JWT secret..."
JWT_SECRET_GENERATED="${JWT_SECRET:-$(openssl rand -hex 32)}"
vault kv put secret/auth/jwt \
    secret="$JWT_SECRET_GENERATED" \
    ttl_seconds="${JWT_TTL:-3600}"

# Notifier SMTP
echo "==> Storing SMTP credentials..."
vault kv put secret/notifier/smtp \
    host="${SMTP_HOST:-127.0.0.1}" \
    port="${SMTP_PORT:-1025}" \
    user="${SMTP_USER:-}" \
    password="${SMTP_PASSWORD:-}" \
    from="${SMTP_FROM:-noreply@goexchange.local}"

# Notifier Resend
echo "==> Storing Resend API key..."
vault kv put secret/notifier/resend \
    api_key="${RESEND_API_KEY:-re_placeholder}" \
    from="${RESEND_FROM:-noreply@goexchange.local}"

# Hot wallet keys (per chain)
echo "==> Storing ETH hot wallet..."
vault kv put secret/eth/hot-wallet \
    address="${ETH_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${ETH_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="1"

echo "==> Storing Polygon hot wallet..."
vault kv put secret/polygon/hot-wallet \
    address="${POLYGON_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${POLYGON_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="137"

echo "==> Storing Arbitrum hot wallet..."
vault kv put secret/arbitrum/hot-wallet \
    address="${ARBITRUM_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${ARBITRUM_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="42161"

echo "==> Storing Optimism hot wallet..."
vault kv put secret/optimism/hot-wallet \
    address="${OPTIMISM_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${OPTIMISM_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="10"

echo "==> Storing Base hot wallet..."
vault kv put secret/base/hot-wallet \
    address="${BASE_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${BASE_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="8453"

echo "==> Storing BSC hot wallet..."
vault kv put secret/bsc/hot-wallet \
    address="${BSC_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${BSC_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="97"

echo "==> Storing BTC hot wallet..."
vault kv put secret/btc/hot-wallet \
    address="${BTC_HOT_WALLET:-bc1qplaceholder}" \
    min_conf="6"

echo "==> Storing ETH hot wallet..."
vault kv put secret/eth/hot-wallet \
    address="${ETH_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${ETH_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="1"

echo "==> Storing Polygon hot wallet..."
vault kv put secret/polygon/hot-wallet \
    address="${POLYGON_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${POLYGON_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="137"

echo "==> Storing Arbitrum hot wallet..."
vault kv put secret/arbitrum/hot-wallet \
    address="${ARBITRUM_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${ARBITRUM_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="42161"

echo "==> Storing Optimism hot wallet..."
vault kv put secret/optimism/hot-wallet \
    address="${OPTIMISM_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${OPTIMISM_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="10"

echo "==> Storing Base hot wallet..."
vault kv put secret/base/hot-wallet \
    address="${BASE_HOT_WALLET:-0x0000000000000000000000000000000000000000}" \
    private_key="${BASE_PRIVATE_KEY:-0x0000000000000000000000000000000000000000000000000000000000000001}" \
    chain_id="8453"

# Policy: goexchange read-only
echo "==> Setting up goexchange policy..."
cat > /tmp/goexchange-policy.hcl <<'EOF'
# Read access to specific secret paths
# DB
path "secret/data/db/postgres" { capabilities = ["read"] }
path "secret/data/auth/jwt"    { capabilities = ["read"] }
path "secret/data/notifier/*"  { capabilities = ["read"] }

# HD wallet mnemonic (shared BIP-39 seed for all EVM chains)
path "secret/data/hd/mnemonic" { capabilities = ["read"] }

# Hot wallets for all chains
path "secret/data/eth/hot-wallet"      { capabilities = ["read"] }
path "secret/data/bsc/hot-wallet"      { capabilities = ["read"] }
path "secret/data/btc/hot-wallet"      { capabilities = ["read"] }
path "secret/data/polygon/hot-wallet"  { capabilities = ["read"] }
path "secret/data/arbitrum/hot-wallet" { capabilities = ["read"] }
path "secret/data/optimism/hot-wallet" { capabilities = ["read"] }
path "secret/data/base/hot-wallet"     { capabilities = ["read"] }

# Wildcard for future chains
path "secret/data/*/hot-wallet"        { capabilities = ["read"] }
EOF
vault policy write goexchange /tmp/goexchange-policy.hcl

# HD wallet mnemonic (for BIP-44 multi-chain wallets)
echo "==> Setting up HD wallet mnemonic..."
vault kv put secret/hd/mnemonic \
    mnemonic="${HD_MNEMONIC:-abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about}"

# AppRole auth (recommended for production)
if [ "${SKIP_APPROLE:-false}" != "true" ]; then
    echo "==> Setting up AppRole auth..."
    # Enable AppRole (idempotent)
    if ! vault auth list 2>/dev/null | grep -q "^approle/"; then
        vault auth enable approle
    fi

    # Create role with read-only policy
    vault write auth/approle/role/goexchange \
        token_policies="goexchange" \
        token_ttl=1h \
        token_max_ttl=24h \
        secret_id_ttl=24h \
        secret_id_num_uses=100

    # Get role-id (stable)
    ROLE_ID=$(vault read -field=role_id auth/approle/role/goexchange/role-id)
    echo "    ROLE_ID=$ROLE_ID"

    # Generate a new secret-id (rotated frequently)
    SECRET_ID=$(vault write -f -field=secret_id auth/approle/role/goexchange/secret-id)
    echo "    SECRET_ID=$SECRET_ID"

    echo ""
    echo "    Save these for goexchange config.yaml:"
    echo "      vault.auth_method: approle"
    echo "      vault.app_role_id: $ROLE_ID"
    echo "      vault.app_secret_id: $SECRET_ID"
fi

echo
echo "==> Done! Secrets stored:"
vault kv list secret/

echo
echo "==> Next steps:"
echo "    1. Update config.yaml: vault.enabled: true"
echo "    2. Set vault.auth_method + role_id + secret_id"
echo "    3. Verify: curl https://api.example.com/admin/vault-health"
echo
echo "    Generated JWT secret: $JWT_SECRET_GENERATED"
echo "    Save this somewhere safe! (will be lost on next Vault restart in dev mode)"