package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Scenario is one declarative eval case loaded from a JSON file.
type Scenario struct {
	Name         string            `json:"name"`
	Prompt       string            `json:"prompt"`
	NoTools      bool              `json:"noTools"`
	Tools        []string          `json:"tools"`
	ExcludeTools []string          `json:"excludeTools"`
	SystemPrompt string            `json:"systemPrompt"`
	Files        map[string]string `json:"files"`
	Expect       Expect            `json:"expect"`
	Path         string            `json:"-"`
}

// Expect is a deterministic string grader. All set fields must pass.
type Expect struct {
	Contains []string `json:"contains"`
	Equals   string   `json:"equals"`
	Regex    string   `json:"regex"`
}

// LoadDir reads *.json scenarios from dir, sorted by filename.
func LoadDir(dir string) ([]Scenario, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []Scenario
	for _, e := range ents {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, err := LoadFile(path)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// LoadFile reads one scenario JSON file.
func LoadFile(path string) (Scenario, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, err
	}
	var s Scenario
	if err := json.Unmarshal(b, &s); err != nil {
		return Scenario{}, fmt.Errorf("%s: %w", path, err)
	}
	if strings.TrimSpace(s.Name) == "" {
		s.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	s.Path = path
	return s, nil
}

func grade(output string, exp Expect) error {
	if exp.Equals != "" && strings.TrimSpace(output) != exp.Equals {
		return fmt.Errorf("equals: got %q want %q", strings.TrimSpace(output), exp.Equals)
	}
	for _, sub := range exp.Contains {
		if !strings.Contains(output, sub) {
			return fmt.Errorf("contains %q: output %q", sub, output)
		}
	}
	if exp.Regex != "" {
		re, err := regexp.Compile(exp.Regex)
		if err != nil {
			return fmt.Errorf("regex: %w", err)
		}
		if !re.MatchString(strings.TrimSpace(output)) {
			return fmt.Errorf("regex %q: output %q", exp.Regex, output)
		}
	}
	return nil
}
