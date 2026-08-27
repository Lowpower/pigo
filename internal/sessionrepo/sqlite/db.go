package sqlite

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/sessionrepo"
	_ "modernc.org/sqlite" // register the pure-Go sqlite driver
)

const (
	defaultTTLMs       = 30_000
	defaultHeartbeatMs = 10_000
)

// WriterLeaseOptions configure per-session write claims.
type WriterLeaseOptions struct {
	TTLMs               int64
	HeartbeatIntervalMs int64
}

type resolvedLease struct {
	ttlMs       int64
	heartbeatMs int64
}

func resolveLease(opts *WriterLeaseOptions) (resolvedLease, error) {
	if opts == nil {
		return resolvedLease{ttlMs: defaultTTLMs, heartbeatMs: defaultHeartbeatMs}, nil
	}
	if opts.TTLMs <= 0 {
		return resolvedLease{}, fmt.Errorf("writerLease.ttlMs must be positive")
	}
	if opts.HeartbeatIntervalMs <= 0 || opts.HeartbeatIntervalMs >= opts.TTLMs {
		return resolvedLease{}, fmt.Errorf("writerLease.heartbeatIntervalMs must be positive and less than ttlMs")
	}
	return resolvedLease{ttlMs: opts.TTLMs, heartbeatMs: opts.HeartbeatIntervalMs}, nil
}

type writerLease struct {
	ownerID     string
	fence       int64
	expiresAtMs int64
}

// Options construct a Repository.
type Options struct {
	DatabasePath string
	WriterLease  *WriterLeaseOptions
}

// Repository is a v4 SQLite SessionRepo.
type Repository struct {
	opts    Options
	lease   resolvedLease
	mu      sync.Mutex
	dbMu    sync.Mutex
	db      *sql.DB
	absPath string
	active  map[string]*storage
	closed  bool
}

// NewRepository opens a lazy SQLite session repository.
func NewRepository(opts Options) (*Repository, error) {
	lease, err := resolveLease(opts.WriterLease)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(opts.DatabasePath)
	if err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to resolve SQLite sessions database "+opts.DatabasePath, err)
	}
	return &Repository{opts: opts, lease: lease, absPath: abs, active: map[string]*storage{}}, nil
}

func (r *Repository) getDB() (*sql.DB, error) {
	if r.db != nil {
		return r.db, nil
	}
	if err := os.MkdirAll(filepath.Dir(r.absPath), 0o755); err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to create SQLite sessions directory "+r.absPath, err)
	}
	db, err := sql.Open("sqlite", r.absPath)
	if err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to open SQLite sessions database", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA synchronous=FULL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	r.db = db
	return db, nil
}

func (r *Repository) withDB(fn func(*sql.DB) error) error {
	r.dbMu.Lock()
	defer r.dbMu.Unlock()
	db, err := r.getDB()
	if err != nil {
		return err
	}
	return fn(db)
}

func (r *Repository) immediate(fn func() error) error {
	return r.withDB(func(db *sql.DB) error {
		if _, err := db.Exec("BEGIN IMMEDIATE"); err != nil {
			return err
		}
		if err := fn(); err != nil {
			_, _ = db.Exec("ROLLBACK")
			return err
		}
		if _, err := db.Exec("COMMIT"); err != nil {
			_, _ = db.Exec("ROLLBACK")
			return err
		}
		return nil
	})
}

func nowMs() int64 { return time.Now().UnixMilli() }

func strPtr(s string) *string { return &s }

func nullStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func tableExists(db *sql.DB, name string) (bool, error) {
	var found int
	err := db.QueryRow(`SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ? LIMIT 1`, name).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
