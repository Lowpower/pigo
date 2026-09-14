package ext

import (
	"fmt"
	"strings"
)

// ToolSchemaError is returned when register_tool has no object JSON Schema.
type ToolSchemaError struct {
	Name string
}

func (e ToolSchemaError) Error() string {
	if e.Name == "" {
		return "tool must define an object parameter schema"
	}
	return fmt.Sprintf("tool %q must define an object parameter schema", e.Name)
}

// ObjectParameterSchema reports whether schema is a JSON Schema object type.
func ObjectParameterSchema(schema map[string]any) bool {
	if schema == nil {
		return false
	}
	t, _ := schema["type"].(string)
	return strings.EqualFold(strings.TrimSpace(t), "object")
}

func checkToolSchema(name string, schema map[string]any) error {
	if ObjectParameterSchema(schema) {
		return nil
	}
	return ToolSchemaError{Name: name}
}
