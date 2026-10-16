package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/models"
)

// Settings controls when and how much to compact.
type Settings struct {
	// ReserveTokens is headroom kept below the context window before compacting.
	ReserveTokens int
	// KeepRecentTokens is roughly how many tokens of recent messages to keep verbatim.
	KeepRecentTokens int
	// CustomInstructions is appended to the summarization prompt (RPC compact).
	CustomInstructions string
	// Provider selects the catalog entry whose samplingParams go out with the summary request.
	Provider string
	// PreviousSummary is the last compaction summary. When set, the summarizer
	// updates it instead of writing a new checkpoint from scratch.
	PreviousSummary string
	// PriorRead and PriorModified are file lists from the previous compaction.
	// They are merged only when UsePriorFiles is set.
	PriorRead     []string
	PriorModified []string
	UsePriorFiles bool
}

// FileLists is the readFiles / modifiedFiles payload stored on compaction and
// branch-summary entries.
type FileLists struct {
	ReadFiles     []string `json:"readFiles"`
	ModifiedFiles []string `json:"modifiedFiles"`
}

// Stored returns lists safe to marshal: empty slices stay empty arrays.
func (f FileLists) Stored() FileLists {
	if f.ReadFiles == nil {
		f.ReadFiles = []string{}
	}
	if f.ModifiedFiles == nil {
		f.ModifiedFiles = []string{}
	}
	return f
}

// DefaultSettings is the built-in compaction window.
func DefaultSettings() Settings {
	return Settings{ReserveTokens: 16384, KeepRecentTokens: 20000}
}

// estimatedImageChars is the fixed per-image stand-in used before dividing by 4
// (1200 tokens). It matches pi's ESTIMATED_IMAGE_CHARS.
const estimatedImageChars = 4800

// EstimateTokens approximates a message's token count as ceil(chars/4).
// Assistant messages include thinking and tool-call arguments. Each image adds
// estimatedImageChars. Display text (Message.Text) stays text-only.
func EstimateTokens(m ai.Message) int {
	chars := estimateChars(m)
	return ceilDiv(chars, 4)
}

func estimateChars(m ai.Message) int {
	chars := 0
	if m.Assistant != nil {
		for _, block := range m.Assistant.Content {
			if block == nil {
				continue
			}
			switch block.Type {
			case ai.KindText:
				chars += len(block.Text)
			case ai.KindThinking:
				chars += len(block.Thinking)
			case ai.KindToolCall:
				chars += len(block.ToolName)
				if block.Arguments != nil {
					if raw, err := json.Marshal(block.Arguments); err == nil {
						chars += len(raw)
					}
				}
			}
		}
	} else {
		chars += len(m.Content)
	}
	chars += len(m.Images) * estimatedImageChars
	return chars
}

// EstimateContextTokens sums the estimate over all messages.
func EstimateContextTokens(msgs []ai.Message) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m)
	}
	return total
}

// ContextTokens is the current context occupancy. A valid assistant usage
// (non-aborted, non-error, non-zero) is the baseline; messages after it are
// estimated. System prompt and tool definitions are included only when no
// usage is available, because a real usage already counted them.
func ContextTokens(msgs []ai.Message, system string, tools []ai.Tool) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		usage, ok := validUsage(msgs[i])
		if !ok {
			continue
		}
		total := usageTokens(usage)
		for _, m := range msgs[i+1:] {
			total += EstimateTokens(m)
		}
		return total
	}
	return EstimateContextTokens(msgs) + estimateOverhead(system, tools)
}

func validUsage(m ai.Message) (ai.Usage, bool) {
	a := m.Assistant
	if a == nil {
		return ai.Usage{}, false
	}
	if a.StopReason == ai.StopAborted || a.StopReason == ai.StopError {
		return ai.Usage{}, false
	}
	if usageTokens(a.Usage) <= 0 {
		return ai.Usage{}, false
	}
	return a.Usage, true
}

func usageTokens(u ai.Usage) int {
	if u.TotalTokens > 0 {
		return u.TotalTokens
	}
	return u.Input + u.Output + u.CacheRead + u.CacheWrite
}

