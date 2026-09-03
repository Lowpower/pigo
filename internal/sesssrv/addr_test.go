package sesssrv

import "testing"

func TestParseUnixPath(t *testing.T) {
	got, err := ParseUnixPath("unix:///tmp/pigo.sock")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/pigo.sock" {
		t.Fatalf("path=%q", got)
	}
	if _, err := ParseUnixPath("http://127.0.0.1:9"); err == nil {
		t.Fatal("expected unsupported transport")
	}
	if _, err := ParseUnixPath(""); err == nil {
		t.Fatal("expected empty address error")
	}
}
