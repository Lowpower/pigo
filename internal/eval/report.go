package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// StatusPass is a graded success.
	StatusPass = "pass"
	// StatusFail is a graded failure.
	StatusFail = "fail"
	// StatusSkip means the case was not run (typically no API key).
	StatusSkip = "skip"
	// StatusError is a runtime or infrastructure failure.
	StatusError = "error"

	// VariantWithoutDocs is the docs-lift baseline arm.
	VariantWithoutDocs = "without_docs"
	// VariantWithDocs is the docs-lift candidate arm.
	VariantWithDocs = "with_docs"
)

// Result is one scenario outcome.
type Result struct {
	Name      string        `json:"name"`
	Variant   string        `json:"variant,omitempty"`
	Repeat    int           `json:"repeat,omitempty"`
	Status    string        `json:"status"`
	Output    string        `json:"output,omitempty"`
	Tokens    int           `json:"tokens"`
	Cost      float64       `json:"cost,omitempty"`
	Latency   time.Duration `json:"-"`
	LatencyMs int64         `json:"latencyMs"`
	Session   string        `json:"session,omitempty"`
	Error     string        `json:"error,omitempty"`
}

// PairedMetric is a candidate-minus-baseline mean over eligible pairs.
type PairedMetric struct {
	EligiblePairs int      `json:"eligiblePairs"`
	ControlMean   *float64 `json:"controlMean"`
	TreatmentMean *float64 `json:"treatmentMean"`
	MeanDelta     *float64 `json:"meanDelta"`
}

// BlockedPair is a planned comparison that did not produce two scores.
type BlockedPair struct {
	Name    string   `json:"name"`
	Repeat  int      `json:"repeat"`
	Reasons []string `json:"reasons"`
}

// Comparison is the docs-lift headline over paired arms.
type Comparison struct {
	Control           string        `json:"control"`
	Treatment         string        `json:"treatment"`
	TotalPairs        int           `json:"totalPairs"`
	EligiblePairs     int           `json:"eligiblePairs"`
	BlockedPairs      int           `json:"blockedPairs"`
	ControlPassRate   *float64      `json:"controlPassRate"`
	TreatmentPassRate *float64      `json:"treatmentPassRate"`
	Lift              *float64      `json:"lift"`
	Tokens            PairedMetric  `json:"tokens"`
	LatencyMs         PairedMetric  `json:"latencyMs"`
	Cost              PairedMetric  `json:"cost"`
	Blocked           []BlockedPair `json:"blocked,omitempty"`
}

// Report aggregates scenario results.
type Report struct {
	Passed     int         `json:"passed"`
	Failed     int         `json:"failed"`
	Skipped    int         `json:"skipped"`
	PassRate   float64     `json:"passRate"`
	Comparison *Comparison `json:"comparison,omitempty"`
	Results    []Result    `json:"results"`
}

func summarize(results []Result) Report {
	rep := Report{Results: results}
	for _, r := range results {
		if r.Variant != "" {
			if r.Status == StatusError {
				rep.Failed++
			}
			continue
		}
		switch r.Status {
		case StatusPass:
			rep.Passed++
		case StatusFail, StatusError:
			rep.Failed++
		case StatusSkip:
			rep.Skipped++
		}
	}
	judged := rep.Passed + rep.Failed
	if judged > 0 {
		rep.PassRate = float64(rep.Passed) / float64(judged)
	}
	rep.Comparison = summarizeComparison(results)
	return rep
}

func summarizeComparison(results []Result) *Comparison {
	type arms struct {
		control, treatment *Result
	}
	groups := map[string]*arms{}
	var keys []string
	for i := range results {
		r := &results[i]
		if r.Variant == "" {
			continue
		}
		key := fmt.Sprintf("%s\x00%d", r.Name, r.Repeat)
		g, ok := groups[key]
		if !ok {
			g = &arms{}
			groups[key] = g
			keys = append(keys, key)
		}
		switch r.Variant {
		case VariantWithoutDocs:
			g.control = r
		case VariantWithDocs:
			g.treatment = r
		}
	}
	if len(groups) == 0 {
		return nil
	}
	cmp := &Comparison{
		Control:    VariantWithoutDocs,
		Treatment:  VariantWithDocs,
		TotalPairs: len(groups),
	}
	var pairs [][2]Result
	for _, key := range keys {
		g := groups[key]
		var reasons []string
		reasons = append(reasons, armBlockReason(g.control, VariantWithoutDocs)...)
		reasons = append(reasons, armBlockReason(g.treatment, VariantWithDocs)...)
		if len(reasons) > 0 {
			name, repeat := splitPairKey(key)
			cmp.Blocked = append(cmp.Blocked, BlockedPair{Name: name, Repeat: repeat, Reasons: reasons})
			continue
		}
		pairs = append(pairs, [2]Result{*g.control, *g.treatment})
	}
	cmp.EligiblePairs = len(pairs)
	cmp.BlockedPairs = cmp.TotalPairs - cmp.EligiblePairs
	if cmp.EligiblePairs > 0 {
		var controlPass, treatmentPass int
		for _, p := range pairs {
			if p[0].Status == StatusPass {
				controlPass++
			}
			if p[1].Status == StatusPass {
				treatmentPass++
			}
		}
		cRate := float64(controlPass) / float64(cmp.EligiblePairs)
		tRate := float64(treatmentPass) / float64(cmp.EligiblePairs)
		lift := tRate - cRate
		cmp.ControlPassRate = &cRate
		cmp.TreatmentPassRate = &tRate
		cmp.Lift = &lift
	}
	cmp.Tokens = pairedMetric(pairs, func(r Result) (float64, bool) { return float64(r.Tokens), true })
	cmp.LatencyMs = pairedMetric(pairs, func(r Result) (float64, bool) {
		if r.LatencyMs <= 0 && r.Latency <= 0 {
			return 0, false
		}
		ms := float64(r.LatencyMs)
		if ms == 0 {
			ms = float64(r.Latency.Milliseconds())
		}
		return ms, true
	})
	cmp.Cost = pairedMetric(pairs, func(r Result) (float64, bool) { return r.Cost, true })
	return cmp
}

