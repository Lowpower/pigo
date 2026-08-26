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

// AutoloadOff reports whether autoload is explicitly false.
func (p PackageEntry) AutoloadOff() bool {
	return p.Autoload != nil && !*p.Autoload
}

// ResourceFilters returns the filter list for a resource type.
func (p PackageEntry) ResourceFilters(kind string) []string {
	switch kind {
	case "extensions":
		return p.Extensions
	case "skills":
		return p.Skills
	case "prompts":
		return p.Prompts
	case "themes":
		return p.Themes
	default:
		return nil
	}
}

// SetResourceFilters writes the filter list and marks the entry as an object.
func (p *PackageEntry) SetResourceFilters(kind string, filters []string) {
	p.asString = false
	switch kind {
	case "extensions":
		p.Extensions = filters
	case "skills":
		p.Skills = filters
	case "prompts":
		p.Prompts = filters
	case "themes":
		p.Themes = filters
	}
}

// HasAnyFilters reports whether any resource-type filter array is set.
func (p PackageEntry) HasAnyFilters() bool {
	return len(p.Extensions) > 0 || len(p.Skills) > 0 || len(p.Prompts) > 0 || len(p.Themes) > 0
}

// AsStringPackage collapses an object with no filters back to a string entry.
func (p *PackageEntry) AsStringPackage() {
	if p.Autoload != nil {
		return
	}
	if p.HasAnyFilters() {
		return
	}
	p.asString = true
	p.Extensions, p.Skills, p.Prompts, p.Themes = nil, nil, nil, nil
}

// StringPackage is a packages[] item stored as a bare source string.
func StringPackage(source string) PackageEntry {
	return PackageEntry{Source: source, asString: true}
}

// ObjectPackage is a packages[] object, optionally with autoload:false.
func ObjectPackage(source string, autoload *bool) PackageEntry {
	return PackageEntry{Source: source, Autoload: autoload, asString: false}
}

// ProjectDir is <cwd>/.pigo.
func ProjectDir(cwd string) string {
	return filepath.Join(cwd, ".pigo")
}
