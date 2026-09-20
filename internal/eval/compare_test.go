package eval

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
)

const indentDocsJSON = `{
  "name": "indent",
  "prompt": "What indent style should we use? Reply with only tabs or spaces.",
  "noTools": true,
  "files": {
    "AGENTS.md": "Always use tabs for indentation. Never use spaces.",
    "readme.txt": "a fixture that is not a doc file"
  },
  "expect": { "contains": ["tabs"] }
}`

func writeJSON(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tabsIfDocs(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
	text, tokens, cost := "spaces", 4, 0.002
	if strings.Contains(req.System, "Always use tabs") {
		text, tokens, cost = "tabs", 5, 0.003
	}
	return ai.EmitMessage(ctx, &ai.AssistantMessage{
		Role:       ai.RoleAssistant,
		StopReason: ai.StopStop,
		Provider:   "mock",
		Model:      "mock",
		Content:    []*ai.Content{{Type: ai.KindText, Text: text}},
		Usage:      ai.Usage{Output: tokens, TotalTokens: tokens, Cost: ai.UsageCost{Total: cost}},
	}), nil
}

func TestLoadDirMarksDocsLiftFromFilename(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	writeJSON(t, dir, "smoke.json", `{
  "name": "smoke",
  "prompt": "hi",
  "noTools": true,
  "expect": {"contains": ["Paris"]}
}`)
	got, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("scenarios = %d", len(got))
	}
	if got[0].Name != "indent" || !got[0].DocsLift {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Name != "smoke" || got[1].DocsLift {
		t.Fatalf("second = %+v", got[1])
	}
}

func TestDocsLiftRunsPairedVariantsAndLift(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	out := t.TempDir()
	rep, err := RunDir(context.Background(), Options{
		Dir:      dir,
		OutDir:   out,
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Stream:   tabsIfDocs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 0 {
		t.Fatalf("baseline grade fail should not fail the suite: %+v results=%+v", rep, rep.Results)
	}
	if len(rep.Results) != 2 {
		t.Fatalf("results = %d, want 2 arms", len(rep.Results))
	}
	byVar := map[string]Result{}
	for _, r := range rep.Results {
		byVar[r.Variant] = r
		if r.Name != "indent" || r.Repeat != 1 {
			t.Fatalf("result = %+v", r)
		}
	}
	base, ok := byVar[VariantWithoutDocs]
	if !ok || base.Status != StatusFail {
		t.Fatalf("without_docs = %+v", base)
	}
	cand, ok := byVar[VariantWithDocs]
	if !ok || cand.Status != StatusPass {
		t.Fatalf("with_docs = %+v", cand)
	}
	if !strings.Contains(cand.Session, filepath.Join("sessions", VariantWithDocs)) {
		t.Fatalf("candidate session path = %s", cand.Session)
	}
	if !strings.Contains(base.Session, filepath.Join("sessions", VariantWithoutDocs)) {
		t.Fatalf("baseline session path = %s", base.Session)
	}
	if _, err := os.Stat(cand.Session); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base.Session); err != nil {
		t.Fatal(err)
	}
	if cand.Cost <= base.Cost {
		t.Fatalf("cost with=%v without=%v", cand.Cost, base.Cost)
	}
	cmp := rep.Comparison
	if cmp == nil {
		t.Fatal("missing comparison")
	}
	if cmp.EligiblePairs != 1 || cmp.BlockedPairs != 0 || cmp.TotalPairs != 1 {
		t.Fatalf("pairs = %+v", cmp)
	}
	if cmp.Lift == nil || *cmp.Lift != 1 {
		t.Fatalf("lift = %v", cmp.Lift)
	}
	if cmp.ControlPassRate == nil || *cmp.ControlPassRate != 0 {
		t.Fatalf("control pass rate = %v", cmp.ControlPassRate)
	}
	if cmp.TreatmentPassRate == nil || *cmp.TreatmentPassRate != 1 {
		t.Fatalf("treatment pass rate = %v", cmp.TreatmentPassRate)
	}
	if cmp.Tokens.MeanDelta == nil || *cmp.Tokens.MeanDelta <= 0 {
		t.Fatalf("tokens delta = %+v", cmp.Tokens)
	}
	if cmp.Cost.MeanDelta == nil || *cmp.Cost.MeanDelta <= 0 {
		t.Fatalf("cost delta = %+v", cmp.Cost)
	}
	text := FormatReport(rep)
	for _, want := range []string{"without_docs", "with_docs", "lift", "+100"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	raw, err := os.ReadFile(filepath.Join(out, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var dumped Report
	if err := json.Unmarshal(raw, &dumped); err != nil {
		t.Fatal(err)
	}
	if dumped.Comparison == nil || dumped.Comparison.Lift == nil || *dumped.Comparison.Lift != 1 {
		t.Fatalf("report.json comparison = %s", raw)
	}
}

func TestHostAndDocsLiftShareADirectory(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	writeJSON(t, dir, "smoke.json", `{
  "name": "smoke",
  "prompt": "capital",
  "noTools": true,
  "expect": {"contains": ["Paris"]}
}`)
	rep, err := RunDir(context.Background(), Options{
		Dir:      dir,
		OutDir:   t.TempDir(),
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			if strings.Contains(req.System, "Always use tabs") {
				return textReply("tabs", 1)(ctx, req, ai.Options{})
			}
			if strings.Contains(strings.Join(messageTexts(req), " "), "capital") {
				return textReply("Paris", 1)(ctx, req, ai.Options{})
			}
			return textReply("spaces", 1)(ctx, req, ai.Options{})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 1 || rep.Failed != 0 {
		t.Fatalf("host counts = %+v results=%+v", rep, rep.Results)
	}
	if len(rep.Results) != 3 {
		t.Fatalf("results = %d", len(rep.Results))
	}
}

func messageTexts(req ai.Context) []string {
	out := make([]string, 0, len(req.Messages)+1)
	out = append(out, req.System)
	for _, m := range req.Messages {
		out = append(out, m.Text())
	}
	return out
}

func TestDocsLiftRepetitions(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	rep, err := RunDir(context.Background(), Options{
		Dir:            dir,
		OutDir:         t.TempDir(),
		Provider:       "anthropic",
		Model:          "claude-sonnet-4",
		RunsPerVariant: 2,
		Stream:         tabsIfDocs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 4 {
		t.Fatalf("results = %d", len(rep.Results))
	}
	if rep.Comparison == nil || rep.Comparison.EligiblePairs != 2 {
		t.Fatalf("comparison = %+v", rep.Comparison)
	}
	seen := map[string]bool{}
	for _, r := range rep.Results {
		if r.Repeat < 1 || r.Repeat > 2 {
			t.Fatalf("repeat = %d", r.Repeat)
		}
		if r.Session == "" {
			t.Fatal("missing session")
		}
		seen[r.Variant+"/"+r.Session] = true
	}
	if len(seen) != 4 {
		t.Fatalf("session paths collided: %v", seen)
	}
}

func TestDocsLiftBlockedPairOmitsLift(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	rep, err := RunDir(context.Background(), Options{
		Dir:      dir,
		OutDir:   t.TempDir(),
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Stream: func(ctx context.Context, req ai.Context, _ ai.Options) (*ai.EventStream, error) {
			if strings.Contains(req.System, "Always use tabs") {
				return tabsIfDocs(ctx, req, ai.Options{})
			}
			return nil, errors.New("baseline boom")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Comparison == nil {
		t.Fatal("missing comparison")
	}
	if rep.Comparison.EligiblePairs != 0 || rep.Comparison.BlockedPairs != 1 {
		t.Fatalf("comparison = %+v", *rep.Comparison)
	}
	if rep.Comparison.Lift != nil {
		t.Fatalf("blocked pair should omit headline lift: %+v", *rep.Comparison)
	}
	if rep.Failed == 0 {
		t.Fatal("runtime error should fail the suite")
	}
}

func TestDocsLiftSkipsWithoutCredential(t *testing.T) {
	clearProviderEnvs(t)
	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	rep, err := RunDir(context.Background(), Options{
		Dir:      dir,
		OutDir:   t.TempDir(),
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || len(rep.Results) != 1 {
		t.Fatalf("skip should not expand arms: %+v results=%+v", rep, rep.Results)
	}
	if rep.Results[0].Status != StatusSkip {
		t.Fatalf("result = %+v", rep.Results[0])
	}
}

func TestDocsLiftSkipsWhenDockerMissing(t *testing.T) {
	clearProviderEnvs(t)
	orig := lookDocker
	lookDocker = func(string) (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() { lookDocker = orig })

	dir := t.TempDir()
	writeJSON(t, dir, "indent.docs.json", indentDocsJSON)
	rep, err := RunDir(context.Background(), Options{
		Dir:            dir,
		OutDir:         t.TempDir(),
		Provider:       "anthropic",
		Model:          "claude-sonnet-4",
		ContainerImage: "debian:bookworm-slim",
		Stream:         tabsIfDocs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped != 1 || rep.Failed != 0 {
		t.Fatalf("report = %+v results=%+v", rep, rep.Results)
	}
	if !strings.Contains(strings.ToLower(rep.Results[0].Error), "docker") {
		t.Fatalf("error = %q", rep.Results[0].Error)
	}
}

func TestRepoDocsLiftFixture(t *testing.T) {
	s, err := LoadFile(filepath.Join("..", "..", "evals", "indent.docs.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "indent" || !s.DocsLift || s.Files["AGENTS.md"] == "" {
		t.Fatalf("fixture = %+v", s)
	}
}