func splitPairKey(key string) (string, int) {
	name, rest, ok := strings.Cut(key, "\x00")
	if !ok {
		return key, 0
	}
	n, _ := strconv.Atoi(rest)
	return name, n
}

func armBlockReason(r *Result, variant string) []string {
	if r == nil {
		return []string{variant + ": missing"}
	}
	switch r.Status {
	case StatusPass, StatusFail:
		return nil
	case StatusSkip:
		return []string{variant + ": skipped"}
	case StatusError:
		return []string{variant + ": error"}
	default:
		return []string{variant + ": " + r.Status}
	}
}

func pairedMetric(pairs [][2]Result, sel func(Result) (float64, bool)) PairedMetric {
	var control, treatment []float64
	for _, p := range pairs {
		c, cok := sel(p[0])
		t, tok := sel(p[1])
		if !cok || !tok {
			continue
		}
		control = append(control, c)
		treatment = append(treatment, t)
	}
	out := PairedMetric{EligiblePairs: len(control)}
	if len(control) == 0 {
		return out
	}
	cMean := mean(control)
	tMean := mean(treatment)
	delta := tMean - cMean
	out.ControlMean = &cMean
	out.TreatmentMean = &tMean
	out.MeanDelta = &delta
	return out
}

func mean(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
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

func resultLabel(r Result) string {
	if r.Variant == "" {
		return r.Name
	}
	label := r.Name + "/" + r.Variant
	if r.Repeat > 1 {
		label += fmt.Sprintf("#%d", r.Repeat)
	}
	return label
}

// FormatReport is a text table of pass rate, tokens, and latency.
func FormatReport(rep Report) string {
	var b strings.Builder
	judged := rep.Passed + rep.Failed
	fmt.Fprintf(&b, "pass rate: %.0f%% (%d/%d judged, %d skipped)\n\n",
		rep.PassRate*100, rep.Passed, judged, rep.Skipped)
	fmt.Fprintf(&b, "%-28s %-6s %8s %10s %s\n", "NAME", "STATUS", "TOKENS", "LATENCY", "")
	for _, r := range rep.Results {
		extra := r.Error
		fmt.Fprintf(&b, "%-28s %-6s %8d %10s %s\n",
			resultLabel(r), r.Status, r.Tokens, r.Latency.Round(time.Millisecond), extra)
	}
	if rep.Comparison != nil {
		b.WriteByte('\n')
		b.WriteString(formatComparison(*rep.Comparison))
	}
	return b.String()
}

func formatComparison(cmp Comparison) string {
	var b strings.Builder
	b.WriteString("docs-lift comparison\n")
	fmt.Fprintf(&b, "  pairs %d/%d eligible\n", cmp.EligiblePairs, cmp.TotalPairs)
	if cmp.Lift == nil || cmp.ControlPassRate == nil || cmp.TreatmentPassRate == nil {
		if cmp.BlockedPairs > 0 {
			b.WriteString("  pass-rate lift withheld (blocked pairs)\n")
		} else {
			b.WriteString("  pass-rate lift unavailable\n")
		}
	} else {
		fmt.Fprintf(&b, "  pass-rate lift: %s pp (with_docs %.0f%%, without_docs %.0f%%)\n",
			signed(*cmp.Lift*100, 0), *cmp.TreatmentPassRate*100, *cmp.ControlPassRate*100)
	}
	b.WriteString(formatPairedMetric("tokens", cmp.Tokens, 1, ""))
	b.WriteString(formatPairedMetric("latency", cmp.LatencyMs, 1, "ms"))
	if cmp.Cost.MeanDelta == nil {
		b.WriteString("  cost unavailable\n")
	} else {
		fmt.Fprintf(&b, "  cost %+0.4f (with_docs %.4f, without_docs %.4f)\n",
			*cmp.Cost.MeanDelta, *cmp.Cost.TreatmentMean, *cmp.Cost.ControlMean)
	}
	return b.String()
}

func formatPairedMetric(label string, m PairedMetric, digits int, unit string) string {
	if m.MeanDelta == nil || m.ControlMean == nil || m.TreatmentMean == nil {
		return fmt.Sprintf("  %s unavailable\n", label)
	}
	return fmt.Sprintf("  %s %s%s (with_docs %s%s, without_docs %s%s)\n",
		label, signed(*m.MeanDelta, digits), unit,
		fmt.Sprintf("%.*f", digits, *m.TreatmentMean), unit,
		fmt.Sprintf("%.*f", digits, *m.ControlMean), unit)
}

func signed(v float64, digits int) string {
	return fmt.Sprintf("%+.*f", digits, v)
}
