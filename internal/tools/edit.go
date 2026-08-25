package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// editTool performs exact-text replacements in a file: each edit's oldText must
// match a unique, non-overlapping region of the original file, and all edits are
// applied against the original (not incrementally).
type editTool struct{}

type editReplace struct {
	OldText string `json:"oldText" jsonschema:"description=Exact text for one replacement. Must be unique in the original file and not overlap other edits."`
	NewText string `json:"newText" jsonschema:"description=Replacement text for this edit."`
}

type editParams struct {
	Path  string        `json:"path" jsonschema:"description=Path to the file to edit (relative or absolute)"`
	Edits []editReplace `json:"edits" jsonschema:"description=One or more exact-text replacements, each matched against the original file."`
}

func (editTool) Name() string { return "edit" }

func (editTool) Description() string {
	return "Edit a file using exact text replacement. Every edit's oldText must match a unique, non-overlapping region of the original file. Merge nearby changes into one edit instead of overlapping edits."
}

func (editTool) Schema() map[string]any { return schemaFor(&editParams{}) }

type editSpan struct {
	start, end int
	newText    string
}

func (editTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	var p editParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Path == "" {
		return "path is required", true
	}
	if len(p.Edits) == 0 {
		return "edits must contain at least one replacement", true
	}

	raw, err := os.ReadFile(p.Path)
	if err != nil {
		return err.Error(), true
	}
	original := string(raw)

	spans := make([]editSpan, 0, len(p.Edits))
	for i, e := range p.Edits {
		if e.OldText == "" {
			return fmt.Sprintf("edits[%d].oldText must not be empty", i), true
		}
		switch strings.Count(original, e.OldText) {
		case 0:
			return fmt.Sprintf("edits[%d].oldText not found in %s", i, p.Path), true
		case 1:
			idx := strings.Index(original, e.OldText)
			spans = append(spans, editSpan{start: idx, end: idx + len(e.OldText), newText: e.NewText})
		default:
			return fmt.Sprintf("edits[%d].oldText is not unique in %s (matches multiple times)", i, p.Path), true
		}
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return "overlapping edits are not allowed; merge them into one edit", true
		}
	}

	var b strings.Builder
	prev := 0
	for _, s := range spans {
		b.WriteString(original[prev:s.start])
		b.WriteString(s.newText)
		prev = s.end
	}
	b.WriteString(original[prev:])
	updated := b.String()

	if err := os.WriteFile(p.Path, []byte(updated), 0o644); err != nil {
		return err.Error(), true
	}

	return fmt.Sprintf("Successfully replaced %d block(s) in %s.\n%s",
		len(spans), p.Path, plainDiff(original, updated)), false
}

// plainDiff renders a compact +/- line diff (no ANSI) using go-diff.
func plainDiff(before, after string) string {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffCleanupSemantic(dmp.DiffMain(before, after, false))

	var b strings.Builder
	for _, d := range diffs {
		lines := strings.Split(strings.TrimRight(d.Text, "\n"), "\n")
		for _, ln := range lines {
			switch d.Type {
			case diffmatchpatch.DiffInsert:
				fmt.Fprintf(&b, "+ %s\n", ln)
			case diffmatchpatch.DiffDelete:
				fmt.Fprintf(&b, "- %s\n", ln)
			}
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
