# HashiCorp Vault Integration

goexchange uses HashiCorp Vault to store all sensitive secrets.

## What is stored in Vault

| Secret path | Contents |
|---|---|
| `secret/db/postgres` | DB host/port/user/password/database |
| `secret/auth/jwt` | JWT signing secret + TTL |
| `secret/notifier/smtp` | SMTP host/port/user/password/from |
| `secret/notifier/resend` | Resend API key + from address |
| `secret/eth/hot-wallet` | ETH hot wallet address + private_key + chain_id |
| `secret/bsc/hot-wallet` | BSC hot wallet address + private_key + chain_id |
| `secret/btc/hot-wallet` | BTC hot wallet address + min_conf |

## Setup (Development)

```bash
# 1. Start Vault dev server
vault server -dev -dev-root-token-id=dev-root-token

# 2. Set env var
export VAULT_ADDR=http://127.0.0.1:8200
export VAULT_TOKEN=dev-root-token

# 3. Run setup script
./deploy/vault/setup-vault.sh

# 4. Update config.yaml
vault:
  enabled: true
  address: "http://127.0.0.1:8200"
  token: "dev-root-token"
```

## Setup (Production)

```bash
# 1. Deploy Vault HA cluster
vault operator init  # save root token + unseal keys
vault operator raft autopilot set-config

# 2. Enable audit
vault audit enable file file_path=/var/log/vault-audit.log

# 3. Enable KV v2
vault secrets enable -path=secret kv-v2

# 4. Create policy (deploy/vault/setup-vault.sh does this)
vault policy write goexchange /etc/vault/policies/goexchange.hcl

# 5. Use AppRole auth (NOT static tokens in production!)
vault auth enable approle
vault write auth/approle/role/goexchange \
    token_policies="goexchange" \
    token_ttl=1h \
    token_max_ttl=24h

# 6. AppRole secret-id for goexchange pod
vault write -f auth/approle/role/goexchange/role-secret-id

# 7. Store secrets (one-time setup)
./setup-vault.sh  # with real credentials from env

# 8. Configure goexchange (use env vars, not static config)
# - VAULT_ADDR=https://vault.example.com:8200
# - VAULT_ROLE_ID=<role-id>
# - VAULT_SECRET_ID=<secret-id>
```

## How it works

1. **Startup**: goexchange connects to Vault, fetches all secrets
2. **In-memory cache**: 5 min TTL avoids hammering Vault
3. **Override**: config.yaml secrets are OVERRIDDEN by Vault values
4. **Health check**: `/api/v1/admin/vault-health` reports status
5. **Manual reload**: `vaultClient.InvalidateCache(path)` for hot rotation

## Health endpoint

```bash
$ curl -H "Authorization: Bearer ***" \
    https://api.example.com/api/v1/admin/vault-health

{"enabled":true,"status":"healthy"}
```

## Startup log

```
{"msg":"vault connected","address":"http://127.0.0.1:8200","cache_ttl":300000000000}
{"msg":"db password loaded from vault","path":"db/postgres"}
{"msg":"jwt secret loaded from vault","path":"auth/jwt"}
{"msg":"smtp password loaded from vault"}
{"msg":"resend api key loaded from vault"}
```

## Best practices

- **Rotate regularly**: Update secrets in Vault, then call `InvalidateCache`
- **Monitor**: Alert on `vault-health` returning non-healthy
- **Audit**: Enable Vault's audit backend to log all reads
- **Restrict**: Use Vault policies to limit which paths each service can access
- **AppRole**: NEVER use static tokens in production, use AppRole or K8s auth
- **Auto-unseal**: Use AWS KMS / GCP CKMS / Azure Key Vault for auto-unseal
- **HA**: 3+ Vault servers with Raft backend for production

## What if Vault is down?

goexchange will fail to start with: "vault health check failed: ..."

This is intentional - we require Vault for all sensitive secrets.
For HA Vault, the client will retry on next request via the cache TTL.

## Rotation example (rotating DB password)

1. Change password in Postgres
2. Update Vault: `vault kv put secret/db/postgres password=newpassword`
3. Wait 5 min (cache TTL) OR restart goexchange
4. Old cached password expires, new one is fetched
5. goexchange reconnects with new password

For instant rotation (programmatic):
```go
vaultClient.InvalidateCache("db/postgres")
```

For instant rotation (manual):
```bash
systemctl restart goexchange-api
```

## Files

- `setup-vault.sh` - One-time setup script
- `README.md` - This file
- goexchange uses: `internal/vault/client.go`

## Why Vault (not just config.yaml)?

| Concern | Solution |
|---|---|
| Secrets in code | ✅ in Vault |
| Secrets in env vars | ✅ ephemeral, fetched from Vault at boot |
| Rotation | ✅ update Vault, no app change |
| Audit trail | ✅ Vault audit logs all reads |
| Compliance | ✅ SOC2 / PCI-DSS / GDPR |
| Team access | ✅ Vault policies per service |
| Encryption at rest | ✅ Vault handles it |

## Production checklist

- [ ] Deploy Vault HA cluster (3+ servers, Raft backend)
- [ ] Auto-unseal via cloud KMS
- [ ] Enable audit backend (file/syslog/grpc)
- [ ] Use AppRole auth (NOT static tokens)
- [ ] Network policies: only goexchange pods access Vault
- [ ] Vault policies: limit paths per service
- [ ] Monitor: Vault health + goexchange vault-health endpoint
- [ ] Rotate tokens every 30 days
- [ ] Rotate secrets every 90 days (DB password, JWT, API keys)
- [ ] Hot wallet keys: rotate every 6 months or after any compromise
