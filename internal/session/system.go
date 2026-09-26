package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/prompt"
)

// ToolRef names a tool removed from the active declaration.
type ToolRef struct {
	Name string `json:"name"`
}

// SystemMessage is a session message with role "system".
// The first one declares every section and tool. Later ones are patches:
// a section value replaces that section, null removes it, toolsAdded upserts
// schemas, and toolsRemoved drops tools by name. replace starts a new baseline.
type SystemMessage struct {
	Role         string         `json:"role"`
	Content      string         `json:"content"`
	Sections     *sectionObject `json:"sections,omitempty"`
	ToolsAdded   []ai.Tool      `json:"toolsAdded,omitempty"`
	ToolsRemoved []ToolRef      `json:"toolsRemoved,omitempty"`
	Replace      bool           `json:"replace,omitempty"`
	Timestamp    int64          `json:"timestamp"`
}

// ReplayResult is the system prompt and tools reconstructed from a branch.
type ReplayResult struct {
	Declared bool
	Prompt   string
	Content  string
	Sections map[string]string
	Tools    []ai.Tool
}

type sectionObject struct {
	keys []string
	vals map[string]*string
}

func (s sectionObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range s.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(s.vals[k])
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (s *sectionObject) UnmarshalJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return fmt.Errorf("sections must be an object")
	}
	s.keys = nil
	s.vals = map[string]*string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("section name must be a string")
		}
		var v *string
		if err := dec.Decode(&v); err != nil {
			return err
		}
		if _, exists := s.vals[key]; !exists {
			s.keys = append(s.keys, key)
		}
		s.vals[key] = v
	}
	_, err = dec.Token()
	return err
}

func (s *sectionObject) set(name string, v *string) {
	if s.vals == nil {
		s.vals = map[string]*string{}
	}
	if _, ok := s.vals[name]; !ok {
		s.keys = append(s.keys, name)
	}
	s.vals[name] = v
}

// DeclareSystem is the full leading declaration: every non-empty section and
// every tool schema. content is empty; the prompt text lives in sections.
func DeclareSystem(sections map[string]string, tools []ai.Tool) SystemMessage {
	msg := SystemMessage{Role: "system", Content: "", Timestamp: nowMillis()}
	msg.Sections = sectionsObject(sections)
	if len(tools) > 0 {
		msg.ToolsAdded = append([]ai.Tool(nil), tools...)
	}
	return msg
}

// CheckpointSystem is the folded snapshot stored on a compaction entry.
func CheckpointSystem(content string, sections map[string]string, tools []ai.Tool) SystemMessage {
	msg := DeclareSystem(sections, tools)
	msg.Content = content
	return msg
}

// PreamblePatch overrides only the leading preamble section.
func PreamblePatch(text string) SystemMessage {
	msg := SystemMessage{Role: "system", Content: "", Timestamp: nowMillis()}
	obj := &sectionObject{}
	s := text
	obj.set(prompt.SectionPreamble, &s)
	msg.Sections = obj
	return msg
}

// DiffSystem returns a patch from prev to sections/tools, or nil when nothing changed.
func DiffSystem(prev ReplayResult, sections map[string]string, tools []ai.Tool) *SystemMessage {
	obj := &sectionObject{}
	changed := false
	for _, name := range sectionNames(prev.Sections, sections) {
		next, nextOK := sections[name]
		old, oldOK := prev.Sections[name]
		if !nextOK || strings.TrimSpace(next) == "" {
			if oldOK && strings.TrimSpace(old) != "" {
				obj.set(name, nil)
				changed = true
			}
			continue
		}
		if !oldOK || old != next {
			s := next
			obj.set(name, &s)
			changed = true
		}
	}
	prevTools := map[string]ai.Tool{}
	var prevOrder []string
	for _, t := range prev.Tools {
		if _, ok := prevTools[t.Name]; !ok {
			prevOrder = append(prevOrder, t.Name)
		}
		prevTools[t.Name] = t
	}
	nextTools := map[string]ai.Tool{}
	for _, t := range tools {
		nextTools[t.Name] = t
	}
	var removed []ToolRef
	for _, name := range prevOrder {
		if _, ok := nextTools[name]; !ok {
			removed = append(removed, ToolRef{Name: name})
		}
	}
	var added []ai.Tool
	seen := map[string]bool{}
	for _, t := range tools {
		if seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		old, ok := prevTools[t.Name]
		if !ok || !toolEqual(old, t) {
			added = append(added, t)
		}
	}
	if !changed && len(removed) == 0 && len(added) == 0 {
		return nil
	}
	msg := SystemMessage{Role: "system", Content: "", Timestamp: nowMillis(), ToolsRemoved: removed, ToolsAdded: added}
	if changed {
		msg.Sections = obj
	}
	return &msg
}

