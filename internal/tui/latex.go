package tui

import "strings"

// joinSymbols are the relational-algebra LaTeX commands pi-tui renders as Unicode.
// Longer names are listed first so \leftouterjoin is not eaten by \Join.
var latexJoinSymbols = []struct {
	cmd string
	sym string
}{
	{`\leftouterjoin`, "⟕"},
	{`\rightouterjoin`, "⟖"},
	{`\fullouterjoin`, "⟗"},
	{`\bowtie`, "⋈"},
	{`\ltimes`, "⋉"},
	{`\rtimes`, "⋊"},
	{`\Join`, "⋈"},
}

func transformLatexJoins(md string) string {
	if md == "" || !strings.Contains(md, `\`) {
		return md
	}
	out := md
	for _, p := range latexJoinSymbols {
		if strings.Contains(out, p.cmd) {
			out = strings.ReplaceAll(out, p.cmd, p.sym)
		}
	}
	return out
}
