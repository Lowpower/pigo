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

func TestFormatSize(t *testing.T) {
	if FormatSize(500) != "500B" {
		t.Fatalf("500: %s", FormatSize(500))
	}
	if FormatSize(1024) != "1.0KB" {
		t.Fatalf("1024: %s", FormatSize(1024))
	}
}
