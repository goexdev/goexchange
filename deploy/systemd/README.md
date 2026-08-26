# goexchange Systemd Units

Production-ready systemd service units for goexchange's 3 binaries.

## Components

| Unit | Purpose | Port | Description |
|---|---|---|---|
| `goexchange-api.service` | HTTP API + WS + workers | 8099 | User-facing API, notifications, deposit tracking |
| `goexchange-matcher.service` | Order matching + WS orderbook | 8098 | Trading engine, real-time order matching |
| `goexchange-scheduler.service` | Chain watcher + migrations | 8097 | Polls chain for deposits/withdrawals, applies DB migrations |
| `goexchange.target` | All 3 units | - | Group unit for starting/stopping all together |

## Features

- **Auto-restart on crash** (RestartSec=3-5 with backoff)
- **Health checks** via `ExecStartPost` (curl /healthz)
- **Dependency ordering**: scheduler runs BEFORE api/matcher (runs migrations first)
- **Hardening**: ProtectSystem, NoNewPrivileges, PrivateTmp, etc.
- **Resource limits**: LimitNOFILE=65536, LimitNPROC=4096
- **Structured logging** via journald (`journalctl -u goexchange-api -f`)
- **Environment file** support (`/root/goexchange/.env`)

## Install

```bash
cd /root/goexchange/deploy/systemd
sudo ./install-systemd.sh          # install + enable on boot
sudo ./install-systemd.sh --start   # also start now
```

## Uninstall

```bash
sudo ./uninstall-systemd.sh
```

## Daily Usage

```bash
# Start all
sudo systemctl start goexchange.target

# Stop all
sudo systemctl stop goexchange.target

# Status of one
systemctl status goexchange-api

# Restart after deploy
sudo systemctl restart goexchange-api goexchange-matcher goexchange-scheduler

# Follow logs
journalctl -u goexchange-api -f

# Last 100 lines
journalctl -u goexchange-api -n 100 --no-pager

# All goexchange logs
journalctl -u 'goexchange-*' -n 50
```

## Why systemd?

Prevents the "stale binary" bug class (e.g. M5.10): a new binary deployed
without restarting the old one. systemd guarantees:
1. Only ONE instance of each binary runs at a time
2. Auto-restart on crash
3. Graceful shutdown on stop
4. Auto-start on boot

## Pre-requisites

- systemd 245+ (Ubuntu 20.04+)
- Root access to install unit files
- Postgres + Redis running first (handled via `After=`)

## File Locations After Install

- Units: `/etc/systemd/system/goexchange-*.service`
- Logs: `journalctl -u goexchange-*`
- State: `/run/systemd/state/`

## Customization

Edit the `.service` files in this directory, then:
```bash
sudo cp *.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl restart goexchange-api
```
