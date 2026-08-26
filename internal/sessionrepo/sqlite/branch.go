package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

type cachedBranch struct {
	branchID string
	leafSeq  int64
}

func deleteBranchCache(db *sql.DB, sessionID string) error {
	if _, err := db.Exec(`DELETE FROM branch_tips WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM branch_entries WHERE session_id = ?`, sessionID)
	return err
}

func insertBranchEntry(db *sql.DB, sessionID, branchID, entryID string, entrySeq int64, entryType string, customType *string) error {
	_, err := db.Exec(`INSERT INTO branch_entries
		(session_id, branch_id, entry_id, entry_seq, entry_type, custom_type)
		VALUES (?, ?, ?, ?, ?, ?)`, sessionID, branchID, entryID, entrySeq, entryType, customType)
	return err
}

func insertBranchTip(db *sql.DB, sessionID, tipID, branchID string) error {
	_, err := db.Exec(`INSERT INTO branch_tips (session_id, tip_id, branch_id) VALUES (?, ?, ?)`, sessionID, tipID, branchID)
	return err
}

func updateBranchTip(db *sql.DB, sessionID, branchID, oldTipID, newTipID string) (bool, error) {
	res, err := db.Exec(`UPDATE branch_tips SET tip_id = ? WHERE session_id = ? AND branch_id = ? AND tip_id = ?`,
		newTipID, sessionID, branchID, oldTipID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func readBranchTipBranchID(db *sql.DB, sessionID, tipID string) (string, bool, error) {
	var id string
	err := db.QueryRow(`SELECT branch_id FROM branch_tips WHERE session_id = ? AND tip_id = ?`, sessionID, tipID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return id, err == nil, err
}

func readBranchTipIDs(db *sql.DB, sessionID string) ([]string, error) {
	rows, err := db.Query(`SELECT tip_id FROM branch_tips WHERE session_id = ? ORDER BY tip_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func readBranchContainingEntry(db *sql.DB, sessionID, entryID string) (branchID string, entrySeq int64, ok bool, err error) {
	err = db.QueryRow(`SELECT b.branch_id, b.entry_seq FROM branch_entries AS b
		WHERE b.session_id = ? AND b.entry_id = ? ORDER BY b.branch_id LIMIT 1`, sessionID, entryID).Scan(&branchID, &entrySeq)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	return branchID, entrySeq, err == nil, err
}

func readCachedBranch(db *sql.DB, sessionID, leafID string) (*cachedBranch, error) {
	var b cachedBranch
	err := db.QueryRow(`SELECT branch_id, entry_seq FROM branch_entries
		WHERE session_id = ? AND entry_id = ? ORDER BY branch_id LIMIT 1`, sessionID, leafID).Scan(&b.branchID, &b.leafSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func copyBranchEntriesThroughSeq(db *sql.DB, sessionID, targetBranchID, sourceBranchID string, throughSeq int64) error {
	_, err := db.Exec(`INSERT INTO branch_entries (session_id, branch_id, entry_id, entry_seq, entry_type, custom_type)
		SELECT session_id, ?, entry_id, entry_seq, entry_type, custom_type
		FROM branch_entries
		WHERE session_id = ? AND branch_id = ? AND entry_seq <= ?`, targetBranchID, sessionID, sourceBranchID, throughSeq)
	return err
}

func customTypeFromPayload(typ, payload, id string) (*string, error) {
	if typ != "custom" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(payload), &m); err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrInvalidEntry,
			fmt.Sprintf("Invalid SQLite session entry %s: failed to decode entry %s", id, id), err)
	}
	ct, ok := m["customType"].(string)
	if !ok {
		return nil, sessionrepo.NewError(sessionrepo.ErrInvalidEntry,
			fmt.Sprintf("Invalid SQLite session entry %s: failed to decode entry %s", id, id))
	}
	return &ct, nil
}

func insertBranchEntriesForPath(db *sql.DB, sessionID, branchID, leafID string) error {
	type pathRow struct {
		id, typ, payload string
		seq              int64
		parentID         *string
	}
	var path []pathRow
	seen := map[string]struct{}{}
	entryID := &leafID
	for entryID != nil {
		if _, ok := seen[*entryID]; ok {
			return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Entry parent cycle at "+*entryID)
		}
		seen[*entryID] = struct{}{}
		var row pathRow
		var parent sql.NullString
		err := db.QueryRow(`SELECT id, seq, parent_id, type, payload FROM entries WHERE session_id = ? AND id = ?`,
			sessionID, *entryID).Scan(&row.id, &row.seq, &parent, &row.typ, &row.payload)
		if err == sql.ErrNoRows {
			return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Entry "+*entryID+" not found")
		}
		if err != nil {
			return err
		}
		row.parentID = nullStr(parent)
		path = append(path, row)
		entryID = row.parentID
	}
	for i := len(path) - 1; i >= 0; i-- {
		row := path[i]
		ct, err := customTypeFromPayload(row.typ, row.payload, row.id)
		if err != nil {
			return err
		}
		if err := insertBranchEntry(db, sessionID, branchID, row.id, row.seq, row.typ, ct); err != nil {
			return err
		}
	}
	return nil
}

func buildCachedBranch(db *sql.DB, sessionID, leafID string) error {
	if _, err := db.Exec(`SAVEPOINT build_branch_cache`); err != nil {
		return err
	}
	fail := func(err error) error {
		_, _ = db.Exec(`ROLLBACK TO SAVEPOINT build_branch_cache`)
		_, _ = db.Exec(`RELEASE SAVEPOINT build_branch_cache`)
		if se, ok := err.(*sessionrepo.Error); ok {
			return se
		}
		return sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to build SQLite branch cache at entry "+leafID, err)
	}
	branchID := sessionrepo.NewID()
	if err := insertBranchEntriesForPath(db, sessionID, branchID, leafID); err != nil {
		return fail(err)
	}
	if err := insertBranchTip(db, sessionID, leafID, branchID); err != nil {
		return fail(err)
	}
	if _, err := db.Exec(`RELEASE SAVEPOINT build_branch_cache`); err != nil {
		return fail(err)
	}
	return nil
}

func rebuildBranchCache(db *sql.DB, sessionID string) error {
	rows, err := db.Query(`SELECT leaf.id FROM entries AS leaf
		WHERE leaf.session_id = ?
			AND NOT EXISTS (
				SELECT 1 FROM entries AS child WHERE child.session_id = leaf.session_id AND child.parent_id = leaf.id
			)
		ORDER BY leaf.seq`, sessionID)
	if err != nil {
		return err
	}
	var tips []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		tips = append(tips, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	if err := deleteBranchCache(db, sessionID); err != nil {
		return err
	}
	for _, tip := range tips {
		if err := buildCachedBranch(db, sessionID, tip); err != nil {
			return err
		}
	}
	return nil
}

func appendEntryToBranchCache(db *sql.DB, sessionID, entryID string, entrySeq int64, entryType string, customType *string, parentID *string) error {
	if parentID == nil {
		branchID := sessionrepo.NewID()
		if err := insertBranchEntry(db, sessionID, branchID, entryID, entrySeq, entryType, customType); err != nil {
			return err
		}
		return insertBranchTip(db, sessionID, entryID, branchID)
	}
	tipBranchID, ok, err := readBranchTipBranchID(db, sessionID, *parentID)
	if err != nil {
		return err
	}
	if ok {
		if err := insertBranchEntry(db, sessionID, tipBranchID, entryID, entrySeq, entryType, customType); err != nil {
			return err
		}
		changed, err := updateBranchTip(db, sessionID, tipBranchID, *parentID, entryID)
		if err != nil {
			return err
		}
		if !changed {
			return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Branch tip "+*parentID+" changed during append")
		}
		return nil
	}
	srcID, srcSeq, found, err := readBranchContainingEntry(db, sessionID, *parentID)
	if err != nil {
		return err
	}
	if !found {
		return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Branch cache has no branch containing parent entry "+*parentID)
	}
	branchID := sessionrepo.NewID()
	if err := copyBranchEntriesThroughSeq(db, sessionID, branchID, srcID, srcSeq); err != nil {
		return err
	}
	if err := insertBranchEntry(db, sessionID, branchID, entryID, entrySeq, entryType, customType); err != nil {
		return err
	}
	return insertBranchTip(db, sessionID, entryID, branchID)
}

type cachedBranchEntryRow struct {
	entryRow
}

func queryCachedBranchRows(db *sql.DB, sessionID string, branch *cachedBranch, q sessionrepo.EntryQuery) ([]cachedBranchEntryRow, error) {
	oldestFirst := q.Order == sessionrepo.OrderOldestFirst
	args := []any{sessionID, branch.branchID, branch.leafSeq}
	pred := `b.session_id = ? AND b.branch_id = ? AND b.entry_seq <= ?`
	if q.StopAtType != "" || q.StopAtID != "" {
		stopPred := []string{}
		stopArgs := []any{}
		if q.StopAtType != "" {
			stopPred = append(stopPred, `stop.entry_type = ?`)
			stopArgs = append(stopArgs, q.StopAtType)
		}
		if q.StopAtID != "" {
			stopPred = append(stopPred, `stop.entry_id = ?`)
			stopArgs = append(stopArgs, q.StopAtID)
		}
		agg := "MAX"
		cmp := ">="
		coalesce := any(int64(0))
		if oldestFirst {
			agg = "MIN"
			cmp = "<="
			coalesce = branch.leafSeq
		}
		boundary := fmt.Sprintf(`SELECT %s(stop.entry_seq) FROM branch_entries AS stop
			WHERE stop.session_id = ? AND stop.branch_id = ? AND stop.entry_seq <= ? AND (%s)`,
			agg, joinOr(stopPred))
		pred += fmt.Sprintf(` AND b.entry_seq %s COALESCE((%s), ?)`, cmp, boundary)
		args = append(args, sessionID, branch.branchID, branch.leafSeq)
		args = append(args, stopArgs...)
		args = append(args, coalesce)
	}
	if q.Cursor != nil {
		if oldestFirst {
			pred += ` AND b.entry_seq > ?`
		} else {
			pred += ` AND b.entry_seq < ?`
		}
		args = append(args, q.Cursor.AfterSeq)
	}
	if q.Type != "" {
		pred += ` AND b.entry_type = ?`
		args = append(args, q.Type)
	}
	if q.CustomType != "" {
		pred += ` AND b.custom_type = ?`
		args = append(args, q.CustomType)
	}
	dir := "DESC"
	if oldestFirst {
		dir = "ASC"
	}
	limit := ""
	if q.HasLimit {
		limit = " LIMIT ?"
		args = append(args, q.Limit)
	}
	query := fmt.Sprintf(`SELECT e.id, e.seq, e.parent_id, e.type, e.timestamp, e.payload
		FROM branch_entries AS b
		JOIN entries AS e ON e.session_id = b.session_id AND e.id = b.entry_id
		WHERE %s
		ORDER BY b.entry_seq %s%s`, pred, dir, limit)
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []cachedBranchEntryRow
	for rows.Next() {
		var r cachedBranchEntryRow
		var parent sql.NullString
		if err := rows.Scan(&r.id, &r.seq, &parent, &r.typ, &r.timestamp, &r.payload); err != nil {
			return nil, err
		}
		r.parentID = nullStr(parent)
		out = append(out, r)
	}
	return out, rows.Err()
}

func joinOr(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " OR "
		}
		out += p
	}
	return out
}

func validateCachedBranchRows(rows []cachedBranchEntryRow, q sessionrepo.EntryQuery) error {
	if len(rows) == 0 || q.Type != "" || q.CustomType != "" {
		return nil
	}
	path := append([]cachedBranchEntryRow(nil), rows...)
	for i := 0; i < len(path); i++ {
		for j := i + 1; j < len(path); j++ {
			if path[i].seq > path[j].seq {
				path[i], path[j] = path[j], path[i]
			}
		}
	}
	shouldIncludeRoot := q.StopAtID == "" && q.StopAtType == "" && q.Cursor == nil &&
		(q.Order == sessionrepo.OrderOldestFirst || !q.HasLimit)
	if shouldIncludeRoot && path[0].parentID != nil {
		return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Entry "+*path[0].parentID+" not found")
	}
	for i := 1; i < len(path); i++ {
		prev := path[i-1]
		cur := path[i]
		if cur.parentID == nil || *cur.parentID != prev.id {
			missing := ""
			if cur.parentID != nil {
				missing = *cur.parentID
			}
			return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Entry "+missing+" not found")
		}
	}
	return nil
}
