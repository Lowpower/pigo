package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

const searchFTS = `
CREATE VIRTUAL TABLE IF NOT EXISTS session_search_fts USING fts5(
  payload,
  content = 'entries',
  content_rowid = 'rowid',
  tokenize = 'trigram remove_diacritics 1'
);
CREATE TRIGGER IF NOT EXISTS session_search_fts_ai AFTER INSERT ON entries BEGIN
  INSERT INTO session_search_fts(rowid, payload) VALUES (new.rowid, new.payload);
END;
CREATE TRIGGER IF NOT EXISTS session_search_fts_ad AFTER DELETE ON entries BEGIN
  INSERT INTO session_search_fts(session_search_fts, rowid, payload) VALUES('delete', old.rowid, old.payload);
END;
CREATE TRIGGER IF NOT EXISTS session_search_fts_au AFTER UPDATE OF payload ON entries BEGIN
  INSERT INTO session_search_fts(session_search_fts, rowid, payload) VALUES('delete', old.rowid, old.payload);
  INSERT INTO session_search_fts(rowid, payload) VALUES (new.rowid, new.payload);
END;
`

func ensureSearchSchema(db *sql.DB) error {
	ftsExists, err := tableExists(db, "session_search_fts")
	if err != nil {
		return err
	}
	entriesExist, err := tableExists(db, "entries")
	if err != nil {
		return err
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
	if _, err := db.Exec(searchFTS); err != nil {
		return err
	}
	if !ftsExists && entriesExist {
		if _, err := db.Exec(`INSERT INTO session_search_fts(session_search_fts) VALUES('rebuild')`); err != nil {
			return err
		}
	}
	if _, err := db.Exec("COMMIT"); err != nil {
		return err
	}
	ok = true
	return nil
}

// Search is an independent FTS service over the same database file.
type Search struct {
	path string
}

// NewSearch constructs an FTS searcher for databasePath.
func NewSearch(databasePath string) (*Search, error) {
	abs, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to resolve SQLite search database "+databasePath, err)
	}
	return &Search{path: abs}, nil
}

func (s *Search) open(ctx context.Context) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to create SQLite search directory "+filepath.Dir(s.path), err)
	}
	db, err := sql.Open("sqlite", s.path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA synchronous=FULL`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := applyMigrations(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureSearchSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Search runs an FTS query. Blank text does not create the FTS schema.
func (s *Search) Search(ctx context.Context, text string, opts sessionrepo.SearchOptions) ([]sessionrepo.SearchHit, error) {
	queryText := strings.TrimSpace(text)
	if queryText == "" || (opts.HasLimit && opts.Limit <= 0) {
		return nil, nil
	}
	if opts.EntryTypes != nil && len(opts.EntryTypes) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	quoted := `"` + strings.ReplaceAll(queryText, `"`, `""`) + `"`
	q := `SELECT s.id, s.created_at, s.metadata, s.cwd, s.parent_session_id,
			name_fact.seq IS NOT NULL AS has_session_name,
			name_fact.value AS session_name,
			se.id AS entry_id, se.timestamp, bm25(session_search_fts) AS score
		FROM session_search_fts
		JOIN entries AS se ON se.rowid = session_search_fts.rowid
		JOIN sessions AS s ON s.id = se.session_id
		LEFT JOIN facts AS name_fact
			ON name_fact.session_id = s.id
			AND name_fact.kind = 'name'
			AND name_fact.key IS NULL
			AND name_fact.seq = (
				SELECT MAX(f.seq)
				FROM facts AS f
				WHERE f.session_id = s.id AND f.kind = 'name' AND f.key IS NULL
			)
		WHERE session_search_fts MATCH ?`
	args := []any{quoted}
	if len(opts.EntryTypes) > 0 {
		ph := strings.Repeat("?,", len(opts.EntryTypes))
		ph = strings.TrimSuffix(ph, ",")
		q += fmt.Sprintf(` AND se.type IN (%s)`, ph)
		for _, t := range opts.EntryTypes {
			args = append(args, t)
		}
	}
	q += ` ORDER BY score LIMIT ?`
	lim := -1
	if opts.HasLimit {
		lim = opts.Limit
	}
	args = append(args, lim)
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var hits []sessionrepo.SearchHit
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var row sessionRow
		var meta, parent, name sql.NullString
		var entryID string
		var ts int64
		var score float64
		if err := rows.Scan(&row.id, &row.createdAt, &meta, &row.cwd, &parent, &row.hasSessionName, &name, &entryID, &ts, &score); err != nil {
			return nil, err
		}
		row.metadata = nullStr(meta)
		row.parentSessionID = nullStr(parent)
		row.sessionName = nullStr(name)
		md, err := decodeSessionMetadata(&row, s.path)
		if err != nil {
			return nil, err
		}
		hits = append(hits, sessionrepo.SearchHit{
			SessionID: row.id,
			EntryID:   entryID,
			Metadata:  md,
			Timestamp: ts,
			Score:     score,
		})
	}
	return hits, rows.Err()
}
