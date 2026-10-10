// Package bom strips a leading UTF-8 byte order mark from files editors
// such as Windows Notepad write by default.
package bom

import "bytes"

// Strip removes one leading UTF-8 BOM (U+FEFF, bytes EF BB BF).
// The input is returned unchanged when that prefix is absent.
func Strip(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte("\ufeff"))
}
