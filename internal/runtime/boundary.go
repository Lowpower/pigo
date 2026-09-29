package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/Lowpower/pigo/internal/agent"
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/session"
)

func (e *Engine) onTurnBoundary(ctx context.Context, ev agent.Event, transcript []ai.Message) bool {
	e.persistBoundary(transcript)
	return e.runBoundary(ctx, "turn_end", &ev)
}

// BeforeSettle is the last actionable extension boundary. Entries are stored
// even when the run is already cancelled. continue asks for one more provider
// request; a cancelled context suppresses that request. agent_settled is
// dispatched when the run will not continue.
func (e *Engine) BeforeSettle(ctx context.Context) bool {
	cont := e.runBoundary(ctx, "agent_before_settle", nil)
	if ctx.Err() != nil {
		cont = false
	}
	if !cont {
		e.DispatchEvent(ctx, "agent_settled", map[string]any{})
	}
	return cont
}

func (e *Engine) refreshModelMessages(msgs []ai.Message) []ai.Message {
	if e == nil || e.Opts.Session == nil || e.contextGen == e.appliedGen {
		return msgs
	}
	e.appliedGen = e.contextGen
	base := session.ModelMessages(session.ContextEntries(e.Opts.Session))
	if e.persisted > len(msgs) {
		e.persisted = len(msgs)
	}
	pending := append([]ai.Message(nil), msgs[e.persisted:]...)
	e.persisted = len(base)
	return append(base, pending...)
}

func (e *Engine) persistBoundary(msgs []ai.Message) {
	if e == nil || e.Opts.Session == nil {
		return
	}
	agentMsgs := make([]agent.Msg, 0, len(msgs))
	for _, m := range msgs {
		agentMsgs = append(agentMsgs, agentMsgFromAI(m))
	}
	if e.persisted > len(agentMsgs) {
		e.persisted = len(agentMsgs)
		return
	}
	e.PersistTranscript(agentMsgs)
}

func (e *Engine) runBoundary(ctx context.Context, event string, ev *agent.Event) bool {
	out := map[string]any{
		"entries":  []any{},
		"continue": false,
	}
	outcome := "completed"
	if ev != nil {
		outcome = boundaryOutcome(ev.Assistant)
		out["message"] = assistantAny(ev.Assistant)
		out["toolResults"] = toolResultsAny(ev.ToolResults)
		out["outcome"] = outcome
		out["messageEntryId"] = ""
		out["toolResultEntryIds"] = []string{}
		if e.Opts.Session != nil {
			id, toolIDs := boundaryTurnIDs(e.Opts.Session)
			out["messageEntryId"] = id
			if toolIDs == nil {
				toolIDs = []string{}
			}
			out["toolResultEntryIds"] = toolIDs
		}
	}
	for _, h := range e.Hosts {
		if h == nil || !h.Subscribed(event) {
			continue
		}
		out["context"] = messagesAsAny(e.previewContext(out["entries"]))
		res, err := h.QueryEvent(ctx, event, out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pigo: extension %q event %s: %v\n", h.Name(), event, err)
			continue
		}
		out = mergePayload(out, res)
	}
	drafts, err := parseBoundaryDrafts(out["entries"])
	if err != nil {
		fmt.Fprintf(os.Stderr, "pigo: %s: %v\n", event, err)
		return false
	}
	if err := e.validateDrafts(drafts); err != nil {
		fmt.Fprintf(os.Stderr, "pigo: %s: %v\n", event, err)
		return false
	}
	if err := e.appendDrafts(drafts); err != nil {
		fmt.Fprintf(os.Stderr, "pigo: %s: %v\n", event, err)
		return false
	}
	if outcome != "completed" {
		return false
	}
	return asBool(out["continue"])
}

func (e *Engine) previewContext(raw any) []ai.Message {
	var path []session.Entry
	if e.Opts.Session != nil {
		path = e.Opts.Session.GetBranch("")
	}
	drafts, err := parseBoundaryDrafts(raw)
	if err != nil {
		return session.ModelMessages(session.BuildContextEntries(path))
	}
	for _, d := range drafts {
		path = append(path, d.previewEntry())
	}
	return session.ModelMessages(session.BuildContextEntries(path))
}

func boundaryOutcome(msg *ai.AssistantMessage) string {
	if msg == nil {
		return "completed"
	}
	switch msg.StopReason {
	case ai.StopError:
		return "error"
	case ai.StopAborted:
		return "aborted"
	default:
		return "completed"
	}
}

func assistantAny(msg *ai.AssistantMessage) any {
	if msg == nil {
		return nil
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

func toolResultsAny(msgs []agent.Msg) []any {
	out := make([]any, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]any{
			"role":       m.Role,
			"text":       m.Text,
			"toolCallId": m.ToolCallID,
			"toolName":   m.ToolName,
			"isError":    m.IsError,
		})
	}
	return out
}

func boundaryTurnIDs(m *session.Manager) (assistantID string, toolIDs []string) {
	path := m.GetBranch("")
	idx := -1
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].Type == "message" && messageRole(path[i]) == "assistant" {
			idx = i
			assistantID = path[i].ID
			break
		}
	}
	if idx < 0 {
		return "", []string{}
	}
	for _, e := range path[idx+1:] {
		if e.Type != "message" {
			continue
		}
		switch messageRole(e) {
		case "toolResult", "tool":
			toolIDs = append(toolIDs, e.ID)
		}
	}
	if toolIDs == nil {
		toolIDs = []string{}
	}
	return assistantID, toolIDs
}

func messageRole(e session.Entry) string {
	var p struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(e.Message, &p)
	return p.Role
}

