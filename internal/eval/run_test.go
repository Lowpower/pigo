package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pigo-eval-home-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", dir)
	_ = os.Setenv("USERPROFILE", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func clearProviderEnvs(t *testing.T) {
	t.Helper()
	for _, id := range models.ProviderIDs() {
		spec, ok := models.LookupProvider(id)
		if !ok {
			continue
		}
		for _, name := range spec.Env {
			t.Setenv(name, "")
		}
	}
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("ANTHROPIC_OAUTH_TOKEN", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("PIGO_PROVIDER", "")
	t.Setenv("PIGO_MODEL", "")
}

func textReply(s string, tokens int) ai.StreamFn {
	return func(ctx context.Context, _ ai.Context, _ ai.Options) (*ai.EventStream, error) {
		return ai.EmitMessage(ctx, &ai.AssistantMessage{
			Role:       ai.RoleAssistant,
			StopReason: ai.StopStop,
			Provider:   "mock",
			Model:      "mock",
			Content:    []*ai.Content{{Type: ai.KindText, Text: s}},
			Usage:      ai.Usage{Output: tokens, TotalTokens: tokens},
		}), nil
	}
}

func TestHasCredential(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	if HasCredential(dir, "anthropic") {
		t.Fatal("empty store and env should have no credential")
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")
	if !HasCredential(dir, "anthropic") {
		t.Fatal("env key should count")
	}
}

func TestRunDirSkipsWithoutCredential(t *testing.T) {
	clearProviderEnvs(t)
	out := t.TempDir()
	rep, err := RunDir(context.Background(), Options{
		Dir:      "testdata",
		OutDir:   out,
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || rep.Failed != 0 || rep.Passed != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Results) != 1 || rep.Results[0].Status != StatusSkip {
		t.Fatalf("results = %+v", rep.Results)
	}
	if _, err := os.Stat(filepath.Join(out, "report.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRunDirFakeProviderPassAndArtifacts(t *testing.T) {
	clearProviderEnvs(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	out := t.TempDir()
	rep, err := RunDir(context.Background(), Options{
		Dir:      "testdata",
		OutDir:   out,
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Stream:   textReply("Paris", 9),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 1 || rep.Failed != 0 || rep.Skipped != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.PassRate != 1 {
		t.Fatalf("passRate = %v", rep.PassRate)
	}
	r := rep.Results[0]
	if r.Tokens < 9 {
		t.Fatalf("tokens = %d", r.Tokens)
	}
	if r.Latency <= 0 {
		t.Fatalf("latency = %s", r.Latency)
	}
	if r.Session == "" {
		t.Fatal("missing session copy")
	}
	if _, err := os.Stat(r.Session); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(r.Session)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"type":"session"`) {
		t.Fatalf("session jsonl: %s", body)
	}
	if _, err := os.Stat(filepath.Join(home, ".pigo")); !os.IsNotExist(err) {
		t.Fatalf("leaked into HOME: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dumped Report
	if err := json.Unmarshal(raw, &dumped); err != nil {
		t.Fatal(err)
	}
	if dumped.Passed != 1 {
		t.Fatalf("report.json = %s", raw)
	}
}

func TestRunDirFakeProviderFail(t *testing.T) {
	clearProviderEnvs(t)
	rep, err := RunDir(context.Background(), Options{
		Dir:      "testdata",
		OutDir:   t.TempDir(),
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Stream:   textReply("London", 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Passed != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if rep.PassRate != 0 {
		t.Fatalf("passRate = %v", rep.PassRate)
	}
}

func TestSeedFilesWritesRelativePaths(t *testing.T) {
	cwd := t.TempDir()
	if err := seedFiles(cwd, map[string]string{"sub/a.txt": "secret-contents-42"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(cwd, "sub", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "secret-contents-42" {
		t.Fatalf("got %q", b)
	}
}

func TestRunDirSeedsFilesWithoutTouchingScenarioDir(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	body := `{
  "name": "files",
  "prompt": "hi",
  "noTools": true,
  "files": {"hello.txt": "secret-contents-42"},
  "expect": {"contains": ["ok"]}
}`
	if err := os.WriteFile(filepath.Join(dir, "files.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := RunDir(context.Background(), Options{
		Dir:      dir,
		OutDir:   t.TempDir(),
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Stream:   textReply("ok", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 1 {
		t.Fatalf("report = %+v results=%+v", rep, rep.Results)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Fatal("seeded file leaked into scenario dir")
	}
}

func TestFormatReportIncludesMetrics(t *testing.T) {
	text := FormatReport(Report{
		Passed:   1,
		Failed:   1,
		Skipped:  1,
		PassRate: 0.5,
		Results: []Result{
			{Name: "a", Status: StatusPass, Tokens: 10, Latency: 12 * time.Millisecond},
			{Name: "b", Status: StatusFail, Tokens: 2, Latency: time.Millisecond, Error: "contains Paris"},
			{Name: "c", Status: StatusSkip, Error: "no API key"},
		},
	})
	for _, want := range []string{"pass rate", "50%", "a", "pass", "10", "b", "fail", "c", "skip"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
