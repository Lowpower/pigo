package tools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Lowpower/pigo/internal/shell"
)

var lookDocker = exec.LookPath

// DockerOptions configures a session-long tool container.
type DockerOptions struct {
	Image      string
	Cwd        string
	Mounts     []Mount
	ExtraAllow []string
}

type dockerRunner struct {
	Image      string
	Cwd        string
	Mounts     []Mount
	ExtraAllow []string

	mu     sync.Mutex
	docker string
	name   string
	ready  bool
	start  error
}

// NewDockerRunner builds a lazy Docker-backed runner. The container starts on first use.
func NewDockerRunner(opt DockerOptions) Runner {
	return &dockerRunner{
		Image:      strings.TrimSpace(opt.Image),
		Cwd:        opt.Cwd,
		Mounts:     append([]Mount{}, opt.Mounts...),
		ExtraAllow: append([]string{}, opt.ExtraAllow...),
	}
}

func (d *dockerRunner) containerPath(p string) (string, error) {
	slash := filepath.ToSlash(p)
	if slash == ContainerWorkDir || strings.HasPrefix(slash, ContainerWorkDir+"/") {
		return slash, nil
	}
	for _, m := range d.Mounts {
		t := strings.TrimSpace(m.Container)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "/") {
			t = "/" + t
		}
		t = path.Clean(t)
		if slash == t || strings.HasPrefix(slash, t+"/") {
			return slash, nil
		}
	}
	return TranslatePath(d.Cwd, d.Mounts, p)
}

func (d *dockerRunner) ensure(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ready {
		return nil
	}
	if d.start != nil {
		return d.start
	}
	bin, err := lookDocker("docker")
	if err != nil || bin == "" {
		d.start = ErrDockerMissing
		return d.start
	}
	d.docker = bin
	if d.name == "" {
		var b [4]byte
		_, _ = rand.Read(b[:])
		d.name = fmt.Sprintf("pigo-tools-%d-%s", os.Getpid(), hex.EncodeToString(b[:]))
	}
	args, err := d.runArgs()
	if err != nil {
		d.start = err
		return err
	}
	cmd := exec.CommandContext(ctx, d.docker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		d.start = fmt.Errorf("container.image is set but Docker failed: %s", msg)
		return d.start
	}
	d.ready = true
	return nil
}

func (d *dockerRunner) runArgs() ([]string, error) {
	cwd, err := absHostPath("", d.Cwd)
	if err != nil {
		return nil, err
	}
	args := []string{
		"run", "-d", "--init",
		"--name", d.name,
		"--label", "pigo.tools=1",
		"-w", ContainerWorkDir,
		"-v", dockerBind(cwd, ContainerWorkDir),
	}
	if uid := os.Getuid(); uid >= 0 {
		args = append(args, "-u", fmt.Sprintf("%d:%d", uid, os.Getgid()))
	}
	for _, m := range d.Mounts {
		host, err := absHostPath(cwd, m.Host)
		if err != nil || strings.TrimSpace(m.Host) == "" {
			continue
		}
		target := strings.TrimSpace(m.Container)
		if target == "" {
			target = path.Join(ContainerWorkDir, filepath.ToSlash(filepath.Base(host)))
		}
		if !strings.HasPrefix(target, "/") {
			target = "/" + target
		}
		args = append(args, "-v", dockerBind(host, path.Clean(target)))
	}
	args = append(args, "--entrypoint", "sh", d.Image, "-c", "while true; do sleep 3600; done")
	return args, nil
}

func dockerBind(host, container string) string {
	return filepath.ToSlash(host) + ":" + container
}

