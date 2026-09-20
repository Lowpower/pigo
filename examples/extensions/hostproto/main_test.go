package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/runtime"
)

func TestHostprotoSessionStartAndExec(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "hostproto-ext")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	h, err := ext.Spawn(context.Background(), "hostproto", []string{bin}, ext.Options{})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { _ = h.Close() }()

	dir := t.TempDir()
	e := &runtime.Engine{Opts: runtime.Options{Cwd: dir, ContextWindow: 99, InputSource: "tui"}}
	e.Hosts = []*ext.Host{h}
	h.SetHostCall(func(name string, args map[string]any) map[string]any {
		return e.HandleHostCall(h, name, args)
	})
	var notified string
	h.SetNotify(func(_, text string) { notified = text })
	var status string
	h.SetStatus(func(_, text string) { status = text })

	if _, err := h.QueryEvent(context.Background(), "session_start", map[string]any{"sessionId": "s"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notified, "cwd="+dir) || !strings.Contains(notified, "mode=tui") {
		t.Fatalf("notify=%q", notified)
	}
	if status != "host protocol" {
		t.Fatalf("status=%q", status)
	}

	out, isErr := h.CallTool(context.Background(), "host_echo", map[string]any{"text": "from-ext"})
	if isErr || !strings.Contains(out, "from-ext") {
		t.Fatalf("host_echo=%q err=%v", out, isErr)
	}
}
