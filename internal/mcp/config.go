// Package mcp loads MCP server config, connects lazily, and names tools.
package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/bom"
	"github.com/Lowpower/pigo/internal/config"
)

const (
	// FileName is the user and project MCP config file.
	FileName = "mcp.json"
	// DefaultExposure is used when a server omits exposure.
	DefaultExposure = "codemode"
	defaultTimeout  = 60 * time.Second
)

// Source is where a server entry came from.
type Source string

const (
	// SourceUser is the user-level mcp.json.
	SourceUser Source = "user"
	// SourceProject is the trusted project mcp.json.
	SourceProject Source = "project"
	// SourceExtension is a server registered by an extension.
	SourceExtension Source = "extension"
)

// ExposureRule is one toolExposure entry, in file order.
type ExposureRule struct {
	Pattern  string
	Exposure string
}

// Config is one MCP server. Raw is the original JSON object, including
// fields this package does not interpret (auth, oauth).
type Config struct {
	Command        string
	Args           []string
	Env            map[string]string
	Cwd            string
	URL            string
	Headers        map[string]string
	TimeoutSeconds int
	Enabled        bool
	Exposure       string
	ToolExposure   []ExposureRule
	Description    string
	Raw            json.RawMessage
}

// Timeout is the connect and call deadline. Zero means 60 seconds.
func (c Config) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return defaultTimeout
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

// ExposureValue is the server exposure, defaulting to codemode.
func (c Config) ExposureValue() string {
	if strings.TrimSpace(c.Exposure) == "" {
		return DefaultExposure
	}
	return normalizeExposure(c.Exposure)
}

// ExposureFor resolves one tool. Exact toolExposure names win over patterns.
// Among patterns, the first match wins.
func (c Config) ExposureFor(tool string) string {
	for _, rule := range c.ToolExposure {
		if !strings.Contains(rule.Pattern, "*") && rule.Pattern == tool {
			return normalizeExposure(rule.Exposure)
		}
	}
	for _, rule := range c.ToolExposure {
		if strings.Contains(rule.Pattern, "*") && matchWild(rule.Pattern, tool) {
			return normalizeExposure(rule.Exposure)
		}
	}
	return c.ExposureValue()
}

// BlocksFirstPrompt reports whether any configured exposure is direct.
func (c Config) BlocksFirstPrompt() bool {
	if c.ExposureValue() == "direct" {
		return true
	}
	for _, rule := range c.ToolExposure {
		if normalizeExposure(rule.Exposure) == "direct" {
			return true
		}
	}
	return false
}

// Server is one configured MCP server after user, project, and extension merge.
type Server struct {
	Name   string
	Config Config
	Source Source
	File   string
	keys   map[string]bool
}

// Diagnostic is a config problem that names the file and server when known.
type Diagnostic struct {
	File    string
	Server  string
	Message string
}

func (d Diagnostic) Error() string {
	switch {
	case d.File != "" && d.Server != "":
		return fmt.Sprintf("%s: server %q: %s", d.File, d.Server, d.Message)
	case d.File != "":
		return d.File + ": " + d.Message
	case d.Server != "":
		return fmt.Sprintf("server %q: %s", d.Server, d.Message)
	default:
		return d.Message
	}
}

// UserPath is <agentDir>/mcp.json.
func UserPath(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	return filepath.Join(agentDir, FileName)
}

// ProjectPath is <cwd>/.pigo/mcp.json.
func ProjectPath(cwd string) string {
	if cwd == "" {
		return ""
	}
	return filepath.Join(config.ProjectDir(cwd), FileName)
}

// Load reads the user file and, when trusted, the project file.
// A missing file is not an error. Invalid entries are returned as diagnostics
// and left out of the server list.
func Load(agentDir, cwd string, trusted bool) ([]Server, []Diagnostic) {
	user, userDiags := readFile(UserPath(agentDir), SourceUser)
	var project []Server
	var projectDiags []Diagnostic
	if trusted {
		project, projectDiags = readFile(ProjectPath(cwd), SourceProject)
	}
	merged, mergeDiags := mergeProject(user, project)
	merged, collDiags := dropCollisions(merged)
	diags := append(userDiags, projectDiags...)
	diags = append(diags, mergeDiags...)
	diags = append(diags, collDiags...)
	return merged, diags
}

// ServerFromMap parses one extension registration. The map matches one
// mcpServers entry.
func ServerFromMap(name string, args map[string]any, source Source) (Server, error) {
	if strings.TrimSpace(name) == "" {
		return Server{}, errors.New("mcp server name is empty")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return Server{}, err
	}
	entry, err := parseEntry(raw)
	if err != nil {
		return Server{}, err
	}
	if !entry.keys["command"] && !entry.keys["url"] {
		return Server{}, errors.New("mcp server needs command or url")
	}
	cfg := finalize(entry)
	if cfg.Command != "" && cfg.URL != "" {
		return Server{}, errors.New("command and url are both set")
	}
	if cfg.Command == "" && cfg.URL == "" {
		return Server{}, errors.New("mcp server needs command or url")
	}
	return Server{Name: name, Config: cfg, Source: source}, nil
}

