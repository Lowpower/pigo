package bugreport

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	indexEntry   = regexp.MustCompile(`/index\.(?:[cm]?[jt]s|go|py)$`)
	singleFile   = regexp.MustCompile(`\.(?:[cm]?[jt]s|go|py)$`)
	remoteSource = regexp.MustCompile(`^(?:npm:|git:|https?://|ssh://)`)
)

// Extension is one loaded extension in a bug report.
type Extension struct {
	Path    string `json:"path"`
	Source  string `json:"source,omitempty"`
	Scope   string `json:"scope,omitempty"`
	Origin  string `json:"origin,omitempty"`
	BaseDir string `json:"-"`
}

// ResourceRef is the discovery record used to label a loaded extension.
type ResourceRef struct {
	Path    string
	Source  string
	Scope   string
	Origin  string
	BaseDir string
}

// DescribeExtensions pairs spawned paths with discovery records.
func DescribeExtensions(paths, specs []string, resources []ResourceRef) []Extension {
	byPath := map[string]ResourceRef{}
	for _, r := range resources {
		if r.Path == "" {
			continue
		}
		byPath[pathKey(r.Path)] = r
	}
	out := make([]Extension, 0, len(paths))
	for i, path := range paths {
		if path == "" {
			continue
		}
		ext := Extension{Path: path, Origin: "top-level"}
		if i < len(specs) && specs[i] != "" {
			ext.Source = specs[i]
		}
		if r, ok := byPath[pathKey(path)]; ok {
			ext.Scope = r.Scope
			if r.Origin != "" {
				ext.Origin = r.Origin
			}
			ext.BaseDir = r.BaseDir
			if r.Source != "" {
				ext.Source = r.Source
			}
		}
		if ext.Source == "" {
			ext.Source = path
		}
		out = append(out, ext)
	}
	return out
}

func pathKey(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(p)
}

// FindExtensionStackMatches returns labels for loaded extensions whose files
// appear in a panic stack. Package directories match descendants. A single
// file matches only that path.
func FindExtensionStackMatches(stack string, exts []Extension) []string {
	if stack == "" {
		return nil
	}
	normalized := normalizeStack(stack)
	var matches []string
	seen := map[string]bool{}
	for _, ext := range exts {
		label, ok := extensionStackMatch(normalized, ext)
		if !ok || seen[label] {
			continue
		}
		seen[label] = true
		matches = append(matches, label)
	}
	return matches
}

// ExtensionHint is the one-line notice shown when a stack names a loaded extension.
func ExtensionHint(stack string, exts []Extension) string {
	matches := FindExtensionStackMatches(stack, exts)
	switch len(matches) {
	case 0:
		return ""
	case 1:
		return "This crash may involve loaded extension " + matches[0] + "."
	default:
		return "This crash may involve loaded extensions: " + strings.Join(matches, ", ") + "."
	}
}

func extensionStackMatch(stack string, ext Extension) (string, bool) {
	resolved := normalizePath(ext.Path)
	source := strings.TrimSpace(ext.Source)
	single := ext.Origin == "package" && source != "" && !remoteSource.MatchString(source) && singleFile.MatchString(normalizePath(source))
	var matched bool
	switch {
	case ext.Origin == "package" && !single && ext.BaseDir != "":
		matched = stackContainsPath(stack, normalizePath(ext.BaseDir), true)
	case indexEntry.MatchString(resolved):
		dir := resolved[:strings.LastIndex(resolved, "/")]
		matched = stackContainsPath(stack, dir, true)
	default:
		matched = stackContainsPath(stack, resolved, false)
	}
	if !matched {
		return "", false
	}
	if ext.Origin == "package" && source != "" {
		return source, true
	}
	if ext.Path != "" {
		return ext.Path, true
	}
	return source, true
}

func normalizeStack(stack string) string {
	lines := strings.Split(stack, "\n")
	for i, line := range lines {
		if dec, err := url.PathUnescape(line); err == nil {
			line = dec
		}
		lines[i] = line
	}
	return strings.ReplaceAll(strings.Join(lines, "\n"), "\\", "/")
}

func normalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimRight(p, "/")
	return p
}

func stackContainsPath(stack, target string, descendants bool) bool {
	if target == "" || strings.HasPrefix(target, "<") {
		return false
	}
	caseFold := len(target) >= 2 && target[1] == ':' && unicode.IsLetter(rune(target[0]))
	haystack := stack
	needle := target
	if caseFold {
		haystack = strings.ToLower(stack)
		needle = strings.ToLower(target)
	}
	if descendants {
		return strings.Contains(haystack, needle+"/")
	}
	from := 0
	for {
		i := strings.Index(haystack[from:], needle)
		if i < 0 {
			return false
		}
		i += from
		nextAt := i + len(needle)
		if nextAt >= len(haystack) {
			return true
		}
		next := haystack[nextAt]
		if next == ':' || next == ')' || next == ' ' || next == '\t' || next == '\n' || next == '\r' {
			return true
		}
		from = i + len(needle)
	}
}
