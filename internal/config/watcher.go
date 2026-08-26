package config

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"path/filepath"
)

// OnChangeFunc is called when config file changes (after successful reload).
// If Reload returns an error, OnChange is NOT called (old config kept).
type OnChangeFunc func(newCfg *Config) error

// Watcher monitors a config file for changes and triggers reload.
type Watcher struct {
	path     string
	log      *slog.Logger
	onChange OnChangeFunc

	// Stats
	reloadCount    atomic.Int64
	lastReloadAt   atomic.Pointer[time.Time]
	lastReloadErr  atomic.Pointer[string]
	lastReloadTime atomic.Int64 // unix nano

	mu       sync.Mutex
	watcher  *fsnotify.Watcher
	stopCh   chan struct{}
	debounce time.Duration
}

// NewWatcher creates a config file watcher.
func NewWatcher(path string, log *slog.Logger, onChange OnChangeFunc) *Watcher {
	return &Watcher{
		path:     path,
		log:      log,
		onChange: onChange,
		stopCh:   make(chan struct{}),
		debounce: 500 * time.Millisecond,
	}
}

// Start begins watching the config file in a goroutine.
// Returns immediately. Call Stop() to terminate.
func (w *Watcher) Start(ctx context.Context) error {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create fsnotify watcher: %w", err)
	}
	w.mu.Lock()
	w.watcher = fsWatcher
	w.mu.Unlock()

	dir := dirOf(w.path)
	if err := fsWatcher.Add(dir); err != nil {
		fsWatcher.Close()
		return fmt.Errorf("watch directory %s: %w", dir, err)
	}

	w.log.Info("config watcher started", "path", w.path, "watch_dir", dir, "debounce", w.debounce)

	go w.run(ctx)
	return nil
}

// Stop terminates the watcher.
func (w *Watcher) Stop() {
	close(w.stopCh)
	w.mu.Lock()
	if w.watcher != nil {
		w.watcher.Close()
	}
	w.mu.Unlock()
}

// Reload reads the config file and calls onChange with the new config.
// If reload fails or validation fails, old config is kept and error is recorded.
func (w *Watcher) Reload(ctx context.Context) error {
	newCfg, err := Load(w.path)
	if err != nil {
		w.lastReloadErr.Store(ptrString(fmt.Sprintf("load: %v", err)))
		w.log.Error("config reload failed (load)", "path", w.path, "error", err)
		return err
	}
	if w.onChange != nil {
		if err := w.onChange(newCfg); err != nil {
			w.lastReloadErr.Store(ptrString(fmt.Sprintf("apply: %v", err)))
			w.log.Error("config reload failed (apply)", "error", err)
			return err
		}
	}
	now := time.Now()
	w.lastReloadAt.Store(&now)
	w.lastReloadTime.Store(now.UnixNano())
	w.lastReloadErr.Store(nil)
	w.reloadCount.Add(1)
	w.log.Info("config reloaded successfully", "path", w.path, "reload_count", w.reloadCount.Load())
	return nil
}

// Stats returns current watcher stats.
func (w *Watcher) Stats() WatcherStats {
	stats := WatcherStats{
		ReloadCount:  w.reloadCount.Load(),
		LastReloadNs: w.lastReloadTime.Load(),
	}
	if lastAt := w.lastReloadAt.Load(); lastAt != nil {
		stats.LastReloadAt = lastAt.Format(time.RFC3339)
	}
	if errPtr := w.lastReloadErr.Load(); errPtr != nil {
		stats.LastReloadErr = *errPtr
	}
	return stats
}

// WatcherStats is returned by Stats().
type WatcherStats struct {
	ReloadCount   int64  `json:"reload_count"`
	LastReloadAt  string `json:"last_reload_at,omitempty"`
	LastReloadNs  int64  `json:"last_reload_ns,omitempty"`
	LastReloadErr string `json:"last_reload_err,omitempty"`
}

// run is the main watcher loop with debouncing.
func (w *Watcher) run(ctx context.Context) {
	var (
		timer *time.Timer
		armed bool
	)

	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.log.Info("GOT EVENT", "op", ev.Op.String(), "name", ev.Name, "match", pathMatches(ev.Name, w.path))
			if !pathMatches(ev.Name, w.path) {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Chmod) != 0 {
				w.log.Debug("config file event", "op", ev.Op.String(), "name", ev.Name)
				if timer != nil {
					timer.Stop()
				}
				timer = time.NewTimer(w.debounce)
				armed = true
			}
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.log.Warn("fsnotify error", "error", err)
		case <-timerChan(timer, armed):
			if armed {
				armed = false
				if err := w.Reload(ctx); err != nil {
					w.log.Warn("auto-reload failed (old config still active)", "error", err)
				}
			}
		}
	}
}

func timerChan(t *time.Timer, armed bool) <-chan time.Time {
	if !armed || t == nil {
		return nil
	}
	return t.C
}

func pathMatches(eventPath, configPath string) bool {
	if eventPath == configPath {
		return true
	}
	return filepath.Base(eventPath) == filepath.Base(configPath)
}
func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return "."
}

func ptrString(s string) *string { return &s }
