package session

import "embed"

//go:embed html/template.html html/template.css html/template.js html/vendor/marked.min.js html/vendor/highlight.min.js html/themes/dark.json html/themes/light.json
var htmlFS embed.FS
