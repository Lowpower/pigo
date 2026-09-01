package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const readDefaultMaxLines = 2000

// readTool returns the contents of a text file, or image content for supported images.
type readTool struct {
	autoResize bool
}

type readParams struct {
	Path   string `json:"path" jsonschema:"description=Path to the file to read (relative or absolute)"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=Line number to start reading from (1-indexed)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Maximum number of lines to read"`
}

func (t readTool) Name() string { return "read" }

func (readTool) Description() string {
	return "Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments. For text files, output is truncated for large files; use offset (1-indexed) and limit to page through them."
}

func (readTool) Schema() map[string]any { return schemaFor(&readParams{}) }

func (t readTool) Execute(_ context.Context, args map[string]any) (string, bool) {
	var p readParams
	if err := decodeArgs(args, &p); err != nil {
		return "invalid arguments: " + err.Error(), true
	}
	if p.Path == "" {
		return "path is required", true
	}
	data, err := os.ReadFile(p.Path)
	if err != nil {
		return err.Error(), true
	}
	if mime := sniffImageMIME(data); mime != "" {
		processed, ok := processImage(data, mime, t.autoResize)
		if !ok {
			return "Read image file [" + mime + "]\n[Image omitted: could not be converted to a supported inline image format.]", false
		}
		return encodeImageResult(imageReadNote(processed.mimeType), processed), false
	}

	lines := strings.Split(string(data), "\n")
	start := 0
	if p.Offset > 0 {
		start = p.Offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}
	lines = lines[start:]

	limit := readDefaultMaxLines
	if p.Limit > 0 && p.Limit < limit {
		limit = p.Limit
	}
	truncated := false
	if len(lines) > limit {
		lines = lines[:limit]
		truncated = true
	}

	out := strings.Join(lines, "\n")
	if truncated {
		out += fmt.Sprintf("\n[truncated to %d lines; continue with offset=%d]", limit, start+limit+1)
	}
	return out, false
}
