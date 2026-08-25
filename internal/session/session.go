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
)

// CurrentVersion is the session file schema version (pi CURRENT_SESSION_VERSION).
const CurrentVersion = 3

// Header is the first line of a session file (pi SessionHeader).
type Header struct {
	Type          string `json:"type"` // always "session"
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	Cwd           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
	Name          string `json:"name,omitempty"`
}

// Entry is one line of a session file after the header (pi SessionEntryBase +
// the message payload). parentId is null for the first entry, forming a tree.
type Entry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Message   json.RawMessage `json:"message,omitempty"`

	// role is used only for the buffer-until-assistant flush rule; not serialized.
	role string
}

// Manager creates and appends to a single session file. It mirrors pi's
// SessionManager persistence: entries are buffered until the first assistant
// message exists, then the whole file is written and subsequent entries are
// appended.
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

// DefaultAgentDir returns pi's config root: ~/.pi/agent (override-free).
func DefaultAgentDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".pi", "agent")
	}
	return filepath.Join(home, ".pi", "agent")
}

// New starts a session for cwd, storing files under agentDir/sessions/--<cwd>--/.
func New(cwd, agentDir string) *Manager {
	resolvedCwd, err := filepath.Abs(cwd)
	if err != nil {
		resolvedCwd = cwd
	}
	id := newUUID()
	ts := isoNow()
	dir := sessionDir(agentDir, resolvedCwd)
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

// ID returns the session id.
func (m *Manager) ID() string { return m.id }

// File returns the session file path.
func (m *Manager) File() string { return m.file }

func (m *Manager) Name() string { return m.header.Name }

func (m *Manager) SetName(name string) { m.header.Name = name }

// LeafID is the current branch tip (pi SessionManager.getLeafId).
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

// sessionDir encodes cwd into a directory name under agentDir/sessions/, matching
// pi's getDefaultSessionDirPath: strip a leading separator, then replace / \ : with -.
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

// fileTimestamp replaces ':' and '.' with '-' (pi's fileTimestamp).
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