func (d *dockerRunner) exec(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	full := append([]string{"exec"}, args...)
	cmd := exec.CommandContext(ctx, d.docker, full...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return stdout.Bytes(), err
		}
		return stdout.Bytes(), fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

func (d *dockerRunner) ReadFile(p string) ([]byte, error) {
	cpath, err := d.containerPath(p)
	if err != nil {
		return nil, err
	}
	out, err := d.exec(context.Background(), nil, d.name, "sh", "-c", `cat -- "$1"`, "sh", cpath)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (d *dockerRunner) WriteFile(p string, data []byte, _ fs.FileMode) error {
	cpath, err := d.containerPath(p)
	if err != nil {
		return err
	}
	script := `mkdir -p -- "$(dirname -- "$1")" && cat > "$1"`
	_, err = d.exec(context.Background(), bytes.NewReader(data), "-i", d.name, "sh", "-c", script, "sh", cpath)
	return err
}

func (d *dockerRunner) MkdirAll(p string, _ fs.FileMode) error {
	cpath, err := d.containerPath(p)
	if err != nil {
		return err
	}
	_, err = d.exec(context.Background(), nil, d.name, "sh", "-c", `mkdir -p -- "$1"`, "sh", cpath)
	return err
}

func (d *dockerRunner) ReadDir(p string) ([]DirEntry, error) {
	cpath, err := d.containerPath(p)
	if err != nil {
		return nil, err
	}
	out, err := d.exec(context.Background(), nil, d.name, "sh", "-c",
		`if [ ! -d "$1" ]; then echo "not a directory" >&2; exit 1; fi; ls -1A -p -- "$1"`,
		"sh", cpath)
	if err != nil {
		return nil, err
	}
	var entries []DirEntry
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		dir := strings.HasSuffix(line, "/")
		name := strings.TrimSuffix(line, "/")
		entries = append(entries, DirEntry{Name: name, IsDir: dir})
	}
	return entries, nil
}

func (d *dockerRunner) Stat(p string) (FileInfo, error) {
	cpath, err := d.containerPath(p)
	if err != nil {
		return FileInfo{}, err
	}
	out, err := d.exec(context.Background(), nil, d.name, "sh", "-c",
		`if [ -d "$1" ]; then printf dir; elif [ -e "$1" ]; then printf file; else exit 1; fi`,
		"sh", cpath)
	if err != nil {
		return FileInfo{}, os.ErrNotExist
	}
	name := path.Base(cpath)
	switch strings.TrimSpace(string(out)) {
	case "dir":
		return FileInfo{Name: name, IsDir: true}, nil
	default:
		return FileInfo{Name: name, IsDir: false}, nil
	}
}

func (d *dockerRunner) LookPath(file string) (string, error) {
	out, err := d.exec(context.Background(), nil, d.name, "sh", "-c", `command -v "$1"`, "sh", file)
	if err != nil {
		return "", fmt.Errorf("%s: executable file not found", file)
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", fmt.Errorf("%s: executable file not found", file)
	}
	return p, nil
}

func (d *dockerRunner) Command(ctx context.Context, name string, args []string, dir string) (*exec.Cmd, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	workdir := ContainerWorkDir
	if dir != "" {
		if p, err := d.containerPath(dir); err == nil {
			workdir = p
		}
	}
	full := []string{"exec", "-w", workdir, d.name, name}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, d.docker, full...)
	shell.PrepareContext(cmd)
	return cmd, nil
}

func (d *dockerRunner) Bash(ctx context.Context, command, dir string, extraEnv map[string]string) (*exec.Cmd, error) {
	if err := d.ensure(ctx); err != nil {
		return nil, err
	}
	workdir := ContainerWorkDir
	if dir != "" {
		if p, err := d.containerPath(dir); err == nil {
			workdir = p
		}
	}
	args := []string{"exec", "-w", workdir}
	for _, kv := range filterContainerEnv(hostEnviron(), d.ExtraAllow, extraEnv) {
		args = append(args, "-e", kv)
	}
	args = append(args, d.name, "sh", "-c", command)
	cmd := exec.CommandContext(ctx, d.docker, args...)
	shell.PrepareContext(cmd)
	return cmd, nil
}

func (d *dockerRunner) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.ready || d.docker == "" || d.name == "" {
		d.ready = false
		return nil
	}
	cmd := exec.Command(d.docker, "rm", "-f", d.name)
	_ = cmd.Run()
	d.ready = false
	return nil
}
