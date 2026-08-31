package runtime

import (
	"context"
	"os"
	"testing"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestRuntimeHelperProcess(_ *testing.T) {
	if os.Getenv("PIGO_RUNTIME_EXT") == "" {
		return
	}
	switch os.Getenv("PIGO_RUNTIME_EXT") {
	case "block":
		_ = ext.Serve(ext.Handler{
			Name:   "block-ext",
			Events: []string{"tool_call", "input"},
			OnEvent: func(event string, _ map[string]any) map[string]any {
				if event == "tool_call" {
					return map[string]any{"block": true, "reason": "nope"}
				}
				if event == "input" {
					return map[string]any{"action": "transform", "text": "transformed"}
				}
				return nil
			},
		})
	case "flag":
		_ = ext.Serve(ext.Handler{
			Name: "flag-ext",
			Flags: []ext.FlagDef{{
				Name: "plan", Type: "boolean", Description: "plan",
			}},
		})
	}
	os.Exit(0)
}

func spawnRuntimeExt(t *testing.T, kind string, unknown []ext.UnknownFlag) *ext.Host {
	t.Helper()
	h, err := ext.Spawn(context.Background(), "runtime-ext",
		[]string{os.Args[0], "-test.run=^TestRuntimeHelperProcess$"},
		ext.Options{Env: []string{"PIGO_RUNTIME_EXT=" + kind}, UnknownFlags: unknown})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func TestExecutorBlocksToolCall(t *testing.T) {
	h := spawnRuntimeExt(t, "block", nil)
	e := &Engine{
		Hosts: []*ext.Host{h},
		Tools: tools.Default(),
		Opts:  Options{Config: config.Config{Model: "x"}},
	}
	got, isErr := e.Executor().Execute(context.Background(), agent.ToolCall{ID: "1", Name: "read", Args: map[string]any{"path": "a"}})
	if !isErr || got != "nope" {
		t.Fatalf("got %q isErr=%v", got, isErr)
	}
}

func TestRunPromptTransformsInput(t *testing.T) {
	h := spawnRuntimeExt(t, "block", nil)
	var seen string
	e := &Engine{
		Hosts: []*ext.Host{h},
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			if len(req.Messages) > 0 {
				seen = req.Messages[len(req.Messages)-1].Content
			}
			return textReply("ok")(ctx, req, ai.Options{})
		},
		Opts: Options{Config: config.Config{Model: "x", Provider: "mock"}},
	}
	e.Steering = e.drainSteer
	e.FollowUp = e.drainFollow
	st := e.RunPrompt(context.Background(), nil, "original", nil)
	_ = st.Collect()
	if seen != "transformed" {
		t.Fatalf("prompt = %q, want transformed", seen)
	}
}

func TestUnclaimedFlags(t *testing.T) {
	h := spawnRuntimeExt(t, "flag", []ext.UnknownFlag{
		{Name: "plan", Present: true},
		{Name: "orphan", Present: true},
	})
	e := &Engine{Hosts: []*ext.Host{h}, Opts: Options{UnknownFlags: []ext.UnknownFlag{
		{Name: "plan", Present: true},
		{Name: "orphan", Present: true},
	}}}
	left := e.UnclaimedFlags()
	if len(left) != 1 || left[0].Name != "orphan" {
		t.Fatalf("leftover=%+v", left)
	}
}
