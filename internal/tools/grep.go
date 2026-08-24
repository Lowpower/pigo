package tools

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const grepDefaultLimit = 100

// grepTool searches file contents for a pattern. Ports pi's grep.ts (without the
// .gitignore integration, which is deferred per the migration plan).
type grepTool struct{}

type grepParams struct {
	Pattern    string `json:"pattern" jsonschema:"description=Search pattern (regex or literal string)"`
	Path       string `json:"path,omitempty" jsonschema:"description=Directory or file to search (default: current directory)"`
	Glob       string `json:"glob,omitempty" jsonschema:"description=Filter files by glob pattern, e.g. '*.go' or '**/*_test.go'"`
	IgnoreCase bool   `json:"ignoreCase,omitempty" jsonschema:"description=Case-insensitive search (default: false)"`
	Literal    bool   `json:"literal,omitempty" jsonschema:"description=Treat pattern as a literal string instead of regex (default: false)"`
	Context    int    `json:"context,omitempty" jsonschema:"description=Number of lines to show before and after each match (default: 0)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"description=Maximum number of matches to return (default: 100)"`
}

func (grepTool) Name() string { return "grep" }

func (grepTool) Description() string {
	return "Search file contents for a pattern. Returns matching lines with file paths and line numbers."
}

func (grepTool) Schema() map[string]any { return schemaFor(&grepParams{}) }

func (grepTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	var p grepParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Pattern == "" {
		return "pattern is required", true
	}

	expr := p.Pattern
	if p.Literal {
		expr = regexp.QuoteMeta(expr)
	}
	if p.IgnoreCase {
		expr = "(?i)" + expr
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return "invalid pattern: " + err.Error(), true
	}

	root := p.Path
	if root == "" {
		root = "."
	}
	var g *globMatcher
	if p.Glob != "" {
		if g, err = compileGlob(p.Glob); err != nil {
			return "invalid glob: " + err.Error(), true
		}
	}
	limit := grepDefaultLimit
	if p.Limit > 0 {
		limit = p.Limit
	}

	var out strings.Builder
	count := 0
	truncated := false

	searchFile := func(path, rel string) bool {
		data, readErr := os.ReadFile(path)
		if readErr != nil || bytes.IndexByte(data, 0) >= 0 { // skip unreadable/binary
			return true
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			if p.Context > 0 {
				lo := max(0, i-p.Context)
				hi := min(len(lines)-1, i+p.Context)
				for j := lo; j <= hi; j++ {
					sep := "-"
					if j == i {
						sep = ":"
					}
					fmt.Fprintf(&out, "%s:%d%s%s\n", rel, j+1, sep, lines[j])
				}
				out.WriteString("--\n")
			} else {
				fmt.Fprintf(&out, "%s:%d:%s\n", rel, i+1, line)
			}
			count++
			if count >= limit {
				truncated = true
				return false
			}
		}
		return true
	}

	info, statErr := os.Stat(root)
	if statErr != nil {
		return statErr.Error(), true
	}
	if !info.IsDir() {
		searchFile(root, filepath.ToSlash(root))
	} else {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if d.Name() == ".git" && path != root {
					return filepath.SkipDir
				}
				return nil
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			if g != nil && !g.Match(rel) {
				return nil
			}
			if !searchFile(path, rel) {
				return fs.SkipAll
			}
			return nil
		})
	}

	if count == 0 {
		return "No matches found for " + p.Pattern, false
	}
	result := strings.TrimRight(out.String(), "\n")
	if truncated {
		result += fmt.Sprintf("\n[%d matches limit reached]", limit)
	}
	return result, false
}
