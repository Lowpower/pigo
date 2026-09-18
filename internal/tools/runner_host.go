package tools

import (
	"context"
	"io/fs"
	"os"
	"os/exec"

	"github.com/Lowpower/pigo/internal/sandbox"
	"github.com/Lowpower/pigo/internal/shell"
)

type hostRunner struct{}

// NewHostRunner returns a Runner that uses the process filesystem and shell.
func NewHostRunner() Runner { return hostRunner{} }

func (hostRunner) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (hostRunner) WriteFile(path string, data []byte, perm fs.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (hostRunner) MkdirAll(path string, perm fs.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (hostRunner) ReadDir(path string) ([]DirEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]DirEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, DirEntry{Name: e.Name(), IsDir: e.IsDir()})
	}
	return out, nil
}

func (hostRunner) Stat(path string) (FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Name: fi.Name(), Size: fi.Size(), IsDir: fi.IsDir()}, nil
}

func (hostRunner) LookPath(file string) (string, error) {
	if file == "rg" {
		return lookRipgrep()
	}
	return exec.LookPath(file)
}

func (hostRunner) Command(ctx context.Context, name string, args []string, dir string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	shell.PrepareContext(cmd)
	return cmd, nil
}

func (hostRunner) Bash(ctx context.Context, command, dir string, extraEnv map[string]string) (*exec.Cmd, error) {
	cfg, err := shell.GetConfig()
	if err != nil {
		return nil, err
	}
	cwd := dir
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	name, argv := sandbox.Command(command, cwd, "")
	var cmd *exec.Cmd
	if name == "bwrap" {
		cmd = exec.CommandContext(ctx, name, argv...)
		shell.PrepareContext(cmd)
	} else {
		cmd = shell.CommandContext(ctx, cfg, command)
	}
	if dir != "" {
		cmd.Dir = dir
	}
	applyExtraEnv(cmd, extraEnv)
	return cmd, nil
}

func (hostRunner) Close() error { return nil }
