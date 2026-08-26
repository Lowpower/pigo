package sqlite

import (
	"github.com/Lowpower/pigo/internal/sessionrepo"
)

func (r *Repository) sessionFromLease(meta sessionrepo.Metadata, lease *writerLease) *sessionrepo.Session {
	st := newStorage(r, meta, lease)
	r.active[meta.ID] = st
	return sessionrepo.NewSession(st, nil)
}

func (r *Repository) claimSession(meta sessionrepo.Metadata) (*sessionrepo.Session, error) {
	if st, ok := r.active[meta.ID]; ok {
		if _, err := readLanes(r.db, meta.ID); err != nil {
			return nil, err
		}
		return sessionrepo.NewSession(st, nil), nil
	}
	var st *storage
	err := r.immediate(func() error {
		if _, err := requireSessionRow(r.db, meta.ID); err != nil {
			return err
		}
		lease, err := claimWriterLease(r.db, meta.ID, r.lease)
		if err != nil {
			return err
		}
		row, err := requireSessionRow(r.db, meta.ID)
		if err != nil {
			return err
		}
		if _, err := readLanes(r.db, meta.ID); err != nil {
			return err
		}
		decoded, err := decodeSessionMetadata(row, r.absPath)
		if err != nil {
			return err
		}
		st = newStorage(r, decoded, lease)
		return nil
	})
	if err != nil {
		return nil, err
	}
	r.active[meta.ID] = st
	return sessionrepo.NewSession(st, nil), nil
}

func (r *Repository) releaseStorages(sessionID string) {
	if st, ok := r.active[sessionID]; ok {
		st.release()
		delete(r.active, sessionID)
	}
}

// Create inserts a new session and opens it for writing.
func (r *Repository) Create(opts sessionrepo.CreateOptions) (*sessionrepo.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "repository is closed")
	}
	if _, err := r.getDB(); err != nil {
		return nil, err
	}
	id := opts.ID
	if id == "" {
		id = sessionrepo.NewID()
	}
	exists, err := sessionExists(r.db, id)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, sessionrepo.NewError(sessionrepo.ErrAlreadyExists, "Session already exists: "+id)
	}
	createdAt := nowMs()
	var parent *string
	if opts.ParentSessionID != "" {
		parent = &opts.ParentSessionID
	}
	var lease *writerLease
	err = r.immediate(func() error {
		if err := insertSessionRow(r.db, id, createdAt, opts.CWD, parent, opts.Metadata); err != nil {
			return err
		}
		if err := createSequence(r.db, id); err != nil {
			return err
		}
		if err := createStats(r.db, id, 0); err != nil {
			return err
		}
		if err := createInitialLane(r.db, id, sessionrepo.MainLane, nil); err != nil {
			return err
		}
		var err error
		lease, err = claimWriterLease(r.db, id, r.lease)
		return err
	})
	if err != nil {
		return nil, wrap(err, sessionrepo.ErrStorage, "Failed to create SQLite session "+id)
	}
	row, err := requireSessionRow(r.db, id)
	if err != nil {
		return nil, err
	}
	meta, err := decodeSessionMetadata(row, r.absPath)
	if err != nil {
		return nil, err
	}
	return r.sessionFromLease(meta, lease), nil
}

// Open acquires a writer lease for an existing session.
func (r *Repository) Open(meta sessionrepo.Metadata) (*sessionrepo.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage, "repository is closed")
	}
	if _, err := r.getDB(); err != nil {
		return nil, err
	}
	return r.claimSession(meta)
}

// RepairBranchCache rebuilds derived branch cache from parent links.
func (r *Repository) RepairBranchCache(meta sessionrepo.Metadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseStorages(meta.ID)
	if _, err := r.getDB(); err != nil {
		return err
	}
	return r.immediate(func() error {
		lease, err := claimWriterLease(r.db, meta.ID, r.lease)
		if err != nil {
			return err
		}
		if _, err := requireSessionRow(r.db, meta.ID); err != nil {
			return err
		}
		if err := rebuildBranchCache(r.db, meta.ID); err != nil {
			return err
		}
		return releaseWriterLease(r.db, meta.ID, lease)
	})
}

