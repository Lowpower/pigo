package sqlite

import (
	"database/sql"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

type sessionRow struct {
	id              string
	createdAt       int64
	metadata        *string
	cwd             string
	parentSessionID *string
	hasSessionName  int
	sessionName     *string
}

func sessionExists(db *sql.DB, id string) (bool, error) {
	var found int
	err := db.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, id).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func insertSessionRow(db *sql.DB, id string, createdAt int64, cwd string, parent *string, metadata map[string]any) error {
	meta, err := serializeMetadata(metadata)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO sessions (id, created_at, metadata, cwd, parent_session_id) VALUES (?, ?, ?, ?, ?)`,
		id, createdAt, meta, cwd, parent)
	return err
}

func readSessionRow(db *sql.DB, sessionID string) (*sessionRow, error) {
	var r sessionRow
	var meta, parent, name sql.NullString
	err := db.QueryRow(`SELECT s.id, s.created_at, s.metadata, s.cwd, s.parent_session_id,
			name_fact.seq IS NOT NULL AS has_session_name,
			name_fact.value AS session_name
		FROM sessions AS s
		LEFT JOIN facts AS name_fact
			ON name_fact.session_id = s.id
			AND name_fact.kind = 'name'
			AND name_fact.key IS NULL
			AND name_fact.seq = (
				SELECT MAX(f.seq) FROM facts AS f
				WHERE f.session_id = s.id AND f.kind = 'name' AND f.key IS NULL
			)
		WHERE s.id = ?`, sessionID).Scan(&r.id, &r.createdAt, &meta, &r.cwd, &parent, &r.hasSessionName, &name)
	if err == sql.ErrNoRows {
		return nil, sessionrepo.NewError(sessionrepo.ErrNotFound, "Session not found: "+sessionID)
	}
	if err != nil {
		return nil, err
	}
	r.metadata = nullStr(meta)
	r.parentSessionID = nullStr(parent)
	r.sessionName = nullStr(name)
	return &r, nil
}

func readSessionRows(db *sql.DB, cwd string) ([]sessionRow, error) {
	q := `SELECT s.id, s.created_at, s.metadata, s.cwd, s.parent_session_id,
			name_fact.seq IS NOT NULL AS has_session_name,
			name_fact.value AS session_name
		FROM sessions AS s
		LEFT JOIN facts AS name_fact
			ON name_fact.session_id = s.id
			AND name_fact.kind = 'name'
			AND name_fact.key IS NULL
			AND name_fact.seq = (
				SELECT MAX(f.seq) FROM facts AS f
				WHERE f.session_id = s.id AND f.kind = 'name' AND f.key IS NULL
			)`
	var args []any
	if cwd != "" {
		q += ` WHERE s.cwd = ?`
		args = append(args, cwd)
	}
	q += ` ORDER BY s.created_at DESC`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []sessionRow
	for rows.Next() {
		var r sessionRow
		var meta, parent, name sql.NullString
		if err := rows.Scan(&r.id, &r.createdAt, &meta, &r.cwd, &parent, &r.hasSessionName, &name); err != nil {
			return nil, err
		}
		r.metadata = nullStr(meta)
		r.parentSessionID = nullStr(parent)
		r.sessionName = nullStr(name)
		out = append(out, r)
	}
	return out, rows.Err()
}

func decodeSessionMetadata(row *sessionRow, path string) (sessionrepo.Metadata, error) {
	meta, err := parseMetadataJSON(row.metadata, row.id)
	if err != nil {
		return sessionrepo.Metadata{}, err
	}
	m := sessionrepo.Metadata{
		ID:        row.id,
		CreatedAt: row.createdAt,
		CWD:       row.cwd,
		Path:      path,
		Metadata:  meta,
	}
	if row.parentSessionID != nil {
		m.ParentSessionID = *row.parentSessionID
	}
	if row.hasSessionName != 0 {
		name, ok, err := decodeSessionName(row.sessionName, row.id)
		if err != nil {
			return sessionrepo.Metadata{}, err
		}
		if ok {
			m.Name = name
			m.HasName = true
		}
	}
	return m, nil
}

func requireSessionRow(db *sql.DB, id string) (*sessionRow, error) {
	return readSessionRow(db, id)
}

func deleteSessionRow(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func createSequence(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`INSERT INTO session_sequences (session_id, next_seq) VALUES (?, 1)`, sessionID)
	return err
}

func getNextSequence(db *sql.DB, sessionID string) (int64, error) {
	var seq int64
	err := db.QueryRow(`SELECT next_seq FROM session_sequences WHERE session_id = ?`, sessionID).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, sessionrepo.NewError(sessionrepo.ErrStorage, "Missing sequence row for session "+sessionID)
	}
	return seq, err
}

func setNextSequence(db *sql.DB, sessionID string, next int64) error {
	_, err := db.Exec(`UPDATE session_sequences SET next_seq = ? WHERE session_id = ?`, next, sessionID)
	return err
}

func advanceSequence(db *sql.DB, sessionID string, seq int64) error {
	return setNextSequence(db, sessionID, seq+1)
}

func deleteSequence(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM session_sequences WHERE session_id = ?`, sessionID)
	return err
}

