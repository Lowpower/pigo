package tui

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/ai"
)

func formatTokens(count int) string {
	if count < 1000 {
		return strconv.Itoa(count)
	}
	if count < 10_000 {
		return strconv.FormatFloat(float64(count)/1000, 'f', 1, 64) + "k"
	}
	if count < 1_000_000 {
		return strconv.Itoa((count+500)/1000) + "k"
	}
	if count < 10_000_000 {
		return strconv.FormatFloat(float64(count)/1_000_000, 'f', 1, 64) + "M"
	}
	return strconv.Itoa((count+500_000)/1_000_000) + "M"
}

func formatCwdForFooter(cwd, home string) string {
	if cwd == "" {
		return ""
	}
	if home != "" {
		if cwd == home {
			return "~"
		}
		sep := string(os.PathSeparator)
		if strings.HasPrefix(cwd, home+sep) {
			return "~" + cwd[len(home):]
		}
	}
	return cwd
}

func gitBranch(cwd string) string {
	if cwd == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func addUsage(dst *ai.Usage, src ai.Usage) {
	dst.Input += src.Input
	dst.Output += src.Output
	dst.CacheRead += src.CacheRead
	dst.CacheWrite += src.CacheWrite
	dst.TotalTokens += src.TotalTokens
	dst.Cost.Input += src.Cost.Input
	dst.Cost.Output += src.Cost.Output
	dst.Cost.CacheRead += src.Cost.CacheRead
	dst.Cost.CacheWrite += src.Cost.CacheWrite
	dst.Cost.Total += src.Cost.Total
}

func expandTool(s string) string {
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	const maxLines = 40
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], " …")
	}
	out := strings.Join(lines, "\n")
	if len(out) > 4000 {
		return out[:4000] + " …"
	}
	return out
}

func toolResultBody(raw string, expanded bool) string {
	if expanded {
		return expandTool(raw)
	}
	return firstLine(raw)
}

func (m *Model) refreshGit() {
	cwd := ""
	if m.engine != nil {
		cwd = m.engine.Opts.Cwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	m.gitCwd = cwd
	m.gitBranch = gitBranch(cwd)
}

func (m Model) footerText() string {
	home, _ := os.UserHomeDir()
	cwd := m.gitCwd
	if cwd == "" && m.engine != nil {
		cwd = m.engine.Opts.Cwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	pwd := formatCwdForFooter(cwd, home)
	if m.gitBranch != "" {
		pwd += " (" + m.gitBranch + ")"
	}
	if m.engine != nil && m.engine.Opts.Session != nil {
		if name := strings.TrimSpace(m.engine.Opts.Session.Name()); name != "" {
			pwd += " • " + name
		}
	}

	var stats []string
	if m.usage.Input > 0 {
		stats = append(stats, "↑"+formatTokens(m.usage.Input))
	}
	if m.usage.Output > 0 {
		stats = append(stats, "↓"+formatTokens(m.usage.Output))
	}
	if m.usage.CacheRead > 0 {
		stats = append(stats, "R"+formatTokens(m.usage.CacheRead))
	}
	if m.usage.CacheWrite > 0 {
		stats = append(stats, "W"+formatTokens(m.usage.CacheWrite))
	}
	if m.usage.Cost.Total > 0 {
		stats = append(stats, "$"+strconv.FormatFloat(m.usage.Cost.Total, 'f', 3, 64))
	}
	win := 0
	if m.engine != nil && m.engine.Opts.ContextWindow > 0 {
		win = m.engine.Opts.ContextWindow
	} else if m.cfg.ContextWindow > 0 {
		win = m.cfg.ContextWindow
	}
	if win > 0 {
		used := m.usage.Input + m.usage.CacheRead
		pct := "?"
		if used > 0 {
			pct = strconv.FormatFloat(float64(used)*100/float64(win), 'f', 1, 64)
		}
		auto := ""
		if m.cfg.CompactionEnabled() {
			auto = " (auto)"
		}
		stats = append(stats, pct+"%/"+formatTokens(win)+auto)
	}
	right := m.cfg.ResolvedModel()
	if th := strings.TrimSpace(m.cfg.Thinking); th != "" {
		if th == "off" {
			right += " • thinking off"
		} else {
			right += " • " + th
		}
	}
	var flags []string
	if m.hideThinking {
		flags = append(flags, "thinking hidden")
	}
	if m.toolsExpanded {
		flags = append(flags, "tools expanded")
	}
	line2 := strings.Join(stats, " ")
	if line2 != "" {
		line2 += "  "
	}
	line2 += right
	if len(flags) > 0 {
		line2 += "  " + strings.Join(flags, " · ")
	}
	hint := "Ctrl+T thinking · Ctrl+O tools · /help · Ctrl+D exit"
	var b strings.Builder
	if pwd != "" {
		b.WriteString(pwd)
		b.WriteByte('\n')
	}
	if strings.TrimSpace(line2) != "" {
		b.WriteString(line2)
		b.WriteByte('\n')
	}
	b.WriteString(hint)
	return strings.TrimRight(b.String(), "\n")
}
