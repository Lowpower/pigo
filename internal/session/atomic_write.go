package session

import (
	"io/fs"
	"os"
	"path/filepath"
)

// syncFile is the fsync used before a session file is published.
// Tests replace it to fail a write before rename or link.
var syncFile = func(f *os.File) error { return f.Sync() }

// writeSessionFile publishes a complete file at path.
//
// exclusive is false for a rewrite of an existing file: path must already
// exist, its permission bits are copied onto the replacement, and the
// contents are swapped in with rename. exclusive is true for a new file:
// perm is applied and the name is created with Link, so an existing path
// is left unchanged.
//
// write must not close f. On any error before publish, path is unchanged
// and the temporary file is removed.
func writeSessionFile(path string, perm fs.FileMode, exclusive bool, write func(f *os.File) error) error {
	if !exclusive {
		st, err := os.Stat(path)
		if err != nil {
			return err
		}
		perm = st.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".pigo-session-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	done := false
	defer func() {
		if !done {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := write(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := syncFile(tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if exclusive {
		if err := os.Link(tmpName, path); err != nil {
			return err
		}
		done = true
		_ = os.Remove(tmpName)
		return nil
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	done = true
	return nil
}
