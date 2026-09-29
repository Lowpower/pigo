package bugreport

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Lowpower/pigo/internal/version"
)

func TestIssueURLTitleSurvivesTerminalWrap(t *testing.T) {
	id := "36dff4e8-dcc4-468f-86dc-f164fe2a906a"
	zipPath := "/Users/szq/deps/pigo/" + ArchiveName(id)
	raw := IssueURL(id, zipPath, "")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("title") != "pigo bug report "+id {
		t.Fatalf("title %q", q.Get("title"))
	}
	// A 241-column terminal used to open only the first visual line, which
	// ended at "pigo bug repo" because Encode put title after body.
	const wrap = 241
	head := raw
	if len(head) > wrap {
		head = head[:wrap]
	}
	if !titleIn(head, "pigo bug report "+id) {
		t.Fatalf("title truncated at column %d:\n%s", wrap, head)
	}

	body := q.Get("body")
	if strings.Contains(body, "/Users/") || strings.Contains(body, zipPath) {
		t.Fatalf("issue body should name the zip, not the local path:\n%s", body)
	}
	name := ArchiveName(id)
	if !strings.Contains(body, name) || !strings.Contains(body, "Report ID: "+id) || !strings.Contains(body, "Version: "+version.Version) {
		t.Fatalf("body %q", body)
	}
	if !strings.Contains(strings.ToLower(body), "attach") {
		t.Fatalf("body should say to attach the zip:\n%s", body)
	}
}

func TestIssueURLUsesHintAsTitle(t *testing.T) {
	u, err := url.Parse(IssueURL("id-1", "/tmp/pigo-bug-report-id-1.zip", "editor froze\nafter save"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("title") != "editor froze" {
		t.Fatalf("title %q", u.Query().Get("title"))
	}
	if !strings.Contains(u.Query().Get("body"), "editor froze\nafter save") {
		t.Fatalf("body %q", u.Query().Get("body"))
	}
}

func titleIn(raw, title string) bool {
	return strings.Contains(raw, url.QueryEscape(title)) || strings.Contains(raw, strings.ReplaceAll(url.QueryEscape(title), "%20", "+"))
}
