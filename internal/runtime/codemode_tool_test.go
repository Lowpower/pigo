package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/config"
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
	if findBuiltin(e, "codemode") == nil {
		t.Fatal("codemode is not registered")
	}
	for _, name := range e.activeSnapshot() {
		if name == "codemode" {
			t.Fatal("codemode is enabled by default")
		}
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

func TestCodemodeModeOnlyDeclaresCodemode(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		ToolAllow:    []string{"codemode", "bash", "read"},
		Config:       config.Config{Codemode: config.CodemodeSettings{Mode: "only"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var names []string
	for _, tool := range e.PromptTools() {
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "codemode" {
		t.Fatalf("tools %v", names)
	}
	if strings.Contains(e.System, "\n- bash:") || strings.Contains(e.System, "\n- read:") {
		t.Fatalf("system prompt listed other tools:\n%s", e.System)
	}
	desc := findBuiltin(e, "codemode").Description()
	if !strings.Contains(desc, "await tools.bash") || !strings.Contains(desc, "await tools.read") {
		t.Fatalf("description %s", desc)
	}
}

func TestCodemodeModeOnKeepsOtherTools(t *testing.T) {
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
	var bash ai.Tool
	seen := map[string]bool{}
	for _, tool := range e.PromptTools() {
		seen[tool.Name] = true
		if tool.Name == "bash" {
			bash = tool
		}
	}
	if !seen["codemode"] || !seen["bash"] {
		t.Fatalf("tools %v", seen)
	}
	if !strings.Contains(bash.Description, "await tools.bash(args)") {
		t.Fatalf("bash description %s", bash.Description)
	}
}

func TestCodemodeInlineBudgetZeroSkipsDeclarations(t *testing.T) {
	ctx := context.Background()
	zero := 0
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		ToolAllow:    []string{"codemode", "bash"},
		Config: config.Config{Codemode: config.CodemodeSettings{
			Mode:         "only",
			InlineBudget: &zero,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	desc := findBuiltin(e, "codemode").Description()
	if strings.Contains(desc, "await tools.bash") {
		t.Fatalf("budget 0 listed bash:\n%s", desc)
	}
}

func TestCodemodeInvalidImageStaysOutOfTheResult(t *testing.T) {
	ctx := context.Background()
	e, err := New(ctx, Options{
		Cwd:          t.TempDir(),
		AgentDir:     t.TempDir(),
		NoExtensions: true,
		ToolAllow:    []string{"codemode"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	out, isErr := findBuiltin(e, "codemode").Execute(ctx, map[string]any{
		"code": `await image({type:"image", data:"@@bad@@", mimeType:"image/png"})`,
	})
	if !isErr || strings.Contains(out, "@@bad@@") {
		t.Fatalf("result %q err %v", out, isErr)
	}
	last := out
	if i := strings.LastIndex(strings.TrimRight(out, "\n"), "\n"); i >= 0 {
		last = out[i+1:]
	}
	if !strings.HasPrefix(last, "Took ") {
		t.Fatalf("duration line %q in %q", last, out)
	}
}

func TestReplayToolsDropHiddenNamesInOnlyMode(t *testing.T) {
	e := &Engine{Opts: Options{Config: config.Config{Codemode: config.CodemodeSettings{Mode: "only"}}}}
	e.activeToolNames = []string{"codemode", "read"}
	got := e.presentTools([]ai.Tool{
		{Name: "read", Description: "read"},
		{Name: "codemode", Description: "code"},
	})
	if len(got) != 1 || got[0].Name != "codemode" {
		t.Fatalf("%v", got)
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
