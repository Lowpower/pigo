package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
)

// CurrentVersion is the session file schema version.
// System messages and compaction systemMessage checkpoints are optional
// fields on version 3; old files stay valid without a version bump.
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

	// Label / branch_summary / session_info fields (top-level).
	TargetID string          `json:"targetId,omitempty"`
	Label    *string         `json:"label,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	FromID   string          `json:"fromId,omitempty"`
	Details  json.RawMessage `json:"details,omitempty"`
	Name     string          `json:"name,omitempty"`

	// Compaction / custom / context_edit fields (top-level).
	FirstKeptEntryID string `json:"firstKeptEntryId,omitempty"`
	// Replacement is set on context_edit. A non-nil value is written even when
	// it is JSON null, which omits the target from later model context.
	Replacement   *JSONValue      `json:"replacement,omitempty"`
	SystemMessage *SystemMessage  `json:"systemMessage,omitempty"`
	TokensBefore  *int            `json:"tokensBefore,omitempty"`
	FromHook      bool            `json:"fromHook,omitempty"`
	CustomType    string          `json:"customType,omitempty"`
	Content       json.RawMessage `json:"content,omitempty"`
	Display       *bool           `json:"display,omitempty"`
	Data          json.RawMessage `json:"data,omitempty"`

	Provider        string   `json:"provider,omitempty"`
	ModelID         string   `json:"modelId,omitempty"`
	Model           string   `json:"model,omitempty"`
	Kind            string   `json:"kind,omitempty"`
	Note            string   `json:"note,omitempty"`
	ThinkingLevel   string   `json:"thinkingLevel,omitempty"`
	ActiveToolNames []string `json:"activeToolNames,omitempty"`

	// role is used only for the buffer-until-user flush rule; not serialized.
	role string
}

// Manager creates and appends to a single session file. Entries are buffered
// until the first user message exists, then the whole file is written and
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
	skips    []Skip
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

// NewWithID starts a session with a caller-supplied id.
func NewWithID(cwd, agentDir, id, sessionDir string) *Manager {
	m := NewAt(cwd, agentDir, sessionDir)
	id = strings.TrimSpace(id)
	if id == "" {
		return m
	}
	m.id = id
	m.header.ID = id
	m.file = filepath.Join(m.dir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp(m.header.Timestamp), id))
	return m
}

// InMemory returns a non-persisted session, optionally with a fixed id.
func InMemory(cwd, id string) *Manager {
	m := NewWithID(cwd, "", id, "")
	m.persist = false
	return m
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

// Header returns the session header.
func (m *Manager) Header() Header { return m.header }

// File returns the session file path.
func (m *Manager) File() string { return m.file }

// LoadWarnings returns lines Load skipped while opening this session.
func (m *Manager) LoadWarnings() []Skip {
	if m == nil || len(m.skips) == 0 {
		return nil
	}
	out := make([]Skip, len(m.skips))
	copy(out, m.skips)
	return out
}

// LoadWarningText is the single warning shown after a session opens.
// It is empty when every line loaded.
func (m *Manager) LoadWarningText() string {
	if m == nil {
		return ""
	}
	return formatSkipWarning(m.file, m.skips)
}

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
// ("user", "assistant", or "toolResult") so the flush rule works. The file is
// created when the first user message is appended. message is any
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
	hasUser := false
	for _, en := range m.entries {
		if en != nil && en.role == "user" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		return nil // buffer until a user message arrives
	}

	if !m.flushed {
		if err := os.MkdirAll(m.dir, 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(m.file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_ = f.Chmod(0o600)
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

	leadingNL, err := repairSessionTail(m.file)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(m.file, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_ = f.Chmod(0o600)
	defer func() { _ = f.Close() }()
	return writeJSONLine(f, e, leadingNL)
}

// maxHeaderBytes is the maximum prefix read from a session file while
// identifying it. A longer first line is treated as a damaged header.
const maxHeaderBytes = 1 << 20

// readHeader reads the first non-empty line of a session file and decodes it
// as a Header. It does not read or parse later entries.
func readHeader(path string) (Header, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer func() { _ = f.Close() }()

	// Read one byte at a time so identification stops at the newline and does
	// not consume later entries (including a body that never arrives).
	buf := make([]byte, 0, 256)
	var b [1]byte
	for {
		n, readErr := f.Read(b[:])
		if n == 0 {
			if readErr == nil {
				return Header{}, fmt.Errorf("empty session header")
			}
			if len(bytes.TrimSpace(buf)) == 0 {
				if readErr == io.EOF {
					return Header{}, fmt.Errorf("empty session header")
				}
				return Header{}, readErr
			}
			break
		}
		if len(buf) == maxHeaderBytes {
			return Header{}, fmt.Errorf("session header exceeds %d bytes", maxHeaderBytes)
		}
		buf = append(buf, b[0])
		if b[0] != '\n' {
			if readErr == nil {
				continue
			}
			if readErr != io.EOF {
				return Header{}, readErr
			}
			break
		}
		if len(bytes.TrimSpace(buf)) == 0 {
			buf = buf[:0]
			continue
		}
		break
	}
	var h Header
	if err := json.Unmarshal(bytes.TrimSpace(buf), &h); err != nil {
		return Header{}, err
	}
	return h, nil
}

// Skip is one session line Load ignored. Line is the 1-based physical line number.
type Skip struct {
	Line   int
	Reason string
}

// maxEntryLineBytes is the largest body line Load will keep. A longer line is
// skipped so one oversized record cannot make the rest of the file unreadable.
const maxEntryLineBytes = 64 << 20

// Load reads a session file into its header and entries. A damaged header fails
// the load. Later lines that are not valid JSON, or that exceed maxEntryLineBytes,
// are returned in skips and do not fail the load.
func Load(path string) (Header, []Entry, []Skip, error) {
	f, err := os.Open(path)
	if err != nil {
		return Header{}, nil, nil, err
	}
	defer func() { _ = f.Close() }()
	_ = os.Chmod(path, 0o600)

	r := bufio.NewReaderSize(f, 64*1024)
	var header Header
	var entries []Entry
	var skips []Skip
	haveHeader := false
	for lineNo := 1; ; lineNo++ {
		limit := maxEntryLineBytes
		if !haveHeader {
			limit = maxHeaderBytes
		}
		line, tooLong, err := readLimitedLine(r, limit)
		if err == io.EOF {
			break
		}
		if err != nil {
			return Header{}, nil, nil, err
		}
		if tooLong {
			if !haveHeader {
				return Header{}, nil, nil, fmt.Errorf("session header exceeds %d bytes", maxHeaderBytes)
			}
			skips = append(skips, Skip{Line: lineNo, Reason: fmt.Sprintf("line exceeds %d MiB", maxEntryLineBytes>>20)})
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !haveHeader {
			if err := json.Unmarshal([]byte(trimmed), &header); err != nil {
				return Header{}, nil, nil, err
			}
			haveHeader = true
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(trimmed), &e); err != nil {
			skips = append(skips, Skip{Line: lineNo, Reason: err.Error()})
			continue
		}
		entries = append(entries, e)
	}
	return header, entries, skips, nil
}

// readLimitedLine reads one physical line without its trailing newline.
// tooLong is set when the line content exceeds limit; the rest of that line
// is discarded so the next line can still be read. io.EOF is returned only
// when no bytes remain.
func readLimitedLine(r *bufio.Reader, limit int) (string, bool, error) {
	var buf bytes.Buffer
	tooLong := false
	sawByte := false
	for {
		chunk, err := r.ReadSlice('\n')
		if len(chunk) > 0 {
			sawByte = true
		}
		if !tooLong && len(chunk) > 0 {
			content := chunk
			if nl := bytes.IndexByte(chunk, '\n'); nl >= 0 {
				content = chunk[:nl]
			}
			if buf.Len()+len(content) > limit {
				tooLong = true
			} else if _, err := buf.Write(content); err != nil {
				return "", false, err
			}
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil && err != io.EOF {
			return "", tooLong, err
		}
		if tooLong {
			return "", true, nil
		}
		if err == io.EOF && !sawByte {
			return "", false, io.EOF
		}
		return buf.String(), false, nil
	}
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
	return writeJSONLine(f, v, false)
}

func writeJSONLine(f *os.File, v any, leadingNL bool) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, len(b)+2)
	if leadingNL {
		buf = append(buf, '\n')
	}
	buf = append(buf, b...)
	buf = append(buf, '\n')
	_, err = f.Write(buf)
	return err
}

// repairSessionTail prepares an append. A partial trailing value is removed
// by rewriting the prefix to a temp file in the same directory and renaming
// it over path. A complete trailing JSON value, including one with extra
// bytes after it, is left in place; leadingNL tells the caller to write a
// newline before the new record. A partial header is left untouched.
func repairSessionTail(path string) (leadingNL bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	if st.Size() == 0 {
		return false, nil
	}
	var last [1]byte
	if _, err := f.ReadAt(last[:], st.Size()-1); err != nil {
		return false, err
	}
	if last[0] == '\n' {
		return false, nil
	}
	nlAt, found, err := lastNewline(f, st.Size())
	if err != nil {
		return false, err
	}
	tailStart := int64(0)
	if found {
		tailStart = nlAt + 1
	}
	if _, err := f.Seek(tailStart, io.SeekStart); err != nil {
		return false, err
	}
	var raw json.RawMessage
	decErr := json.NewDecoder(f).Decode(&raw)
	if err := f.Close(); err != nil {
		return false, err
	}
	if decErr == nil {
		return true, nil
	}
	if !found {
		return false, fmt.Errorf("session file %s ends with a partial header", path)
	}
	return false, replaceWithPrefix(path, tailStart)
}

func lastNewline(f *os.File, size int64) (int64, bool, error) {
	const chunk = 64 * 1024
	buf := make([]byte, chunk)
	for end := size; end > 0; {
		n := end
		if n > chunk {
			n = chunk
		}
		start := end - n
		if _, err := f.ReadAt(buf[:n], start); err != nil {
			return 0, false, err
		}
		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return start + i, true, nil
			}
		}
		end = start
	}
	return 0, false, nil
}

func replaceWithPrefix(path string, keep int64) error {
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".pigo-session-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, io.LimitReader(src, keep)); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := src.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	done = true
	return nil
}

func formatSkipWarning(path string, skips []Skip) string {
	if len(skips) == 0 {
		return ""
	}
	parts := make([]string, len(skips))
	for i, s := range skips {
		if s.Reason != "" {
			parts[i] = fmt.Sprintf("%d (%s)", s.Line, s.Reason)
		} else {
			parts[i] = strconv.Itoa(s.Line)
		}
	}
	noun := "line"
	if len(skips) != 1 {
		noun = "lines"
	}
	return fmt.Sprintf("Warning: session %s: skipped %d unreadable %s: %s", path, len(skips), noun, strings.Join(parts, ", "))
}

// newUUID returns a random UUIDv4 string.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
