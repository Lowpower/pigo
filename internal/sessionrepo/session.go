package sessionrepo

// Session wraps Storage and implements Tree for the main lane.
type Session struct {
	storage    Storage
	idGen      func() string
	viewLane   string
	isMainView bool
}

// NewSession wraps storage. idGen may be nil (defaults to NewID).
func NewSession(storage Storage, idGen func() string) *Session {
	if idGen == nil {
		idGen = NewID
	}
	return &Session{storage: storage, idGen: idGen, viewLane: MainLane, isMainView: true}
}

// View returns a Tree bound to lane. The main lane returns the Session itself.
func (s *Session) View(lane string) Tree {
	if lane == MainLane {
		return s
	}
	return &Session{storage: s.storage, idGen: s.idGen, viewLane: lane, isMainView: false}
}

// GetMetadata returns cloned session metadata.
func (s *Session) GetMetadata() (Metadata, error) {
	m, err := s.storage.GetMetadata()
	if err != nil {
		return Metadata{}, err
	}
	return cloneMeta(m), nil
}

// GetLeafID returns the view lane's current leaf.
func (s *Session) GetLeafID() (*string, error) {
	return s.leafFor(s.viewLane)
}

func (s *Session) leafFor(lane string) (*string, error) {
	lanes, err := s.storage.GetLanes()
	if err != nil {
		return nil, err
	}
	for _, p := range lanes {
		if p.Lane == lane {
			return p.LeafID, nil
		}
	}
	return nil, NewError(ErrInvalidLane, "Lane not found: "+lane)
}

// GetEntry returns a cloned entry or nil if missing.
func (s *Session) GetEntry(id string) (*Entry, error) {
	e, err := s.storage.GetEntry(id)
	if err != nil || e == nil {
		return e, err
	}
	c := cloneEntry(*e)
	return &c, nil
}

// GetStats returns the usage ledger.
func (s *Session) GetStats() (Stats, error) { return s.storage.GetStats() }

// GetName returns the latest session name fact.
func (s *Session) GetName() (*string, error) { return s.storage.GetName() }

// SetName writes a name fact (nil clears).
func (s *Session) SetName(name *string) error { return s.storage.SetName(name) }

// GetLabel returns the latest label for an entry.
func (s *Session) GetLabel(targetID string) (*string, error) { return s.storage.GetLabel(targetID) }

// SetLabel writes a label fact (nil clears).
func (s *Session) SetLabel(targetID string, label *string) error {
	return s.storage.SetLabel(targetID, label)
}

// GetLanes lists lane pointers.
func (s *Session) GetLanes() ([]LanePointer, error) { return s.storage.GetLanes() }

// CreateLane adds a lane at at (nil = empty).
func (s *Session) CreateLane(lane string, at *string) error { return s.storage.CreateLane(lane, at) }

// MoveLane moves a lane leaf to to (nil = empty).
func (s *Session) MoveLane(lane string, to *string) error { return s.storage.MoveLane(lane, to) }

// FindEntries lists matching entries.
func (s *Session) FindEntries(query EntryQuery) ([]Entry, error) {
	return s.queryEntries(query)
}

// FindEntry returns the first matching entry.
func (s *Session) FindEntry(query EntryQuery) (*Entry, error) {
	q := query
	q.Limit = 1
	q.HasLimit = true
	ents, err := s.queryEntries(q)
	if err != nil || len(ents) == 0 {
		return nil, err
	}
	e := ents[0]
	return &e, nil
}

func (s *Session) queryEntries(query EntryQuery) ([]Entry, error) {
	if err := assertValidLimit(query); err != nil {
		return nil, err
	}
	if err := assertValidCursor(query.Cursor); err != nil {
		return nil, err
	}
	ents, err := s.storage.FindEntries(query)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(ents))
	for i := range ents {
		out[i] = cloneEntry(ents[i])
	}
	return out, nil
}

// FindEntriesOnBranch walks from start toward root on this view's lane.
func (s *Session) FindEntriesOnBranch(query EntryQuery) ([]Entry, error) {
	return s.queryBranch(s.viewLane, query, query.HasLimit)
}

// FindEntryOnBranch returns the first branch match.
func (s *Session) FindEntryOnBranch(query EntryQuery) (*Entry, error) {
	q := query
	q.Limit = 1
	q.HasLimit = true
	ents, err := s.queryBranch(s.viewLane, q, true)
	if err != nil || len(ents) == 0 {
		return nil, err
	}
	e := ents[0]
	return &e, nil
}

