package tools

import "github.com/Lowpower/pigo/internal/ai"

var preferJSONSchema = map[string]bool{
	"read":       true,
	"bash":       true,
	"powershell": true,
	"edit":       true,
	"write":      true,
}

func preferConstrainedSampling(name string) *ai.ConstrainedSampling {
	if !preferJSONSchema[name] {
		return nil
	}
	return &ai.ConstrainedSampling{Type: "json_schema", Strict: "prefer"}
}
