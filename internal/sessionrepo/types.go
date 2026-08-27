package sessionrepo

// MainLane is the default session lane.
const MainLane = "main"

// Order is a sequence scan direction.
type Order string

// Order values.
const (
	OrderNewestFirst Order = "newestFirst"
	OrderOldestFirst Order = "oldestFirst"
)

// Metadata is durable session identity.
type Metadata struct {
	ID              string         `json:"id"`
	CreatedAt       int64          `json:"createdAt"`
	ParentSessionID string         `json:"parentSessionId,omitempty"`
	CWD             string         `json:"cwd"`
	Path            string         `json:"path"`
	Name            string         `json:"name,omitempty"`
	HasName         bool           `json:"-"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// CreateOptions are inputs to SessionRepo.Create.
type CreateOptions struct {
	ID              string
	ParentSessionID string
	CWD             string
	Metadata        map[string]any
}

// ListOptions filter SessionRepo.List.
type ListOptions struct {
	CWD string
}

// ForkOptions control SessionRepo.Fork.
type ForkOptions struct {
	Scope    string // "" / "branch" / "tree"
	EntryID  string
	HasEntry bool
	Position string // "" / "before" / "at"
	CreateOptions
}

// Stats is the session usage ledger.
type Stats struct {
	MessageCount   int     `json:"messageCount"`
	CachedTokens   float64 `json:"cachedTokens"`
	UncachedTokens float64 `json:"uncachedTokens"`
	TotalTokens    float64 `json:"totalTokens"`
	CostTotal      float64 `json:"costTotal"`
}

// Usage is a usage record payload (JS numbers → float64).
type Usage struct {
	Input       float64   `json:"input"`
	Output      float64   `json:"output"`
	CacheRead   float64   `json:"cacheRead"`
	CacheWrite  float64   `json:"cacheWrite"`
	TotalTokens float64   `json:"totalTokens"`
	Cost        UsageCost `json:"cost"`
}

// UsageCost is the dollar breakdown on Usage.
type UsageCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// Cursor is an exclusive sequence bound.
type Cursor struct {
	AfterSeq int64
}

// EntryQuery filters canonical entries.
type EntryQuery struct {
	Type       string
	CustomType string
	Order      Order
	Limit      int
	HasLimit   bool
	Cursor     *Cursor
	Start      string
	StopAtType string
	StopAtID   string
}

// RecordQuery filters lane records.
type RecordQuery struct {
	Lane          string
	Type          string
	RunID         string
	HasRunID      bool
	OperationKind string
	AfterSeq      int64
	HasAfterSeq   bool
	Order         Order
	Limit         int
	HasLimit      bool
}

// LogOptions bound getLog.
type LogOptions struct {
	AfterSeq int64
	HasAfter bool
	Limit    int
	HasLimit bool
}

// OpenOpOptions bound findOpenOperations.
type OpenOpOptions struct {
	Limit    int
	HasLimit bool
}

// LanePointer is a lane name and current leaf.
type LanePointer struct {
	Lane   string  `json:"lane"`
	LeafID *string `json:"leafId"`
}

// Entry is a canonical tree node. Type-specific fields live alongside the base.
type Entry struct {
	Type      string  `json:"type"`
	ID        string  `json:"id"`
	Seq       int64   `json:"seq"`
	ParentID  *string `json:"parentId"`
	Timestamp int64   `json:"timestamp"`

	Message         any      `json:"message,omitempty"`
	Terminate       *bool    `json:"terminate,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	ModelID         string   `json:"modelId,omitempty"`
	ThinkingLevel   string   `json:"thinkingLevel,omitempty"`
	ActiveToolNames []string `json:"activeToolNames,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	RetainedTail    any      `json:"retainedTail,omitempty"`
	TokensBefore    float64  `json:"tokensBefore,omitempty"`
	HasTokensBefore bool     `json:"-"`
	Details         any      `json:"details,omitempty"`
	HasDetails      bool     `json:"-"`
	Usage           *Usage   `json:"usage,omitempty"`
	FromID          string   `json:"fromId,omitempty"`
	CustomType      string   `json:"customType,omitempty"`
	Data            any      `json:"data,omitempty"`
	HasData         bool     `json:"-"`
}

// Record is a lane mutation that is not a tree entry.
type Record struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Seq       int64  `json:"seq"`
	Lane      string `json:"lane"`
	Timestamp int64  `json:"timestamp"`

	SourceLeafID     *string        `json:"sourceLeafId,omitempty"`
	Intent           map[string]any `json:"intent,omitempty"`
	RunID            string         `json:"runId,omitempty"`
	HasRunID         bool           `json:"-"`
	Outcome          string         `json:"outcome,omitempty"`
	Error            any            `json:"error,omitempty"`
	Step             string         `json:"step,omitempty"`
	Attempt          int            `json:"attempt,omitempty"`
	ResultEntryID    string         `json:"resultEntryId,omitempty"`
	CompactionReason string         `json:"compactionReason,omitempty"`
	AssistantEntryID string         `json:"assistantEntryId,omitempty"`
	ToolIndex        int            `json:"toolIndex,omitempty"`
	ToolCallID       string         `json:"toolCallId,omitempty"`
	ToolName         string         `json:"toolName,omitempty"`
	EffectiveArgs    map[string]any `json:"effectiveArgs,omitempty"`
	Replay           string         `json:"replay,omitempty"`
	Queue            string         `json:"queue,omitempty"`
	Target           any            `json:"target,omitempty"`
	EntryID          string         `json:"entryId,omitempty"`
	Usage            *Usage         `json:"usage,omitempty"`
	Cause            string         `json:"cause,omitempty"`
	StopReason       string         `json:"stopReason,omitempty"`
	Details          any            `json:"details,omitempty"`
}

// LogItem is one getLog row.
type LogItem struct {
	Kind     string  `json:"kind"`
	Seq      int64   `json:"seq"`
	Entry    *Entry  `json:"entry,omitempty"`
	Record   *Record `json:"record,omitempty"`
	Lane     string  `json:"lane,omitempty"`
	LeafID   *string `json:"leafId,omitempty"`
	Fact     string  `json:"fact,omitempty"`
	Name     *string `json:"name,omitempty"`
	TargetID string  `json:"targetId,omitempty"`
	Label    *string `json:"label,omitempty"`
}

// SearchOptions filter FTS search.
type SearchOptions struct {
	EntryTypes []string
	Limit      int
	HasLimit   bool
}

// SearchHit is a portable search result.
type SearchHit struct {
	SessionID string   `json:"sessionId"`
	EntryID   string   `json:"entryId"`
	Metadata  Metadata `json:"metadata"`
	Timestamp int64    `json:"timestamp"`
	Score     float64  `json:"score"`
}

// Storage is the durable surface for one opened session.
type Storage interface {
	GetMetadata() (Metadata, error)
	GetLanes() ([]LanePointer, error)
	CreateLane(lane string, at *string) error
	MoveLane(lane string, to *string) error
	AppendEntry(entry Entry, lane string) (Entry, error)
	AppendRecord(record Record) (Record, error)
	GetEntry(id string) (*Entry, error)
	FindEntries(query EntryQuery) ([]Entry, error)
	FindEntriesOnBranch(query EntryQuery) ([]Entry, error)
	FindRecords(query RecordQuery) ([]Record, error)
	FindOpenOperations(lane string, opts OpenOpOptions) ([]Record, error)
	GetLog(opts LogOptions) ([]LogItem, error)
	GetName() (*string, error)
	SetName(name *string) error
	GetLabel(id string) (*string, error)
	SetLabel(id string, label *string) error
	GetStats() (Stats, error)
}

// Tree is the per-lane read/write view.
type Tree interface {
	GetLeafID() (*string, error)
	GetEntry(id string) (*Entry, error)
	GetStats() (Stats, error)
	GetName() (*string, error)
	SetName(name *string) error
	GetLabel(targetID string) (*string, error)
	SetLabel(targetID string, label *string) error
	FindEntries(query EntryQuery) ([]Entry, error)
	FindEntry(query EntryQuery) (*Entry, error)
	FindEntriesOnBranch(query EntryQuery) ([]Entry, error)
	FindEntryOnBranch(query EntryQuery) (*Entry, error)
	AppendMessage(message any) (string, error)
	AppendCustomEntry(customType string, data any, hasData bool) (string, error)
}

// Repo is the catalog of sessions.
type Repo interface {
	Create(opts CreateOptions) (*Session, error)
	Open(meta Metadata) (*Session, error)
	List(opts ListOptions) ([]Metadata, error)
	Delete(meta Metadata) error
	Fork(source Metadata, opts ForkOptions) (*Session, error)
	Close() error
}
