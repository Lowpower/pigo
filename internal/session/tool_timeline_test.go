package session

import (
	"testing"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/prompt"
)

func TestToolMarkersBaselinePatchAndCheckpoint(t *testing.T) {
	m := New(t.TempDir(), t.TempDir())
	declare := DeclareSystem(map[string]string{prompt.SectionPreamble: "base"}, []ai.Tool{{Name: "read", Description: "r"}})
	if _, err := m.AppendMessage("system", declare); err != nil {
		t.Fatal(err)
	}
	user, err := m.AppendMessage("user", map[string]any{"role": "user", "content": "keep"})
	if err != nil {
		t.Fatal(err)
	}
	patch := DiffSystem(ReplaySystem(m.GetBranch("")), map[string]string{prompt.SectionPreamble: "base"}, []ai.Tool{
		{Name: "read", Description: "r"},
		{Name: "write", Description: "w"},
	})
	if patch == nil {
		t.Fatal("expected a tool patch")
	}
	if _, err := m.AppendMessage("system", *patch); err != nil {
		t.Fatal(err)
	}
	check := CheckpointSystem("", map[string]string{prompt.SectionPreamble: "base"}, []ai.Tool{
		{Name: "read", Description: "r"},
		{Name: "write", Description: "w"},
	})
	if _, err := m.AppendCompaction("sum", user.ID, 3, CompactionMeta{SystemMessage: &check}); err != nil {
		t.Fatal(err)
	}
	removed := DiffSystem(ReplaySystem(m.GetBranch("")), map[string]string{prompt.SectionPreamble: "base"}, []ai.Tool{{Name: "read", Description: "r"}})
	if removed == nil {
		t.Fatal("expected a removal")
	}
	if _, err := m.AppendMessage("system", *removed); err != nil {
		t.Fatal(err)
	}

	markers := ToolMarkers(ContextEntries(m))
	if len(markers) != 2 {
		t.Fatalf("markers = %+v", markers)
	}
	if markers[0].Before != 0 || len(markers[0].Added) != 2 || markers[0].Added[1].Name != "write" {
		t.Fatalf("baseline = %+v", markers[0])
	}
	if markers[1].Before != 2 || len(markers[1].Removed) != 1 || markers[1].Removed[0] != "write" || len(markers[1].Added) != 0 {
		t.Fatalf("removal = %+v", markers[1])
	}
}
