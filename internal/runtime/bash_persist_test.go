package runtime

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/session"
)

func TestPersistBashWritesSessionAndExcludeFlag(t *testing.T) {
	dir := t.TempDir()
	cwd := t.TempDir()
	sess := session.New(cwd, dir)
	e := &Engine{Opts: Options{Cwd: cwd, AgentDir: dir, Session: sess}}
	code := 0
	e.PersistBash("printf secret", BashResult{Output: "secret", ExitCode: &code}, true)
	entries := sess.Entries()
	if len(entries) == 0 {
		t.Fatal("missing bashExecution entry")
	}
	var payload map[string]any
	if err := json.Unmarshal(entries[len(entries)-1].Message, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["role"] != "bashExecution" || payload["command"] != "printf secret" {
		t.Fatalf("payload=%#v", payload)
	}
	if payload["excludeFromContext"] != true {
		t.Fatalf("excludeFromContext=%#v", payload["excludeFromContext"])
	}
	if payload["output"] != "secret" {
		t.Fatalf("output=%#v", payload["output"])
	}
}

func TestRunBashTruncatesLongOutput(t *testing.T) {
	res := RunBash(t.Context(), t.TempDir(), "seq 1 2100", nil)
	if res.FullOutputPath != "" {
		t.Cleanup(func() { _ = os.Remove(res.FullOutputPath) })
	}
	if res.ExitCode == nil || *res.ExitCode != 0 {
		t.Fatalf("exit=%v out=%q", res.ExitCode, res.Output)
	}
	if !res.Truncated || res.FullOutputPath == "" {
		t.Fatalf("truncated=%v path=%q", res.Truncated, res.FullOutputPath)
	}
	if !strings.Contains(res.Output, "2100") {
		t.Fatalf("missing tail: %q", res.Output[max(0, len(res.Output)-80):])
	}
	full, err := os.ReadFile(res.FullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(full), "1\n") || !strings.Contains(string(full), "2100") {
		t.Fatalf("temp file missing head/tail, len=%d", len(full))
	}
}
