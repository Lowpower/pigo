package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// writeTool creates or overwrites a file. Ports pi's write.ts.
type writeTool struct{}

type writeParams struct {
	Path    string `json:"path" jsonschema:"description=Path to the file to write (relative or absolute)"`
	Content string `json:"content" jsonschema:"description=Content to write to the file"`
}

func (writeTool) Name() string { return "write" }

func (writeTool) Description() string {
	return "Write content to a file, creating it (and parent directories) or overwriting it if it exists."
}

func (writeTool) Schema() map[string]any { return schemaFor(&writeParams{}) }

func (writeTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	var p writeParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Path == "" {
		return "path is required", true
	}
	if dir := filepath.Dir(p.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err.Error(), true
		}
	}
	if err := os.WriteFile(p.Path, []byte(p.Content), 0o644); err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("Wrote %d bytes to %s.", len(p.Content), p.Path), false
}
