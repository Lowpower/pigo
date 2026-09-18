package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// StatusPass is a graded success.
	StatusPass = "pass"
	// StatusFail is a graded or runtime failure.
	StatusFail = "fail"
	// StatusSkip means the case was not run (typically no API key).
	StatusSkip = "skip"
)

// Result is one scenario outcome.
type Result struct {
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Output    string        `json:"output,omitempty"`
	Tokens    int           `json:"tokens"`
	Latency   time.Duration `json:"-"`
	LatencyMs int64         `json:"latencyMs"`
	Session   string        `json:"session,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// Report aggregates scenario results.
type Report struct {
	Passed   int      `json:"passed"`
	Failed   int      `json:"failed"`
	Skipped  int      `json:"skipped"`
	PassRate float64  `json:"passRate"`
	Results  []Result `json:"results"`
}

func summarize(results []Result) Report {
	rep := Report{Results: results}
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			rep.Passed++
		case StatusFail:
			rep.Failed++
		case StatusSkip:
			rep.Skipped++
		}
	}
	judged := rep.Passed + rep.Failed
	if judged > 0 {
		rep.PassRate = float64(rep.Passed) / float64(judged)
	}
	return rep
}

func writeReport(dir string, rep Report) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(filepath.Join(dir, "report.json"), b, 0o644)
}

// FormatReport is a text table of pass rate, tokens, and latency.
func FormatReport(rep Report) string {
	var b strings.Builder
	judged := rep.Passed + rep.Failed
	fmt.Fprintf(&b, "pass rate: %.0f%% (%d/%d judged, %d skipped)\n\n",
		rep.PassRate*100, rep.Passed, judged, rep.Skipped)
	fmt.Fprintf(&b, "%-20s %-6s %8s %10s %s\n", "NAME", "STATUS", "TOKENS", "LATENCY", "")
	for _, r := range rep.Results {
		extra := r.Error
		fmt.Fprintf(&b, "%-20s %-6s %8d %10s %s\n",
			r.Name, r.Status, r.Tokens, r.Latency.Round(time.Millisecond), extra)
	}
	return b.String()
}
