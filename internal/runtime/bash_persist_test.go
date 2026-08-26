package runtime

import (
	"encoding/json"
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
