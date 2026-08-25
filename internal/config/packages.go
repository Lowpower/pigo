package config

import (
	"bytes"
	"encoding/json"
	"path/filepath"
)

// PackageEntry is one packages[] item: a source string, or an object with filters.
type PackageEntry struct {
	Source     string   `json:"source"`
	Autoload   *bool    `json:"autoload,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	Skills     []string `json:"skills,omitempty"`
	Prompts    []string `json:"prompts,omitempty"`
	Themes     []string `json:"themes,omitempty"`
	asString   bool
}

// UnmarshalJSON accepts a string or an object.
func (p *PackageEntry) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &p.Source); err != nil {
			return err
		}
		p.asString = true
		return nil
	}
	type alias PackageEntry
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	*p = PackageEntry(a)
	p.asString = false
	return nil
}

// MarshalJSON writes a string when the entry has no filters.
func (p PackageEntry) MarshalJSON() ([]byte, error) {
	if p.asString || (p.Autoload == nil && len(p.Extensions) == 0 && len(p.Skills) == 0 && len(p.Prompts) == 0 && len(p.Themes) == 0) {
		return json.Marshal(p.Source)
	}
	type alias PackageEntry
	return json.Marshal(alias(p))
}

// SourceString returns the package source specifier.
func (p PackageEntry) SourceString() string {
	return p.Source
}

// Filtered reports whether the entry carries resource filters.
func (p PackageEntry) Filtered() bool {
	return !p.asString && (p.Autoload != nil || len(p.Extensions) > 0 || len(p.Skills) > 0 || len(p.Prompts) > 0 || len(p.Themes) > 0)
}

// StringPackage is a packages[] item stored as a bare source string.
func StringPackage(source string) PackageEntry {
	return PackageEntry{Source: source, asString: true}
}

// ProjectDir is <cwd>/.pigo.
func ProjectDir(cwd string) string {
	return filepath.Join(cwd, ".pigo")
}
