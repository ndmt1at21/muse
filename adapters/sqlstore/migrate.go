package sqlstore

import (
	"embed"
	"fmt"

	"github.com/muse/pkg/dialect"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/postgres/*.sql migrations/mysql/*.sql
var migrationsFS embed.FS

// Migrate runs all pending up migrations for the connected engine using goose
// with the embedded per-dialect SQL.
func (db *DB) Migrate() error {
	goose.SetBaseFS(migrationsFS)
	var (
		dir          string
		gooseDialect string
	)
	switch db.kind {
	case dialect.Postgres:
		dir, gooseDialect = "migrations/postgres", "postgres"
	case dialect.MySQL:
		dir, gooseDialect = "migrations/mysql", "mysql"
	default:
		return fmt.Errorf("sqlstore: no migrations for dialect %q", db.kind)
	}
	if err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("sqlstore: goose dialect: %w", err)
	}
	if err := goose.Up(db.x.DB, dir); err != nil {
		return fmt.Errorf("sqlstore: migrate: %w", err)
	}
	return nil
}