// List returns session metadata without acquiring writer leases.
func (r *Repository) List(opts sessionrepo.ListOptions) ([]sessionrepo.Metadata, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !fileExists(r.absPath) {
		return []sessionrepo.Metadata{}, nil
	}
	if _, err := r.getDB(); err != nil {
		return nil, err
	}
	rows, err := readSessionRows(r.db, opts.CWD)
	if err != nil {
		return nil, err
	}
	out := make([]sessionrepo.Metadata, 0, len(rows))
	for i := range rows {
		m, err := decodeSessionMetadata(&rows[i], r.absPath)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Delete removes a session. Missing sessions are a no-op.
func (r *Repository) Delete(meta sessionrepo.Metadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseStorages(meta.ID)
	if _, err := r.getDB(); err != nil {
		return err
	}
	return r.immediate(func() error {
		exists, err := sessionExists(r.db, meta.ID)
		if err != nil {
			return err
		}
		if !exists {
			return deleteWriterLease(r.db, meta.ID)
		}
		if _, err := claimWriterLease(r.db, meta.ID, r.lease); err != nil {
			return err
		}
		if err := deleteBranchCache(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteFactRows(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteLaneRows(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteRecordRows(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteEntryRows(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteWriterLease(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteStats(r.db, meta.ID); err != nil {
			return err
		}
		if err := deleteSequence(r.db, meta.ID); err != nil {
			return err
		}
		return deleteSessionRow(r.db, meta.ID)
	})
}

// Fork copies a branch or the full tree into a new session.
func (r *Repository) Fork(source sessionrepo.Metadata, opts sessionrepo.ForkOptions) (*sessionrepo.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.getDB(); err != nil {
		return nil, err
	}
	srcRow, err := requireSessionRow(r.db, source.ID)
	if err != nil {
		return nil, err
	}
	sourceMeta, err := decodeSessionMetadata(srcRow, r.absPath)
	if err != nil {
		return nil, err
	}
	id := opts.ID
	if id == "" {
		id = sessionrepo.NewID()
	}
	exists, err := sessionExists(r.db, id)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, sessionrepo.NewError(sessionrepo.ErrAlreadyExists, "Session already exists: "+id)
	}

	var entries []entryRow
	var lanes []sessionrepo.LanePointer
	var branchTips []string
	var branchForkTargetID *string

	if opts.Scope == "tree" {
		entries, err = readEntryRows(r.db, source.ID, entryReadOpts{order: sessionrepo.OrderOldestFirst})
		if err != nil {
			return nil, err
		}
		lanes, err = readLanes(r.db, source.ID)
		if err != nil {
			return nil, err
		}
		branchTips, err = readBranchTipIDs(r.db, source.ID)
		if err != nil {
			return nil, err
		}
	} else {
		var found bool
		found, err = laneExists(r.db, source.ID, sessionrepo.MainLane)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, sessionrepo.NewError(sessionrepo.ErrInvalidLane, "Lane not found: main")
		}
		leaf, err := readLaneHead(r.db, source.ID, sessionrepo.MainLane)
		if err != nil {
			return nil, err
		}
		selected := leaf
		if opts.HasEntry {
			selected = &opts.EntryID
		}
		if selected != nil {
			target, err := readEntryRow(r.db, source.ID, *selected)
			if err != nil {
				return nil, err
			}
			if target == nil || target.typ != "message" {
				return nil, sessionrepo.NewError(sessionrepo.ErrInvalidForkTarget, "Fork target is not a message entry: "+*selected)
			}
			position := opts.Position
			if position == "" {
				if opts.HasEntry {
					position = "before"
				} else {
					position = "at"
				}
			}
			if position == "at" {
				branchForkTargetID = &target.id
			} else {
				branchForkTargetID = target.parentID
			}
		}
		lanes = []sessionrepo.LanePointer{{Lane: sessionrepo.MainLane, LeafID: branchForkTargetID}}
		if branchForkTargetID != nil {
			cached, err := readCachedBranch(r.db, source.ID, *branchForkTargetID)
			if err != nil {
				return nil, err
			}
			if cached == nil {
				return nil, sessionrepo.NewError(sessionrepo.ErrInvalidForkTarget, "Fork target is not on a cached branch: "+*branchForkTargetID)
			}
			crows, err := queryCachedBranchRows(r.db, source.ID, cached, sessionrepo.EntryQuery{Order: sessionrepo.OrderOldestFirst})
			if err != nil {
				return nil, err
			}
			for _, cr := range crows {
				entries = append(entries, cr.entryRow)
			}
			branchTips = append(branchTips, *branchForkTargetID)
		}
	}

	copied := map[string]struct{}{}
	for _, e := range entries {
		copied[e.id] = struct{}{}
	}
	latestName, err := readLatestFact(r.db, source.ID, "name", nil)
	if err != nil {
		return nil, err
	}
	latestLabels, err := readLatestLabelFacts(r.db, source.ID)
	if err != nil {
		return nil, err
	}
	var labelsToCopy []factRow
	for _, lab := range latestLabels {
		if opts.Scope == "tree" || (lab.key != nil && containsKey(copied, *lab.key)) {
			labelsToCopy = append(labelsToCopy, lab)
		}
	}
	createdAt := nowMs()
	meta := opts.Metadata
	if meta == nil {
		meta = sourceMeta.Metadata
	}
	parentID := opts.ParentSessionID
	if parentID == "" {
		parentID = source.ID
	}
	cwd := opts.CWD
	if cwd == "" {
		cwd = sourceMeta.CWD
	}
	msgCount := 0
	for _, e := range entries {
		if e.typ == "message" {
			msgCount++
		}
	}
	var lease *writerLease
	err = r.immediate(func() error {
		p := parentID
		if err := insertSessionRow(r.db, id, createdAt, cwd, &p, meta); err != nil {
			return err
		}
		if err := createSequence(r.db, id); err != nil {
			return err
		}
		if err := createStats(r.db, id, msgCount); err != nil {
			return err
		}
		nextSeq := int64(1)
		alloc := func() int64 {
			s := nextSeq
			nextSeq++
			return s
		}
		for _, e := range entries {
			if err := insertEntryRow(r.db, id, alloc(), e.id, e.parentID, e.typ, e.timestamp, e.payload); err != nil {
				return err
			}
		}
		if opts.Scope == "tree" {
			for _, ln := range lanes {
				if err := insertLane(r.db, id, alloc(), ln.Lane, ln.LeafID); err != nil {
					return err
				}
			}
		} else {
			if err := createInitialLane(r.db, id, sessionrepo.MainLane, branchForkTargetID); err != nil {
				return err
			}
		}
		if latestName != nil && latestName.value != nil {
			if err := appendFact(r.db, id, alloc(), "name", nil, latestName.value); err != nil {
				return err
			}
		}
		for _, lab := range labelsToCopy {
			if err := appendFact(r.db, id, alloc(), "label", lab.key, lab.value); err != nil {
				return err
			}
		}
		if err := setNextSequence(r.db, id, nextSeq); err != nil {
			return err
		}
		for _, tip := range branchTips {
			if err := buildCachedBranch(r.db, id, tip); err != nil {
				return err
			}
		}
		var err error
		lease, err = claimWriterLease(r.db, id, r.lease)
		return err
	})
	if err != nil {
		if se, ok := err.(*sessionrepo.Error); ok {
			return nil, se
		}
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage, "Failed to fork SQLite session "+id, err)
	}
	row, err := requireSessionRow(r.db, id)
	if err != nil {
		return nil, err
	}
	decoded, err := decodeSessionMetadata(row, r.absPath)
	if err != nil {
		return nil, err
	}
	return r.sessionFromLease(decoded, lease), nil
}

func containsKey(m map[string]struct{}, k string) bool {
	_, ok := m[k]
	return ok
}

// Close releases writer leases and the shared database connection.
func (r *Repository) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	for id, st := range r.active {
		st.release()
		delete(r.active, id)
	}
	if r.db != nil {
		err := r.db.Close()
		r.db = nil
		return err
	}
	return nil
}
