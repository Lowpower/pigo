package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ext"
)

func TestParseUserBashOverride(t *testing.T) {
	t.Parallel()
	if _, err := parseUserBashOverride(map[string]any{}); err == nil || !strings.Contains(err.Error(), "invalid user_bash") {
		t.Fatalf("empty = %v", err)
	}
	if _, err := parseUserBashOverride(map[string]any{"block": true, "reason": "nope"}); err == nil || err.Error() != "nope" {
		t.Fatalf("block = %v", err)
	}
	got, err := parseUserBashOverride(map[string]any{"result": map[string]any{
		"output": "hi", "cancelled": false, "truncated": true, "exitCode": 3.0,
	}})
	if err != nil || got == nil || got.Output != "hi" || got.ExitCode == nil || *got.ExitCode != 3 || !got.Truncated {
		t.Fatalf("new shape = %+v err=%v", got, err)
	}
	got, err = parseUserBashOverride(map[string]any{"result": map[string]any{
		"stdout": "a", "stderr": "b", "exitCode": 1.0,
	}})
	if err != nil || got == nil || got.Output != "a\nb" || got.ExitCode == nil || *got.ExitCode != 1 {
		t.Fatalf("old shape = %+v err=%v", got, err)
	}
	if _, err := parseUserBashOverride(map[string]any{"operations": map[string]any{"exec": true}}); err == nil {
		t.Fatal("operations should fail closed")
	}
	if _, err := parseUserBashOverride(map[string]any{"result": map[string]any{"output": "x"}}); err == nil {
		t.Fatal("missing cancelled/truncated should fail")
	}
}

func TestRunUserBashUndefinedFallsThrough(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=undefined")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	got := e.RunUserBash(context.Background(), "printf local-ran", false, nil)
	if got.Error != "" || !strings.Contains(got.Output, "local-ran") {
		t.Fatalf("%+v", got)
	}
}

func TestRunUserBashEmptyFailsClosed(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=empty")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	got := e.RunUserBash(context.Background(), "printf local-ran", false, nil)
	if got.Error == "" || !strings.Contains(got.Error, "invalid user_bash") {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(got.Output, "local-ran") {
		t.Fatalf("local bash ran: %+v", got)
	}
}

func TestRunUserBashBlockFailsClosed(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=block")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	got := e.RunUserBash(context.Background(), "printf local-ran", false, nil)
	if got.Error != "blocked-by-ext" || strings.Contains(got.Output, "local-ran") {
		t.Fatalf("%+v", got)
	}
}

func TestRunUserBashResultReplacesLocal(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=result")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	got := e.RunUserBash(context.Background(), "printf local-ran", false, nil)
	if got.Error != "" || got.Output != "from-ext" || strings.Contains(got.Output, "local-ran") {
		t.Fatalf("%+v", got)
	}
}

func TestRunUserBashOldResultReplacesLocal(t *testing.T) {
	h := spawnRuntimeExt(t, "userbash", nil, "PIGO_USER_BASH=old")
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	got := e.RunUserBash(context.Background(), "printf local-ran", false, nil)
	if got.Error != "" || got.Output != "old-out" {
		t.Fatalf("%+v", got)
	}
}

func TestRunUserBashTimeoutFailsClosed(t *testing.T) {
	h, err := ext.Spawn(context.Background(), "runtime-ext",
		[]string{os.Args[0], "-test.run=^TestRuntimeHelperProcess$"},
		ext.Options{
			Env:         []string{"PIGO_RUNTIME_EXT=userbash", "PIGO_USER_BASH=hang"},
			CallTimeout: 200 * time.Millisecond,
		})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{Cwd: t.TempDir()}}
	got := e.RunUserBash(context.Background(), "printf local-ran", false, nil)
	if got.Error == "" || strings.Contains(got.Output, "local-ran") {
		t.Fatalf("timeout should fail closed: %+v", got)
	}
}
