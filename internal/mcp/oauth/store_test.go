package oauth

import (
	"os"
	"path/filepath"
	"testing"
)

func statPerm(path string) (os.FileMode, error) {
	st, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return st.Mode().Perm(), nil
}

func TestStoreKeysDoNotOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "agent")
	s := Open(dir)
	const raw = "https://mcp.example.com/mcp#session"
	if err := s.Save("alpha", raw, Record{Tokens: &Tokens{AccessToken: "alpha-token"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("beta", "https://mcp.example.com/mcp", Record{Tokens: &Tokens{AccessToken: "beta-token"}}); err != nil {
		t.Fatal(err)
	}
	gotA, ok, err := s.Load("alpha", "https://mcp.example.com/mcp")
	if err != nil || !ok || gotA.Tokens == nil || gotA.Tokens.AccessToken != "alpha-token" {
		t.Fatalf("alpha = %+v ok=%v err=%v", gotA.Tokens, ok, err)
	}
	gotB, ok, err := s.Load("beta", "https://mcp.example.com/mcp")
	if err != nil || !ok || gotB.Tokens == nil || gotB.Tokens.AccessToken != "beta-token" {
		t.Fatalf("beta = %+v ok=%v err=%v", gotB.Tokens, ok, err)
	}

	if err := s.Delete("alpha", raw); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Load("alpha", raw); err != nil || ok {
		t.Fatalf("alpha still stored ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.Load("beta", "https://mcp.example.com/mcp"); err != nil || !ok {
		t.Fatalf("beta removed ok=%v err=%v", ok, err)
	}

	st, err := statPerm(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if st != 0o600 {
		t.Fatalf("mcp-auth.json mode = %o", st)
	}
	dirMode, err := statPerm(dir)
	if err != nil {
		t.Fatal(err)
	}
	if dirMode != 0o700 {
		t.Fatalf("agent dir mode = %o", dirMode)
	}
}
