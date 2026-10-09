package runtime

import (
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/compaction"
	"github.com/Lowpower/pigo/internal/session"
)

// compactionBody is the message span to summarize. A previous compaction summary
// is removed from the span and returned on base so the caller can pass it as
// <previous-summary>. The span starts at that compaction's kept boundary.
func compactionBody(msgs []ai.Message, path []session.Entry) ([]ai.Message, session.CompactionBase) {
	base := session.CompactionBaseFrom(path)
	if !base.Found {
		return msgs, base
	}
	rest := msgs
	if base.Summary != "" && len(rest) > 0 && rest[0].Content == compaction.SummaryPrefix+base.Summary+compaction.SummarySuffix {
		rest = rest[1:]
	}
	expected := session.ModelMessages(base.Entries)
	switch {
	case hasMessagePrefix(rest, expected):
		return rest, base
	case hasMessagePrefix(msgs, expected):
		return msgs, base
	default:
		return expected, base
	}
}

func priorCompactionFiles(base session.CompactionBase) (compaction.FileLists, bool) {
	if !base.Found || base.FromHook {
		return compaction.FileLists{}, false
	}
	return compaction.ParseStoredFiles(base.Details)
}

func branchPriorFiles(entries []session.Entry) (read, modified []string) {
	for _, e := range entries {
		if e.Type != "branch_summary" || e.FromHook {
			continue
		}
		lists, ok := compaction.ParseStoredFiles(e.Details)
		if !ok {
			continue
		}
		read = append(read, lists.ReadFiles...)
		modified = append(modified, lists.ModifiedFiles...)
	}
	return read, modified
}

func hasMessagePrefix(msgs, prefix []ai.Message) bool {
	if len(prefix) == 0 {
		return len(msgs) == 0
	}
	if len(msgs) < len(prefix) {
		return false
	}
	for i := range prefix {
		if !sameCompactionMessage(msgs[i], prefix[i]) {
			return false
		}
	}
	return true
}

func sameCompactionMessage(a, b ai.Message) bool {
	if a.Role != b.Role || a.Content != b.Content || a.ToolCallID != b.ToolCallID || a.ToolName != b.ToolName {
		return false
	}
	return toolSig(a) == toolSig(b)
}

func toolSig(m ai.Message) string {
	if m.Assistant == nil {
		return ""
	}
	sig := ""
	for _, c := range m.Assistant.ToolCalls() {
		path, _ := c.Arguments["path"].(string)
		sig += c.ToolName + "\x00" + path + "\x01"
	}
	return sig
}
