package tools

import (
	"strings"

	"github.com/gobwas/glob"
)

// globMatcher matches slash-separated paths against a fast-glob-style pattern,
// including `**` (globstar: matches zero or more path segments). gobwas/glob has
// no globstar, so segments are matched individually (a non-`**` segment matches a
// single path component; `*` within a segment does not cross `/`).
//
// `*.go` matches only top-level files, while `**/*.go` matches at any depth.
type globMatcher struct {
	segs []globSeg
}

type globSeg struct {
	doubleStar bool
	g          glob.Glob
}

func compileGlob(pattern string) (*globMatcher, error) {
	m := &globMatcher{}
	for _, part := range strings.Split(pattern, "/") {
		if part == "**" {
			m.segs = append(m.segs, globSeg{doubleStar: true})
			continue
		}
		g, err := glob.Compile(part) // no separators: matches a single segment
		if err != nil {
			return nil, err
		}
		m.segs = append(m.segs, globSeg{g: g})
	}
	return m, nil
}

// Match reports whether the slash-separated path matches the pattern.
func (m *globMatcher) Match(path string) bool {
	return matchSegments(m.segs, strings.Split(path, "/"))
}

func matchSegments(segs []globSeg, parts []string) bool {
	if len(segs) == 0 {
		return len(parts) == 0
	}
	seg := segs[0]
	if seg.doubleStar {
		for i := 0; i <= len(parts); i++ {
			if matchSegments(segs[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 || !seg.g.Match(parts[0]) {
		return false
	}
	return matchSegments(segs[1:], parts[1:])
}
