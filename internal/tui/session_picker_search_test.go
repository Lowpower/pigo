package tui

import (
	"testing"
	"time"

	"github.com/Lowpower/pigo/internal/session"
)

func TestFilterPickerNamedAndRegex(t *testing.T) {
	now := time.Now()
	src := []session.Summary{
		{ID: "1", Name: "Alpha", FirstMessage: "hello", SearchText: "1 Alpha hello", Modified: now},
		{ID: "2", Name: "", FirstMessage: "beta", SearchText: "2 beta", Modified: now.Add(-time.Hour)},
	}
	named := filterPickerSessions(src, "", sortRecent, nameNamed)
	if len(named) != 1 || named[0].session.Name != "Alpha" {
		t.Fatalf("named = %+v", named)
	}
	re := filterPickerSessions(src, "re:bet", sortRecent, nameAll)
	if len(re) != 1 || re[0].session.FirstMessage != "beta" {
		t.Fatalf("regex = %+v", re)
	}
}

func TestFilterPickerThreadedUsesParent(t *testing.T) {
	now := time.Now()
	parent := "/tmp/parent.jsonl"
	src := []session.Summary{
		{Path: parent, ID: "p", Name: "root", SearchText: "p root", Modified: now.Add(-time.Hour)},
		{Path: "/tmp/child.jsonl", ID: "c", Name: "child", ParentSession: parent, SearchText: "c child", Modified: now},
	}
	rows := filterPickerSessions(src, "", sortThreaded, nameAll)
	if len(rows) != 2 {
		t.Fatalf("len=%d", len(rows))
	}
	var child pickerRow
	for _, r := range rows {
		if r.session.ID == "c" {
			child = r
		}
	}
	if child.depth != 1 || pickerTreePrefix(child) == "" {
		t.Fatalf("child depth=%d prefix=%q rows=%+v", child.depth, pickerTreePrefix(child), rows)
	}
}

func TestCycleSortMode(t *testing.T) {
	if cycleSortMode(sortThreaded) != sortRecent {
		t.Fatal(cycleSortMode(sortThreaded))
	}
	if cycleSortMode(sortRecent) != sortRelevance {
		t.Fatal(cycleSortMode(sortRecent))
	}
	if cycleSortMode(sortRelevance) != sortThreaded {
		t.Fatal(cycleSortMode(sortRelevance))
	}
}
