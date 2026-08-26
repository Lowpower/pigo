package sqlite

import _ "embed"

//go:embed migrations/001_initial.sql
var initialSQL string
