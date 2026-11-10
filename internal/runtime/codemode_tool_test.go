package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/tools"
)

func TestCodemodeStaysOffByDefault(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if findBuiltin(e, "codemode") != nil {
		t.Fatal("codemode is in the default tool set")
	}
	if strings.Contains(e.System, "await searchTools()") {
		t.Fatal("default system prompt describes codemode")
	}
}

func TestCodemodeScriptCallsBash(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		ToolAllow:    []string{"codemode", "bash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	tool := findBuiltin(e, "codemode")
	if tool == nil {
		t.Fatal("codemode was not registered")
	}
	if !strings.Contains(tool.Description(), "await searchTools()") || strings.Contains(tool.Description(), "pi.dev") {
		t.Fatal(tool.Description())
	}
	if !strings.Contains(e.System, "await searchTools()") {
		t.Fatal("system prompt omitted the codemode description")
	}
	out, isErr := tool.Execute(ctx, map[string]any{
		"code": `await tools.bash({command:"printf hi"})`,
	})
	if isErr || !strings.HasPrefix(out, "hi") {
		t.Fatalf("bash %q err %v", out, isErr)
	}
	out, isErr = tool.Execute(ctx, map[string]any{
		"code": `await tools.Bash({})`,
	})
	if !isErr || !strings.Contains(out, "tools.bash") {
		t.Fatalf("suggest %q err %v", out, isErr)
	}
	out, isErr = tool.Execute(ctx, map[string]any{
		"code": `const row = await models.classify({provider:"x", id:"y"}, {}); return row.errorMessage;`,
	})
	if isErr || !strings.Contains(out, "not implemented") {
		t.Fatalf("classify %q err %v", out, isErr)
	}
	_, isErr = tool.Execute(ctx, map[string]any{"code": "   "})
	if !isErr {
		t.Fatal("blank code ran")
	}
}

func findBuiltin(e *Engine, name string) tools.Tool {
	if e == nil || e.Tools == nil {
		return nil
	}
	for _, tool := range e.Tools.List() {
		if tool.Name() == name {
			return tool
		}
	}
	return nil
}
