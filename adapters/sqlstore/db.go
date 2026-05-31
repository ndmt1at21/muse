// Package sqlstore is the default, batteries-included implementation of the
// gamekit data ports over SQL — supporting both PostgreSQL and MySQL with no
// ORM (database/sql + sqlx, raw SQL, the pkg/dialect abstraction). The gamekit
// SDK never imports this; consumers may swap their own port implementations.
package sqlstore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/muse/pkg/dialect"

	_ "github.com/go-sql-driver/mysql" // mysql driver
	_ "github.com/jackc/pgx/v5/stdlib" // pgx stdlib driver registered as "pgx"
)

// DB is a dual-engine connection pool plus the dialect it speaks. It also
// implements the gamekit ports.TxRunner.
type DB struct {
	x    *sqlx.DB
	kind dialect.Kind
}

// Open connects to the database. engine is "postgres" or "mysql"; dsn is the
// driver-specific connection string.
func Open(ctx context.Context, engine, dsn string) (*DB, error) {
	kind, err := dialect.Parse(engine)
	if err != nil {
		return nil, err
	}
	x, err := sqlx.ConnectContext(ctx, kind.Driver(), dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlstore: connect %s: %w", kind, err)
	}
	// Bound the pool so a burst of concurrent Play transactions queues rather
	// than exhausting the database's connection limit.
	x.SetMaxOpenConns(20)
	x.SetMaxIdleConns(20)
	x.SetConnMaxLifetime(30 * time.Minute)
	return &DB{x: x, kind: kind}, nil
}

// Kind returns the SQL engine in use.
func (db *DB) Kind() dialect.Kind { return db.kind }

// SQL exposes the underlying *sql.DB (e.g. for the migration runner).
func (db *DB) SQL() *sql.DB { return db.x.DB }

// Close closes the pool.
func (db *DB) Close() error { return db.x.Close() }

// Ping verifies connectivity (used by /readyz).
func (db *DB) Ping(ctx context.Context) error { return db.x.PingContext(ctx) }

// rebind converts `?` placeholders to the engine's native form.
func (db *DB) rebind(q string) string { return db.kind.Rebind(q) }

type txKey struct{}

// WithTx runs fn inside a transaction. The *sqlx.Tx is stashed in the context
// so every *Store method invoked within fn uses the same transaction. This is
// how the engine's Play achieves atomic consume+deduct+history.
func (db *DB) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return fn(ctx) // already in a transaction; reuse it (nested-safe)
	}
	tx, err := db.x.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlstore: begin tx: %w", err)
	}
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlstore: commit: %w", err)
	}
	return nil
}

// ext returns the active transaction if one is in the context, else the pool.
// Both satisfy sqlx.ExtContext (Exec/Query/Get/Select over context).
func (db *DB) ext(ctx context.Context) sqlx.ExtContext {
	if tx, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return tx
	}
	return db.x
}

// getContext runs a single-row query through the active ext.
func (db *DB) getContext(ctx context.Context, dest any, query string, args ...any) error {
	return sqlx.GetContext(ctx, db.ext(ctx), dest, db.rebind(query), args...)
}

// selectContext runs a multi-row query through the active ext.
func (db *DB) selectContext(ctx context.Context, dest any, query string, args ...any) error {
	return sqlx.SelectContext(ctx, db.ext(ctx), dest, db.rebind(query), args...)
}

// execContext runs a statement through the active ext.
func (db *DB) execContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.ext(ctx).ExecContext(ctx, db.rebind(query), args...)
}