func (s *Session) queryBranch(lane string, query EntryQuery, useLimit bool) ([]Entry, error) {
	if err := assertValidLimit(query); err != nil {
		return nil, err
	}
	if err := assertValidCursor(query.Cursor); err != nil {
		return nil, err
	}
	q := query
	if !useLimit {
		q.HasLimit = false
		q.Limit = 0
	}
	if q.Start == "" {
		leaf, err := s.leafFor(lane)
		if err != nil {
			return nil, err
		}
		if leaf == nil {
			return []Entry{}, nil
		}
		q.Start = *leaf
	}
	ents, err := s.storage.FindEntriesOnBranch(q)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, len(ents))
	for i := range ents {
		out[i] = cloneEntry(ents[i])
	}
	return out, nil
}

// AppendMessage appends a message entry on this view's lane.
func (s *Session) AppendMessage(message any) (string, error) {
	e, err := s.AppendEntry(Entry{Type: "message", ID: s.idGen(), Message: message}, s.viewLane)
	if err != nil {
		return "", err
	}
	return e.ID, nil
}

// AppendCustomEntry appends a custom entry on this view's lane.
func (s *Session) AppendCustomEntry(customType string, data any, hasData bool) (string, error) {
	e := Entry{Type: "custom", ID: s.idGen(), CustomType: customType, Data: data, HasData: hasData}
	committed, err := s.AppendEntry(e, s.viewLane)
	if err != nil {
		return "", err
	}
	return committed.ID, nil
}

// AppendEntry commits a provisioned entry after JSON checks.
func (s *Session) AppendEntry(entry Entry, lane string) (Entry, error) {
	if err := AssertJSONSerializable(entry); err != nil {
		return Entry{}, err
	}
	committed, err := s.storage.AppendEntry(entry, lane)
	if err != nil {
		return Entry{}, err
	}
	return cloneEntry(committed), nil
}

// AppendRecord commits a provisioned record after JSON checks.
func (s *Session) AppendRecord(record Record) (Record, error) {
	if err := AssertJSONSerializable(record); err != nil {
		return Record{}, err
	}
	committed, err := s.storage.AppendRecord(record)
	if err != nil {
		return Record{}, err
	}
	return cloneRecord(committed), nil
}

// FindRecords lists matching records.
func (s *Session) FindRecords(query RecordQuery) ([]Record, error) {
	if err := assertRecordQuery(query); err != nil {
		return nil, err
	}
	recs, err := s.storage.FindRecords(query)
	if err != nil {
		return nil, err
	}
	out := make([]Record, len(recs))
	for i := range recs {
		out[i] = cloneRecord(recs[i])
	}
	return out, nil
}

// FindOpenOperations returns unfinished operation_started records.
func (s *Session) FindOpenOperations(lane string, opts OpenOpOptions) ([]Record, error) {
	if opts.HasLimit && opts.Limit <= 0 {
		return nil, NewError(ErrInvalidQuery, "limit must be a positive integer")
	}
	recs, err := s.storage.FindOpenOperations(lane, opts)
	if err != nil {
		return nil, err
	}
	out := make([]Record, len(recs))
	for i := range recs {
		out[i] = cloneRecord(recs[i])
	}
	return out, nil
}

// GetLog returns the merged mutation log.
func (s *Session) GetLog(opts LogOptions) ([]LogItem, error) {
	if opts.HasLimit && opts.Limit <= 0 {
		return nil, NewError(ErrInvalidQuery, "limit must be a positive integer")
	}
	if opts.HasAfter && opts.AfterSeq < 0 {
		return nil, NewError(ErrInvalidQuery, "cursor sequence must be a non-negative integer")
	}
	return s.storage.GetLog(opts)
}

func assertValidLimit(q EntryQuery) error {
	if q.HasLimit && q.Limit <= 0 {
		return NewError(ErrInvalidQuery, "limit must be a positive integer")
	}
	return nil
}

func assertValidCursor(c *Cursor) error {
	if c != nil && c.AfterSeq < 0 {
		return NewError(ErrInvalidQuery, "cursor sequence must be a non-negative integer")
	}
	return nil
}

func assertRecordQuery(q RecordQuery) error {
	if q.HasLimit && q.Limit <= 0 {
		return NewError(ErrInvalidQuery, "limit must be a positive integer")
	}
	if q.HasAfterSeq && q.AfterSeq < 0 {
		return NewError(ErrInvalidQuery, "cursor sequence must be a non-negative integer")
	}
	if q.OperationKind != "" && q.Type != "operation_started" {
		return NewError(ErrInvalidQuery, `operationKind requires type "operation_started"`)
	}
	return nil
}
