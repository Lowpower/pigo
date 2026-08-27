package sqlite

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

type storage struct {
	repo     *Repository
	meta     sessionrepo.Metadata
	lease    *writerLease
	leaseOpt resolvedLease
	mu       sync.Mutex
	closing  bool
	leaseErr error
	hbStop   chan struct{}
	onceStop sync.Once
}

func newStorage(repo *Repository, meta sessionrepo.Metadata, lease *writerLease) *storage {
	s := &storage{
		repo:     repo,
		meta:     meta,
		lease:    lease,
		leaseOpt: repo.lease,
		hbStop:   make(chan struct{}),
	}
	s.scheduleHeartbeat()
	return s
}

func (s *storage) scheduleHeartbeat() {
	go func() {
		t := time.NewTicker(time.Duration(s.leaseOpt.heartbeatMs) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-s.hbStop:
				return
			case <-t.C:
				s.mu.Lock()
				if s.closing || s.leaseErr != nil {
					s.mu.Unlock()
					return
				}
				now := nowMs()
				var ok bool
				err := s.repo.withDB(func(db *sql.DB) error {
					var e error
					ok, e = renewWriterLease(db, s.meta.ID, s.lease, now, now+s.leaseOpt.ttlMs)
					return e
				})
				if err == nil && !ok {
					s.leaseErr = lostWriterError(s.meta.ID)
				}
				s.mu.Unlock()
			}
		}
	}()
}

func (s *storage) release() {
	s.onceStop.Do(func() { close(s.hbStop) })
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closing = true
	_ = s.repo.immediate(func() error {
		return releaseWriterLease(s.repo.db, s.meta.ID, s.lease)
	})
}

func (s *storage) enqueueWrite(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return closedWriterError(s.meta.ID)
	}
	if s.leaseErr != nil {
		return s.leaseErr
	}
	return s.repo.immediate(func() error {
		now := nowMs()
		ok, err := renewWriterLease(s.repo.db, s.meta.ID, s.lease, now, now+s.leaseOpt.ttlMs)
		if err != nil {
			return err
		}
		if !ok {
			s.leaseErr = lostWriterError(s.meta.ID)
			return s.leaseErr
		}
		return fn()
	})
}

func (s *storage) GetMetadata() (sessionrepo.Metadata, error) {
	var m sessionrepo.Metadata
	err := s.repo.withDB(func(db *sql.DB) error {
		row, err := readSessionRow(db, s.meta.ID)
		if err != nil {
			return err
		}
		decoded, err := decodeSessionMetadata(row, s.meta.Path)
		if err != nil {
			return err
		}
		m = decoded
		return nil
	})
	return m, err
}

func (s *storage) GetLanes() ([]sessionrepo.LanePointer, error) {
	var lanes []sessionrepo.LanePointer
	err := s.repo.withDB(func(db *sql.DB) error {
		var e error
		lanes, e = readLanes(db, s.meta.ID)
		return e
	})
	return lanes, err
}

func (s *storage) CreateLane(lane string, at *string) error {
	return s.enqueueWrite(func() error {
		exists, err := laneExists(s.repo.db, s.meta.ID, lane)
		if err != nil {
			return err
		}
		if exists {
			return sessionrepo.NewError(sessionrepo.ErrAlreadyExists, "Lane already exists: "+lane)
		}
		if at != nil {
			ok, err := entryExists(s.repo.db, s.meta.ID, *at)
			if err != nil {
				return err
			}
			if !ok {
				return sessionrepo.NewError(sessionrepo.ErrNotFound, "Entry not found: "+*at)
			}
		}
		seq, err := getNextSequence(s.repo.db, s.meta.ID)
		if err != nil {
			return err
		}
		if err := insertLane(s.repo.db, s.meta.ID, seq, lane, at); err != nil {
			return err
		}
		return advanceSequence(s.repo.db, s.meta.ID, seq)
	})
}

func (s *storage) MoveLane(lane string, to *string) error {
	return s.enqueueWrite(func() error {
		exists, err := laneExists(s.repo.db, s.meta.ID, lane)
		if err != nil {
			return err
		}
		if !exists {
			return sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: "+lane)
		}
		if to != nil {
			ok, err := entryExists(s.repo.db, s.meta.ID, *to)
			if err != nil {
				return err
			}
			if !ok {
				return sessionrepo.NewError(sessionrepo.ErrNotFound, "Entry not found: "+*to)
			}
		}
		seq, err := getNextSequence(s.repo.db, s.meta.ID)
		if err != nil {
			return err
		}
		if err := updateLane(s.repo.db, s.meta.ID, seq, lane, to); err != nil {
			return err
		}
		return advanceSequence(s.repo.db, s.meta.ID, seq)
	})
}

