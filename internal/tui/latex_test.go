package tui

import "testing"

func TestTransformLatexJoins(t *testing.T) {
	got := transformLatexJoins(`R\Join S, R\bowtie T, R\ltimes U, R\rtimes V`)
	want := "R⋈ S, R⋈ T, R⋉ U, R⋊ V"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = transformLatexJoins(`A\leftouterjoin B \rightouterjoin C \fullouterjoin D`)
	if got != "A⟕ B ⟖ C ⟗ D" {
		t.Fatalf("outer: %q", got)
	}
	if transformLatexJoins("no math") != "no math" {
		t.Fatal("plain text")
	}
}
