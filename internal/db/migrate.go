package db

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationsTableDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    checksum   TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`

// ErrMigrationChecksumMismatch is returned when an already-applied migration
// file has been edited on disk since it was first run.
var ErrMigrationChecksumMismatch = errors.New("migration checksum mismatch")

// migrationLockID is a stable advisory-lock key used to serialise concurrent
// RunMigrations calls from multiple pods. The value is arbitrary but must be
// consistent across deployments.
const migrationLockID int64 = 5_765_169_143_718_794_700

// RunMigrations applies every *.sql file in fsys/dir, in lexicographic order,
// that has not been applied yet. Each migration is recorded in the
// schema_migrations table together with a SHA-256 checksum so that future
// runs can detect tampering with already-applied files.
//
// A session-level PostgreSQL advisory lock serialises concurrent calls so that
// multiple pods starting simultaneously do not race on schema_migrations table
// creation or on inserting migration records.
//
// Each migration runs in its own transaction. If a migration fails the
// transaction is rolled back and the error is returned — no partial state
// is left behind.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, fsys embed.FS, dir string) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	// pg_advisory_lock is session-scoped: it is NOT released when the connection
	// is returned to the pool. Defer an explicit unlock so the lock is freed
	// before conn.Release() recycles the connection.
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, migrationsTableDDL); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read migrations dir %q: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	applied, err := loadAppliedMigrations(ctx, conn)
	if err != nil {
		return err
	}

	for _, name := range names {
		data, err := fsys.ReadFile(dir + "/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])

		if prev, ok := applied[name]; ok {
			if prev != checksum {
				return fmt.Errorf("%w: %s (db=%s, file=%s)",
					ErrMigrationChecksumMismatch, name, prev, checksum)
			}
			continue
		}

		if err := applyMigration(ctx, pool, name, string(data), checksum); err != nil {
			return err
		}
	}
	return nil
}

func loadAppliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[string]string, error) {
	rows, err := conn.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var name, sum string
		if err := rows.Scan(&name, &sum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		out[name] = sum
	}
	return out, rows.Err()
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, name, sql, checksum string) (rerr error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", name, err)
	}
	defer func() {
		if rerr != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("exec %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`,
		name, checksum,
	); err != nil {
		return fmt.Errorf("record %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s: %w", name, err)
	}
	return nil
}
