// Package migrations configures the shared goose migration runner.
package migrations

import (
	"database/sql"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func NewProvider(db *sql.DB, source fs.FS) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockTimeout(1, 5),
		lock.WithUnlockTimeout(1, 3),
	)
	if err != nil {
		return nil, err
	}
	return goose.NewProvider(goose.DialectPostgres, db, source, goose.WithSessionLocker(locker))
}
