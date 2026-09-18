package tools

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTranslatePathCwdAndMounts(t *testing.T) {
	cwd := t.TempDir()
	extra := t.TempDir()
	inside := filepath.Join(cwd, "src", "a.go")
	got, err := TranslatePath(cwd, nil, inside)
	if err != nil {
		t.Fatal(err)
	}
	if got != ContainerWorkDir+"/src/a.go" {
		t.Fatalf("cwd map = %q", got)
	}
	got, err = TranslatePath(cwd, nil, cwd)
	if err != nil || got != ContainerWorkDir {
		t.Fatalf("cwd root = %q err=%v", got, err)
	}
	mounts := []Mount{{Host: extra, Container: "/cache"}}
	file := filepath.Join(extra, "x.bin")
	got, err = TranslatePath(cwd, mounts, file)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/cache/x.bin" {
		t.Fatalf("mount map = %q", got)
	}
	_, err = TranslatePath(cwd, mounts, filepath.Join(t.TempDir(), "secret"))
	if !errors.Is(err, ErrPathOutsideContainer) {
		t.Fatalf("outside: %v", err)
	}
}

func TestFilterContainerEnvDropsSecrets(t *testing.T) {
	host := []string{
		"PATH=/bin",
		"HOME=/home/u",
		"ANTHROPIC_API_KEY=sk-secret",
		"OPENAI_API_KEY=sk-2",
		"MY_TOKEN=abc",
		"AUTHORIZATION=Bearer x",
		"OAUTH_ACCESS=tok",
		"LANG=C",
		"LC_ALL=C",
		"PIGO_SESSION_FILE=/host/auth/session.jsonl",
	}
	extra := map[string]string{
		"AI_AGENT":          "pigo",
		"PIGO_SESSION_ID":   "sess",
		"PIGO_SESSION_FILE": "/leaked",
		"CUSTOM_API_KEY":    "nope",
		"GOPATH":            "/go",
	}
	pairs := filterContainerEnv(host, []string{"GOPATH", "ANTHROPIC_API_KEY"}, extra)
	joined := strings.Join(pairs, "\n")
	for _, secret := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "MY_TOKEN", "AUTHORIZATION", "OAUTH_ACCESS", "CUSTOM_API_KEY", "PIGO_SESSION_FILE"} {
		if strings.Contains(joined, secret+"=") {
			t.Fatalf("secret %s leaked:\n%s", secret, joined)
		}
	}
	want := []string{"PATH=", "HOME=", "LANG=", "LC_ALL=", "AI_AGENT=pigo", "PIGO_SESSION_ID=sess", "GOPATH=/go"}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Fatalf("missing %q in\n%s", w, joined)
		}
	}
}

func TestFakeRunnerJailsAndFileTools(t *testing.T) {
	cwd := t.TempDir()
	f := &fakeRunner{Cwd: cwd, Jail: true}
	f.putFile(filepath.Join(cwd, "note.txt"), []byte("hello\n"))
	reg := NewBuiltins(Options{Cwd: cwd, Runner: f})
	out, isErr := reg.Execute(t.Context(), "read", map[string]any{"path": "note.txt"})
	if isErr || out != "hello\n" {
		t.Fatalf("read = %q isErr=%v", out, isErr)
	}
	out, isErr = reg.Execute(t.Context(), "write", map[string]any{"path": "out.txt", "content": "x"})
	if isErr {
		t.Fatalf("write: %s", out)
	}
	out, isErr = reg.Execute(t.Context(), "ls", map[string]any{})
	if isErr || !strings.Contains(out, "note.txt") || !strings.Contains(out, "out.txt") {
		t.Fatalf("ls = %q", out)
	}
	out, isErr = reg.Execute(t.Context(), "read", map[string]any{"path": filepath.Join(t.TempDir(), "id_rsa")})
	if !isErr || !strings.Contains(out, ErrPathOutsideContainer.Error()) {
		t.Fatalf("jail read = %q isErr=%v", out, isErr)
	}
}

func TestFakeRunnerBashDropsAPIKeys(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	t.Setenv("ANTHROPIC_API_KEY", "sk-live")
	t.Setenv("OPENAI_API_KEY", "sk-oai")
	cwd := t.TempDir()
	f := &fakeRunner{Cwd: cwd, Jail: true, FilterEnv: true}
	tool := bashTool{cwd: cwd, fs: f, env: map[string]string{"AI_AGENT": "pigo", "ANTHROPIC_API_KEY": "from-extra"}}
	out, isErr := tool.Execute(t.Context(), map[string]any{
		"command": `printf '%s' "$ANTHROPIC_API_KEY$OPENAI_API_KEY"`,
	})
	if isErr {
		t.Fatalf("bash: %s", out)
	}
	if strings.Contains(out, "sk-") {
		t.Fatalf("key leaked in output: %q", out)
	}
	joined := strings.Join(f.BashEnv, "\n")
	if strings.Contains(joined, "ANTHROPIC_API_KEY") || strings.Contains(joined, "OPENAI_API_KEY") {
		t.Fatalf("key in env: %s", joined)
	}
	if !strings.Contains(joined, "AI_AGENT=pigo") {
		t.Fatalf("session env missing: %s", joined)
	}
}

func TestDockerRunnerMissingBinary(t *testing.T) {
	orig := lookDocker
	lookDocker = func(string) (string, error) { return "", errors.New("missing") }
	t.Cleanup(func() { lookDocker = orig })

	cwd := t.TempDir()
	r := NewDockerRunner(DockerOptions{Image: "debian:bookworm-slim", Cwd: cwd})
	_, err := r.ReadFile("README.md")
	if !errors.Is(err, ErrDockerMissing) {
		t.Fatalf("err=%v", err)
	}
	cmd, err := r.Bash(t.Context(), "echo hi", cwd, nil)
	if cmd != nil || !errors.Is(err, ErrDockerMissing) {
		t.Fatalf("bash cmd=%v err=%v", cmd, err)
	}
}

func TestHostRunnerStillReadsTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("z\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, isErr := readTool{cwd: dir}.Execute(t.Context(), map[string]any{"path": "a.txt"})
	if isErr || out != "z\n" {
		t.Fatalf("host nil runner = %q isErr=%v", out, isErr)
	}
}
