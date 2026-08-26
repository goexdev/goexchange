package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcher_DetectsChange(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	original := `
app:
  port: 8099
database:
  url: postgres://x:y@127.0.0.1:5432/test
jwt:
  secret: "test-secret-12345678"
  ttl: 3600
vault:
  enabled: false
chainwatcher:
  chains: {}
`
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var reloadCount atomic.Int64
	var lastCfg atomic.Pointer[Config]
	onChange := func(c *Config) error {
		reloadCount.Add(1)
		lastCfg.Store(c)
		return nil
	}

	w := NewWatcher(cfgPath, testLog(t), onChange)
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	newContent := `
app:
  port: 9099
database:
  url: postgres://x:y@127.0.0.1:5432/test
jwt:
  secret: "test-secret-12345678"
  ttl: 3600
vault:
  enabled: false
chainwatcher:
  chains: {}
`
	if err := os.WriteFile(cfgPath, []byte(newContent), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	if n := reloadCount.Load(); n < 1 {
		t.Errorf("expected reload count >= 1, got %d", n)
	}

	cfg := lastCfg.Load()
	if cfg == nil {
		t.Fatal("no config loaded")
	}
	if cfg.App.Port != 9099 {
		t.Errorf("expected port 9099, got %d", cfg.App.Port)
	}
}

func TestWatcher_KeepsOldConfigOnError(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	original := `
app:
  port: 8099
database:
  url: postgres://x:y@127.0.0.1:5432/test
jwt:
  secret: "test-secret-12345678"
  ttl: 3600
vault:
  enabled: false
chainwatcher:
  chains: {}
`
	if err := os.WriteFile(cfgPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var reloadCount atomic.Int64
	onChange := func(c *Config) error {
		reloadCount.Add(1)
		if c.App.Port != 9099 {
			return fmt.Errorf("simulated apply error")
		}
		return nil
	}

	w := NewWatcher(cfgPath, testLog(t), onChange)
	defer w.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	bad := `
app:
  port: 9099
  this is: [invalid yaml
`
	if err := os.WriteFile(cfgPath, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second)

	stats := w.Stats()
	if stats.LastReloadErr == "" {
		t.Error("expected error in stats")
	}
}

func TestWatcher_Stats(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
app:
  port: 8099
database:
  url: postgres://x:y@127.0.0.1:5432/test
jwt:
  secret: "test-secret-12345678"
  ttl: 3600
vault:
  enabled: false
chainwatcher:
  chains: {}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewWatcher(cfgPath, testLog(t), nil)
	defer w.Stop()
	stats := w.Stats()
	if stats.ReloadCount != 0 {
		t.Errorf("expected 0 reloads, got %d", stats.ReloadCount)
	}
}

func TestDirOf(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"a/b/c", "a/b"},
		{"/etc/config.yaml", "/etc"},
		{"local.yaml", "."},
	}
	for _, tc := range tests {
		if got := dirOf(tc.in); got != tc.want {
			t.Errorf("dirOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func testLog(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}