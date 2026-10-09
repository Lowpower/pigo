package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/skills"
	"github.com/Lowpower/pigo/internal/slash"
)

func skillSlashFixture() []slash.Command {
	return append(slash.Builtins(),
		slash.Command{Name: "skill:deep-research", Description: "deep"},
		slash.Command{Name: "skill:research-idea", Description: "idea"},
		slash.Command{Name: "skill:review", Description: "Review a change"},
		slash.Command{Name: "commit", Description: "commit template"},
	)
}

func completeValues(items []completeItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Value
	}
	return out
}

func indexOfValue(items []completeItem, want string) int {
	for i, it := range items {
		if it.Value == want {
			return i
		}
	}
	return -1
}

func TestSlashSuggestionsRejectBareSlashLineWithArgs(t *testing.T) {
	if _, _, ok := slashSuggestions("/model claude", slash.Builtins()); ok {
		t.Fatal("arguments are not slash-name completions")
	}
}

func TestApplyCompleteSlashAndFile(t *testing.T) {
	got, col := applyComplete("/hotk", "/hotk", 5, completeItem{Value: "hotkeys"})
	if got != "/hotkeys " || col != len("/hotkeys ") {
		t.Fatalf("slash got %q col=%d", got, col)
	}
	got, _ = applyComplete("@unique", "@unique", 7, completeItem{Value: "@unique_alpha.go"})
	if got != "@unique_alpha.go " {
		t.Fatalf("file got %q", got)
	}
	got, _ = applyComplete("src/", "src/", 4, completeItem{Value: "src/pkg/", Dir: true})
	if got != "src/pkg/" {
		t.Fatalf("dir must not gain a trailing space, got %q", got)
	}
}

func TestSlashSkillPrefixListsLoadedSkills(t *testing.T) {
	items, _, ok := slashSuggestions("/skill:", skillSlashFixture())
	if !ok {
		t.Fatal("expected skill prefix completions")
	}
	got := completeValues(items)
	for _, want := range []string{"skill:deep-research", "skill:research-idea", "skill:review"} {
		if indexOfValue(items, want) < 0 {
			t.Fatalf("missing %s in %v", want, got)
		}
		if items[indexOfValue(items, want)].Label != "/"+want {
			t.Fatalf("label = %q", items[indexOfValue(items, want)].Label)
		}
	}
	for _, it := range items {
		if !strings.HasPrefix(it.Value, "skill:") {
			t.Fatalf("non-skill item %q in %v", it.Value, got)
		}
	}
}

func TestSlashSkillPrefixFiltersReview(t *testing.T) {
	items, _, ok := slashSuggestions("/skill:re", skillSlashFixture())
	if !ok || indexOfValue(items, "skill:review") < 0 {
		t.Fatalf("items=%v ok=%v", completeValues(items), ok)
	}
}

func TestSlashSkillCompletionExpands(t *testing.T) {
	items, prefix, ok := slashSuggestions("/skill:review", skillSlashFixture())
	if !ok || len(items) == 0 || items[0].Value != "skill:review" {
		t.Fatalf("items=%v ok=%v", completeValues(items), ok)
	}
	got, _ := applyComplete("/skill:review", prefix, len("/skill:review"), items[0])
	if got != "/skill:review " {
		t.Fatalf("inserted %q", got)
	}
	cmd, ok := slash.Parse(got)
	if !ok {
		t.Fatalf("parse %q", got)
	}
	body, ok := skills.ExpandCommand([]skills.Skill{{
		Name: "review", Body: "SKILL-BODY", FilePath: "/tmp/review/SKILL.md",
	}}, cmd.Name, cmd.Rest)
	if !ok || !strings.Contains(body, "SKILL-BODY") || !strings.Contains(body, `name="review"`) {
		t.Fatalf("expand %q ok=%v", body, ok)
	}
}

func TestSlashBareSkillNameStillCompletes(t *testing.T) {
	items, prefix, ok := slashSuggestions("/review", skillSlashFixture())
	if !ok || len(items) == 0 || items[0].Value != "skill:review" {
		t.Fatalf("items=%v ok=%v", completeValues(items), ok)
	}
	got, _ := applyComplete("/review", prefix, len("/review"), items[0])
	if got != "/skill:review " {
		t.Fatalf("inserted %q", got)
	}
}

