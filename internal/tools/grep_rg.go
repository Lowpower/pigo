package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var lookRipgrep = func() (string, error) {
	return exec.LookPath("rg")
}

type rgEvent struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		LineNumber int `json:"line_number"`
		Lines      struct {
			Text string `json:"text"`
		} `json:"lines"`
	} `json:"data"`
}

type rgMatch struct {
	path string
	line int
	text string
}

func grepRipgrep(ctx context.Context, rg, root string, isDir bool, p grepParams, limit int) (string, bool, bool) {
	args := []string{"--json", "--line-number", "--color=never", "--hidden"}
	if p.IgnoreCase {
		args = append(args, "--ignore-case")
	}
	if p.Literal {
		args = append(args, "--fixed-strings")
	}
	if p.Glob != "" {
		args = append(args, "--glob", p.Glob)
	}
	args = append(args, "--", p.Pattern, root)

	cmd := exec.CommandContext(ctx, rg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", false, false
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return "", false, false
	}

	var matches []rgMatch
	killed := false
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		var ev rgEvent
		if json.Unmarshal([]byte(line), &ev) != nil || ev.Type != "match" {
			continue
		}
		if ev.Data.Path.Text == "" || ev.Data.LineNumber <= 0 {
			continue
		}
		text := strings.TrimRight(ev.Data.Lines.Text, "\r\n")
		matches = append(matches, rgMatch{path: ev.Data.Path.Text, line: ev.Data.LineNumber, text: text})
		if len(matches) >= limit {
			killed = true
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			break
		}
	}
	_ = stdout.Close()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err().Error(), true, true
	}
	if !killed && waitErr != nil {
		ee, ok := waitErr.(*exec.ExitError)
		if !ok {
			return "", false, false
		}
		if ee.ExitCode() != 1 {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = waitErr.Error()
			}
			return msg, true, true
		}
	}

	if len(matches) == 0 {
		return "No matches found for " + p.Pattern, false, true
	}

	var b strings.Builder
	for _, m := range matches {
		rel := grepDisplayPath(root, isDir, m.path)
		if p.Context > 0 {
			writeGrepContext(&b, m.path, rel, m.line, p.Context)
			continue
		}
		fmt.Fprintf(&b, "%s:%d:%s\n", rel, m.line, TruncateLine(m.text, GrepMaxLineLength))
	}
	return finishGrepResult(p.Pattern, b.String(), len(matches), killed || len(matches) >= limit, limit)
}

func grepDisplayPath(root string, isDir bool, filePath string) string {
	if !isDir {
		return filepath.ToSlash(root)
	}
	rel, err := filepath.Rel(root, filePath)
	if err != nil {
		return filepath.ToSlash(filePath)
	}
	return filepath.ToSlash(rel)
}

func writeGrepContext(b *strings.Builder, path, rel string, lineNum, context int) {
	data, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(data, 0) >= 0 {
		fmt.Fprintf(b, "%s:%d: (unable to read file)\n", rel, lineNum)
		return
	}
	lines := strings.Split(string(data), "\n")
	i := lineNum - 1
	if i < 0 || i >= len(lines) {
		fmt.Fprintf(b, "%s:%d: (unable to read file)\n", rel, lineNum)
		return
	}
	lo := max(0, i-context)
	hi := min(len(lines)-1, i+context)
	for j := lo; j <= hi; j++ {
		sep := "-"
		if j == i {
			sep = ":"
		}
		fmt.Fprintf(b, "%s:%d%s%s\n", rel, j+1, sep, TruncateLine(lines[j], GrepMaxLineLength))
	}
	b.WriteString("--\n")
}

func finishGrepResult(pattern, raw string, count int, truncated bool, limit int) (string, bool, bool) {
	if count == 0 {
		return "No matches found for " + pattern, false, true
	}
	result := strings.TrimRight(raw, "\n")
	if truncated {
		result += fmt.Sprintf("\n[%d matches limit reached]", limit)
	}
	tr := TruncateHead(result, DefaultMaxLines, DefaultMaxBytes)
	if tr.Truncated && !tr.FirstLineExceedsLimit {
		result = tr.Content + fmt.Sprintf("\n[truncated to %s]", FormatSize(DefaultMaxBytes))
	}
	return result, false, true
}