func (s *storage) AppendEntry(entry sessionrepo.Entry, lane string) (sessionrepo.Entry, error) {
	var committed sessionrepo.Entry
	err := s.enqueueWrite(func() error {
		parent, err := readLaneHead(s.repo.db, s.meta.ID, lane)
		if err != nil {
			return err
		}
		if err := assertUnusedID(s.repo.db, s.meta.ID, entry.ID); err != nil {
			return err
		}
		seq, err := getNextSequence(s.repo.db, s.meta.ID)
		if err != nil {
			return err
		}
		committed = entry
		committed.ParentID = parent
		committed.Seq = seq
		committed.Timestamp = nowMs()
		payload, err := entryPayloadJSON(committed)
		if err != nil {
			return err
		}
		if _, err := s.repo.db.Exec(`INSERT INTO entries (session_id, id, seq, parent_id, type, timestamp, payload)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, s.meta.ID, committed.ID, seq, committed.ParentID, committed.Type, committed.Timestamp, payload); err != nil {
			return err
		}
		if err := setLaneLeaf(s.repo.db, s.meta.ID, lane, committed.ID); err != nil {
			return err
		}
		var ct *string
		if committed.Type == "custom" {
			ct = &committed.CustomType
		}
		if err := appendEntryToBranchCache(s.repo.db, s.meta.ID, committed.ID, seq, committed.Type, ct, committed.ParentID); err != nil {
			return err
		}
		if committed.Type == "message" {
			if err := incrementMessageCount(s.repo.db, s.meta.ID); err != nil {
				return err
			}
		}
		return advanceSequence(s.repo.db, s.meta.ID, seq)
	})
	return committed, err
}

func (s *storage) AppendRecord(record sessionrepo.Record) (sessionrepo.Record, error) {
	var committed sessionrepo.Record
	err := s.enqueueWrite(func() error {
		exists, err := laneExists(s.repo.db, s.meta.ID, record.Lane)
		if err != nil {
			return err
		}
		if !exists {
			return sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: "+record.Lane)
		}
		if err := assertUnusedID(s.repo.db, s.meta.ID, record.ID); err != nil {
			return err
		}
		seq, err := getNextSequence(s.repo.db, s.meta.ID)
		if err != nil {
			return err
		}
		committed = record
		committed.Seq = seq
		committed.Timestamp = nowMs()
		if record.Type == "operation_started" {
			if err := startLaneOperation(s.repo.db, s.meta.ID, record.Lane, record.ID); err != nil {
				return err
			}
		}
		payload, err := recordPayloadJSON(record)
		if err != nil {
			return err
		}
		runID := recordRunID(record)
		opKind := recordOpKind(record)
		if _, err := s.repo.db.Exec(`INSERT INTO records
			(session_id, seq, id, lane, run_id, type, op_kind, timestamp, payload)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			s.meta.ID, seq, record.ID, record.Lane, runID, record.Type, opKind, committed.Timestamp, payload); err != nil {
			return err
		}
		if record.Type == "operation_finished" {
			if err := finishLaneOperation(s.repo.db, s.meta.ID, record.Lane, record.RunID); err != nil {
				return err
			}
		}
		if record.Type == "usage" && record.Usage != nil {
			if err := addUsageToStats(s.repo.db, s.meta.ID, *record.Usage); err != nil {
				return err
			}
		}
		return advanceSequence(s.repo.db, s.meta.ID, seq)
	})
	return committed, err
}

func (s *storage) GetEntry(id string) (*sessionrepo.Entry, error) {
	var entry *sessionrepo.Entry
	err := s.repo.withDB(func(db *sql.DB) error {
		row, err := readEntryRow(db, s.meta.ID, id)
		if err != nil || row == nil {
			return err
		}
		e, err := decodeEntry(*row)
		if err != nil {
			return err
		}
		entry = &e
		return nil
	})
	return entry, err
}

func (s *storage) FindEntries(query sessionrepo.EntryQuery) ([]sessionrepo.Entry, error) {
	var ents []sessionrepo.Entry
	err := s.repo.withDB(func(db *sql.DB) error {
		sqlType := query.Type
		if sqlType == "" && query.CustomType != "" {
			sqlType = "custom"
		}
		sqlLimit := query.HasLimit
		limit := query.Limit
		if query.CustomType != "" {
			sqlLimit = false
		}
		rows, err := readEntryRows(db, s.meta.ID, entryReadOpts{
			cursor: query.Cursor,
			limit:  limit,
			hasLim: sqlLimit,
			order:  query.Order,
			typ:    sqlType,
		})
		if err != nil {
			return err
		}
		ents = nil
		for _, row := range rows {
			e, err := decodeEntry(row)
			if err != nil {
				return err
			}
			if matchesEntryQuery(e, query) {
				ents = append(ents, e)
			}
		}
		if query.HasLimit && len(ents) > query.Limit {
			ents = ents[:query.Limit]
		}
		return nil
	})
	return ents, err
}

