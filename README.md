# GoExchange

Open-source digital asset exchange platform: spot trading, multi-asset
wallet, KYC/risk controls, chain monitoring, and admin dashboard.

## What lives here

This repository contains every part of the exchange **except the
proprietary matching engine**. Specifically:

- `cmd/api/` — HTTP API server (Go)
- `cmd/scheduler/` — background jobs, trigger orders, chain polling
- `web/` — React/Vite frontend
- `internal/` — all shared packages (wallet, user, KYC, trading
  service, settlement glue, audit, etc.)
- `migrations/` — PostgreSQL schema
- `deploy/` — Prometheus, Grafana, alerting, and other ops assets

## What does NOT live here

The **matching engine** (central limit order book) is closed source
and ships as a Docker image:

- Image: `ghcr.io/goexdev/goexchange-core:latest`
- Protocol: gRPC on port 50051
- Source: private `goexchange-core` repository
- License: commercial (Ed25519-signed license tokens issued by the
  license server at `license.goexchange.top`)

Detailed architecture, deployment, and operator runbooks are kept in
the project's private documentation archive and are not mirrored here.

## Matching engine contract

The public repository exposes only the **contract** it relies on:

- `internal/matching/types.go` — `Order`, `Trade`, `Status`, etc.
- `internal/matching/client.go` — `Client` interface (PlaceOrder,
  CancelOrder, AmendOrder, GetOrderBook, GetTicker, StreamTrades)
- `internal/matching/order_source_adapter.go` — adapts `Client` to
  the narrow `trading.OrderSource` interface

The gRPC implementation lives in `internal/matching/grpc_client.go`
and speaks to the matching core over gRPC on port 50051. Generated
stubs live in `internal/matching/matchingv1/` (run
`scripts/gen_proto.sh` to regenerate from `proto/matching.proto`).

## Quick start

```bash
git clone https://github.com/goexdev/goexchange
cd goexchange
cp .env.example .env
# Edit .env (DB password, JWT secret, Vault token)
docker compose up -d
```

Open `http://localhost:3000` and follow the registration screen.

## License

Apache License 2.0. See [`LICENSE`](LICENSE) for the full text.

## Contributing

Issues and PRs are welcome. Please read `CONTRIBUTING.md` first.