func createStats(db *sql.DB, sessionID string, messageCount int) error {
	_, err := db.Exec(`INSERT INTO session_stats
		(session_id, message_count, cached_tokens, uncached_tokens, total_tokens, cost_total)
		VALUES (?, ?, 0, 0, 0, 0)`, sessionID, messageCount)
	return err
}

func readStats(db *sql.DB, sessionID string) (sessionrepo.Stats, error) {
	var s sessionrepo.Stats
	err := db.QueryRow(`SELECT message_count, cached_tokens, uncached_tokens, total_tokens, cost_total
		FROM session_stats WHERE session_id = ?`, sessionID).Scan(
		&s.MessageCount, &s.CachedTokens, &s.UncachedTokens, &s.TotalTokens, &s.CostTotal)
	if err == sql.ErrNoRows {
		return s, sessionrepo.NewError(sessionrepo.ErrStorage, "Missing stats row for session "+sessionID)
	}
	return s, err
}

func incrementMessageCount(db *sql.DB, sessionID string) error {
	res, err := db.Exec(`UPDATE session_stats SET message_count = message_count + 1 WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sessionrepo.NewError(sessionrepo.ErrStorage, "Missing stats row for session "+sessionID)
	}
	return nil
}

func addUsageToStats(db *sql.DB, sessionID string, u sessionrepo.Usage) error {
	res, err := db.Exec(`UPDATE session_stats SET
		cached_tokens = cached_tokens + ?,
		uncached_tokens = uncached_tokens + ?,
		total_tokens = total_tokens + ?,
		cost_total = cost_total + ?
		WHERE session_id = ?`, u.CacheRead, u.Input+u.CacheWrite, u.TotalTokens, u.Cost.Total, sessionID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sessionrepo.NewError(sessionrepo.ErrStorage, "Missing stats row for session "+sessionID)
	}
	return nil
}

func deleteStats(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM session_stats WHERE session_id = ?`, sessionID)
	return err
}

func createInitialLane(db *sql.DB, sessionID, lane string, leafID *string) error {
	_, err := db.Exec(`INSERT INTO lanes (session_id, lane, leaf_id, open_operation_id) VALUES (?, ?, ?, NULL)`,
		sessionID, lane, leafID)
	return err
}

func readLanes(db *sql.DB, sessionID string) ([]sessionrepo.LanePointer, error) {
	rows, err := db.Query(`SELECT l.lane, l.leaf_id,
			(l.leaf_id IS NULL OR EXISTS (
				SELECT 1 FROM entries AS e WHERE e.session_id = l.session_id AND e.id = l.leaf_id
			)) AS leaf_exists
		FROM lanes AS l WHERE l.session_id = ? ORDER BY l.lane`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []sessionrepo.LanePointer
	for rows.Next() {
		var lane string
		var leaf sql.NullString
		var exists int
		if err := rows.Scan(&lane, &leaf, &exists); err != nil {
			return nil, err
		}
		if exists == 0 {
			return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "Lane "+lane+" points at missing entry "+leaf.String)
		}
		out = append(out, sessionrepo.LanePointer{Lane: lane, LeafID: nullStr(leaf)})
	}
	return out, rows.Err()
}