func estimateOverhead(system string, tools []ai.Tool) int {
	chars := len(system)
	if len(tools) > 0 {
		if raw, err := json.Marshal(tools); err == nil {
			chars += len(raw)
		}
	}
	return ceilDiv(chars, 4)
}

// ShouldCompact reports whether the context should be compacted:
// contextTokens > contextWindow - reserveTokens.
func ShouldCompact(contextTokens, contextWindow int, s Settings) bool {
	return contextTokens > contextWindow-s.ReserveTokens
}

// ErrNoLegalCut is returned when every message in the span is a tool result.
// Compaction refuses to cut there, because a kept tail would start with a
// tool result that has no matching tool call.
var ErrNoLegalCut = errors.New("compaction: no valid cut point")

// CutPoint is where compaction keeps the verbatim tail.
// Index is the first kept message. When Split is set, TurnStart is the user
// message that opened the turn being split, and messages before that are the
// history summary. Skip means the span fits in keepRecentTokens, or the only
// legal cut leaves nothing to summarize.
type CutPoint struct {
	Index     int
	TurnStart int
	Split     bool
	Skip      bool
}

// FindCutPoint chooses a legal cut. Valid cuts are user and assistant messages
// (bash, custom, and summary entries are already user messages). Tool results
// are never cuts. Walking from the newest message, the first index i whose
// tail reaches keepRecentTokens snaps to the nearest legal cut at or after i.
// If that tail is only tool results, the cut falls back to the newest legal
// cut before i so those results stay with their tool call.
func FindCutPoint(msgs []ai.Message, keepRecentTokens int) (CutPoint, error) {
	none := CutPoint{TurnStart: -1, Skip: true}
	if len(msgs) == 0 {
		return none, nil
	}
	var cutPoints []int
	for i, m := range msgs {
		if isCutPointMessage(m) {
			cutPoints = append(cutPoints, i)
		}
	}
	if len(cutPoints) == 0 {
		return CutPoint{}, ErrNoLegalCut
	}

	acc := 0
	cutIndex := 0
	exceeded := false
	for i := len(msgs) - 1; i >= 0; i-- {
		n := EstimateTokens(msgs[i])
		if n == 0 {
			continue
		}
		acc += n
		if acc >= keepRecentTokens {
			exceeded = true
			cutIndex = -1
			for _, candidate := range cutPoints {
				if candidate >= i {
					cutIndex = candidate
					break
				}
			}
			if cutIndex < 0 {
				cutIndex = cutPoints[len(cutPoints)-1]
			}
			break
		}
	}
	if !exceeded {
		return none, nil
	}

	startsTurn := isTurnStartMessage(msgs[cutIndex])
	turnStart := -1
	if !startsTurn {
		turnStart = findTurnStart(msgs, cutIndex)
	}
	split := !startsTurn && turnStart >= 0
	if cutIndex == 0 && !split {
		return none, nil
	}
	return CutPoint{Index: cutIndex, TurnStart: turnStart, Split: split}, nil
}

func isCutPointMessage(m ai.Message) bool {
	return m.Role == ai.RoleUser || m.Role == ai.RoleAssistant
}

func isTurnStartMessage(m ai.Message) bool {
	return m.Role == ai.RoleUser
}

func findTurnStart(msgs []ai.Message, index int) int {
	for i := index; i >= 0; i-- {
		if isTurnStartMessage(msgs[i]) {
			return i
		}
	}
	return -1
}

// SummarizationPrompt is the structured checkpoint prompt. It is appended as the
// final user turn.
const SummarizationPrompt = `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// UpdateSummarizationPrompt merges new messages into an existing summary.
const UpdateSummarizationPrompt = `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`

// TurnPrefixSummarizationPrompt checkpoints the part of a turn that will not
// be kept verbatim. It is not the full history checkpoint.
const TurnPrefixSummarizationPrompt = `The messages above are earlier context from an ongoing conversation. Later messages are stored separately and do not need to be reconstructed.

Create a concise checkpoint of the user's request and the progress shown above. This checkpoint will be placed before the later messages so the conversation can continue with the necessary context.

