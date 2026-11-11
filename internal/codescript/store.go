package codescript

import (
	"encoding/json"
	"sync"
)

// Store keeps small JSON values across codemode runs in one session.
type Store struct {
	mu sync.Mutex
	m  map[string]json.RawMessage
}

// Set stores one JSON value, replacing any previous value for key.
func (s *Store) Set(key string, v json.RawMessage) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]json.RawMessage{}
	}
	s.m[key] = append(json.RawMessage(nil), v...)
}

// Get returns a copy of the stored JSON, or false when key is absent.
func (s *Store) Get(key string) (json.RawMessage, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), v...), true
}