// MergeExtensions appends extension servers whose names are not already
// configured. A file entry wins. Servers whose names differ only by '-' and
// '_' are removed.
func MergeExtensions(servers, regs []Server) ([]Server, []Diagnostic) {
	have := map[string]bool{}
	for _, s := range servers {
		have[s.Name] = true
	}
	for _, reg := range regs {
		if have[reg.Name] {
			continue
		}
		servers = append(servers, reg)
		have[reg.Name] = true
	}
	return dropCollisions(servers)
}

func mergeProject(user, project []Server) ([]Server, []Diagnostic) {
	byName := map[string]int{}
	for i, s := range user {
		byName[s.Name] = i
	}
	var diags []Diagnostic
	for _, p := range project {
		idx, ok := byName[p.Name]
		partial := p.Config.Command == "" && p.Config.URL == ""
		if partial {
			if !ok {
				diags = append(diags, Diagnostic{
					File: p.File, Server: p.Name, Message: "no user server to override",
				})
				continue
			}
			user[idx].Config = applyOverride(user[idx].Config, p.Config, p.keys)
			user[idx].Source = SourceProject
			user[idx].File = p.File
			continue
		}
		if ok {
			user[idx] = p
			continue
		}
		user = append(user, p)
		byName[p.Name] = len(user) - 1
	}
	return user, diags
}

func applyOverride(base, over Config, keys map[string]bool) Config {
	out := base
	if keys["enabled"] {
		out.Enabled = over.Enabled
	}
	if keys["exposure"] {
		out.Exposure = over.Exposure
	}
	if keys["toolExposure"] {
		out.ToolExposure = over.ToolExposure
	}
	return out
}

func dropCollisions(servers []Server) ([]Server, []Diagnostic) {
	groups := map[string][]int{}
	var order []string
	for i, s := range servers {
		key := Canon(s.Name)
		if _, ok := groups[key]; !ok {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}
	drop := map[int]bool{}
	var diags []Diagnostic
	for _, key := range order {
		idx := groups[key]
		if len(idx) < 2 {
			continue
		}
		names := make([]string, len(idx))
		for j, i := range idx {
			names[j] = servers[i].Name
			drop[i] = true
		}
		diags = append(diags, Diagnostic{
			Message: "servers " + quoteList(names) + " differ only by '-' and '_'",
		})
	}
	kept := make([]Server, 0, len(servers))
	for i, s := range servers {
		if !drop[i] {
			kept = append(kept, s)
		}
	}
	return kept, diags
}

func quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = strconv.Quote(name)
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	default:
		return strings.Join(quoted[:len(quoted)-1], ", ") + ", and " + quoted[len(quoted)-1]
	}
}

type parsedEntry struct {
	cfg  Config
	keys map[string]bool
}

func readFile(path string, source Source) ([]Server, []Diagnostic) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, []Diagnostic{{File: path, Message: err.Error()}}
	}
	entries, err := parseServers(bom.Strip(b))
	if err != nil {
		return nil, []Diagnostic{{File: path, Message: err.Error()}}
	}
	var servers []Server
	var diags []Diagnostic
	seen := map[string]int{}
	for _, item := range entries {
		entry, err := parseEntry(item.raw)
		if err != nil {
			diags = append(diags, Diagnostic{File: path, Server: item.name, Message: err.Error()})
			continue
		}
		partial := !entry.keys["command"] && !entry.keys["url"]
		if partial {
			if extra := disallowedOverride(entry.keys); extra != "" {
				diags = append(diags, Diagnostic{
					File: path, Server: item.name,
					Message: "entry without command or url may only set enabled, exposure, and toolExposure",
				})
				continue
			}
		}
		cfg := finalize(entry)
		if !partial && cfg.Command != "" && cfg.URL != "" {
			diags = append(diags, Diagnostic{File: path, Server: item.name, Message: "command and url are both set"})
			continue
		}
		if !partial && cfg.Command == "" && cfg.URL == "" {
			diags = append(diags, Diagnostic{File: path, Server: item.name, Message: "command or url is required"})
			continue
		}
		srv := Server{Name: item.name, Config: cfg, Source: source, File: path, keys: entry.keys}
		if i, ok := seen[item.name]; ok {
			servers[i] = srv
			continue
		}
		seen[item.name] = len(servers)
		servers = append(servers, srv)
	}
	return servers, diags
}

func disallowedOverride(keys map[string]bool) string {
	for key := range keys {
		switch key {
		case "enabled", "exposure", "toolExposure":
		default:
			return key
		}
	}
	return ""
}

func finalize(entry parsedEntry) Config {
	cfg := entry.cfg
	if !entry.keys["enabled"] {
		cfg.Enabled = true
	}
	return cfg
}

type rawServer struct {
	name string
	raw  json.RawMessage
}