## Original Request
[What did the user ask for?]

## Progress So Far
- [Key decisions and work completed in these messages]

## Context Needed to Continue
- [Information from these messages needed to understand the later work]

Only summarize information explicitly present above. Do not infer or recreate later messages.`

// SplitTurnJoiner separates the history checkpoint from the split-turn prefix.
const SplitTurnJoiner = "\n\n---\n\n**Turn Context (split turn):**\n\n"

// NoPriorHistory is the history half of a split turn that has no earlier messages.
const NoPriorHistory = "No prior history."

// SummaryPrefix / SummarySuffix wrap a compaction summary for the LLM.
const SummaryPrefix = `The conversation history before this point was compacted into the following summary:

<summary>
`

// SummarySuffix closes the compaction summary wrapper.
const SummarySuffix = `
</summary>`

// BranchSummaryPrefix wraps a branch summary for the LLM.
const BranchSummaryPrefix = `The following is a summary of a branch that this conversation came back from:

<summary>
`

// BranchSummarySuffix closes the branch summary wrapper.
const BranchSummarySuffix = `</summary>`

// SummaryMarker prefixes the synthetic message that replaces compacted history.
const SummaryMarker = SummaryPrefix

func modelCallOptions(provider, model string, reserveTokens int) ai.Options {
	return ai.Options{
		Model:          model,
		Provider:       provider,
		MaxTokens:      summaryMaxTokens(reserveTokens, models.MaxTokens(provider, model)),
		CacheRetention: "none",
		SamplingParams: ai.ResolveSamplingParams(provider, model, nil),
	}
}

// summaryMaxTokens is floor(0.8 * reserveTokens), capped by the catalog output
// limit when one is known. A non-positive reserve uses the built-in 16384.
func summaryMaxTokens(reserve, modelMax int) int {
	if reserve <= 0 {
		reserve = DefaultSettings().ReserveTokens
	}
	maxTokens := int((int64(reserve) * 8) / 10)
	if maxTokens < 1 {
		maxTokens = 1
	}
	if modelMax > 0 && maxTokens > modelMax {
		return modelMax
	}
	return maxTokens
}

func incompleteSummaryError(label string) error {
	return errors.New(label + " failed: generation hit the token cap and the summary is incomplete")
}

// Summarize asks the model to summarize the given messages using StreamFn,
// returning the assistant's text and the usage of that request. provider is
// the catalog id used to attach samplingParams; an empty provider leaves those
// fields unset. previousSummary selects the update prompt and is sent in a
// <previous-summary> block. reserveTokens sets the output budget; 0 uses
// DefaultSettings. A successful call returns final.Usage even when every
// counter is zero.
func Summarize(ctx context.Context, sf ai.StreamFn, model string, toSummarize []ai.Message, extra, provider, previousSummary string, reserveTokens int) (string, ai.Usage, error) {
	return completeSummary(ctx, sf, model, provider, reserveTokens, summaryUserContent(toSummarize, previousSummary, extra))
}

// SummarizeTurnPrefix checkpoints the kept turn's dropped prefix. It does not
// receive the previous summary or custom instructions. The usage is that
// request's usage.
func SummarizeTurnPrefix(ctx context.Context, sf ai.StreamFn, model string, prefix []ai.Message, provider string, reserveTokens int) (string, ai.Usage, error) {
	return completeSummary(ctx, sf, model, provider, reserveTokens, turnPrefixUserContent(prefix))
}

func completeSummary(ctx context.Context, sf ai.StreamFn, model, provider string, reserveTokens int, userContent string) (string, ai.Usage, error) {
	req := ai.Context{
		System: SummarizationSystemPrompt,
		Messages: []ai.Message{{
			Role:    ai.RoleUser,
			Content: userContent,
		}},
	}
	stream, err := sf(ctx, req, modelCallOptions(provider, model, reserveTokens))
	if err != nil {
		return "", ai.Usage{}, err
	}
	_, final := stream.Collect()
	if final == nil {
		return "", ai.Usage{}, errors.New("compaction: summarization produced no message")
	}
	if final.StopReason == ai.StopAborted {
		return "", ai.Usage{}, ErrSummarizeAborted
	}
	if final.StopReason == ai.StopError {
		return "", ai.Usage{}, &SummarizeError{Cause: final.ErrorMessage}
	}
	if final.StopReason == ai.StopLength {
		return "", ai.Usage{}, incompleteSummaryError("Summarization")
	}
	summary := final.Text()
	if summary == "" {
		return "", ai.Usage{}, errors.New("compaction: summarization produced empty text")
	}
	return summary, final.Usage, nil
}

// Compact summarizes older messages and returns the compacted message list (a
// single summary message followed by the recent tail), the summary, and the
// usage of the summary request. A split turn sums both summary requests.
// When nothing needs summarizing it returns the input unchanged with an empty
// summary and zero usage.
func Compact(ctx context.Context, sf ai.StreamFn, model string, msgs []ai.Message, s Settings) ([]ai.Message, string, ai.Usage, error) {
	cut, err := FindCutPoint(msgs, s.KeepRecentTokens)
	if err != nil {
		return msgs, "", ai.Usage{}, err
	}
	return CompactPrepared(ctx, sf, model, msgs, s, cut)
}

// CompactPrepared summarizes using a cut already chosen by FindCutPoint.
func CompactPrepared(ctx context.Context, sf ai.StreamFn, model string, msgs []ai.Message, s Settings, cut CutPoint) ([]ai.Message, string, ai.Usage, error) {
	if cut.Skip || (cut.Index <= 0 && !cut.Split) {
		return msgs, "", ai.Usage{}, nil
	}
	if cut.Index < 0 || cut.Index > len(msgs) {
		return msgs, "", ai.Usage{}, ErrNoLegalCut
	}
	var summary string
	var usage ai.Usage
	var err error
	if cut.Split && cut.TurnStart >= 0 && cut.TurnStart < cut.Index {
		summary, usage, err = summarizeSplit(ctx, sf, model, msgs, s, cut)
	} else if cut.Index <= 0 {
		return msgs, "", ai.Usage{}, nil
	} else {
		summary, usage, err = Summarize(ctx, sf, model, msgs[:cut.Index], s.CustomInstructions, s.Provider, s.PreviousSummary, s.ReserveTokens)
	}
	if err != nil {
		return msgs, "", ai.Usage{}, err
	}
	files := CollectFileLists(msgs[:cut.Index], s.PriorRead, s.PriorModified, s.UsePriorFiles)
	summary = AppendFileSections(summary, files)
	compacted := make([]ai.Message, 0, len(msgs)-cut.Index+1)
	compacted = append(compacted, ai.Message{Role: ai.RoleUser, Content: SummaryPrefix + summary + SummarySuffix})
	compacted = append(compacted, msgs[cut.Index:]...)
	return compacted, summary, usage, nil
}

func summarizeSplit(ctx context.Context, sf ai.StreamFn, model string, msgs []ai.Message, s Settings, cut CutPoint) (string, ai.Usage, error) {
	history := s.PreviousSummary
	if history == "" {
		history = NoPriorHistory
	}
	var usage ai.Usage
	if hist := msgs[:cut.TurnStart]; len(hist) > 0 {
		var histUsage ai.Usage
		var err error
		history, histUsage, err = Summarize(ctx, sf, model, hist, s.CustomInstructions, s.Provider, s.PreviousSummary, s.ReserveTokens)
		if err != nil {
			return "", ai.Usage{}, err
		}
		usage = addSummaryUsage(usage, histUsage)
	}
	prefix, prefixUsage, err := SummarizeTurnPrefix(ctx, sf, model, msgs[cut.TurnStart:cut.Index], s.Provider, s.ReserveTokens)
	if err != nil {
		return "", ai.Usage{}, err
	}
	return history + SplitTurnJoiner + prefix, addSummaryUsage(usage, prefixUsage), nil
}

func addSummaryUsage(a, b ai.Usage) ai.Usage {
	a.Input += b.Input
	a.Output += b.Output
	a.CacheRead += b.CacheRead
	a.CacheWrite += b.CacheWrite
	a.CacheWrite1h += b.CacheWrite1h
	a.Reasoning += b.Reasoning
	a.TotalTokens += b.TotalTokens
	a.Cost.Input += b.Cost.Input
	a.Cost.Output += b.Cost.Output
	a.Cost.CacheRead += b.Cost.CacheRead
	a.Cost.CacheWrite += b.Cost.CacheWrite
	a.Cost.Total += b.Cost.Total
	return a
}

func turnPrefixUserContent(msgs []ai.Message) string {
	var b strings.Builder
	b.WriteString("<conversation>\n")
	b.WriteString(serializeConversation(msgs))
	b.WriteString("\n</conversation>\n\n")
	b.WriteString(TurnPrefixSummarizationPrompt)
	return b.String()
}

func summaryUserContent(msgs []ai.Message, previous, extra string) string {
	var b strings.Builder
	b.WriteString("<conversation>\n")
	b.WriteString(serializeConversation(msgs))
	b.WriteString("\n</conversation>\n\n")
	prompt := SummarizationPrompt
	if previous != "" {
		b.WriteString("<previous-summary>\n")
		b.WriteString(previous)
		b.WriteString("\n</previous-summary>\n\n")
		prompt = UpdateSummarizationPrompt
	}
	b.WriteString(prompt)
	if extra != "" {
		b.WriteString("\n\nAdditional focus: ")
		b.WriteString(extra)
	}
	return b.String()
}

// CollectFileLists extracts read, write, and edit paths from assistant tool
// calls. usePrior merges an earlier compaction's lists. A file that was both
// read and modified is only listed as modified. Both slices are sorted and
// non-nil.
func CollectFileLists(msgs []ai.Message, priorRead, priorModified []string, usePrior bool) FileLists {
	read := map[string]struct{}{}
	modified := map[string]struct{}{}
	add := func(set map[string]struct{}, path string) {
		if path != "" {
			set[path] = struct{}{}
		}
	}
	if usePrior {
		for _, path := range priorRead {
			add(read, path)
		}
		for _, path := range priorModified {
			add(modified, path)
		}
	}
	for _, msg := range msgs {
		if msg.Assistant == nil {
			continue
		}
		for _, call := range msg.Assistant.ToolCalls() {
			path, _ := call.Arguments["path"].(string)
			switch call.ToolName {
			case "read":
				add(read, path)
			case "write", "edit":
				add(modified, path)
			}
		}
	}
	readFiles := make([]string, 0)
	for path := range read {
		if _, ok := modified[path]; ok {
			continue
		}
		readFiles = append(readFiles, path)
	}
	modifiedFiles := make([]string, 0, len(modified))
	for path := range modified {
		modifiedFiles = append(modifiedFiles, path)
	}
	sort.Strings(readFiles)
	sort.Strings(modifiedFiles)
	return FileLists{ReadFiles: readFiles, ModifiedFiles: modifiedFiles}
}

// AppendFileSections adds the file lists the model did not write. Empty lists
// add no tags.
func AppendFileSections(summary string, lists FileLists) string {
	var sections []string
	if len(lists.ReadFiles) > 0 {
		sections = append(sections, "<read-files>\n"+strings.Join(lists.ReadFiles, "\n")+"\n</read-files>")
	}
	if len(lists.ModifiedFiles) > 0 {
		sections = append(sections, "<modified-files>\n"+strings.Join(lists.ModifiedFiles, "\n")+"\n</modified-files>")
	}
	if len(sections) == 0 {
		return summary
	}
	return summary + "\n\n" + strings.Join(sections, "\n\n")
}

// ParseStoredFiles reads readFiles and modifiedFiles from a compaction or
// branch-summary details object.
func ParseStoredFiles(raw []byte) (FileLists, bool) {
	if len(raw) == 0 {
		return FileLists{}, false
	}
	var lists FileLists
	if err := json.Unmarshal(raw, &lists); err != nil {
		return FileLists{}, false
	}
	return lists, true
}

func ceilDiv(a, b int) int {
	if a <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