func laneExists(db *sql.DB, sessionID, lane string) (bool, error) {
	var found int
	err := db.QueryRow(`SELECT 1 FROM lanes WHERE session_id = ? AND lane = ?`, sessionID, lane).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func readLaneHead(db *sql.DB, sessionID, lane string) (*string, error) {
	var leaf sql.NullString
	var exists int
	err := db.QueryRow(`SELECT l.leaf_id,
			(l.leaf_id IS NULL OR EXISTS (
				SELECT 1 FROM entries AS e WHERE e.session_id = l.session_id AND e.id = l.leaf_id
			)) AS leaf_exists
		FROM lanes AS l WHERE l.session_id = ? AND l.lane = ?`, sessionID, lane).Scan(&leaf, &exists)
	if err == sql.ErrNoRows {
		return nil, sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: "+lane)
	}
	if err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "Entry "+leaf.String+" not found")
	}
	return nullStr(leaf), nil
}

func insertLane(db *sql.DB, sessionID string, seq int64, lane string, leafID *string) error {
	if _, err := db.Exec(`INSERT INTO lanes (session_id, lane, leaf_id, open_operation_id) VALUES (?, ?, ?, NULL)`,
		sessionID, lane, leafID); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO lane_moves (session_id, seq, lane, leaf_id) VALUES (?, ?, ?, ?)`, sessionID, seq, lane, leafID)
	return err
}

func updateLane(db *sql.DB, sessionID string, seq int64, lane string, leafID *string) error {
	res, err := db.Exec(`UPDATE lanes SET leaf_id = ? WHERE session_id = ? AND lane = ?`, leafID, sessionID, lane)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: "+lane)
	}
	_, err = db.Exec(`INSERT INTO lane_moves (session_id, seq, lane, leaf_id) VALUES (?, ?, ?, ?)`, sessionID, seq, lane, leafID)
	return err
}

func setLaneLeaf(db *sql.DB, sessionID, lane, leafID string) error {
	res, err := db.Exec(`UPDATE lanes SET leaf_id = ? WHERE session_id = ? AND lane = ?`, leafID, sessionID, lane)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: "+lane)
	}
	return nil
}

func startLaneOperation(db *sql.DB, sessionID, lane, runID string) error {
	res, err := db.Exec(`UPDATE lanes SET open_operation_id = ? WHERE session_id = ? AND lane = ? AND open_operation_id IS NULL`,
		runID, sessionID, lane)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var current sql.NullString
	err = db.QueryRow(`SELECT open_operation_id FROM lanes WHERE session_id = ? AND lane = ?`, sessionID, lane).Scan(&current)
	if err == sql.ErrNoRows {
		return sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: "+lane)
	}
	if err != nil {
		return err
	}
	return sessionrepo.NewError(sessionrepo.ErrStorage, "Lane "+lane+" already has an open operation "+current.String)
}

func finishLaneOperation(db *sql.DB, sessionID, lane, runID string) error {
	_, err := db.Exec(`UPDATE lanes SET open_operation_id = NULL WHERE session_id = ? AND lane = ? AND open_operation_id = ?`,
		sessionID, lane, runID)
	return err
}

func deleteLaneRows(db *sql.DB, sessionID string) error {
	if _, err := db.Exec(`DELETE FROM lane_moves WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	_, err := db.Exec(`DELETE FROM lanes WHERE session_id = ?`, sessionID)
	return err
}

type laneMoveRow struct {
	seq    int64
	lane   string
	leafID *string
}

