// Package dialect is the thin SQL dialect abstraction that lets the sqlstore
// adapter target both PostgreSQL and MySQL from one set of queries. We write
// SQL with `?` placeholders and Rebind for Postgres; helpers paper over the
// JSON column type and the RETURNING-vs-LastInsertId difference.
package dialect

import (
	"fmt"
	"strings"
)

// Kind identifies the SQL engine.
type Kind string

const (
	Postgres Kind = "postgres"
	MySQL    Kind = "mysql"
)

// Parse maps a driver/string to a Kind.
func Parse(s string) (Kind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "postgres", "postgresql", "pgx", "pg":
		return Postgres, nil
	case "mysql", "mariadb":
		return MySQL, nil
	default:
		return "", fmt.Errorf("dialect: unsupported engine %q (want postgres|mysql)", s)
	}
}

// Driver returns the database/sql driver name to open with.
func (k Kind) Driver() string {
	if k == Postgres {
		return "pgx"
	}
	return "mysql"
}

// Rebind converts a query written with `?` placeholders into the engine's
// native form ($1,$2,... for Postgres; `?` unchanged for MySQL). This mirrors
// sqlx.Rebind but is dependency-free so dialect stays a leaf package.
func (k Kind) Rebind(query string) string {
	if k != Postgres {
		return query
	}
	var b strings.Builder
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(itoa(n))
			continue
		}
		b.WriteByte(query[i])
	}
	return b.String()
}

// JSONType returns the column type for a JSON column in DDL.
func (k Kind) JSONType() string {
	if k == Postgres {
		return "JSONB"
	}
	return "JSON"
}

// SupportsReturning reports whether INSERT ... RETURNING is available
// (Postgres yes; MySQL no — use LastInsertId or pre-generated IDs).
func (k Kind) SupportsReturning() bool { return k == Postgres }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
