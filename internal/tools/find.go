package tools

import (
	"context"
	"fmt"
	"io/fs"
	"strings"
)

const findDefaultLimit = 1000

// findTool searches for files by glob pattern. Honors .gitignore; outside a git
// repository it still applies .gitignore files (fd --no-require-git).
type findTool struct{}

type findParams struct {
	Pattern string `json:"pattern" jsonschema:"description=Glob pattern to match files, e.g. '*.go', '**/*.json', or 'src/**/*_test.go'"`
	Path    string `json:"path,omitempty" jsonschema:"description=Directory to search in (default: current directory)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=Maximum number of results (default: 1000)"`
}

func (findTool) Name() string { return "find" }

func (findTool) Description() string {
	return "Search for files by glob pattern. Respects .gitignore. Returns matching file paths relative to the search directory."
}

func (findTool) Schema() map[string]any { return schemaFor(&findParams{}) }

func (findTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	var p findParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Pattern == "" {
		return "pattern is required", true
	}
	root := p.Path
	if root == "" {
		root = "."
	}
	g, err := compileGlob(p.Pattern)
	if err != nil {
		return "invalid glob pattern: " + err.Error(), true
	}
	limit := findDefaultLimit
	if p.Limit > 0 {
		limit = p.Limit
	}

	var matches []string
	truncated := false
	walkErr := walkUnignored(root, ignoreNoRequireGit, func(_, rel string) error {
		if g.Match(rel) {
			matches = append(matches, rel)
			if len(matches) >= limit {
				truncated = true
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil && walkErr != fs.SkipAll {
		return walkErr.Error(), true
	}
	if len(matches) == 0 {
		return "No files matched " + p.Pattern, false
	}
	out := strings.Join(matches, "\n")
	if truncated {
		out += fmt.Sprintf("\n[%d results limit reached]", limit)
	}
	return out, false
}
