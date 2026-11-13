package oauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var errEmptyName = errors.New("mcp server name is empty")

// Store is mcp-auth.json. Keys are "name|canonical URL".
type Store struct {
	dir string
	mu  sync.Mutex
}

// Open returns the store for agentDir. The file is created on the first write.
func Open(agentDir string) *Store {
	return &Store{dir: agentDir}
}

func (s *Store) path() string { return filepath.Join(s.dir, FileName) }

// Load returns the record for one server. The fragment of serverURL is ignored.
func (s *Store) Load(name, serverURL string) (Record, bool, error) {
	key, err := storageKey(name, serverURL)
	if err != nil {
		return Record{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var rec Record
	var ok bool
	err = s.withLock(func() error {
		data, err := s.read()
		if err != nil {
			return err
		}
		rec, ok = data[key]
		return nil
	})
	return rec, ok, err
}

// Save replaces the record for one server.
func (s *Store) Save(name, serverURL string, rec Record) error {
	return s.Update(name, serverURL, func(*Record) (*Record, error) {
		cp := rec
		return &cp, nil
	})
}

// Delete removes one server's record. A missing record is not an error.
func (s *Store) Delete(name, serverURL string) error {
	key, err := storageKey(name, serverURL)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withLock(func() error {
		data, err := s.read()
		if err != nil {
			return err
		}
		if _, ok := data[key]; !ok {
			return nil
		}
		delete(data, key)
		return s.write(data)
	})
}

// Update reads and writes one server while holding the process and file locks.
// fn returning a nil record leaves the file unchanged.
func (s *Store) Update(name, serverURL string, fn func(*Record) (*Record, error)) error {
	key, err := storageKey(name, serverURL)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withLock(func() error {
		data, err := s.read()
		if err != nil {
			return err
		}
		var cur *Record
		if existing, ok := data[key]; ok {
			cp := existing
			cur = &cp
		}
		next, err := fn(cur)
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		data[key] = *next
		return s.write(data)
	})
}

func storageKey(name, serverURL string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errEmptyName
	}
	u, err := parseServerURL(serverURL)
	if err != nil {
		return "", err
	}
	return name + "|" + u.String(), nil
}

func (s *Store) read() (map[string]Record, error) {
	b, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Record{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]Record{}, nil
	}
	var out map[string]Record
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]Record{}
	}
	return out, nil
}

func (s *Store) write(data map[string]Record) error {
	if data == nil {
		data = map[string]Record{}
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.WriteFile(s.path(), b, 0o600); err != nil {
		return err
	}
	return os.Chmod(s.path(), 0o600)
}

func (s *Store) withLock(fn func() error) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := lockExclusive(f); err != nil {
		return err
	}
	defer func() { _ = unlockFile(f) }()
	return fn()
}
