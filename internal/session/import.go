package session

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Import copies src into the session directory for cwd and opens the copy.
// The source file is not modified. When src is already the destination path,
// that file is opened in place. Header bytes, including cwd, are copied as-is.
func Import(src, cwd, agentDir, sessionDir string) (*Manager, error) {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(srcAbs); err != nil {
		return nil, err
	}
	dir := StorageDir(cwd, agentDir, sessionDir)
	base := filepath.Base(srcAbs)
	if filepath.Join(dir, base) == srcAbs {
		return Open(srcAbs)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	created, err := copySessionExclusive(srcAbs, dir, base)
	if err != nil {
		return nil, err
	}
	opened, err := Open(created)
	if err != nil {
		_ = os.Remove(created)
		return nil, err
	}
	return opened, nil
}

// copySessionExclusive writes src to dir/base, or dir/name-N.ext when that
// name is taken. Creation uses O_EXCL so an existing file is never replaced.
func copySessionExclusive(src, dir, base string) (string, error) {
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	for n := 0; ; n++ {
		candidate := filepath.Join(dir, base)
		if n > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s-%d%s", name, n, ext))
		}
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		if err := copySessionBytes(f, src); err != nil {
			_ = f.Close()
			_ = os.Remove(candidate)
			return "", err
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(candidate)
			return "", err
		}
		return candidate, nil
	}
}

func copySessionBytes(dst *os.File, src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	_, err = io.Copy(dst, in)
	return err
}
