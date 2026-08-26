package sqlite

import (
	"database/sql"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func acquireWriterLease(db *sql.DB, sessionID, ownerID string, now, expiresAtMs int64) (*writerLease, error) {
	row := db.QueryRow(`INSERT INTO writer_leases (session_id, owner_id, fence, expires_at_ms)
		VALUES (?, ?, 1, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			owner_id = excluded.owner_id,
			fence = writer_leases.fence + 1,
			expires_at_ms = excluded.expires_at_ms
		WHERE writer_leases.expires_at_ms <= ?
		RETURNING owner_id, fence, expires_at_ms`, sessionID, ownerID, expiresAtMs, now)
	var l writerLease
	err := row.Scan(&l.ownerID, &l.fence, &l.expiresAtMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func claimWriterLease(db *sql.DB, sessionID string, lease resolvedLease) (*writerLease, error) {
	now := nowMs()
	l, err := acquireWriterLease(db, sessionID, sessionrepo.NewID(), now, now+lease.ttlMs)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "SQLite session "+sessionID+" already has an active writer")
	}
	return l, nil
}

func renewWriterLease(db *sql.DB, sessionID string, lease *writerLease, now, expiresAtMs int64) (bool, error) {
	res, err := db.Exec(`UPDATE writer_leases SET expires_at_ms = ?
		WHERE session_id = ? AND owner_id = ? AND fence = ? AND expires_at_ms > ?`,
		expiresAtMs, sessionID, lease.ownerID, lease.fence, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 1 {
		lease.expiresAtMs = expiresAtMs
		return true, nil
	}
	return false, nil
}

func releaseWriterLease(db *sql.DB, sessionID string, lease *writerLease) error {
	_, err := db.Exec(`DELETE FROM writer_leases WHERE session_id = ? AND owner_id = ? AND fence = ?`,
		sessionID, lease.ownerID, lease.fence)
	return err
}

func deleteWriterLease(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM writer_leases WHERE session_id = ?`, sessionID)
	return err
}

func lostWriterError(id string) error {
	return sessionrepo.NewError(sessionrepo.ErrStorage, "SQLite session "+id+" writer lease was lost")
}

func closedWriterError(id string) error {
	return sessionrepo.NewError(sessionrepo.ErrStorage, "SQLite session "+id+" is closed")
}
