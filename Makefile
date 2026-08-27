SHELL := /bin/bash
export PATH := /usr/local/go/bin:$(HOME)/go/bin:$(PATH)

DATABASE_URL ?= postgres://exchange:exchange@localhost:5433/exchange?sslmode=disable
REDIS_ADDR ?= localhost:6379

# ---------- help ----------
.PHONY: help
help:
	@echo "goexchange Makefile"
	@echo ""
	@echo "Infra:    make infra-up / infra-down"
	@echo "DB:       make db-migrate / db-reset / db-shell"
	@echo "Build:    make build / run / test / lint / fmt"
	@echo "Tools:    make migrate-tool"

# ---------- infra ----------
.PHONY: infra-up infra-down infra-ps infra-logs
infra-up:
	docker compose up -d postgres redis
	@echo "Waiting for services..."
	@sleep 5
	docker compose ps

infra-down:
	docker compose down

infra-ps:
	docker compose ps

infra-logs:
	docker compose logs -f --tail=200

# ---------- db ----------
.PHONY: db-migrate db-reset db-shell migrate-tool
migrate-tool:
	@command -v migrate >/dev/null || (echo "Installing golang-migrate..." && \
		GOBIN=/usr/local/bin go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest)

db-migrate: migrate-tool
	migrate -path migrations -database "$(DATABASE_URL)" up

db-reset:
	@read -p "Drop and recreate DB? [y/N] " r && [ "$$r" = "y" ] || exit 1
	docker exec -it goexchange-postgres psql -U exchange -d postgres \
		-c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='exchange' AND pid <> pg_backend_pid();" >/dev/null
	docker exec -it goexchange-postgres psql -U exchange -d postgres \
		-c "DROP DATABASE IF EXISTS exchange; CREATE DATABASE exchange;"
	$(MAKE) db-migrate

db-shell:
	docker exec -it goexchange-postgres psql -U exchange -d exchange

# ---------- build/run ----------
.PHONY: build run test lint fmt clean
build:
	mkdir -p bin
	go build -o bin/server ./cmd/server

run: build
	./bin/server

test:
	go test ./... -v

lint:
	@command -v golangci-lint >/dev/null || (echo "Installing golangci-lint..." && \
		GOBIN=/usr/local/bin go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

fmt:
	gofmt -w .
	goimports -w .

clean:
	rm -rf bin
	go clean -cache -testcache
# ---------- clean ----------
.PHONY: clean
clean:
	rm -rf bin/

# ---------- systemd ----------
# Production deployment: install systemd units + auto-start on boot
.PHONY: systemd-install systemd-uninstall systemd-start systemd-stop systemd-restart systemd-status systemd-logs

systemd-install:
	./deploy/systemd/install-systemd.sh --start

systemd-uninstall:
	./deploy/systemd/uninstall-systemd.sh

systemd-start:
	systemctl start goexchange.target

systemd-stop:
	systemctl stop goexchange-api goexchange-matcher goexchange-scheduler

systemd-restart:
	systemctl restart goexchange-api goexchange-matcher goexchange-scheduler

systemd-status:
	@systemctl status goexchange-api goexchange-matcher goexchange-scheduler --no-pager

systemd-logs:
	journalctl -u goexchange-api -u goexchange-matcher -u goexchange-scheduler -f
