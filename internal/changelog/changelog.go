// Package changelog parses CHANGELOG.md and formats /changelog plus startup notices.
package changelog

import (
	_ "embed"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

//go:embed changelog.md
var embedded string

const (
	githubRepo            = "Lowpower/pigo"
	changelogLinkBasePath = ""
	installTelemetryURL   = "https://pi.dev/api/report-install"
)

var (
	legacyRepoRe         = regexp.MustCompile(`^https://github\.com/(?:badlogic|earendil-works)/pi-mono`)
	urlSchemeRe          = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*:`)
	inlineMarkdownLinkRe = regexp.MustCompile(`(!?\[[^\]\n]+\]\()([^\s)]+)((?:\s+[^)]*)?\))`)
	versionHeaderRe      = regexp.MustCompile(`##\s+\[?(\d+)\.(\d+)\.(\d+)\]?`)
	versionTripleRe      = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
)

// Entry is one ## x.y.z section.
type Entry struct {
	Major   int
	Minor   int
	Patch   int
	Content string
}

// Version returns major.minor.patch.
func (e Entry) Version() string {
	return strconv.Itoa(e.Major) + "." + strconv.Itoa(e.Minor) + "." + strconv.Itoa(e.Patch)
}

// Embedded is the changelog shipped in the binary.
func Embedded() string { return embedded }

// Parse parses CHANGELOG.md content into version entries.
func Parse(content string) []Entry {
	lines := strings.Split(content, "\n")
	var entries []Entry
	var currentLines []string
	var current *Entry
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			if current != nil && len(currentLines) > 0 {
				current.Content = strings.TrimSpace(strings.Join(currentLines, "\n"))
				entries = append(entries, *current)
			}
			m := versionHeaderRe.FindStringSubmatch(line)
			if m != nil {
				current = &Entry{
					Major: atoi(m[1]),
					Minor: atoi(m[2]),
					Patch: atoi(m[3]),
				}
				currentLines = []string{line}
			} else {
				current = nil
				currentLines = nil
			}
			continue
		}
		if current != nil {
			currentLines = append(currentLines, line)
		}
	}
	if current != nil && len(currentLines) > 0 {
		current.Content = strings.TrimSpace(strings.Join(currentLines, "\n"))
		entries = append(entries, *current)
	}
	return entries
}

// Compare returns -1, 0, or 1.
func Compare(a, b Entry) int {
	if a.Major != b.Major {
		return a.Major - b.Major
	}
	if a.Minor != b.Minor {
		return a.Minor - b.Minor
	}
	return a.Patch - b.Patch
}

// NewEntries returns entries newer than lastVersion (e.g. "0.0.1-dev").
func NewEntries(entries []Entry, lastVersion string) []Entry {
	last := parseVersionString(lastVersion)
	var out []Entry
	for _, e := range entries {
		if Compare(e, last) > 0 {
			out = append(out, e)
		}
	}
	return out
}

func parseVersionString(s string) Entry {
	m := versionTripleRe.FindStringSubmatch(s)
	if m == nil {
		return Entry{}
	}
	return Entry{Major: atoi(m[1]), Minor: atoi(m[2]), Patch: atoi(m[3])}
}

// NormalizeLinks rewrites relative and floating-ref markdown links to a tagged GitHub URL.
func NormalizeLinks(markdown, version string) string {
	tag := version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	return inlineMarkdownLinkRe.ReplaceAllStringFunc(markdown, func(match string) string {
		parts := inlineMarkdownLinkRe.FindStringSubmatch(match)
		if len(parts) != 4 {
			return match
		}
		return parts[1] + normalizeLinkTarget(parts[2], tag) + parts[3]
	})
}

func normalizeLinkTarget(target, tag string) string {
	canonical := legacyRepoRe.ReplaceAllString(target, "https://github.com/"+githubRepo)
	repoURL := "https://github.com/" + githubRepo
	for _, route := range []string{"blob", "tree"} {
		for _, branch := range []string{"main", "master"} {
			prefix := repoURL + "/" + route + "/" + branch + "/"
			if strings.HasPrefix(canonical, prefix) {
				canonical = repoURL + "/" + route + "/" + tag + "/" + canonical[len(prefix):]
			}
		}
	}
	if strings.HasPrefix(canonical, "#") || strings.HasPrefix(canonical, "//") || urlSchemeRe.MatchString(canonical) {
		return canonical
	}
	fragment, pathPart, query := splitLocalTarget(canonical)
	if pathPart == "" {
		return canonical
	}
	repoPath := resolveRepositoryPath(pathPart)
	if repoPath == "" {
		return canonical
	}
	route := "blob"
	if isDirectoryTarget(pathPart, repoPath) {
		route = "tree"
	}
	return repoURL + "/" + route + "/" + tag + "/" + pathEscape(repoPath) + query + fragment
}

func splitLocalTarget(target string) (fragment, pathPart, query string) {
	hash := strings.Index(target, "#")
	before := target
	if hash >= 0 {
		before = target[:hash]
		fragment = target[hash:]
	}
	q := strings.Index(before, "?")
	if q < 0 {
		return fragment, before, ""
	}
	return fragment, before[:q], before[q:]
}

func resolveRepositoryPath(targetPath string) string {
	normalized := strings.ReplaceAll(targetPath, "\\", "/")
	var joined string
	if strings.HasPrefix(normalized, "/") {
		joined = path.Clean(strings.TrimLeft(normalized, "/"))
	} else if changelogLinkBasePath == "" {
		joined = path.Clean(normalized)
	} else {
		joined = path.Clean(path.Join(changelogLinkBasePath, normalized))
	}
	if joined == "." || joined == ".." || strings.HasPrefix(joined, "../") {
		return ""
	}
	return joined
}

func isDirectoryTarget(originalPath, repositoryPath string) bool {
	if strings.HasSuffix(originalPath, "/") {
		return true
	}
	base := path.Base(repositoryPath)
	return !strings.Contains(base, ".")
}

func pathEscape(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = queryEscapePath(part)
	}
	return strings.Join(parts, "/")
}

func queryEscapePath(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			b.WriteRune(r)
			continue
		}
		if r == '/' {
			b.WriteByte('/')
			continue
		}
		for _, by := range []byte(string(r)) {
			b.WriteString("%")
			b.WriteString(strings.ToUpper(strconv.FormatInt(int64(by), 16)))
		}
	}
	return b.String()
}

// FullMarkdown is /changelog output (newest first).
func FullMarkdown() string {
	entries := Parse(embedded)
	if len(entries) == 0 {
		return "No changelog entries found."
	}
	var parts []string
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		parts = append(parts, NormalizeLinks(e.Content, e.Version()))
	}
	return strings.Join(parts, "\n\n")
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// Path returns a filesystem changelog path if one exists next to the process.
func Path() string {
	if p := os.Getenv("PIGO_CHANGELOG_PATH"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "CHANGELOG.md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat("CHANGELOG.md"); err == nil {
		return "CHANGELOG.md"
	}
	return ""
}
