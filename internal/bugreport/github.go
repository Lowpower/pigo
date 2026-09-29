package bugreport

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/version"
)

const issuePath = "/Lowpower/pigo/issues/new"

// IssueURL is a prefilled GitHub new-issue link. Title is the first query
// parameter so a wrapped terminal line still opens the full title. The body
// names the zip file and asks the reporter to attach it. Diagnostics stay in
// the zip; the local directory is not copied into the issue.
func IssueURL(id, zipPath, hint string) string {
	title := "pigo bug report " + id
	if line := firstLine(hint); line != "" {
		title = line
		if len(title) > 80 {
			title = title[:80]
		}
	}
	var body strings.Builder
	body.WriteString("Report ID: ")
	body.WriteString(id)
	body.WriteString("\nVersion: ")
	body.WriteString(version.Version)
	body.WriteString("\n\nAttach `")
	body.WriteString(zipBase(zipPath))
	body.WriteString("` to this issue.\n")
	if hint != "" {
		body.WriteString("\n")
		text := hint
		if len(text) > 1500 {
			text = text[:1500]
		}
		body.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			body.WriteString("\n")
		}
	}
	u := url.URL{Scheme: "https", Host: "github.com", Path: issuePath}
	u.RawQuery = "title=" + url.QueryEscape(title) + "&body=" + url.QueryEscape(body.String())
	return u.String()
}

func zipBase(path string) string {
	name := filepath.Base(path)
	if name == "" || name == "." || name == string(filepath.Separator) {
		if path == "" {
			return "pigo-bug-report.zip"
		}
		return path
	}
	return name
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
