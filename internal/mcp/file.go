package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UpsertSpec is one server written by `pigo mcp add`.
// Empty optional fields leave an existing value in place.
type UpsertSpec struct {
	Name              string
	Command           string
	Args              []string
	URL               string
	Description       string
	Exposure          string
	Env               map[string]string
	Headers           map[string]string
	OAuthClientID     string
	OAuthClientSecret string
	OAuthClientName   string
	OAuthCallbackPort int
	BearerTokenEnvVar string
}

// ConfigPath is the mcp.json add and remove should write.
// A project path is refused when the project is not trusted.
func ConfigPath(agentDir, cwd string, local, trusted bool) (string, error) {
	if local {
		if !trusted {
			return "", errors.New("project is not trusted; not writing .pigo/mcp.json")
		}
		return ProjectPath(cwd), nil
	}
	if agentDir == "" {
		return "", errors.New("agent directory is empty")
	}
	return UserPath(agentDir), nil
}

// Upsert adds or replaces one server. Unknown fields already in the entry,
// including auth and oauth keys this call does not set, are kept.
func Upsert(path string, spec UpsertSpec) error {
	if strings.TrimSpace(spec.Name) == "" {
		return errors.New("mcp server name is empty")
	}
	if spec.Command != "" && spec.URL != "" {
		return errors.New("command and url are both set")
	}
	if spec.Command == "" && spec.URL == "" {
		return errors.New("mcp server needs command or url")
	}
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	obj := doc.servers[spec.Name]
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}
	if spec.URL != "" {
		putJSON(obj, "url", spec.URL)
		delete(obj, "command")
		delete(obj, "args")
		delete(obj, "env")
		delete(obj, "cwd")
	} else {
		putJSON(obj, "command", spec.Command)
		if len(spec.Args) > 0 {
			putJSON(obj, "args", spec.Args)
		} else {
			delete(obj, "args")
		}
		delete(obj, "url")
		delete(obj, "headers")
	}
	if spec.Description != "" {
		putJSON(obj, "description", spec.Description)
	}
	if spec.Exposure != "" {
		putJSON(obj, "exposure", spec.Exposure)
	}
	if spec.Env != nil {
		putJSON(obj, "env", spec.Env)
	}
	if spec.Headers != nil {
		putJSON(obj, "headers", spec.Headers)
	}
	if spec.BearerTokenEnvVar != "" {
		putJSON(obj, "bearerTokenEnvVar", spec.BearerTokenEnvVar)
	}
	if err := mergeOAuth(obj, spec); err != nil {
		return err
	}
	putJSON(obj, "enabled", true)
	if doc.order == nil || !doc.has(spec.Name) {
		doc.order = append(doc.order, spec.Name)
	}
	doc.servers[spec.Name] = obj
	return writeDoc(path, doc)
}

// Remove deletes one server entry. Other entries and top-level keys stay.
func Remove(path, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("mcp server name is empty")
	}
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	if !doc.has(name) {
		return fmt.Errorf("mcp server %q not found", name)
	}
	delete(doc.servers, name)
	kept := make([]string, 0, len(doc.order))
	for _, n := range doc.order {
		if n != name {
			kept = append(kept, n)
		}
	}
	doc.order = kept
	return writeDoc(path, doc)
}

// SetEnabled sets enabled on one existing entry and leaves every other field.
func SetEnabled(path, name string, enabled bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("mcp server name is empty")
	}
	doc, err := readDoc(path)
	if err != nil {
		return err
	}
	obj, ok := doc.servers[name]
	if !ok {
		return fmt.Errorf("mcp server %q not found", name)
	}
	putJSON(obj, "enabled", enabled)
	doc.servers[name] = obj
	return writeDoc(path, doc)
}

func mergeOAuth(obj map[string]json.RawMessage, spec UpsertSpec) error {
	if spec.OAuthClientID == "" && spec.OAuthClientSecret == "" && spec.OAuthClientName == "" && spec.OAuthCallbackPort == 0 {
		return nil
	}
	oauth := map[string]json.RawMessage{}
	if raw, ok := obj["oauth"]; ok {
		if err := json.Unmarshal(raw, &oauth); err != nil {
			return errors.New("oauth must be an object")
		}
	}
	if spec.OAuthClientID != "" {
		putJSON(oauth, "clientId", spec.OAuthClientID)
	}
	if spec.OAuthClientSecret != "" {
		putJSON(oauth, "clientSecret", spec.OAuthClientSecret)
	}
	if spec.OAuthClientName != "" {
		putJSON(oauth, "clientName", spec.OAuthClientName)
	}
	if spec.OAuthCallbackPort != 0 {
		putJSON(oauth, "callbackPort", spec.OAuthCallbackPort)
	}
	putJSON(obj, "oauth", oauth)
	return nil
}

type docFile struct {
	extra   map[string]json.RawMessage
	servers map[string]map[string]json.RawMessage
	order   []string
}

func (d docFile) has(name string) bool {
	_, ok := d.servers[name]
	return ok
}

func readDoc(path string) (docFile, error) {
	out := docFile{
		extra:   map[string]json.RawMessage{},
		servers: map[string]map[string]json.RawMessage{},
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return docFile{}, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return docFile{}, errors.New("invalid JSON")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return docFile{}, errors.New("invalid JSON")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return docFile{}, errors.New("invalid JSON")
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return docFile{}, errors.New("invalid JSON")
		}
		if key != "mcpServers" {
			out.extra[key] = raw
			continue
		}
		if err := decodeServers(raw, &out); err != nil {
			return docFile{}, err
		}
	}
	return out, nil
}

func decodeServers(raw json.RawMessage, doc *docFile) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return errors.New("mcpServers must be an object")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return errors.New("mcpServers must be an object")
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return errors.New("mcpServers must be an object")
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("server %q: invalid JSON", key)
		}
		obj := map[string]json.RawMessage{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			return fmt.Errorf("server %q: must be an object", key)
		}
		doc.servers[key] = obj
		doc.order = append(doc.order, key)
	}
	return nil
}

func writeDoc(path string, doc docFile) error {
	servers := map[string]json.RawMessage{}
	var order []string
	if len(doc.order) == 0 {
		for name := range doc.servers {
			order = append(order, name)
		}
	} else {
		order = doc.order
	}
	for _, name := range order {
		obj := doc.servers[name]
		if obj == nil {
			continue
		}
		raw, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		servers[name] = raw
	}
	body := map[string]any{}
	for key, raw := range doc.extra {
		body[key] = raw
	}
	body["mcpServers"] = servers
	b, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func putJSON(obj map[string]json.RawMessage, key string, val any) {
	if m, ok := val.(map[string]json.RawMessage); ok {
		raw, err := json.Marshal(m)
		if err != nil {
			return
		}
		obj[key] = raw
		return
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return
	}
	obj[key] = raw
}
