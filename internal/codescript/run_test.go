package codescript

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/codemode"
	"github.com/Lowpower/pigo/internal/tools"
)

func TestMain(m *testing.M) {
	codemode.DispatchWorker()
	os.Exit(m.Run())
}

func TestScriptBashStdout(t *testing.T) {
	reg := tools.NewBuiltins(tools.Options{})
	res, err := Run(context.Background(), `await tools.bash({command:"printf hi"})`, Env{
		Tools: []Tool{{Name: "bash", Description: "run a command"}},
		Exec: func(ctx context.Context, name string, args map[string]any) (string, bool) {
			return reg.Execute(ctx, name, args)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res.Value, "hi") {
		t.Fatalf("stdout %q", res.Value)
	}
}

func TestScriptUnknownToolSuggests(t *testing.T) {
	_, err := Run(context.Background(), `await tools.Bash({})`, Env{
		Tools: []Tool{{Name: "bash"}},
		Exec: func(context.Context, string, map[string]any) (string, bool) {
			t.Fatal("unknown tool ran")
			return "", true
		},
	})
	if err == nil || !strings.Contains(err.Error(), "tools.bash") {
		t.Fatalf("err %v", err)
	}
}

func TestScriptDescribeNamespace(t *testing.T) {
	env := Env{
		Namespaces: []Namespace{{Name: "dev-radius", Description: "docs", Aliases: []string{"radius"}}},
		Tools:      []Tool{{Name: "mcp__dev-radius__search", Description: "find", Namespace: "dev-radius"}},
	}
	res, err := Run(context.Background(), `
		const a = await describeNamespace("mcp__dev-radius");
		const b = await describeNamespace("dev_radius");
		const c = await describeNamespace("radius");
		return a.name === "dev-radius" && JSON.stringify(a) === JSON.stringify(b) && a.name === c.name && a.tools[0].name === "mcp__dev-radius__search";
	`, env)
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "true" {
		t.Fatalf("value %q", res.Value)
	}
}

func TestScriptDescribeToolDeclaration(t *testing.T) {
	res, err := Run(context.Background(), `const d = await describeTool("search"); return d.declaration;`, Env{
		Tools: []Tool{{
			Name:      "mcp__dev-radius__search",
			Aliases:   []string{"search"},
			Namespace: "dev-radius",
			Schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "minimum": 1},
				},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Value, "@integer") || !strings.Contains(res.Value, "@minimum 1") || strings.Contains(res.Value, "CallToolResult") {
		t.Fatalf("declaration %s", res.Value)
	}
}

func TestScriptCallsOverlap(t *testing.T) {
	var mu sync.Mutex
	var releases []chan struct{}
	entered := make(chan struct{}, 2)
	exec := func(context.Context, string, map[string]any) (string, bool) {
		ch := make(chan struct{})
		mu.Lock()
		releases = append(releases, ch)
		mu.Unlock()
		entered <- struct{}{}
		<-ch
		return "ok", false
	}
	done := make(chan struct{})
	var res Result
	var err error
	go func() {
		defer close(done)
		res, err = Run(context.Background(), `const both = await Promise.all([tools.one({}), tools.two({})]); return both.join("");`, Env{
			Tools: []Tool{{Name: "one"}, {Name: "two"}},
			Exec:  exec,
		})
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(20 * time.Second):
			t.Fatal("calls did not overlap")
		}
	}
	mu.Lock()
	for _, ch := range releases {
		close(ch)
	}
	mu.Unlock()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("script did not finish")
	}
	if err != nil || res.Value != "okok" {
		t.Fatalf("got %q err %v", res.Value, err)
	}
}

func TestScriptSchemaFailureResolves(t *testing.T) {
	res, err := Run(context.Background(), `return await tools.typed({})`, Env{
		Tools: []Tool{{Name: "typed", OutputSchema: map[string]any{"type": "object"}}},
		Exec: func(context.Context, string, map[string]any) (string, bool) {
			return "exit 1", true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "exit 1" {
		t.Fatalf("value %q", res.Value)
	}
}

func TestScriptPlainFailureRejects(t *testing.T) {
	_, err := Run(context.Background(), `await tools.bash({})`, Env{
		Tools: []Tool{{Name: "bash"}},
		Exec: func(context.Context, string, map[string]any) (string, bool) {
			return "command is required", true
		},
	})
	if err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Fatalf("err %v", err)
	}
}

func TestScriptStoreLoadAndConsole(t *testing.T) {
	store := &Store{}
	env := Env{Store: store}
	if _, err := Run(context.Background(), `await store("k", {n: 1}); text("hello"); console.log("line"); image({type:"image", data:"aa"}); return "done";`, env); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), `const v = await load("k"); return v.n;`, env)
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "1" {
		t.Fatalf("loaded %q", res.Value)
	}
	raw, ok := store.Get("k")
	if !ok || !strings.Contains(string(raw), `"n"`) {
		t.Fatalf("stored %s", raw)
	}
}

func TestScriptCollectsOutput(t *testing.T) {
	res, err := Run(context.Background(), `await text("hello"); await console.log("line"); await image({type:"image"}); return "done";`, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "done" || len(res.Texts) != 1 || res.Texts[0] != "hello" || len(res.Console) != 1 || !strings.Contains(res.Console[0], "line") || len(res.Images) != 1 {
		t.Fatalf("%#v", res)
	}
}

func TestScriptModels(t *testing.T) {
	res, err := Run(context.Background(), `
		const bad = await models.classify({provider:"x", id:"y"}, {});
		const imgs = await models.generateImages({provider:"x", id:"y"}, {});
		const rows = await models.getAvailableOfType("chat", "local");
		return bad.errorMessage === "not implemented" && imgs.stopReason === "error" && rows.length === 1 && rows[0].id === "m";
	`, Env{
		Models: func(typ, provider string) ([]ModelInfo, error) {
			if typ != "chat" || provider != "local" {
				return nil, nil
			}
			return []ModelInfo{{Type: "chat", Provider: "local", ID: "m", Name: "M", API: "openai", Input: []string{"text"}}}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "true" {
		t.Fatalf("value %q", res.Value)
	}
}

func TestScriptSearchTools(t *testing.T) {
	res, err := Run(context.Background(), `const found = await searchTools("read", {limit: 1}); return found[0].name;`, Env{
		Tools: []Tool{{Name: "read", Description: "read files"}, {Name: "bash", Description: "shell"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Value != "read" {
		t.Fatalf("found %q", res.Value)
	}
}