func readLaneMoveRows(db *sql.DB, sessionID string, afterSeq int64, limit *int) ([]laneMoveRow, error) {
	q := `SELECT seq, lane, leaf_id FROM lane_moves WHERE session_id = ? AND seq > ? ORDER BY seq`
	args := []any{sessionID, afterSeq}
	if limit != nil {
		q += ` LIMIT ?`
		args = append(args, *limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []laneMoveRow
	for rows.Next() {
		var r laneMoveRow
		var leaf sql.NullString
		if err := rows.Scan(&r.seq, &r.lane, &leaf); err != nil {
			return nil, err
		}
		r.leafID = nullStr(leaf)
		out = append(out, r)
	}
	return out, rows.Err()
}

func entryExists(db *sql.DB, sessionID, id string) (bool, error) {
	var found int
	err := db.QueryRow(`SELECT 1 FROM entries WHERE session_id = ? AND id = ? LIMIT 1`, sessionID, id).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func idExistsInRecords(db *sql.DB, sessionID, id string) (bool, error) {
	var found int
	err := db.QueryRow(`SELECT 1 FROM records WHERE session_id = ? AND id = ? LIMIT 1`, sessionID, id).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

func assertUnusedID(db *sql.DB, sessionID, id string) error {
	ok, err := entryExists(db, sessionID, id)
	if err != nil {
		return err
	}
	if ok {
		return sessionrepo.NewError(sessionrepo.ErrAlreadyExists, "Entry already exists: "+id)
	}
	ok, err = idExistsInRecords(db, sessionID, id)
	if err != nil {
		return err
	}
	if ok {
		return sessionrepo.NewError(sessionrepo.ErrAlreadyExists, "Entry already exists: "+id)
	}
	return nil
}

func readEntryRow(db *sql.DB, sessionID, id string) (*entryRow, error) {
	var r entryRow
	var parent sql.NullString
	err := db.QueryRow(`SELECT id, seq, parent_id, type, timestamp, payload FROM entries WHERE session_id = ? AND id = ?`,
		sessionID, id).Scan(&r.id, &r.seq, &parent, &r.typ, &r.timestamp, &r.payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.parentID = nullStr(parent)
	return &r, nil
}

type entryReadOpts struct {
	afterSeq int64
	hasAfter bool
	cursor   *sessionrepo.Cursor
	limit    int
	hasLim   bool
	order    sessionrepo.Order
	typ      string
}

func readEntryRows(db *sql.DB, sessionID string, opts entryReadOpts) ([]entryRow, error) {
	oldest := opts.order == sessionrepo.OrderOldestFirst
	q := `SELECT id, seq, parent_id, type, timestamp, payload FROM entries WHERE session_id = ?`
	args := []any{sessionID}
	if opts.hasAfter {
		q += ` AND seq > ?`
		args = append(args, opts.afterSeq)
	}
	if opts.cursor != nil {
		if oldest {
			q += ` AND seq > ?`
		} else {
			q += ` AND seq < ?`
		}
		args = append(args, opts.cursor.AfterSeq)
	}
	if opts.typ != "" {
		q += ` AND type = ?`
		args = append(args, opts.typ)
	}
	dir := "DESC"
	if oldest {
		dir = "ASC"
	}
	q += ` ORDER BY seq ` + dir
	if opts.hasLim {
		q += ` LIMIT ?`
		args = append(args, opts.limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []entryRow
	for rows.Next() {
		var r entryRow
		var parent sql.NullString
		if err := rows.Scan(&r.id, &r.seq, &parent, &r.typ, &r.timestamp, &r.payload); err != nil {
			return nil, err
		}
		r.parentID = nullStr(parent)
		out = append(out, r)
	}
	return out, rows.Err()
}

func insertEntryRow(db *sql.DB, sessionID string, seq int64, id string, parentID *string, typ string, timestamp int64, payload string) error {
	_, err := db.Exec(`INSERT INTO entries (session_id, id, seq, parent_id, type, timestamp, payload)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, sessionID, id, seq, parentID, typ, timestamp, payload)
	return err
}

func deleteEntryRows(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM entries WHERE session_id = ?`, sessionID)
	return err
}

type recRow struct {
	seq       int64
	id        string
	lane      string
	runID     *string
	typ       string
	opKind    *string
	timestamp int64
	payload   string
}

func readRecordRows(db *sql.DB, sessionID string, q sessionrepo.RecordQuery) ([]recRow, error) {
	query := `SELECT seq, id, lane, run_id, type, op_kind, timestamp, payload FROM records WHERE session_id = ?`
	args := []any{sessionID}
	if q.Lane != "" {
		query += ` AND lane = ?`
		args = append(args, q.Lane)
	}
	if q.Type != "" {
		query += ` AND type = ?`
		args = append(args, q.Type)
	}
	if q.HasRunID || q.RunID != "" {
		query += ` AND run_id = ?`
		args = append(args, q.RunID)
	}
	if q.OperationKind != "" {
		query += ` AND op_kind = ?`
		args = append(args, q.OperationKind)
	}
	if q.HasAfterSeq {
		query += ` AND seq > ?`
		args = append(args, q.AfterSeq)
	}
	dir := "DESC"
	if q.Order == sessionrepo.OrderOldestFirst {
		dir = "ASC"
	}
	query += ` ORDER BY seq ` + dir
	if q.HasLimit {
		query += ` LIMIT ?`
		args = append(args, q.Limit)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []recRow
	for rows.Next() {
		var r recRow
		var runID, opKind sql.NullString
		if err := rows.Scan(&r.seq, &r.id, &r.lane, &runID, &r.typ, &opKind, &r.timestamp, &r.payload); err != nil {
			return nil, err
		}
		r.runID = nullStr(runID)
		r.opKind = nullStr(opKind)
		out = append(out, r)
	}
	return out, rows.Err()
}

func readOpenOperationRows(db *sql.DB, sessionID, lane string) ([]recRow, error) {
	var openID sql.NullString
	err := db.QueryRow(`SELECT open_operation_id FROM lanes WHERE session_id = ? AND lane = ?`, sessionID, lane).Scan(&openID)
	if err == sql.ErrNoRows || !openID.Valid {
		if err == sql.ErrNoRows {
			return []recRow{}, nil
		}
		return []recRow{}, err
	}
	var r recRow
	var runID, opKind sql.NullString
	err = db.QueryRow(`SELECT seq, id, lane, run_id, type, op_kind, timestamp, payload FROM records
		WHERE session_id = ? AND id = ?`, sessionID, openID.String).Scan(
		&r.seq, &r.id, &r.lane, &runID, &r.typ, &opKind, &r.timestamp, &r.payload)
	if err == sql.ErrNoRows {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "Lane "+lane+" points at missing open operation "+openID.String)
	}
	if err != nil {
		return nil, err
	}
	if r.lane != lane || r.typ != "operation_started" {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "Lane "+lane+" points at invalid open operation "+openID.String)
	}
	r.runID = nullStr(runID)
	r.opKind = nullStr(opKind)
	return []recRow{r}, nil
}

func deleteRecordRows(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM records WHERE session_id = ?`, sessionID)
	return err
}

type factRow struct {
	seq   int64
	kind  string
	key   *string
	value *string
}

func appendFact(db *sql.DB, sessionID string, seq int64, kind string, key, value *string) error {
	_, err := db.Exec(`INSERT INTO facts (session_id, seq, kind, key, value) VALUES (?, ?, ?, ?, ?)`,
		sessionID, seq, kind, key, value)
	return err
}

func readLatestFact(db *sql.DB, sessionID, kind string, key *string) (*factRow, error) {
	var r factRow
	var k, v sql.NullString
	var err error
	if key == nil {
		err = db.QueryRow(`SELECT seq, kind, key, value FROM facts INDEXED BY idx_facts_session_kind_key_seq
			WHERE session_id = ? AND kind = ? AND key IS NULL ORDER BY seq DESC LIMIT 1`, sessionID, kind).
			Scan(&r.seq, &r.kind, &k, &v)
	} else {
		err = db.QueryRow(`SELECT seq, kind, key, value FROM facts INDEXED BY idx_facts_session_kind_key_seq
			WHERE session_id = ? AND kind = ? AND key = ? ORDER BY seq DESC LIMIT 1`, sessionID, kind, *key).
			Scan(&r.seq, &r.kind, &k, &v)
	}
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.key = nullStr(k)
	r.value = nullStr(v)
	return &r, nil
}

func readLatestLabelFacts(db *sql.DB, sessionID string) ([]factRow, error) {
	rows, err := db.Query(`SELECT f.key, f.value FROM facts AS f INDEXED BY idx_facts_session_kind_key_seq
		WHERE f.session_id = ? AND f.kind = 'label' AND f.value IS NOT NULL
			AND f.seq = (
				SELECT MAX(candidate.seq) FROM facts AS candidate INDEXED BY idx_facts_session_kind_key_seq
				WHERE candidate.session_id = f.session_id AND candidate.kind = f.kind AND candidate.key IS f.key
			)
		ORDER BY f.key`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []factRow
	for rows.Next() {
		var r factRow
		var k, v sql.NullString
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		r.key = nullStr(k)
		r.value = nullStr(v)
		out = append(out, r)
	}
	return out, rows.Err()
}

func readFactRows(db *sql.DB, sessionID string, afterSeq int64, limit *int) ([]factRow, error) {
	q := `SELECT seq, kind, key, value FROM facts WHERE session_id = ? AND seq > ? ORDER BY seq`
	args := []any{sessionID, afterSeq}
	if limit != nil {
		q += ` LIMIT ?`
		args = append(args, *limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []factRow
	for rows.Next() {
		var r factRow
		var k, v sql.NullString
		if err := rows.Scan(&r.seq, &r.kind, &k, &v); err != nil {
			return nil, err
		}
		r.key = nullStr(k)
		r.value = nullStr(v)
		out = append(out, r)
	}
	return out, rows.Err()
}

func deleteFactRows(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM facts WHERE session_id = ?`, sessionID)
	return err
}
