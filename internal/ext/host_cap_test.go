package ext

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCapHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_EXT_HELPER") != "caps" {
		return
	}
	_ = Serve(Handler{
		Name: "cap-ext",
		Commands: []CommandDef{{
			Name:        "ping",
			Description: "pong",
			Fn:          func(string) {},
		}},
		Shortcuts: []ShortcutDef{{
			Name:        "ctrl+shift+p",
			Description: "ping shortcut",
			Fn:          func() {},
		}},
		Flags: []FlagDef{{
			Name:        "plan",
			Description: "plan mode",
			Type:        "boolean",
			Default:     false,
		}},
		Events: []string{"tool_call"},
		OnEvent: func(event string, payload map[string]any) map[string]any {
			if event == "tool_call" {
				return map[string]any{"block": true, "reason": "blocked"}
			}
			return nil
		},
	})
	os.Exit(0)
}

func spawnCaps(t *testing.T, extra ...string) *Host {
	t.Helper()
	env := []string{"PIGO_EXT_HELPER=caps"}
	env = append(env, extra...)
	h, err := Spawn(context.Background(), "cap-ext",
		[]string{os.Args[0], "-test.run=^TestCapHelperProcess$"},
		Options{Env: env, UnknownFlags: []UnknownFlag{{Name: "plan", Present: true}}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	return h
}

func TestHostRegistersCommandAndBlocksToolCall(t *testing.T) {
	h := spawnCaps(t)
	defer func() { _ = h.Close() }()

	cmds := h.Commands()
	if len(cmds) != 1 || cmds[0].Name != "ping" {
		t.Fatalf("commands = %+v", cmds)
	}
	if !h.HasShortcut("ctrl+shift+p") {
		t.Fatal("missing shortcut")
	}
	v, ok := h.FlagValue("plan")
	if !ok || v != true {
		t.Fatalf("plan flag = %v %v, want true", v, ok)
	}
	if leftover := h.UnclaimedFlags(); len(leftover) != 0 {
		t.Fatalf("unclaimed = %+v", leftover)
	}

	res, err := h.QueryEvent(context.Background(), "tool_call", map[string]any{"toolName": "bash"})
	if err != nil {
		t.Fatal(err)
	}
	if res["block"] != true || res["reason"] != "blocked" {
		t.Fatalf("event result = %#v", res)
	}
}

func TestQueryEventTimesOutWhenUnanswered(t *testing.T) {
	h := spawnReverseExt(t)
	defer func() { _ = h.Close() }()
	h.callTimeout = 50 * time.Millisecond
	// reverse-ext does not subscribe; QueryEvent should return immediately.
	res, err := h.QueryEvent(context.Background(), "tool_call", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 0 {
		t.Fatalf("unsubscribed result = %#v", res)
	}
}

func TestSendCommandDoesNotHang(t *testing.T) {
	h := spawnCaps(t)
	defer func() { _ = h.Close() }()
	if err := h.SendCommand("ping", "x"); err != nil {
		t.Fatal(err)
	}
	if err := h.SendShortcut("ctrl+shift+p"); err != nil {
		t.Fatal(err)
	}
}
