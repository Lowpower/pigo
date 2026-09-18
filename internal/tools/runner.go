package tools

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// ContainerWorkDir is the cwd mount point inside a tool container.
const ContainerWorkDir = "/workspace"

// ErrPathOutsideContainer is returned when a host path is not under cwd or extra mounts.
var ErrPathOutsideContainer = errors.New("path is outside the container mounts")

// ErrDockerMissing is returned when container.image is set but docker is not on PATH.
var ErrDockerMissing = errors.New("container.image is set but docker was not found in PATH; install Docker or pass --no-sandbox")

// FileInfo is the subset of fs.FileInfo tools need.
type FileInfo struct {
	Name  string
	Size  int64
	IsDir bool
}

// DirEntry is one ReadDir result.
type DirEntry struct {
	Name  string
	IsDir bool
}

// Mount is one extra host→container bind.
type Mount struct {
	Host      string
	Container string
}

// Runner is filesystem + process execution for built-in tools and bang.
type Runner interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	ReadDir(path string) ([]DirEntry, error)
	Stat(path string) (FileInfo, error)
	LookPath(file string) (string, error)
	Command(ctx context.Context, name string, args []string, dir string) (*exec.Cmd, error)
	Bash(ctx context.Context, command, dir string, extraEnv map[string]string) (*exec.Cmd, error)
	Close() error
}

func useRunner(r Runner) Runner {
	if r == nil {
		return hostRunner{}
	}
	return r
}

func isHostRunner(r Runner) bool {
	switch r.(type) {
	case nil, hostRunner, *hostRunner:
		return true
	default:
		return false
	}
}

// TranslatePath maps a host path into the container. cwd is mounted at /workspace.
func TranslatePath(cwd string, mounts []Mount, hostPath string) (string, error) {
	abs, err := absHostPath(cwd, hostPath)
	if err != nil {
		return "", err
	}
	if cwd != "" {
		cwdAbs, err := absHostPath("", cwd)
		if err == nil && inRoot(abs, cwdAbs) {
			return joinContainer(ContainerWorkDir, cwdAbs, abs)
		}
	}
	for _, m := range mounts {
		host := strings.TrimSpace(m.Host)
		if host == "" {
			continue
		}
		hostAbs, err := absHostPath(cwd, host)
		if err != nil || !inRoot(abs, hostAbs) {
			continue
		}
		target := strings.TrimSpace(m.Container)
		if target == "" {
			target = path.Join(ContainerWorkDir, filepath.ToSlash(filepath.Base(hostAbs)))
		}
		if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		target = path.Clean(target)
		return joinContainer(target, hostAbs, abs)
	}
	return "", fmt.Errorf("%w: %s", ErrPathOutsideContainer, hostPath)
}

func absHostPath(cwd, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		p = cwd
	}
	if p == "" {
		return filepath.Abs(".")
	}
	if !filepath.IsAbs(p) {
		if cwd != "" {
			p = filepath.Join(cwd, p)
		}
	}
	return filepath.Abs(p)
}

func joinContainer(root, hostRoot, abs string) (string, error) {
	rel, err := filepath.Rel(hostRoot, abs)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return root, nil
	}
	return path.Join(root, filepath.ToSlash(rel)), nil
}

func inRoot(abs, root string) bool {
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	sep := string(filepath.Separator)
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+sep))
}

func runnerSearchPath(r Runner, hostPath string) string {
	if d, ok := r.(*dockerRunner); ok {
		if p, err := d.containerPath(hostPath); err == nil {
			return p
		}
	}
	return hostPath
}

// SameDockerContainer reports whether r is a Docker runner for this image and cwd.
func SameDockerContainer(r Runner, image, cwd string) bool {
	d, ok := r.(*dockerRunner)
	return ok && d.Image == image && d.Cwd == cwd
}
