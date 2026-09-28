package bugreport

import (
	"net/url"
	"strings"

	"github.com/Lowpower/pigo/internal/version"
)

const issuePath = "/Lowpower/pigo/issues/new"

// IssueURL is a prefilled GitHub new-issue link. The body carries the report
// id, version, zip path, and the user's description. Diagnostics stay in the zip.
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
	body.WriteString("\nZip: ")
	body.WriteString(zipPath)
	if hint != "" {
		body.WriteString("\n\n")
		text := hint
		if len(text) > 1500 {
			text = text[:1500]
		}
		body.WriteString(text)
	}
	body.WriteString("\n")
	u := url.URL{Scheme: "https", Host: "github.com", Path: issuePath}
	q := url.Values{}
	q.Set("title", title)
	q.Set("body", body.String())
	u.RawQuery = q.Encode()
	return u.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
