package theme

import (
	"embed"
	"strings"
)

//go:embed dark.json light.json
var builtinThemeFiles embed.FS

func init() {
	for _, name := range []string{"dark", "light"} {
		raw, err := builtinThemeFiles.ReadFile(name + ".json")
		if err != nil {
			panic(err)
		}
		t, problems := parseThemeFile(raw)
		if len(problems) > 0 {
			panic(name + ": " + strings.Join(problems, "; "))
		}
		builtins = append(builtins, t)
	}
}
