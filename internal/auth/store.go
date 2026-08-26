package auth

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Store is the auth.json credential store (pi AuthStorage / CredentialStore).
type Store struct {
	dir string
	mu  sync.Mutex
	ro  bool
}

// Open returns a store for agentDir/auth.json. Missing files are empty.
func Open(agentDir string) *Store {
	_ = Migrate(agentDir)
	return &Store{dir: agentDir}
}

// ReadOnly opens a store that rejects modify/delete (auth check --no-refresh).
func ReadOnly(agentDir string) *Store {
	s := Open(agentDir)
	s.ro = true
	return s
}

func (s *Store) path() string { return filepath.Join(s.dir, "auth.json") }

// Read returns the stored credential, resolving api_key templates/commands.
func (s *Store) Read(provider string) (Credential, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadUnlocked()
	if err != nil {
		return Credential{}, false, err
	}
	c, ok := data[provider]
	if !ok {
		return Credential{}, false, nil
	}
	if c.Type != TypeAPIKey || c.Key == "" {
		return c, true, nil
	}
	if s.ro && IsCommandConfigValue(c.Key) {
		return c, true, nil
	}
	if resolved := ResolveConfigValue(c.Key, c.Env); resolved != "" {
		c.Key = resolved
	}
	return c, true, nil
}

// List returns non-secret credential metadata. It does not execute key commands.
func (s *Store) List() ([]Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.loadUnlocked()
	if err != nil {
		return nil, err
	}
	out := make([]Info, 0, len(data))
	for id, c := range data {
		out = append(out, Info{ProviderID: id, Type: c.Type})
	}
	return out, nil
}

// Modify is the only write path. fn seeing nil current means the entry is missing.
// Returning nil from fn leaves the entry unchanged. Returning a credential writes it.
func (s *Store) Modify(provider string, fn func(current *Credential) (*Credential, error)) (*Credential, error) {
	if s.ro {
		return nil, errReadOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withLock(func() (*Credential, error) {
		data, err := s.loadUnlocked()
		if err != nil {
			return nil, err
		}
		var cur *Credential
		if c, ok := data[provider]; ok {
			cc := c
			cur = &cc
		}
		next, err := fn(cur)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return cur, nil
		}
		data[provider] = *next
		if err := s.saveUnlocked(data); err != nil {
			return nil, err
		}
		out := *next
		return &out, nil
	})
}

// Delete removes a provider credential.
func (s *Store) Delete(provider string) error {
	if s.ro {
		return errReadOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.withLock(func() (*Credential, error) {
		data, err := s.loadUnlocked()
		if err != nil {
			return nil, err
		}
		delete(data, provider)
		return nil, s.saveUnlocked(data)
	})
	return err
}

func (s *Store) loadUnlocked() (map[string]Credential, error) {
	b, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Credential{}, nil
		}
		return nil, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return map[string]Credential{}, nil
	}
	return decodeAuthJSON(b)
}

func (s *Store) saveUnlocked(data map[string]Credential) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	if data == nil {
		data = map[string]Credential{}
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(), append(b, '\n'), 0o600)
}

func (s *Store) withLock(fn func() (*Credential, error)) (*Credential, error) {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return nil, err
	}
	p := s.path()
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, err
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}

func decodeAuthJSON(b []byte) (map[string]Credential, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	if wrapped, ok := unwrapLegacyProviders(raw); ok {
		return wrapped, nil
	}
	out := map[string]Credential{}
	for k, v := range raw {
		var c Credential
		if err := json.Unmarshal(v, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, nil
}

func unwrapLegacyProviders(raw map[string]json.RawMessage) (map[string]Credential, bool) {
	p, ok := raw["providers"]
	if !ok {
		return nil, false
	}
	var inner map[string]Credential
	if err := json.Unmarshal(p, &inner); err != nil {
		return nil, false
	}
	for k, v := range raw {
		if k == "providers" {
			continue
		}
		var c Credential
		if json.Unmarshal(v, &c) == nil && (c.Type == TypeAPIKey || c.Type == TypeOAuth) {
			return nil, false
		}
	}
	if inner == nil {
		inner = map[string]Credential{}
	}
	return inner, true
}