func (s *storage) FindEntriesOnBranch(query sessionrepo.EntryQuery) ([]sessionrepo.Entry, error) {
	var ents []sessionrepo.Entry
	err := s.repo.withDB(func(db *sql.DB) error {
		cached, err := readCachedBranch(db, s.meta.ID, query.Start)
		if err != nil {
			return err
		}
		if cached == nil {
			ok, err := entryExists(db, s.meta.ID, query.Start)
			if err != nil {
				return err
			}
			if !ok {
				return sessionrepo.NewError(sessionrepo.ErrNotFound, "Entry not found: "+query.Start)
			}
			return sessionrepo.NewError(sessionrepo.ErrInvalidEntry, "Branch cache missing entry "+query.Start)
		}
		rows, err := queryCachedBranchRows(db, s.meta.ID, cached, query)
		if err != nil {
			return err
		}
		if err := validateCachedBranchRows(rows, query); err != nil {
			return err
		}
		ents = nil
		for _, row := range rows {
			e, err := decodeEntry(row.entryRow)
			if err != nil {
				return err
			}
			if matchesEntryQuery(e, query) {
				ents = append(ents, e)
			}
		}
		if query.HasLimit && len(ents) > query.Limit {
			ents = ents[:query.Limit]
		}
		return nil
	})
	return ents, err
}

