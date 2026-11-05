package session

import (
	"encoding/json"

	"github.com/Lowpower/pigo/internal/ai"
)

// ToolMarker is one tool declaration or patch in provider-transcript order.
// Before is the number of non-tool model messages that precede it.
type ToolMarker struct {
	Before  int
	Added   []ai.Tool
	Removed []string
}

// ToolMarkers reads the provider-facing context entries. A compaction
// checkpoint with tools becomes the baseline at that point in the transcript.
// System messages from before that compaction are already folded into the
// checkpoint and are not emitted again.
func ToolMarkers(entries []Entry) []ToolMarker {
	latest := map[string]json.RawMessage{}
	for _, e := range entries {
		if e.Type != "context_edit" || e.TargetID == "" || e.Replacement == nil {
			continue
		}
		latest[e.TargetID] = e.Replacement.Raw
	}
	var checkpoint *Entry
	for i := range entries {
		if entries[i].Type == "compaction" && entries[i].SystemMessage != nil {
			checkpoint = &entries[i]
		}
	}
	var after map[string]bool
	if checkpoint != nil {
		after = entriesAfter(entries, checkpoint.ID)
	}
	var markers []ToolMarker
	nonTool := 0
	for _, e := range entries {
		if e.Type == "context_edit" {
			continue
		}
		if raw, ok := latest[e.ID]; ok && isNullJSON(raw) {
			continue
		}
		if e.Type == "compaction" && e.SystemMessage != nil {
			if marker, ok := toolMarkerFrom(*e.SystemMessage, nonTool); ok {
				markers = append(markers, marker)
			}
		}
		if msg, ok := systemFromEntry(e); ok {
			if checkpoint != nil && !after[e.ID] {
				continue
			}
			if marker, ok := toolMarkerFrom(msg, nonTool); ok {
				markers = append(markers, marker)
			}
			continue
		}
		nonTool += len(RestoreAIMessages([]Entry{e}))
	}
	return markers
}

func toolMarkerFrom(m SystemMessage, before int) (ToolMarker, bool) {
	if len(m.ToolsAdded) == 0 && len(m.ToolsRemoved) == 0 {
		return ToolMarker{}, false
	}
	removed := make([]string, 0, len(m.ToolsRemoved))
	for _, ref := range m.ToolsRemoved {
		if ref.Name != "" {
			removed = append(removed, ref.Name)
		}
	}
	added := append([]ai.Tool(nil), m.ToolsAdded...)
	if len(added) == 0 && len(removed) == 0 {
		return ToolMarker{}, false
	}
	return ToolMarker{Before: before, Added: added, Removed: removed}, true
}

func entriesAfter(entries []Entry, root string) map[string]bool {
	after := map[string]bool{root: true}
	changed := true
	for changed {
		changed = false
		for _, e := range entries {
			if e.ID == "" || after[e.ID] {
				continue
			}
			if e.ParentID != nil && after[*e.ParentID] {
				after[e.ID] = true
				changed = true
			}
		}
	}
	return after
}