// ReplaySystem folds system messages on a branch into the current prompt and tools.
// A compaction systemMessage checkpoint replaces everything before it. System
// messages in the kept range before that compaction are ignored. Without a
// checkpoint, system messages from firstKeptEntryId onward are applied in order.
func ReplaySystem(path []Entry) ReplayResult {
	last := -1
	for i, e := range path {
		if e.Type == "compaction" {
			last = i
		}
	}
	var msgs []SystemMessage
	if last >= 0 && path[last].SystemMessage != nil {
		msgs = append(msgs, *path[last].SystemMessage)
		for i := last + 1; i < len(path); i++ {
			if m, ok := systemFromEntry(path[i]); ok {
				msgs = append(msgs, m)
			}
		}
	} else {
		start := 0
		if last >= 0 && path[last].FirstKeptEntryID != "" {
			for i := 0; i < last; i++ {
				if path[i].ID == path[last].FirstKeptEntryID {
					start = i
					break
				}
			}
		}
		for i := start; i < len(path); i++ {
			if i == last {
				continue
			}
			if m, ok := systemFromEntry(path[i]); ok {
				msgs = append(msgs, m)
			}
		}
	}
	var st systemState
	for _, m := range msgs {
		st.apply(m)
	}
	res := st.result()
	res.Declared = len(msgs) > 0
	return res
}

type systemState struct {
	content  string
	sections map[string]string
	unknown  []string
	tools    map[string]ai.Tool
	order    []string
}

func (st *systemState) apply(m SystemMessage) {
	if m.Replace {
		*st = systemState{}
	}
	if strings.TrimSpace(m.Content) != "" {
		st.content = m.Content
	}
	if m.Sections != nil {
		for _, name := range m.Sections.keys {
			v := m.Sections.vals[name]
			if v == nil {
				delete(st.sections, name)
				st.unknown = removeString(st.unknown, name)
				continue
			}
			if st.sections == nil {
				st.sections = map[string]string{}
			}
			if _, ok := st.sections[name]; !ok && !prompt.IsKnownSection(name) {
				st.unknown = append(st.unknown, name)
			}
			st.sections[name] = *v
		}
	}
	for _, rem := range m.ToolsRemoved {
		delete(st.tools, rem.Name)
		st.order = removeString(st.order, rem.Name)
	}
	for _, add := range m.ToolsAdded {
		if add.Name == "" {
			continue
		}
		if st.tools == nil {
			st.tools = map[string]ai.Tool{}
		}
		if _, ok := st.tools[add.Name]; !ok {
			st.order = append(st.order, add.Name)
		}
		st.tools[add.Name] = add
	}
}

func (st systemState) result() ReplayResult {
	sections := make(map[string]string, len(st.sections))
	for k, v := range st.sections {
		sections[k] = v
	}
	var tools []ai.Tool
	for _, name := range st.order {
		if t, ok := st.tools[name]; ok {
			tools = append(tools, t)
		}
	}
	return ReplayResult{
		Prompt:   prompt.Render(st.content, sections, st.unknown),
		Content:  st.content,
		Sections: sections,
		Tools:    tools,
	}
}

func systemFromEntry(e Entry) (SystemMessage, bool) {
	if e.Type != "message" && e.Type != "" {
		return SystemMessage{}, false
	}
	if len(e.Message) == 0 {
		return SystemMessage{}, false
	}
	var probe struct {
		Role string `json:"role"`
	}
	if json.Unmarshal(e.Message, &probe) != nil || probe.Role != "system" {
		return SystemMessage{}, false
	}
	var msg SystemMessage
	if json.Unmarshal(e.Message, &msg) != nil {
		return SystemMessage{}, false
	}
	return msg, true
}

func sectionsObject(sections map[string]string) *sectionObject {
	obj := &sectionObject{}
	seen := map[string]bool{}
	add := func(name string) {
		if seen[name] {
			return
		}
		v, ok := sections[name]
		if !ok || strings.TrimSpace(v) == "" {
			return
		}
		seen[name] = true
		s := v
		obj.set(name, &s)
	}
	for _, name := range prompt.SectionOrder {
		add(name)
	}
	var rest []string
	for name := range sections {
		if !seen[name] && strings.TrimSpace(sections[name]) != "" {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name)
	}
	if len(obj.keys) == 0 {
		return nil
	}
	return obj
}

func sectionNames(prev, next map[string]string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, name := range prompt.SectionOrder {
		if _, ok := prev[name]; ok {
			add(name)
		}
		if _, ok := next[name]; ok {
			add(name)
		}
	}
	var rest []string
	for name := range prev {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	for name := range next {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		add(name)
	}
	return names
}

func toolEqual(a, b ai.Tool) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ab, bb)
}

func removeString(ss []string, name string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != name {
			out = append(out, s)
		}
	}
	return out
}

func nowMillis() int64 {
	return time.Now().UnixMilli()
}
