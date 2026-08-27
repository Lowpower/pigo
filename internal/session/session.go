package session

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
)

// CurrentVersion is the session file schema version.
const CurrentVersion = 3

// Header is the first line of a session file.
type Header struct {
	Type          string `json:"type"` // always "session"
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
	Name          string `json:"name,omitempty"`
}

// Entry is one line of a session file after the header. parentId is null for
// the first entry, forming a tree.
type Entry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message,omitempty"`
	Usage     *ai.Usage       `json:"usage,omitempty"`

	// Label / branch_summary / session_info fields (top-level, like pi).
	TargetID string          `json:"targetId,omitempty"`
	Label    *string         `json:"label,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	FromID   string          `json:"fromId,omitempty"`
	Details  json.RawMessage `json:"details,omitempty"`
	Name     string          `json:"name,omitempty"`

	// Compaction / custom fields (top-level, like pi).
	FirstKeptEntryID string          `json:"firstKeptEntryId,omitempty"`
	TokensBefore     *int            `json:"tokensBefore,omitempty"`
	FromHook         bool            `json:"fromHook,omitempty"`
	CustomType       string          `json:"customType,omitempty"`
	Content          json.RawMessage `json:"content,omitempty"`
	Display          *bool           `json:"display,omitempty"`
	Data             json.RawMessage `json:"data,omitempty"`

	// role is used only for the buffer-until-assistant flush rule; not serialized.
	role string
}

// Manager creates and appends to a single session file. Entries are buffered
// until the first assistant message exists, then the whole file is written and
// subsequent entries are appended.
type Manager struct {
	agentDir string
	cwd      string
	id       string
	header   Header
	dir      string
	file     string
	entries  []*Entry
	flushed  bool
	persist  bool
	leafID   string
}

// DefaultAgentDir returns the config root: ~/.pigo/agent (override-free).
func DefaultAgentDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".pigo", "agent")
	}
	return filepath.Join(home, ".pigo", "agent")
}

// New starts a session for cwd, storing files under agentDir/sessions/--<cwd>--/.
func New(cwd, agentDir string) *Manager {
	resolvedCwd, err := filepath.Abs(cwd)
	if err != nil {
		resolvedCwd = cwd
	}
	id := newUUID()
	ts := isoNow()
	dir := StorageDir(resolvedCwd, agentDir, "")
	return &Manager{
		agentDir: agentDir,
		cwd:      resolvedCwd,
		id:       id,
		header:   Header{Type: "session", Version: CurrentVersion, ID: id, Timestamp: ts, Cwd: resolvedCwd},
		dir:      dir,
		file:     filepath.Join(dir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp(ts), id)),
		persist:  true,
	}
}

// NewAt starts a session stored in sessionDir (empty means the default cwd encoding).
func NewAt(cwd, agentDir, sessionDir string) *Manager {
	m := New(cwd, agentDir)
	if strings.TrimSpace(sessionDir) == "" {
		return m
	}
	m.dir = StorageDir(m.cwd, agentDir, sessionDir)
	m.file = filepath.Join(m.dir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp(m.header.Timestamp), m.id))
	return m
}

// StorageDir is the directory that holds session jsonl files.
// An override (CLI --session-dir, settings.sessionDir, or env) is used as-is.
func StorageDir(cwd, agentDir, override string) string {
	if s := strings.TrimSpace(override); s != "" {
		if abs, err := filepath.Abs(s); err == nil {
			return abs
		}
		return s
	}
	resolved := cwd
	if abs, err := filepath.Abs(cwd); err == nil {
		resolved = abs
	}
	return sessionDir(agentDir, resolved)
}

// ID returns the session id.
func (m *Manager) ID() string { return m.id }

// File returns the session file path.
func (m *Manager) File() string { return m.file }

// Name is the display name (latest session_info, else header).
func (m *Manager) Name() string {
	for i := len(m.entries) - 1; i >= 0; i-- {
		if m.entries[i] != nil && m.entries[i].Type == "session_info" && m.entries[i].Name != "" {
			return m.entries[i].Name
		}
	}
	return m.header.Name
}

// SetName updates the display name. After the file is flushed it also appends
// a session_info entry so the name survives reload.
func (m *Manager) SetName(name string) {
	m.header.Name = name
	if m.flushed {
		_, _ = m.AppendSessionInfo(name)
	}
}

// SetParentSession records the parent session path in the header (RPC new_session).
func (m *Manager) SetParentSession(path string) { m.header.ParentSession = path }

// ParentSession returns the parent session path, if any.
func (m *Manager) ParentSession() string { return m.header.ParentSession }

// LeafID is the current branch tip.
func (m *Manager) LeafID() string { return m.leafID }

// Entries returns a copy of session entries (header excluded).
func (m *Manager) Entries() []Entry {
	out := make([]Entry, 0, len(m.entries))
	for _, e := range m.entries {
		if e != nil {
			out = append(out, *e)
		}
	}
	return out
}

// AppendMessage records a message entry. role must be the message's role
// ("user", "assistant", or "toolResult") so the flush rule works. message is any
// JSON-serializable payload (its shape is written verbatim under "message").
func (m *Manager) AppendMessage(role string, message any) (*Entry, error) {
	raw, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	e := &Entry{
		Type:      "message",
		ID:        newUUID(),
		Timestamp: isoNow(),
		Message:   raw,
		role:      role,
	}
	return m.appendEntry(e)
}

func (m *Manager) appendEntry(e *Entry) (*Entry, error) {
	if m.leafID != "" {
		prev := m.leafID
		e.ParentID = &prev
	}
	m.entries = append(m.entries, e)
	m.leafID = e.ID
	if err := m.persistEntry(e); err != nil {
		return nil, err
	}
	return e, nil
}

// EntryByID returns a copy of the named entry.
func (m *Manager) EntryByID(id string) (Entry, bool) {
	for _, e := range m.entries {
		if e != nil && e.ID == id {
			return *e, true
		}
	}
	return Entry{}, false
}

func (m *Manager) persistEntry(e *Entry) error {
	if !m.persist {
		return nil
	}
	hasAssistant := false
	for _, en := range m.entries {
		if en.role == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return nil // buffer until an assistant message arrives
	}

	if !m.flushed {
		if err := os.MkdirAll(m.dir, 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(m.file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		if err := writeLine(f, m.header); err != nil {
			return err
		}
		for _, en := range m.entries {
			if err := writeLine(f, en); err != nil {
				return err
			}
		}
		m.flushed = true
		return nil
	}

	f, err := os.OpenFile(m.file, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return writeLine(f, e)
}

// Load reads a session file into its header and entries.
func Load(path string) (Header, []Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, nil, err
	}
	defer func() { _ = f.Close() }()

	var header Header
	var entries []Entry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if first {
			if err := json.Unmarshal([]byte(line), &header); err != nil {
				return Header{}, nil, err
			}
			first = false
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return Header{}, nil, err
		}
		entries = append(entries, e)
	}
	return header, entries, scanner.Err()
}

// sessionDir encodes cwd into a directory name under agentDir/sessions/:
// strip a leading separator, then replace / \ : with -.
func sessionDir(agentDir, resolvedCwd string) string {
	trimmed := strings.TrimLeft(resolvedCwd, `/\`)
	safe := strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(trimmed)
	return filepath.Join(agentDir, "sessions", "--"+safe+"--")
}

// isoNow returns an ISO-8601 UTC timestamp with millisecond precision, matching
// JavaScript's Date.toISOString().
func isoNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// fileTimestamp replaces ':' and '.' with '-'.
func fileTimestamp(ts string) string {
	return strings.NewReplacer(":", "-", ".", "-").Replace(ts)
}

func writeLine(f *os.File, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// newUUID returns a random UUIDv4 string.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
