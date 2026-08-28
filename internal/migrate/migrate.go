// Package migrate runs SQL migrations against a Postgres database.
//
// Supports both embedded migrations (compiled into binary via go:embed)
// and file-based migrations from a directory.
package migrate

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Migrator runs migrations.
type Migrator struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// New creates a new Migrator.
func New(pool *pgxpool.Pool, log *slog.Logger) *Migrator {
	return &Migrator{pool: pool, log: log}
}

// Run executes all pending up-migrations from an embedded FS.
//
// Migrations must follow the naming convention: NNNN_name.up.sql / NNNN_name.down.sql
func (m *Migrator) Run(ctx context.Context, fs embed.FS, dir string) error {
	// 1. Create schema_migrations table if not exists
	if _, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version BIGINT PRIMARY KEY,
			dirty BOOLEAN NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// 2. Read all *.up.sql files
	entries, err := fs.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}

	type migration struct {
		version int64
		name    string
		sql     string
	}
	var migrations []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		// Parse version from filename: 0001_init.up.sql → 1
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			m.log.Warn("skipping malformed migration filename", "name", name)
			continue
		}
		var version int64
		if _, err := fmt.Sscanf(parts[0], "%d", &version); err != nil {
			m.log.Warn("skipping non-numeric migration version", "name", name)
			continue
		}
		data, err := fs.ReadFile(dir + "/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		migrations = append(migrations, migration{
			version: version,
			name:    name,
			sql:     string(data),
		})
	}

	// 3. Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})

	// 4. Get applied versions
	applied := make(map[int64]bool)
	rows, err := m.pool.Query(ctx, `SELECT version FROM schema_migrations WHERE dirty = false`)
	if err != nil {
		return fmt.Errorf("query applied: %w", err)
	}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scan: %w", err)
		}
		applied[v] = true
	}
	rows.Close()

	// 5. Apply pending migrations
	appliedCount := 0
	for _, mig := range migrations {
		if applied[mig.version] {
			continue
		}

		m.log.Info("applying migration", "version", mig.version, "name", mig.name)

		// Mark as dirty
		if _, err := m.pool.Exec(ctx,
			`INSERT INTO schema_migrations (version, dirty) VALUES ($1, true)
			 ON CONFLICT (version) DO UPDATE SET dirty = true`,
			mig.version); err != nil {
			return fmt.Errorf("mark dirty %d: %w", mig.version, err)
		}

		// Run in transaction. pgx's tx.Exec executes a single statement at
		// a time; for migration files that contain several CREATE TABLE /
		// INSERT / ALTER statements separated by `;`, we split on `;` and
		// execute each statement individually. (The earlier single-Exec
		// path broke on scheduler/migrations/0001_init.up.sql where the
		// currencies INSERT used to omit its trailing `;` in an attempt
		// to make pgx treat the whole file as one statement — pgx never
		// did, so the migrate step crashed with a syntax error. Bug #3.)
		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}
		stmts := splitSQLStatements(mig.sql)
		for _, stmt := range stmts {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("exec migration %d statement %q: %w", mig.version, stmt, err)
			}
		}
		// Mark as clean
		if _, err := tx.Exec(ctx,
			`UPDATE schema_migrations SET dirty = false WHERE version = $1`,
			mig.version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("mark clean %d: %w", mig.version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		appliedCount++
	}

	if appliedCount == 0 {
		m.log.Info("no pending migrations")
	} else {
		m.log.Info("migrations applied", "count", appliedCount)
	}
	return nil
}

// ErrNoMigrations signals no migrations to apply.
var ErrNoMigrations = errors.New("no migrations to apply")

// splitSQLStatements splits a SQL script into individual statements.
// Statements end at a top-level `;`. We intentionally do not try to
// parse string literals or dollar-quoted bodies — migration files in
// this repo do not contain them, and keeping the splitter simple
// keeps the failure mode obvious when a file does.
//
// We strip `-- line comments` so a comment that contains a `;` does
// not accidentally split a statement. Multi-line `/* ... */` block
// comments are not stripped; if a future migration needs them we can
// extend this helper.
func splitSQLStatements(sql string) []string {
	// Drop line comments first so their embedded `;` is ignored.
	var cleaned strings.Builder
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		cleaned.WriteString(line)
		cleaned.WriteByte('\n')
	}
	parts := strings.Split(cleaned.String(), ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}