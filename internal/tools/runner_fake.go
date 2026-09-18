package tools

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type fakeRunner struct {
	mu sync.Mutex

	files map[string][]byte
	dirs  map[string]struct{}

	Cwd        string
	Mounts     []Mount
	Jail       bool
	FilterEnv  bool
	ExtraAllow []string
	Look       map[string]string

	BashCommand string
	BashEnv     []string
	Execs       []fakeExec
}

type fakeExec struct {
	Name string
	Args []string
	Dir  string
}

func (f *fakeRunner) init() {
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	if f.dirs == nil {
		f.dirs = map[string]struct{}{}
		if f.Cwd != "" {
			f.dirs[f.key(f.Cwd)] = struct{}{}
		}
		f.dirs["/"] = struct{}{}
	}
}

func (f *fakeRunner) key(p string) string {
	abs, err := absHostPath(f.Cwd, p)
	if err != nil {
		abs = filepath.Clean(p)
	}
	return abs
}

func (f *fakeRunner) guard(p string) error {
	if !f.Jail {
		return nil
	}
	_, err := TranslatePath(f.Cwd, f.Mounts, p)
	return err
}

func (f *fakeRunner) ReadFile(path string) ([]byte, error) {
	if err := f.guard(path); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	k := f.key(path)
	data, ok := f.files[k]
	if !ok {
		return nil, os.ErrNotExist
	}
	out := make([]byte, len(data))
	copy(out, data)
	return out, nil
}

func (f *fakeRunner) WriteFile(path string, data []byte, _ fs.FileMode) error {
	if err := f.guard(path); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	k := f.key(path)
	_ = f.mkdirAllLocked(filepath.Dir(k))
	cp := make([]byte, len(data))
	copy(cp, data)
	f.files[k] = cp
	delete(f.dirs, k)
	return nil
}

func (f *fakeRunner) MkdirAll(path string, _ fs.FileMode) error {
	if err := f.guard(path); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	return f.mkdirAllLocked(f.key(path))
}

func (f *fakeRunner) mkdirAllLocked(dir string) error {
	dir = filepath.Clean(dir)
	for {
		f.dirs[dir] = struct{}{}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil
}

func (f *fakeRunner) ReadDir(path string) ([]DirEntry, error) {
	if err := f.guard(path); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	dir := f.key(path)
	if _, ok := f.dirs[dir]; !ok {
		if _, isFile := f.files[dir]; isFile {
			return nil, fmt.Errorf("not a directory: %s", path)
		}
		return nil, os.ErrNotExist
	}
	seen := map[string]DirEntry{}
	prefix := dir + string(filepath.Separator)
	for k := range f.files {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		name, after, split := strings.Cut(rest, string(filepath.Separator))
		if name == "" {
			continue
		}
		seen[name] = DirEntry{Name: name, IsDir: split && after != ""}
	}
	for k := range f.dirs {
		if k == dir || !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		name, after, split := strings.Cut(rest, string(filepath.Separator))
		if name == "" {
			continue
		}
		if split && after != "" {
			seen[name] = DirEntry{Name: name, IsDir: true}
			continue
		}
		if _, ok := seen[name]; !ok {
			seen[name] = DirEntry{Name: name, IsDir: true}
		}
	}
	out := make([]DirEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (f *fakeRunner) Stat(path string) (FileInfo, error) {
	if err := f.guard(path); err != nil {
		return FileInfo{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	k := f.key(path)
	if data, ok := f.files[k]; ok {
		return FileInfo{Name: filepath.Base(k), Size: int64(len(data)), IsDir: false}, nil
	}
	if _, ok := f.dirs[k]; ok {
		return FileInfo{Name: filepath.Base(k), IsDir: true}, nil
	}
	return FileInfo{}, os.ErrNotExist
}

func (f *fakeRunner) LookPath(file string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Look != nil {
		if p, ok := f.Look[file]; ok && p != "" {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s: executable file not found", file)
}

func (f *fakeRunner) Command(_ context.Context, name string, args []string, dir string) (*exec.Cmd, error) {
	f.mu.Lock()
	f.Execs = append(f.Execs, fakeExec{Name: name, Args: args, Dir: dir})
	f.mu.Unlock()
	return nil, fmt.Errorf("fake runner cannot exec %s", name)
}

func (f *fakeRunner) Bash(ctx context.Context, command, dir string, extraEnv map[string]string) (*exec.Cmd, error) {
	if err := f.guard(dir); err != nil && dir != "" {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	}
	if f.FilterEnv {
		env := filterContainerEnv(hostEnviron(), f.ExtraAllow, extraEnv)
		f.mu.Lock()
		f.BashCommand = command
		f.BashEnv = append([]string{}, env...)
		f.mu.Unlock()
		cmd.Env = env
		return cmd, nil
	}
	f.mu.Lock()
	f.BashCommand = command
	f.mu.Unlock()
	applyExtraEnv(cmd, extraEnv)
	return cmd, nil
}

func (f *fakeRunner) Close() error { return nil }

func (f *fakeRunner) putFile(path string, data []byte) {
	_ = f.MkdirAll(filepath.Dir(path), 0o755)
	_ = f.WriteFile(path, data, 0o644)
}
