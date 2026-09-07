package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// readTool returns the contents of a text file, or image content for supported images.
type readTool struct {
	autoResize   bool
	cwd          string
	imageCapable func() bool
}

type readParams struct {
	Path   string `json:"path" jsonschema:"description=Path to the file to read (relative or absolute)"`
	Offset int    `json:"offset,omitempty" jsonschema:"description=Line number to start reading from (1-indexed)"`
	Limit  int    `json:"limit,omitempty" jsonschema:"description=Maximum number of lines to read"`
}

func (t readTool) Name() string { return "read" }

func (readTool) Description() string {
	return fmt.Sprintf("Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments. For text files, output is truncated to %d lines or %dKB (whichever is hit first). Use offset (1-indexed) and limit to page through them.", DefaultMaxLines, DefaultMaxBytes/1024)
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
	path := resolvePath(t.cwd, p.Path)
	data, err := os.ReadFile(path)
	if err != nil {
		return err.Error(), true
	}
	if mime := sniffImageMIME(data); mime != "" {
		processed, ok := processImage(data, mime, t.autoResize)
		note := ""
		if t.imageCapable != nil && !t.imageCapable() {
			note = "\n[Current model does not support images. The image will be omitted from this request.]"
		}
		if !ok {
			return "Read image file [" + mime + "]\n[Image omitted: could not be converted to a supported inline image format.]" + note, false
		}
		return encodeImageResult(imageReadNote(processed.mimeType)+note, processed), false
	}

	allLines := strings.Split(string(data), "\n")
	totalFileLines := len(allLines)
	start := 0
	if p.Offset > 0 {
		start = p.Offset - 1
	}
	if start >= totalFileLines {
		return fmt.Sprintf("Offset %d is beyond end of file (%d lines total)", p.Offset, totalFileLines), true
	}
	var selected string
	userLimited := false
	if p.Limit > 0 {
		end := start + p.Limit
		if end > totalFileLines {
			end = totalFileLines
		} else {
			userLimited = end < totalFileLines
		}
		selected = strings.Join(allLines[start:end], "\n")
	} else {
		selected = strings.Join(allLines[start:], "\n")
	}
	tr := TruncateHead(selected, DefaultMaxLines, DefaultMaxBytes)
	startDisplay := start + 1
	if tr.FirstLineExceedsLimit {
		firstSize := FormatSize(utf8ByteLen(allLines[start]))
		return fmt.Sprintf("[Line %d is %s, exceeds %s limit. Use bash: sed -n '%dp' %s | head -c %d]",
			startDisplay, firstSize, FormatSize(DefaultMaxBytes), startDisplay, p.Path, DefaultMaxBytes), false
	}
	out := tr.Content
	if tr.Truncated {
		endDisplay := startDisplay + tr.OutputLines - 1
		next := endDisplay + 1
		if tr.TruncatedBy == "lines" {
			out += fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Use offset=%d to continue.]", startDisplay, endDisplay, totalFileLines, next)
		} else {
			out += fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]", startDisplay, endDisplay, totalFileLines, FormatSize(DefaultMaxBytes), next)
		}
	} else if userLimited {
		remaining := totalFileLines - (start + p.Limit)
		next := start + p.Limit + 1
		out += fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, next)
	}
	return out, false
}
