package bugreport

import "testing"

func TestExtensionStackMatch(t *testing.T) {
	exts := []Extension{
		{Path: "/opt/ext/tool", Source: "npm:tool", Origin: "package", BaseDir: "/opt/pkg/tool"},
		{Path: "/opt/single/main.go", Source: "/opt/single/main.go", Origin: "package"},
		{Path: "/opt/dir/index.go", Source: "/opt/dir", Origin: "top-level"},
	}
	stack := "goroutine 1 [running]:\nmain.f()\n\t/opt/pkg/tool/lib/file.go:12 +0x20\n"
	got := FindExtensionStackMatches(stack, exts)
	if len(got) != 1 || got[0] != "npm:tool" {
		t.Fatalf("%v", got)
	}
	fileStack := "goroutine 1 [running]:\nmain.f()\n\t/opt/single/main.go:3\n"
	got = FindExtensionStackMatches(fileStack, exts)
	if len(got) != 1 || got[0] != "/opt/single/main.go" {
		t.Fatalf("file %v", got)
	}
	dirStack := "goroutine 1 [running]:\nmain.f()\n\t/opt/dir/index.go:1\n"
	got = FindExtensionStackMatches(dirStack, exts)
	if len(got) != 1 || got[0] != "/opt/dir/index.go" {
		t.Fatalf("dir %v", got)
	}
	if hint := ExtensionHint(stack, exts); hint == "" {
		t.Fatal("empty hint")
	}
}

func TestDescribeExtensions(t *testing.T) {
	got := DescribeExtensions(
		[]string{"/ext/bin"},
		[]string{"/ext/bin --flag"},
		[]ResourceRef{{Path: "/ext/bin", Source: "user/bin", Scope: "user", Origin: "top-level"}},
	)
	if len(got) != 1 || got[0].Source != "user/bin" || got[0].Scope != "user" {
		t.Fatalf("%+v", got)
	}
	cli := DescribeExtensions([]string{"/tmp/extra"}, []string{"/tmp/extra"}, nil)
	if len(cli) != 1 || cli[0].Origin != "top-level" || cli[0].Source != "/tmp/extra" {
		t.Fatalf("%+v", cli)
	}
}
