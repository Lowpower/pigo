package prompt

import (
	"sort"
	"strings"
)

// Section names written into session system messages. Order is the render order.
const (
	SectionPreamble       = "preamble"
	SectionAppend         = "append"
	SectionTools          = "tools"
	SectionProjectContext = "project_context"
	SectionSkills         = "skills"
	SectionCwd            = "cwd"
	SectionDate           = "date"
)

// SectionOrder is the stable order for known sections. Unknown names follow it.
var SectionOrder = []string{
	SectionPreamble,
	SectionAppend,
	SectionTools,
	SectionProjectContext,
	SectionSkills,
	SectionCwd,
	SectionDate,
}

// IsKnownSection reports whether name is one of SectionOrder.
func IsKnownSection(name string) bool {
	for _, n := range SectionOrder {
		if n == name {
			return true
		}
	}
	return false
}

// SectionSet is the named pieces of a system prompt.
type SectionSet struct {
	text map[string]string
}

// Get returns the text of a section.
func (s SectionSet) Get(name string) string {
	return s.text[name]
}

// Set replaces one section. Blank text removes it.
func (s *SectionSet) Set(name, text string) {
	if strings.TrimSpace(text) == "" {
		delete(s.text, name)
		return
	}
	if s.text == nil {
		s.text = map[string]string{}
	}
	s.text[name] = text
}

// Map returns a copy of the section texts.
func (s SectionSet) Map() map[string]string {
	out := make(map[string]string, len(s.text))
	for k, v := range s.text {
		out[k] = v
	}
	return out
}

// Render joins content and sections. Known sections stay in SectionOrder.
// unknown lists extra names in first-seen order. Remaining names are sorted.
func Render(content string, sections map[string]string, unknown []string) string {
	var parts []string
	if strings.TrimSpace(content) != "" {
		parts = append(parts, content)
	}
	emitted := map[string]bool{}
	emit := func(name string) {
		if emitted[name] {
			return
		}
		emitted[name] = true
		text := sections[name]
		if strings.TrimSpace(text) == "" {
			return
		}
		parts = append(parts, text)
	}
	for _, name := range SectionOrder {
		emit(name)
	}
	for _, name := range unknown {
		emit(name)
	}
	var rest []string
	for name := range sections {
		if !emitted[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		emit(name)
	}
	return strings.Join(parts, "\n\n")
}

// Render returns the prompt text for this set.
func (s SectionSet) Render() string {
	return Render("", s.text, nil)
}
