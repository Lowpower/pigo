package pkgmgr

import "testing"

func TestParseSource(t *testing.T) {
	cases := []struct {
		in   string
		kind Kind
		name string // npm name or git host/path or local path
		ver  string
	}{
		{in: "npm:left-pad", kind: KindNPM, name: "left-pad"},
		{in: "npm:left-pad@1.2.3", kind: KindNPM, name: "left-pad", ver: "1.2.3"},
		{in: "npm:@foo/bar", kind: KindNPM, name: "@foo/bar"},
		{in: "npm:@foo/bar@2.0.0", kind: KindNPM, name: "@foo/bar", ver: "2.0.0"},
		{in: "./relative/path", kind: KindLocal, name: "./relative/path"},
		{in: "../up/path", kind: KindLocal, name: "../up/path"},
		{in: "/absolute/path/to/package", kind: KindLocal, name: "/absolute/path/to/package"},
		{in: "github.com/user/repo", kind: KindLocal, name: "github.com/user/repo"},
		{in: "git:github.com/user/repo", kind: KindGit, name: "github.com/user/repo"},
		{in: "git:github.com/user/repo@v1", kind: KindGit, name: "github.com/user/repo", ver: "v1"},
		{in: "https://github.com/user/repo", kind: KindGit, name: "github.com/user/repo"},
		{in: "https://github.com/user/repo.git", kind: KindGit, name: "github.com/user/repo"},
		{in: "https://github.com/user/repo@v1.2.3", kind: KindGit, name: "github.com/user/repo", ver: "v1.2.3"},
		{in: "git:https://github.com/user/repo", kind: KindGit, name: "github.com/user/repo"},
		{in: "git:git@github.com:user/repo", kind: KindGit, name: "github.com/user/repo"},
		{in: "ssh://git@github.com/user/repo", kind: KindGit, name: "github.com/user/repo"},
		{in: "https://gitlab.com/user/repo", kind: KindGit, name: "gitlab.com/user/repo"},
	}
	for _, tc := range cases {
		got, err := ParseSource(tc.in)
		if err != nil {
			t.Fatalf("ParseSource(%q): %v", tc.in, err)
		}
		if got.Kind != tc.kind {
			t.Errorf("ParseSource(%q).Kind = %s, want %s", tc.in, got.Kind, tc.kind)
		}
		switch tc.kind {
		case KindNPM:
			if got.Name != tc.name || got.Version != tc.ver {
				t.Errorf("ParseSource(%q) npm name=%q ver=%q, want %q %q", tc.in, got.Name, got.Version, tc.name, tc.ver)
			}
		case KindGit:
			id := got.Host + "/" + got.RepoPath
			if id != tc.name {
				t.Errorf("ParseSource(%q) git = %q, want %q", tc.in, id, tc.name)
			}
			if got.Ref != tc.ver {
				t.Errorf("ParseSource(%q) ref = %q, want %q", tc.in, got.Ref, tc.ver)
			}
		case KindLocal:
			if got.Path != tc.name {
				t.Errorf("ParseSource(%q) path = %q, want %q", tc.in, got.Path, tc.name)
			}
		}
	}
}

func TestPackageIdentity(t *testing.T) {
	a, _ := ParseSource("npm:@foo/bar@1.0.0")
	b, _ := ParseSource("npm:@foo/bar@2.0.0")
	if a.Identity() != b.Identity() || a.Identity() != "npm:@foo/bar" {
		t.Fatalf("npm identity: %q vs %q", a.Identity(), b.Identity())
	}
	g1, _ := ParseSource("https://github.com/user/repo")
	g2, _ := ParseSource("git:git@github.com:user/repo")
	if g1.Identity() != g2.Identity() || g1.Identity() != "git:github.com/user/repo" {
		t.Fatalf("git identity: %q vs %q", g1.Identity(), g2.Identity())
	}
}
