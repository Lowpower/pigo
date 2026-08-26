package sqlite

import (
	"database/sql"
	"time"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func applyMigrations(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS migrations (
		id TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	var applied string
	err := db.QueryRow(`SELECT id FROM migrations WHERE id = ?`, "001_initial.sql").Scan(&applied)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if applied == "001_initial.sql" {
		return nil
	}
	if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_, _ = db.Exec("ROLLBACK")
		}
	}()
	if _, err := db.Exec(initialSQL); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO migrations (id, applied_at) VALUES (?, ?)`, "001_initial.sql", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if _, err := db.Exec("COMMIT"); err != nil {
		return err
	}
	ok = true
	return nil
}

func wrap(err error, code sessionrepo.ErrorCode, msg string) error {
	if err == nil {
		return nil
	}
	if se, ok := err.(*sessionrepo.Error); ok {
		return se
	}
	return sessionrepo.NewErrorCause(code, msg, err)
}
