package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

const lsDefaultLimit = 500

// listTool lists directory contents. Ports pi's ls.ts. The tool name is "ls".
type listTool struct{}

type lsParams struct {
	Path  string `json:"path,omitempty" jsonschema:"description=Directory to list (default: current directory)"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Maximum number of entries to return (default: 500)"`
}

func (listTool) Name() string { return "ls" }

func (listTool) Description() string {
	return "List directory contents, sorted alphabetically, with a '/' suffix for directories. Includes dotfiles."
}

func (listTool) Schema() map[string]any { return schemaFor(&lsParams{}) }

func (listTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	var p lsParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	dir := p.Path
	if dir == "" {
		dir = "."
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err.Error(), true
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})

	limit := lsDefaultLimit
	if p.Limit > 0 {
		limit = p.Limit
	}
	truncated := false
	if len(names) > limit {
		names = names[:limit]
		truncated = true
	}
	if len(names) == 0 {
		return "(empty directory)", false
	}
	out := strings.Join(names, "\n")
	if truncated {
		out += fmt.Sprintf("\n[%d entries limit reached; use limit=%d for more]", limit, limit*2)
	}
	return out, false
}