func agentMsgFromAI(m ai.Message) agent.Msg {
	switch {
	case m.Assistant != nil || m.Role == ai.RoleAssistant:
		text := m.Content
		if m.Assistant != nil && m.Assistant.Text() != "" {
			text = m.Assistant.Text()
		}
		return agent.Msg{Role: agent.RoleAssistant, Text: text, Assistant: m.Assistant}
	case m.Role == ai.RoleToolResult || m.ToolCallID != "":
		return agent.Msg{
			Role:           agent.RoleToolResult,
			Text:           m.Content,
			ToolCallID:     m.ToolCallID,
			ToolName:       m.ToolName,
			IsError:        m.IsError,
			AddedToolNames: append([]string(nil), m.AddedToolNames...),
		}
	default:
		return agent.Msg{Role: agent.RoleUser, Text: m.Content, Images: m.Images}
	}
}

type boundaryDraft struct {
	Type         string
	CustomType   string
	Data         any
	Content      string
	Display      bool
	TargetID     string
	Replacement  json.RawMessage
	Summary      string
	FirstKept    string
	RetainNone   bool
	TokensBefore int
}

func (d boundaryDraft) previewEntry() session.Entry {
	e := session.Entry{Type: d.Type, ID: "preview", TargetID: d.TargetID, CustomType: d.CustomType, Summary: d.Summary}
	switch d.Type {
	case "context_edit":
		raw := d.Replacement
		if len(raw) == 0 {
			raw = json.RawMessage("null")
		}
		e.Replacement = &session.JSONValue{Raw: raw}
	case "custom_message":
		b, _ := json.Marshal(d.Content)
		e.Content = b
	case "compaction":
		e.FirstKeptEntryID = d.FirstKept
		if d.RetainNone {
			e.FirstKeptEntryID = e.ID
		}
	}
	return e
}

func parseBoundaryDrafts(v any) ([]boundaryDraft, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("entries must be a list")
	}
	out := make([]boundaryDraft, 0, len(items))
	for _, item := range items {
		d, err := decodeBoundaryDraft(item)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func decodeBoundaryDraft(raw json.RawMessage) (boundaryDraft, error) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return boundaryDraft{}, fmt.Errorf("entry must be an object")
	}
	typ := jsonStringField(obj["type"])
	switch typ {
	case "custom", "custom_message", "context_edit", "compaction":
	default:
		return boundaryDraft{}, fmt.Errorf("entry type %q is not allowed", typ)
	}
	d := boundaryDraft{Type: typ, CustomType: jsonStringField(obj["customType"]), TargetID: jsonStringField(obj["targetId"]), Summary: jsonStringField(obj["summary"])}
	d.Display = true
	if rawDisplay, ok := obj["display"]; ok {
		var b bool
		if json.Unmarshal(rawDisplay, &b) == nil {
			d.Display = b
		}
	}
	if data, ok := obj["data"]; ok && string(data) != "null" {
		var v any
		if json.Unmarshal(data, &v) == nil {
			d.Data = v
		}
	}
	if content, ok := obj["content"]; ok && string(content) != "null" {
		var s string
		if json.Unmarshal(content, &s) == nil {
			d.Content = s
		} else {
			d.Content = string(content)
		}
	}
	if repl, ok := obj["replacement"]; ok {
		d.Replacement = append(json.RawMessage(nil), repl...)
	}
	if kept, ok := obj["firstKeptEntryId"]; ok {
		if string(kept) == "null" {
			d.RetainNone = true
		} else {
			d.FirstKept = jsonStringField(kept)
		}
	}
	if tok, ok := obj["tokensBefore"]; ok {
		var n int
		if json.Unmarshal(tok, &n) == nil {
			d.TokensBefore = n
		}
	}
	return d, nil
}

func jsonStringField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

func (e *Engine) validateDrafts(drafts []boundaryDraft) error {
	for _, d := range drafts {
		switch d.Type {
		case "custom":
			if d.CustomType == "" {
				return fmt.Errorf("custom entry requires customType")
			}
		case "custom_message":
			if d.CustomType == "" {
				return fmt.Errorf("custom_message requires customType")
			}
		case "context_edit":
			if d.TargetID == "" {
				return fmt.Errorf("context_edit requires targetId")
			}
			if e.Opts.Session == nil {
				return fmt.Errorf("no session")
			}
			target, ok := e.Opts.Session.EntryByID(d.TargetID)
			if !ok {
				return fmt.Errorf("entry %s not found", d.TargetID)
			}
			switch target.Type {
			case "custom_message", "message", "":
			default:
				return fmt.Errorf("entry %s cannot be a context edit target", d.TargetID)
			}
		case "compaction":
			if d.Summary == "" {
				return fmt.Errorf("compaction requires summary")
			}
		}
	}
	return nil
}

func (e *Engine) appendDrafts(drafts []boundaryDraft) error {
	if len(drafts) == 0 {
		return nil
	}
	if e.Opts.Session == nil {
		return fmt.Errorf("no session")
	}
	for _, d := range drafts {
		var err error
		switch d.Type {
		case "custom":
			_, err = e.Opts.Session.AppendCustomEntry(d.CustomType, d.Data)
		case "custom_message":
			_, err = e.Opts.Session.AppendCustomMessage(d.CustomType, d.Content, d.Display)
		case "context_edit":
			repl := d.Replacement
			if len(repl) == 0 || string(repl) == "null" {
				repl = nil
			}
			_, err = e.Opts.Session.AppendContextEdit(d.TargetID, repl)
		case "compaction":
			meta := session.CompactionMeta{RetainNone: d.RetainNone}
			_, err = e.Opts.Session.AppendCompaction(d.Summary, d.FirstKept, d.TokensBefore, meta)
		default:
			err = fmt.Errorf("entry type %q is not allowed", d.Type)
		}
		if err != nil {
			return err
		}
	}
	e.contextGen++
	return nil
}
