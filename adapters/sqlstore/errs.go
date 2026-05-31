package sqlstore

import (
	"errors"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation reports whether err is a unique/primary-key constraint
// violation on either engine — Postgres SQLSTATE 23505, MySQL error 1062. Used
// to translate a racing contact insert into a clean CONTACT_CONFLICT.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	var myErr *mysqldriver.MySQLError
	if errors.As(err, &myErr) {
		return myErr.Number == 1062
	}
	return false
}