func TestSlashSkillIdeaRanksBareNameFirst(t *testing.T) {
	items, _, ok := slashSuggestions("/idea", skillSlashFixture())
	if !ok {
		t.Fatal("expected matches")
	}
	idea := indexOfValue(items, "skill:research-idea")
	deep := indexOfValue(items, "skill:deep-research")
	if idea < 0 || deep < 0 || idea > deep {
		t.Fatalf("order=%v", completeValues(items))
	}
	if items[0].Value != "skill:research-idea" {
		t.Fatalf("first=%q order=%v", items[0].Value, completeValues(items))
	}
}

func TestSlashBuiltinAndTemplateNamesStayBare(t *testing.T) {
	items, _, ok := slashSuggestions("/hotk", skillSlashFixture())
	if !ok || len(items) == 0 || items[0].Value != "hotkeys" {
		t.Fatalf("builtin items=%v ok=%v", completeValues(items), ok)
	}
	items, _, ok = slashSuggestions("/commit", skillSlashFixture())
	if !ok || len(items) == 0 || items[0].Value != "commit" {
		t.Fatalf("template items=%v ok=%v", completeValues(items), ok)
	}
}

func TestExtraSlashCommandsUseSkillPrefix(t *testing.T) {
	m := editorModel()
	m.engine = &runtime.Engine{
		Skills: []skills.Skill{
			{Name: "review", Description: "Review a change"},
			{Name: "summarize", Description: "Summarize"},
		},
		Templates: []prompt.Template{{Name: "commit", Description: "commit template"}},
	}
	var names []string
	for _, c := range m.extraSlashCommands() {
		names = append(names, c.Name)
	}
	joined := strings.Join(names, ",")
	if !strings.Contains(joined, "skill:review") || !strings.Contains(joined, "skill:summarize") {
		t.Fatalf("names=%s", joined)
	}
	for _, name := range names {
		if name == "review" || name == "summarize" {
			t.Fatalf("bare skill name registered: %s", joined)
		}
	}
	if !strings.Contains(joined, "commit") {
		t.Fatalf("template dropped: %s", joined)
	}
	m.editor.SetValue("/skill:rev")
	m = send(m, tea.KeyMsg{Type: tea.KeyTab})
	if m.editor.Value() != "/skill:review " {
		t.Fatalf("tab inserted %q", m.editor.Value())
	}
}

func TestExtraSlashCommandsOmitSkillsWhenDisabled(t *testing.T) {
	cfg := testCfg()
	off := false
	cfg.EnableSkillCommands = &off
	m := New(cfg)
	m.engine = &runtime.Engine{
		Skills:    []skills.Skill{{Name: "review", Description: "Review a change"}},
		Templates: []prompt.Template{{Name: "commit", Description: "commit template"}},
	}
	for _, c := range m.extraSlashCommands() {
		if c.Name == "review" || c.Name == "skill:review" || strings.HasPrefix(c.Name, "skill:") {
			t.Fatalf("skill command registered: %+v", m.extraSlashCommands())
		}
	}
	m.editor.SetValue("/skill:")
	m.refreshComplete(false)
	for _, it := range m.complete.items {
		if strings.Contains(it.Value, "review") || strings.HasPrefix(it.Value, "skill:") {
			t.Fatalf("disabled completion listed %+v", m.complete.items)
		}
	}
	m.editor.SetValue("/review")
	m.refreshComplete(false)
	for _, it := range m.complete.items {
		if it.Value == "review" || it.Value == "skill:review" {
			t.Fatalf("bare skill listed %+v", m.complete.items)
		}
	}
	m.editor.SetValue("/commit")
	m.refreshComplete(false)
	if !m.complete.active || len(m.complete.items) == 0 || m.complete.items[0].Value != "commit" {
		t.Fatalf("template completion %+v active=%v", m.complete.items, m.complete.active)
	}
}
