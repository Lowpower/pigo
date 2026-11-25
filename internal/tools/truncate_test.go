package tools

import (
	"os"
	"strings"
	"testing"
)

func TestTruncateTailNoOp(t *testing.T) {
	got := TruncateTail("hello\nworld\n", DefaultMaxLines, DefaultMaxBytes)
	if got.Truncated || got.Content != "hello\nworld\n" {
		t.Fatalf("%+v", got)
	}
}

func TestTruncateTailByLines(t *testing.T) {
	var b strings.Builder
	for i := 1; i <= 2001; i++ {
		if i > 1 {
			b.WriteByte('\n')
		}
		b.WriteString("x")
	}
	got := TruncateTail(b.String(), DefaultMaxLines, DefaultMaxBytes)
	if !got.Truncated || got.TruncatedBy != "lines" {
		t.Fatalf("%+v", got)
	}
	if got.TotalLines != 2001 || got.OutputLines != 2000 {
		t.Fatalf("lines total=%d out=%d", got.TotalLines, got.OutputLines)
	}
	if strings.Count(got.Content, "\n") != 1999 {
		t.Fatalf("newlines=%d", strings.Count(got.Content, "\n"))
	}
}

func TestTruncateTailByBytesPartialLine(t *testing.T) {
	line := strings.Repeat("a", DefaultMaxBytes+100)
	got := TruncateTail(line, DefaultMaxLines, DefaultMaxBytes)
	if !got.Truncated || got.TruncatedBy != "bytes" || !got.LastLinePartial {
		t.Fatalf("%+v", got)
	}
	if utf8ByteLen(got.Content) > DefaultMaxBytes {
		t.Fatalf("output bytes = %d", utf8ByteLen(got.Content))
	}
}

func TestToolBoundOutputWritesTempFile(t *testing.T) {
	full := strings.Repeat("z\n", DefaultMaxLines+10)
	out := ToolBoundOutput(full, "pigo-test")
	if !strings.Contains(out, "Full output:") {
		t.Fatalf("missing footer: %s", out[len(out)-200:])
	}
	idx := strings.LastIndex(out, "Full output: ")
	path := strings.TrimSuffix(out[idx+len("Full output: "):], "]")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if string(b) != full {
		t.Fatalf("temp file len=%d want %d", len(b), len(full))
	}
}

func TestStructuredShellOutputHeadAndTail(t *testing.T) {
	const headMark = "HEAD"
	const tailMark = "TAIL"
	full := headMark + strings.Repeat("M", structuredOutputMaxBytes) + tailMark
	got, truncated := structuredShellOutput(full)
	if !truncated {
		t.Fatal("expected truncation")
	}
	marker := "\n\n[... 8 bytes omitted ...]\n\n"
	if !strings.HasPrefix(got, headMark) || !strings.HasSuffix(got, tailMark) || !strings.Contains(got, marker) {
		t.Fatalf("structured = %q ... %q", got[:40], got[len(got)-40:])
	}
	headBytes := structuredOutputMaxBytes / 2
	tailBytes := structuredOutputMaxBytes - headBytes
	if !strings.HasPrefix(got, string([]byte(full)[:headBytes])) {
		t.Fatal("head bytes were not kept")
	}
	if !strings.HasSuffix(got, string([]byte(full)[len(full)-tailBytes:])) {
		t.Fatal("tail bytes were not kept")
	}
}

func TestStructuredShellOutputKeepsShortAndEmpty(t *testing.T) {
	if got, truncated := structuredShellOutput(""); truncated || got != "" {
		t.Fatalf("empty = %q truncated %v", got, truncated)
	}
	exact := strings.Repeat("a", structuredOutputMaxBytes)
	got, truncated := structuredShellOutput(exact)
	if truncated || got != exact {
		t.Fatalf("exact truncated=%v len=%d", truncated, len(got))
	}
}

func TestStructuredShellOutputUTF8Boundary(t *testing.T) {
	const rune3 = "中"
	full := strings.Repeat(rune3, structuredOutputMaxBytes/len(rune3)+4)
	got, truncated := structuredShellOutput(full)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(got, "\n\n[... ") || !strings.Contains(got, " bytes omitted ...]\n\n") {
		t.Fatalf("missing marker: %q", got[len(got)/2-20:len(got)/2+20])
	}
	parts := strings.Split(got, "\n\n[... ")
	if len(parts) != 2 || !utf8Valid(parts[0]) {
		t.Fatal("head is not valid UTF-8")
	}
	tail := parts[1]
	if i := strings.Index(tail, "]\n\n"); i < 0 || !utf8Valid(tail[i+3:]) {
		t.Fatal("tail is not valid UTF-8")
	}
}

func utf8Valid(s string) bool {
	return strings.ToValidUTF8(s, "") == s
}

func TestFormatSize(t *testing.T) {
	if FormatSize(500) != "500B" {
		t.Fatalf("500: %s", FormatSize(500))
	}
	if FormatSize(1024) != "1.0KB" {
		t.Fatalf("1024: %s", FormatSize(1024))
	}
}

func TestTruncateHeadByBytes(t *testing.T) {
	line := strings.Repeat("a", DefaultMaxBytes+50)
	got := TruncateHead(line, DefaultMaxLines, DefaultMaxBytes)
	if !got.Truncated || !got.FirstLineExceedsLimit {
		t.Fatalf("%+v", got)
	}
}
