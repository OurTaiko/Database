// Package database opens the OurTaikoDatabase PostgreSQL pool and applies its
// numbered schema files.
package database

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var migrations embed.FS

// Tables live in the public, internal and auth schemas of the design; the
// code refers to them unqualified.
const searchPath = "public,internal,auth"

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("invalid DATABASE_URL")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// Migrate applies every schema file newer than the recorded version, each in
// its own transaction. Files are named NNN_description.sql and record their
// own version.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	// One migrator at a time, across processes.
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('ourtaiko_database:migrate',0))`); err != nil {
		return err
	}
	defer conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended('ourtaiko_database:migrate',0))`)
	current := 0
	err = conn.QueryRow(ctx, `SELECT COALESCE(max(version),0) FROM internal.schema_migrations`).Scan(&current)
	var missing *pgconn.PgError
	if errors.As(err, &missing) && (missing.Code == "42P01" || missing.Code == "3F000") {
		err = nil
	}
	if err != nil {
		return err
	}
	entries, err := migrations.ReadDir(".")
	if err != nil {
		return err
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		prefix, _, _ := strings.Cut(name, "_")
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return fmt.Errorf("migration %s needs a numeric prefix", name)
		}
		if version <= current {
			continue
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		current = version
	}
	return nil
}