func (s *storage) FindRecords(query sessionrepo.RecordQuery) ([]sessionrepo.Record, error) {
	var out []sessionrepo.Record
	err := s.repo.withDB(func(db *sql.DB) error {
		rows, err := readRecordRows(db, s.meta.ID, query)
		if err != nil {
			return err
		}
		out = make([]sessionrepo.Record, 0, len(rows))
		for _, row := range rows {
			r, err := decodeRecord(row.seq, row.timestamp, row.payload)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

func (s *storage) FindOpenOperations(lane string, opts sessionrepo.OpenOpOptions) ([]sessionrepo.Record, error) {
	var out []sessionrepo.Record
	err := s.repo.withDB(func(db *sql.DB) error {
		rows, err := readOpenOperationRows(db, s.meta.ID, lane)
		if err != nil {
			return err
		}
		out = make([]sessionrepo.Record, 0, len(rows))
		for _, row := range rows {
			r, err := decodeRecord(row.seq, row.timestamp, row.payload)
			if err != nil {
				return err
			}
			if r.Type != "operation_started" {
				return sessionrepo.NewError(sessionrepo.ErrStorage, "Expected operation_started record")
			}
			out = append(out, r)
		}
		if opts.HasLimit && len(out) > opts.Limit {
			out = out[:opts.Limit]
		}
		return nil
	})
	return out, err
}

type logRow struct {
	seq    int64
	decode func() (sessionrepo.LogItem, error)
}

func (s *storage) GetLog(opts sessionrepo.LogOptions) ([]sessionrepo.LogItem, error) {
	var out []sessionrepo.LogItem
	err := s.repo.withDB(func(db *sql.DB) error {
		after := int64(0)
		if opts.HasAfter {
			after = opts.AfterSeq
		}
		var lim *int
		if opts.HasLimit {
			lim = &opts.Limit
		}
		entries, err := readEntryRows(db, s.meta.ID, entryReadOpts{afterSeq: after, hasAfter: true, order: sessionrepo.OrderOldestFirst, limit: opts.Limit, hasLim: opts.HasLimit})
		if err != nil {
			return err
		}
		recs, err := readRecordRows(db, s.meta.ID, sessionrepo.RecordQuery{AfterSeq: after, HasAfterSeq: true, Order: sessionrepo.OrderOldestFirst, Limit: opts.Limit, HasLimit: opts.HasLimit})
		if err != nil {
			return err
		}
		lanes, err := readLaneMoveRows(db, s.meta.ID, after, lim)
		if err != nil {
			return err
		}
		facts, err := readFactRows(db, s.meta.ID, after, lim)
		if err != nil {
			return err
		}
		var logRows []logRow
		for _, row := range entries {
			row := row
			logRows = append(logRows, logRow{seq: row.seq, decode: func() (sessionrepo.LogItem, error) {
				e, err := decodeEntry(row)
				if err != nil {
					return sessionrepo.LogItem{}, err
				}
				return sessionrepo.LogItem{Kind: "entry", Seq: row.seq, Entry: &e}, nil
			}})
		}
		for _, row := range recs {
			row := row
			logRows = append(logRows, logRow{seq: row.seq, decode: func() (sessionrepo.LogItem, error) {
				r, err := decodeRecord(row.seq, row.timestamp, row.payload)
				if err != nil {
					return sessionrepo.LogItem{}, err
				}
				return sessionrepo.LogItem{Kind: "record", Seq: row.seq, Record: &r}, nil
			}})
		}
		for _, row := range lanes {
			row := row
			logRows = append(logRows, logRow{seq: row.seq, decode: func() (sessionrepo.LogItem, error) {
				return sessionrepo.LogItem{Kind: "lane", Seq: row.seq, Lane: row.lane, LeafID: row.leafID}, nil
			}})
		}
		for _, row := range facts {
			row := row
			logRows = append(logRows, logRow{seq: row.seq, decode: func() (sessionrepo.LogItem, error) {
				if row.kind == "name" {
					var name *string
					if row.value != nil {
						var parsed string
						if err := json.Unmarshal([]byte(*row.value), &parsed); err != nil {
							return sessionrepo.LogItem{}, err
						}
						name = &parsed
					}
					return sessionrepo.LogItem{Kind: "fact", Seq: row.seq, Fact: "name", Name: name}, nil
				}
				var label *string
				if row.value != nil {
					var parsed string
					if err := json.Unmarshal([]byte(*row.value), &parsed); err != nil {
						return sessionrepo.LogItem{}, err
					}
					label = &parsed
				}
				tid := ""
				if row.key != nil {
					tid = *row.key
				}
				return sessionrepo.LogItem{Kind: "fact", Seq: row.seq, Fact: "label", TargetID: tid, Label: label}, nil
			}})
		}
		for i := 0; i < len(logRows); i++ {
			for j := i + 1; j < len(logRows); j++ {
				if logRows[i].seq > logRows[j].seq {
					logRows[i], logRows[j] = logRows[j], logRows[i]
				}
			}
		}
		if lim != nil && len(logRows) > *lim {
			logRows = logRows[:*lim]
		}
		out = make([]sessionrepo.LogItem, 0, len(logRows))
		for _, row := range logRows {
			item, err := row.decode()
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		return nil
	})
	return out, err
}

func (s *storage) GetName() (*string, error) {
	var parsed *string
	err := s.repo.withDB(func(db *sql.DB) error {
		row, err := readLatestFact(db, s.meta.ID, "name", nil)
		if err != nil || row == nil || row.value == nil {
			return err
		}
		var name string
		if err := json.Unmarshal([]byte(*row.value), &name); err != nil {
			return err
		}
		parsed = &name
		return nil
	})
	return parsed, err
}

func (s *storage) SetName(name *string) error {
	return s.enqueueWrite(func() error {
		seq, err := getNextSequence(s.repo.db, s.meta.ID)
		if err != nil {
			return err
		}
		var val *string
		if name != nil {
			b, err := json.Marshal(*name)
			if err != nil {
				return err
			}
			s := string(b)
			val = &s
		}
		if err := appendFact(s.repo.db, s.meta.ID, seq, "name", nil, val); err != nil {
			return err
		}
		return advanceSequence(s.repo.db, s.meta.ID, seq)
	})
}

func (s *storage) GetLabel(id string) (*string, error) {
	var parsed *string
	err := s.repo.withDB(func(db *sql.DB) error {
		row, err := readLatestFact(db, s.meta.ID, "label", &id)
		if err != nil || row == nil || row.value == nil {
			return err
		}
		var label string
		if err := json.Unmarshal([]byte(*row.value), &label); err != nil {
			return err
		}
		parsed = &label
		return nil
	})
	return parsed, err
}

func (s *storage) SetLabel(id string, label *string) error {
	return s.enqueueWrite(func() error {
		ok, err := entryExists(s.repo.db, s.meta.ID, id)
		if err != nil {
			return err
		}
		if !ok {
			return sessionrepo.NewError(sessionrepo.ErrNotFound, "Entry not found: "+id)
		}
		seq, err := getNextSequence(s.repo.db, s.meta.ID)
		if err != nil {
			return err
		}
		var val *string
		if label != nil {
			b, err := json.Marshal(*label)
			if err != nil {
				return err
			}
			s := string(b)
			val = &s
		}
		if err := appendFact(s.repo.db, s.meta.ID, seq, "label", &id, val); err != nil {
			return err
		}
		return advanceSequence(s.repo.db, s.meta.ID, seq)
	})
}

func (s *storage) GetStats() (sessionrepo.Stats, error) {
	var st sessionrepo.Stats
	err := s.repo.withDB(func(db *sql.DB) error {
		var e error
		st, e = readStats(db, s.meta.ID)
		return e
	})
	return st, err
}
