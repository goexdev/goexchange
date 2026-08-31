// Package main is the goexchange scheduler binary.
//
// Runs background tasks:
//   - DB migrations
//   - chainwatcher poll loop (deposits + withdrawals)
//
// Exposes /health endpoint for monitoring.
package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/goexdev/goexchange/internal/chainwatcher"
	"github.com/goexdev/goexchange/internal/config"
	"github.com/goexdev/goexchange/internal/db"
	"github.com/goexdev/goexchange/internal/logger"
	"github.com/goexdev/goexchange/internal/migrate"
	"github.com/goexdev/goexchange/internal/notifier"
	"github.com/goexdev/goexchange/internal/user"
	"github.com/goexdev/goexchange/internal/vaultinit"
	"github.com/goexdev/goexchange/internal/wallet"
)

//go:embed migrations
var migrationsFS embed.FS

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := logger.New(cfg.App.Env)
	log.Info("starting goexchange-scheduler", "env", cfg.App.Env, "port", cfg.App.SchedulerPort)

	// Initialize vault first so the scheduler picks up the same
	// DB password (and JWT secret, etc.) as the API binary.
	// Without this step the scheduler would try to connect with
	// the dev placeholder password and fail with
	// "password authentication failed for user exchange".
	if cfg.Vault.Enabled {
		initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := vaultinit.Init(cfg, initCtx, log)
		initCancel()
		if err != nil {
			return fmt.Errorf("init vault: %w", err)
		}
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer initCancel()

	pool, err := db.Connect(initCtx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	migrator := migrate.New(pool, log)
	if err := migrator.Run(initCtx, migrationsFS, "migrations"); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nlog := log.With("component", "notifier")
	nprovider, err := notifier.NewProvider(
		notifier.ProviderType(cfg.Notifier.Provider),
		notifier.SMTPConfig{Host: cfg.Notifier.SMTP.Host, Port: cfg.Notifier.SMTP.Port, User: cfg.Notifier.SMTP.User, Password: cfg.Notifier.SMTP.Password, From: cfg.Notifier.SMTP.From},
		notifier.ResendConfig{APIKey: cfg.Notifier.Resend.APIKey, From: cfg.Notifier.Resend.From},
		nlog,
	)
	if err != nil {
		return fmt.Errorf("init notifier: %w", err)
	}
	notifierSvc := notifier.NewService(pool, nprovider, cfg.Notifier.From, nlog)
	userSvc := user.NewService(pool, log)
	walletSvc := wallet.NewService(pool, log)
	cw := chainwatcher.New(pool, walletSvc, userSvc, notifierSvc, log, cfg.ChainWatcher, cfg.ChainWatcher.Driver)

	cw.Start(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"status":"ok","driver":"%s","deposits_count":%d}`, cw.DriverName(), cw.DepositsCount())
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.App.SchedulerPort),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Info("scheduler HTTP listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("scheduler HTTP failed", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("scheduler shutdown")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	return srv.Shutdown(shutdownCtx)
}