func parseServers(data []byte) ([]rawServer, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, errors.New("invalid JSON")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("invalid JSON")
	}
	var serversRaw json.RawMessage
	found := false
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, errors.New("invalid JSON")
		}
		key, _ := keyTok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, errors.New("invalid JSON")
		}
		if key == "mcpServers" {
			serversRaw = raw
			found = true
		}
	}
	if !found {
		return nil, nil
	}
	dec = json.NewDecoder(bytes.NewReader(serversRaw))
	tok, err = dec.Token()
	if err != nil {
		return nil, errors.New("mcpServers must be an object")
	}
	delim, ok = tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("mcpServers must be an object")
	}
	var out []rawServer
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, errors.New("invalid JSON")
		}
		key, ok := keyTok.(string)
		if !ok || strings.TrimSpace(key) == "" {
			return nil, errors.New("mcp server name is empty")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, fmt.Errorf("server %q: invalid JSON", key)
		}
		out = append(out, rawServer{name: key, raw: raw})
	}
	return out, nil
}

func parseEntry(raw json.RawMessage) (parsedEntry, error) {
	fields, err := objectFields(raw)
	if err != nil {
		return parsedEntry{}, err
	}
	var cfg Config
	cfg.Raw = append(json.RawMessage(nil), raw...)
	keys := map[string]bool{}
	for _, field := range fields {
		keys[field.key] = true
		if err := applyField(&cfg, field.key, field.raw); err != nil {
			return parsedEntry{}, err
		}
	}
	return parsedEntry{cfg: cfg, keys: keys}, nil
}

type objectField struct {
	key string
	raw json.RawMessage
}

func objectFields(raw json.RawMessage) ([]objectField, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, errors.New("server entry must be an object")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("server entry must be an object")
	}
	var fields []objectField
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, errors.New("server entry must be an object")
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("server entry must be an object")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, fmt.Errorf("%s: invalid JSON", key)
		}
		fields = append(fields, objectField{key: key, raw: val})
	}
	return fields, nil
}

func applyField(cfg *Config, key string, raw json.RawMessage) error {
	switch key {
	case "command":
		return decodeString(raw, &cfg.Command)
	case "cwd":
		return decodeString(raw, &cfg.Cwd)
	case "url":
		return decodeString(raw, &cfg.URL)
	case "description":
		return decodeString(raw, &cfg.Description)
	case "exposure":
		return decodeString(raw, &cfg.Exposure)
	case "args":
		return decodeStringSlice(raw, &cfg.Args)
	case "env":
		return decodeStringMap(raw, &cfg.Env)
	case "headers":
		return decodeStringMap(raw, &cfg.Headers)
	case "timeout":
		var n int
		if err := json.Unmarshal(raw, &n); err != nil || n < 0 {
			return errors.New("timeout must be a non-negative number of seconds")
		}
		cfg.TimeoutSeconds = n
	case "enabled":
		var on bool
		if err := json.Unmarshal(raw, &on); err != nil {
			return errors.New("enabled must be a boolean")
		}
		cfg.Enabled = on
	case "toolExposure":
		rules, err := parseToolExposure(raw)
		if err != nil {
			return err
		}
		cfg.ToolExposure = rules
	default:
		// Kept on Config.Raw for a later auth change.
	}
	return nil
}

func parseToolExposure(raw json.RawMessage) ([]ExposureRule, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, errors.New("toolExposure must be an object")
	}
	delim, ok := tok.(json.Delim)
	if !ok || delim != '{' {
		return nil, errors.New("toolExposure must be an object")
	}
	var rules []ExposureRule
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, errors.New("toolExposure must be an object")
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, errors.New("toolExposure key must be a string")
		}
		var exposure string
		if err := dec.Decode(&exposure); err != nil {
			return nil, fmt.Errorf("toolExposure %q must be a string", key)
		}
		rules = append(rules, ExposureRule{Pattern: key, Exposure: exposure})
	}
	return rules, nil
}

func decodeString(raw json.RawMessage, dst *string) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return errors.New("expected a string")
	}
	return nil
}

func decodeStringSlice(raw json.RawMessage, dst *[]string) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return errors.New("expected an array of strings")
	}
	return nil
}

func decodeStringMap(raw json.RawMessage, dst *map[string]string) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return errors.New("expected an object of strings")
	}
	return nil
}

func normalizeExposure(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "direct":
		return "direct"
	case "model-only":
		return "model-only"
	case "codemode":
		return "codemode"
	case "deferred":
		return "deferred"
	case "hidden":
		return "hidden"
	default:
		return "direct"
	}
}

func matchWild(pattern, name string) bool {
	for {
		if pattern == "" {
			return name == ""
		}
		if pattern[0] == '*' {
			pattern = pattern[1:]
			if pattern == "" {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchWild(pattern, name[i:]) {
					return true
				}
			}
			return false
		}
		if name == "" || name[0] != pattern[0] {
			return false
		}
		pattern = pattern[1:]
		name = name[1:]
	}
}
