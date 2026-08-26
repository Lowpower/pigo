package sqlite

import (
	"encoding/json"
	"fmt"

	"github.com/Lowpower/pigo/internal/sessionrepo"
)

type entryRow struct {
	id        string
	seq       int64
	parentID  *string
	typ       string
	timestamp int64
	payload   string
}

func entryPayloadJSON(e sessionrepo.Entry) (string, error) {
	m := map[string]any{}
	switch e.Type {
	case "message":
		m["message"] = e.Message
		if e.Terminate != nil && *e.Terminate {
			m["terminate"] = true
		}
	case "model_change":
		m["provider"] = e.Provider
		m["modelId"] = e.ModelID
	case "thinking_level_change":
		m["thinkingLevel"] = e.ThinkingLevel
	case "active_tools_change":
		m["activeToolNames"] = e.ActiveToolNames
	case "compaction":
		tail := e.RetainedTail
		if tail == nil {
			tail = []any{}
		}
		m["summary"] = e.Summary
		m["retainedTail"] = tail
		m["tokensBefore"] = e.TokensBefore
		if e.HasDetails {
			m["details"] = e.Details
		}
		if e.Usage != nil {
			m["usage"] = e.Usage
		}
	case "branch_summary":
		m["fromId"] = e.FromID
		m["summary"] = e.Summary
		if e.HasDetails {
			m["details"] = e.Details
		}
		if e.Usage != nil {
			m["usage"] = e.Usage
		}
	case "custom":
		m["customType"] = e.CustomType
		if e.HasData {
			m["data"] = e.Data
		}
	default:
		b, err := json.Marshal(e)
		if err != nil {
			return "", err
		}
		var all map[string]any
		if err := json.Unmarshal(b, &all); err != nil {
			return "", err
		}
		delete(all, "type")
		delete(all, "id")
		delete(all, "seq")
		delete(all, "parentId")
		delete(all, "timestamp")
		m = all
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeEntry(row entryRow) (sessionrepo.Entry, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(row.payload), &payload); err != nil {
		return sessionrepo.Entry{}, sessionrepo.NewErrorCause(sessionrepo.ErrInvalidEntry,
			fmt.Sprintf("Invalid SQLite session entry %s: failed to decode entry %s", row.id, row.id), err)
	}
	base := sessionrepo.Entry{ID: row.id, Seq: row.seq, ParentID: row.parentID, Timestamp: row.timestamp, Type: row.typ}
	fail := func(reason string) (sessionrepo.Entry, error) {
		return sessionrepo.Entry{}, sessionrepo.NewErrorCause(sessionrepo.ErrInvalidEntry,
			fmt.Sprintf("Invalid SQLite session entry %s: failed to decode entry %s", row.id, row.id), fmt.Errorf("%s", reason))
	}
	switch row.typ {
	case "message":
		msg, ok := payload["message"]
		if !ok || msg == nil {
			return fail("Missing message")
		}
		base.Message = msg
		if t, ok := payload["terminate"].(bool); ok && t {
			tt := true
			base.Terminate = &tt
		}
		return base, nil
	case "model_change":
		p, _ := payload["provider"].(string)
		m, _ := payload["modelId"].(string)
		if p == "" || m == "" {
			return fail("Invalid model_change payload")
		}
		base.Provider, base.ModelID = p, m
		return base, nil
	case "thinking_level_change":
		tl, _ := payload["thinkingLevel"].(string)
		if tl == "" {
			return fail("Invalid thinking_level_change payload")
		}
		base.ThinkingLevel = tl
		return base, nil
	case "active_tools_change":
		raw, ok := payload["activeToolNames"].([]any)
		if !ok {
			return fail("Invalid active_tools_change payload")
		}
		names := make([]string, len(raw))
		for i, v := range raw {
			s, ok := v.(string)
			if !ok {
				return fail("Invalid active_tools_change payload")
			}
			names[i] = s
		}
		base.ActiveToolNames = names
		return base, nil
	case "compaction":
		sum, _ := payload["summary"].(string)
		tail, okTail := payload["retainedTail"].([]any)
		tb, okTB := payload["tokensBefore"].(float64)
		if sum == "" || !okTail || !okTB {
			return fail("Invalid compaction payload")
		}
		base.Summary = sum
		base.RetainedTail = tail
		base.TokensBefore = tb
		base.HasTokensBefore = true
		if _, ok := payload["details"]; ok {
			base.Details = payload["details"]
			base.HasDetails = true
		}
		if u, ok := payload["usage"]; ok {
			base.Usage = decodeUsage(u)
		}
		return base, nil
	case "branch_summary":
		from, _ := payload["fromId"].(string)
		sum, _ := payload["summary"].(string)
		if from == "" || sum == "" {
			return fail("Invalid branch_summary payload")
		}
		base.FromID, base.Summary = from, sum
		if _, ok := payload["details"]; ok {
			base.Details = payload["details"]
			base.HasDetails = true
		}
		if u, ok := payload["usage"]; ok {
			base.Usage = decodeUsage(u)
		}
		return base, nil
	case "custom":
		ct, _ := payload["customType"].(string)
		if ct == "" {
			return fail("Invalid custom payload")
		}
		base.CustomType = ct
		if _, ok := payload["data"]; ok {
			base.Data = payload["data"]
			base.HasData = true
		}
		return base, nil
	default:
		return fail("unknown type")
	}
}

func decodeUsage(v any) *sessionrepo.Usage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var u sessionrepo.Usage
	if err := json.Unmarshal(b, &u); err != nil {
		return nil
	}
	return &u
}

