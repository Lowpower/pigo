package tools

import (
	"fmt"
	"os"
	"strings"
)

// DefaultMaxLines and DefaultMaxBytes cap bash/powershell output (last N lines or bytes).
const (
	DefaultMaxLines = 2000
	DefaultMaxBytes = 50 * 1024
)

// Truncation is the result of TruncateTail.
type Truncation struct {
	Content         string
	Truncated       bool
	TruncatedBy     string // "lines", "bytes", or empty
	TotalLines      int
	TotalBytes      int
	OutputLines     int
	OutputBytes     int
	LastLinePartial bool
	MaxLines        int
	MaxBytes        int
}

func splitLinesForCounting(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func utf8ByteLen(s string) int { return len(s) }

// TruncateTail keeps the last maxLines/maxBytes (whichever limit hits first).
func TruncateTail(content string, maxLines, maxBytes int) Truncation {
	if maxLines <= 0 {
		maxLines = DefaultMaxLines
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	totalBytes := utf8ByteLen(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)
	if totalLines <= maxLines && totalBytes <= maxBytes {
		return Truncation{
			Content: content, Truncated: false,
			TotalLines: totalLines, TotalBytes: totalBytes,
			OutputLines: totalLines, OutputBytes: totalBytes,
			MaxLines: maxLines, MaxBytes: maxBytes,
		}
	}

	output := make([]string, 0, maxLines)
	outputBytes := 0
	truncatedBy := "lines"
	lastLinePartial := false

	for i := len(lines) - 1; i >= 0 && len(output) < maxLines; i-- {
		line := lines[i]
		lineBytes := utf8ByteLen(line)
		if len(output) > 0 {
			lineBytes++
		}
		if outputBytes+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(output) == 0 {
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				output = append([]string{truncatedLine}, output...)
				outputBytes = utf8ByteLen(truncatedLine)
				lastLinePartial = true
			}
			break
		}
		output = append([]string{line}, output...)
		outputBytes += lineBytes
	}
	if len(output) >= maxLines && outputBytes <= maxBytes {
		truncatedBy = "lines"
	}
	joined := strings.Join(output, "\n")
	return Truncation{
		Content: joined, Truncated: true, TruncatedBy: truncatedBy,
		TotalLines: totalLines, TotalBytes: totalBytes,
		OutputLines: len(output), OutputBytes: utf8ByteLen(joined),
		LastLinePartial: lastLinePartial, MaxLines: maxLines, MaxBytes: maxBytes,
	}
}

func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	b := []byte(s)
	if len(b) <= maxBytes {
		return s
	}
	start := len(b) - maxBytes
	for start < len(b) && b[start]&0xc0 == 0x80 {
		start++
	}
	return string(b[start:])
}

// FormatSize is a human-readable byte count (B/KB/MB).
func FormatSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
	}
}

func lastLineBytes(s string) int {
	i := strings.LastIndexByte(s, '\n')
	if i < 0 {
		return utf8ByteLen(s)
	}
	return utf8ByteLen(s[i+1:])
}

func persistFullOutput(prefix, full string) (string, error) {
	if prefix == "" {
		prefix = "pigo-output"
	}
	f, err := os.CreateTemp("", prefix+"-*.log")
	if err != nil {
		return "", err
	}
	_, werr := f.WriteString(full)
	cerr := f.Close()
	if werr != nil {
		return "", werr
	}
	if cerr != nil {
		return "", cerr
	}
	return f.Name(), nil
}

// BoundOutput keeps the tail of full and writes a temp file when truncated.
func BoundOutput(full, tempPrefix string) (content, path string, truncated bool) {
	tr := TruncateTail(full, DefaultMaxLines, DefaultMaxBytes)
	if !tr.Truncated {
		return full, "", false
	}
	path, err := persistFullOutput(tempPrefix, full)
	if err != nil {
		return tr.Content, "", true
	}
	return tr.Content, path, true
}

func truncationFooter(tr Truncation, path string, orig string) string {
	if !tr.Truncated || path == "" {
		return ""
	}
	startLine := tr.TotalLines - tr.OutputLines + 1
	if startLine < 1 {
		startLine = 1
	}
	endLine := tr.TotalLines
	switch {
	case tr.LastLinePartial:
		return fmt.Sprintf("\n\n[Showing last %s of line %d (line is %s). Full output: %s]",
			FormatSize(tr.OutputBytes), endLine, FormatSize(lastLineBytes(orig)), path)
	case tr.TruncatedBy == "lines":
		return fmt.Sprintf("\n\n[Showing lines %d-%d of %d. Full output: %s]",
			startLine, endLine, tr.TotalLines, path)
	default:
		return fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Full output: %s]",
			startLine, endLine, tr.TotalLines, FormatSize(DefaultMaxBytes), path)
	}
}

// ToolBoundOutput is the LLM-facing tool result: truncated tail plus a footer.
func ToolBoundOutput(full, tempPrefix string) string {
	tr := TruncateTail(full, DefaultMaxLines, DefaultMaxBytes)
	if !tr.Truncated {
		return full
	}
	path, err := persistFullOutput(tempPrefix, full)
	if err != nil {
		return tr.Content
	}
	return tr.Content + truncationFooter(tr, path, full)
}
