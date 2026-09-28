package bugreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/version"
)

const (
	// KindUncaught is a panic recovered outside the TUI.
	KindUncaught = "uncaught_exception"
	// KindFatal is a panic recovered from the interactive TUI.
	KindFatal = "fatal_error"

	maxCrashRecords = 5
	crashMaxAge     = 7 * 24 * time.Hour
)

// CrashRecord is one entry in crashes.json.
type CrashRecord struct {
	Timestamp   string  `json:"timestamp"`
	Version     string  `json:"version"`
	Kind        string  `json:"kind"`
	Message     string  `json:"message"`
	Stack       *string `json:"stack"`
	SessionFile *string `json:"sessionFile"`
	Cwd         string  `json:"cwd"`
	Notified    bool    `json:"notified,omitempty"`
}

// CrashInput is the panic a caller wants stored.
type CrashInput struct {
	Kind        string
	Err         any
	Stack       string
	SessionFile string
	Cwd         string
}

var crashAgentDir string

// SetCrashAgentDir records the agent directory used when RecordCrash is called
// with an empty directory (process-level panic recovery).
func SetCrashAgentDir(dir string) {
	if dir != "" {
		crashAgentDir = dir
	}
}

// CrashPath is agentDir/crashes.json.
func CrashPath(agentDir string) string {
	return filepath.Join(resolveAgentDir(agentDir), "crashes.json")
}

func resolveAgentDir(agentDir string) string {
	if agentDir != "" {
		return agentDir
	}
	if crashAgentDir != "" {
		return crashAgentDir
	}
	return config.DefaultConfigDir()
}

// ReadCrashLog returns stored crashes. A missing or invalid file is empty.
func ReadCrashLog(agentDir string) []CrashRecord {
	b, err := os.ReadFile(CrashPath(agentDir))
	if err != nil {
		return nil
	}
	var records []CrashRecord
	if err := json.Unmarshal(b, &records); err != nil {
		return nil
	}
	out := make([]CrashRecord, 0, len(records))
	for _, r := range records {
		if r.Timestamp == "" || r.Message == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// RecordCrash appends one crash and keeps the newest maxCrashRecords entries.
// It returns false when nothing was written.
func RecordCrash(agentDir string, in CrashInput) (CrashRecord, bool) {
	kind := in.Kind
	if kind != KindUncaught && kind != KindFatal {
		kind = KindUncaught
	}
	rec := CrashRecord{
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		Version:   version.Version,
		Kind:      kind,
		Message:   crashMessage(in.Err),
		Cwd:       in.Cwd,
	}
	if in.Stack != "" {
		s := in.Stack
		rec.Stack = &s
	}
	if in.SessionFile != "" {
		s := in.SessionFile
		rec.SessionFile = &s
	}
	path := CrashPath(agentDir)
	records := append(ReadCrashLog(agentDir), rec)
	if len(records) > maxCrashRecords {
		records = records[len(records)-maxCrashRecords:]
	}
	if err := writeCrashLog(path, records); err != nil {
		return CrashRecord{}, false
	}
	return rec, true
}

// TakeUnnotifiedCrash returns the newest crash from the last 7 days that has
// not been announced, then marks every unnotified record as announced.
func TakeUnnotifiedCrash(agentDir string, now time.Time) *CrashRecord {
	if now.IsZero() {
		now = time.Now()
	}
	records := ReadCrashLog(agentDir)
	var found *CrashRecord
	for i := len(records) - 1; i >= 0; i-- {
		rec := records[i]
		if rec.Notified {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil || now.Sub(ts) > crashMaxAge {
			continue
		}
		copyRec := rec
		found = &copyRec
		break
	}
	if found == nil {
		return nil
	}
	for i := range records {
		if !records[i].Notified {
			records[i].Notified = true
		}
	}
	_ = writeCrashLog(CrashPath(agentDir), records)
	return found
}

// ClearCrashLog removes crashes.json. A missing file is success.
func ClearCrashLog(agentDir string) {
	_ = os.Remove(CrashPath(agentDir))
}

func writeCrashLog(path string, records []CrashRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func crashMessage(err any) string {
	if err == nil {
		return "unknown panic"
	}
	if e, ok := err.(error); ok && e.Error() != "" {
		return e.Error()
	}
	s := fmt.Sprint(err)
	if s == "" {
		return "unknown panic"
	}
	return s
}
