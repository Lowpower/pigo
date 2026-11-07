package config

import (
	"fmt"
	"regexp"
	"strings"
)

// IsToolModifier reports whether entry is a +name or -name adjustment.
func IsToolModifier(entry string) bool {
	return strings.HasPrefix(entry, "+") || strings.HasPrefix(entry, "-")
}

// ToolModifiers returns entries when every one is +name or -name.
// An empty list and any list that contains a plain name return nil.
func ToolModifiers(entries []string) []string {
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if !IsToolModifier(entry) {
			return nil
		}
	}
	return entries
}

// ValidateToolList checks a --tools list. A plain list (names and * patterns)
// is valid. A list of only +name/-name entries is valid when those names are
// exact. Mixing the two, or using * inside +name/-name, is an error.
// Settings files are not validated here.
func ValidateToolList(entries []string) error {
	mods := ToolModifiers(entries)
	if len(entries) == 0 || (mods == nil && !hasModifier(entries)) {
		return nil
	}
	if mods == nil {
		return fmt.Errorf("tool names cannot be mixed with +name or -name entries")
	}
	for _, entry := range mods {
		if strings.Contains(entry, "*") {
			return fmt.Errorf("+name and -name entries take exact tool names, not patterns: %s", entry)
		}
	}
	return nil
}

func hasModifier(entries []string) bool {
	for _, entry := range entries {
		if IsToolModifier(entry) {
			return true
		}
	}
	return false
}

// ApplyToolModifiers applies +name and -name entries to base, in order.
// +name appends a missing name. -name removes the first occurrence.
// Other entries are ignored. An empty name after the sign is ignored.
func ApplyToolModifiers(base, entries []string) []string {
	tools := append([]string(nil), base...)
	for _, entry := range entries {
		if !IsToolModifier(entry) || len(entry) < 2 {
			continue
		}
		name := entry[1:]
		index := toolIndex(tools, name)
		switch {
		case strings.HasPrefix(entry, "+") && index < 0:
			tools = append(tools, name)
		case strings.HasPrefix(entry, "-") && index >= 0:
			tools = append(tools[:index], tools[index+1:]...)
		}
	}
	return tools
}

// ResolveDefaultTools turns one merged defaultTools list into enabled names.
// Plain names, or an empty list, replace the built-in default. A list of only
// +name/-name entries starts from read, bash, edit, write. Modifiers then
// apply in list order.
func ResolveDefaultTools(entries []string) []string {
	var plain []string
	for _, entry := range entries {
		if !IsToolModifier(entry) {
			plain = append(plain, entry)
		}
	}
	base := DefaultBuiltinTools()
	if len(plain) > 0 || len(entries) == 0 {
		base = append([]string(nil), plain...)
	}
	return ApplyToolModifiers(base, entries)
}

// MergeDefaultTools merges an override layer onto base.
// A nil override keeps base. An empty override, or one that contains a plain
// name, replaces base. A list of only +name/-name entries is appended, so it
// still adjusts whatever the base resolves to.
func MergeDefaultTools(base, overrides *[]string) *[]string {
	if overrides == nil {
		return cloneToolList(base)
	}
	over := append([]string(nil), (*overrides)...)
	if len(over) == 0 || ToolModifiers(over) == nil {
		return &over
	}
	if base == nil {
		return &over
	}
	merged := append(append([]string(nil), (*base)...), over...)
	return &merged
}

func cloneToolList(list *[]string) *[]string {
	if list == nil {
		return nil
	}
	cp := append([]string(nil), (*list)...)
	return &cp
}

// ToolNameMatches reports whether name matches pattern.
// A pattern without * is an exact name. Each * matches any characters, and
// the match covers the whole name.
func ToolNameMatches(pattern, name string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	var b strings.Builder
	b.WriteByte('^')
	for i, part := range parts {
		if i > 0 {
			b.WriteString(".*")
		}
		b.WriteString(regexp.QuoteMeta(part))
	}
	b.WriteByte('$')
	re, err := regexp.Compile(b.String())
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

// ToolMatchesAny reports whether name matches any pattern.
func ToolMatchesAny(patterns []string, name string) bool {
	for _, pattern := range patterns {
		if ToolNameMatches(pattern, name) {
			return true
		}
	}
	return false
}

// MatchingToolNames returns names that match any pattern, in names order.
func MatchingToolNames(patterns, names []string) []string {
	var out []string
	for _, name := range names {
		if ToolMatchesAny(patterns, name) {
			out = append(out, name)
		}
	}
	return out
}

// RejectToolNames returns names that match none of the patterns, in order.
// An empty pattern list returns names unchanged.
func RejectToolNames(names, patterns []string) []string {
	if len(patterns) == 0 {
		return names
	}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !ToolMatchesAny(patterns, name) {
			out = append(out, name)
		}
	}
	return out
}

func toolIndex(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}