func recordPayloadJSON(r sessionrepo.Record) (string, error) {
	cp := r
	cp.Seq = 0
	cp.Timestamp = 0
	b, err := json.Marshal(cp)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodeRecord(seq, timestamp int64, payload string) (sessionrepo.Record, error) {
	var rec sessionrepo.Record
	if err := json.Unmarshal([]byte(payload), &rec); err != nil {
		return sessionrepo.Record{}, sessionrepo.NewErrorCause(sessionrepo.ErrStorage,
			fmt.Sprintf("Invalid SQLite session record at sequence %d: failed to decode payload", seq), err)
	}
	rec.Seq = seq
	rec.Timestamp = timestamp
	if rec.RunID != "" {
		rec.HasRunID = true
	}
	return rec, nil
}

func recordRunID(r sessionrepo.Record) *string {
	if r.Type == "operation_started" {
		return &r.ID
	}
	if r.HasRunID || r.RunID != "" {
		id := r.RunID
		return &id
	}
	return nil
}

func recordOpKind(r sessionrepo.Record) *string {
	if r.Type != "operation_started" || r.Intent == nil {
		return nil
	}
	k, _ := r.Intent["kind"].(string)
	if k == "" {
		return nil
	}
	return &k
}

func matchesEntryQuery(e sessionrepo.Entry, q sessionrepo.EntryQuery) bool {
	if q.Type != "" && e.Type != q.Type {
		return false
	}
	if q.CustomType != "" && (e.Type != "custom" || e.CustomType != q.CustomType) {
		return false
	}
	if q.Cursor != nil {
		if q.Order == sessionrepo.OrderOldestFirst {
			if e.Seq <= q.Cursor.AfterSeq {
				return false
			}
		} else if e.Seq >= q.Cursor.AfterSeq {
			return false
		}
	}
	return true
}

func decodeSessionName(value *string, sessionID string) (string, bool, error) {
	if value == nil {
		return "", false, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(*value), &parsed); err != nil {
		return "", false, sessionrepo.NewErrorCause(sessionrepo.ErrStorage,
			fmt.Sprintf("Invalid SQLite session %s: name is not valid JSON", sessionID), err)
	}
	s, ok := parsed.(string)
	if !ok {
		return "", false, sessionrepo.NewError(sessionrepo.ErrStorage,
			fmt.Sprintf("Invalid SQLite session %s: name must be a string", sessionID))
	}
	return s, true, nil
}

func parseMetadataJSON(raw *string, sessionID string) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	var parsed any
	if err := json.Unmarshal([]byte(*raw), &parsed); err != nil {
		return nil, sessionrepo.NewErrorCause(sessionrepo.ErrStorage,
			fmt.Sprintf("Invalid SQLite session %s: metadata is not valid JSON", sessionID), err)
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		return nil, sessionrepo.NewError(sessionrepo.ErrStorage,
			fmt.Sprintf("Invalid SQLite session %s: metadata must be an object", sessionID))
	}
	return obj, nil
}

func serializeMetadata(m map[string]any) (*string, error) {
	if m == nil {
		return nil, nil
	}
	if err := sessionrepo.AssertJSONSerializable(m); err != nil {
		return nil, sessionrepo.NewError(sessionrepo.ErrInvalidPayload, "SQLite session metadata must be an object")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, sessionrepo.NewError(sessionrepo.ErrInvalidPayload, "SQLite session metadata must be an object")
	}
	s := string(b)
	return &s, nil
}
