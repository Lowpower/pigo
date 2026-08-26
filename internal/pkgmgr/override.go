package pkgmgr

import (
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"

	"github.com/Lowpower/pigo/internal/config"
)

// Resource kinds stored in settings.json.
const (
	KindExtensions = "extensions"
	KindSkills     = "skills"
	KindPrompts    = "prompts"
	KindThemes     = "themes"
)

// OverrideState is the project-local cycle: inherit → load (+) → unload (−).
type OverrideState string

// Project override states for `pigo config -l`.
const (
	OverrideInherit OverrideState = "inherit"
	OverrideLoad    OverrideState = "load"
	OverrideUnload  OverrideState = "unload"
)

func toPosix(p string) string {
	return filepath.ToSlash(p)
}

func relPosix(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return toPosix(target)
	}
	return toPosix(rel)
}

func stripOverridePrefix(pattern string) string {
	if pattern == "" {
		return pattern
	}
	switch pattern[0] {
	case '!', '+', '-':
		return pattern[1:]
	default:
		return pattern
	}
}

func isOverridePattern(pattern string) bool {
	return strings.HasPrefix(pattern, "!") || strings.HasPrefix(pattern, "+") || strings.HasPrefix(pattern, "-")
}

func normalizeExactPattern(pattern string) string {
	p := toPosix(pattern)
	p = strings.TrimPrefix(p, "./")
	return p
}

func matchesAnyExactPattern(filePath string, patterns []string, baseDir string) bool {
	if len(patterns) == 0 {
		return false
	}
	rel := relPosix(baseDir, filePath)
	name := filepath.Base(filePath)
	filePosix := toPosix(filePath)
	isSkill := name == "SKILL.md"
	var parentRel, parentPosix string
	if isSkill {
		parent := filepath.Dir(filePath)
		parentRel = relPosix(baseDir, parent)
		parentPosix = toPosix(parent)
	}
	for _, pattern := range patterns {
		n := normalizeExactPattern(pattern)
		if n == rel || n == filePosix {
			return true
		}
		if isSkill && (n == parentRel || n == parentPosix) {
			return true
		}
	}
	return false
}

type globSeg struct {
	doubleStar bool
	g          glob.Glob
}

type pathGlob struct {
	segs []globSeg
}

func compilePathGlob(pattern string) (*pathGlob, error) {
	m := &pathGlob{}
	for _, part := range strings.Split(toPosix(pattern), "/") {
		if part == "**" {
			m.segs = append(m.segs, globSeg{doubleStar: true})
			continue
		}
		g, err := glob.Compile(part)
		if err != nil {
			return nil, err
		}
		m.segs = append(m.segs, globSeg{g: g})
	}
	return m, nil
}

func matchGlobSegs(segs []globSeg, parts []string) bool {
	if len(segs) == 0 {
		return len(parts) == 0
	}
	seg := segs[0]
	if seg.doubleStar {
		for i := 0; i <= len(parts); i++ {
			if matchGlobSegs(segs[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 || !seg.g.Match(parts[0]) {
		return false
	}
	return matchGlobSegs(segs[1:], parts[1:])
}

func globMatch(pattern, name string) bool {
	m, err := compilePathGlob(pattern)
	if err != nil {
		return false
	}
	return matchGlobSegs(m.segs, strings.Split(toPosix(name), "/"))
}

func matchesAnyPattern(filePath string, patterns []string, baseDir string) bool {
	rel := relPosix(baseDir, filePath)
	name := filepath.Base(filePath)
	filePosix := toPosix(filePath)
	isSkill := name == "SKILL.md"
	var parentRel, parentName, parentPosix string
	if isSkill {
		parent := filepath.Dir(filePath)
		parentRel = relPosix(baseDir, parent)
		parentName = filepath.Base(parent)
		parentPosix = toPosix(parent)
	}
	for _, pattern := range patterns {
		n := toPosix(pattern)
		if globMatch(n, rel) || globMatch(n, name) || globMatch(n, filePosix) {
			return true
		}
		if isSkill && (globMatch(n, parentRel) || globMatch(n, parentName) || globMatch(n, parentPosix)) {
			return true
		}
	}
	return false
}

func overridePatterns(entries []string) []string {
	var out []string
	for _, p := range entries {
		if isOverridePattern(p) {
			out = append(out, p)
		}
	}
	return out
}

// IsEnabledByOverrides applies !glob, +exact, and -exact in that order.
func IsEnabledByOverrides(filePath string, patterns []string, baseDir string) bool {
	overrides := overridePatterns(patterns)
	var excludes, forceIncludes, forceExcludes []string
	for _, p := range overrides {
		switch p[0] {
		case '!':
			excludes = append(excludes, p[1:])
		case '+':
			forceIncludes = append(forceIncludes, p[1:])
		case '-':
			forceExcludes = append(forceExcludes, p[1:])
		}
	}
	enabled := len(excludes) == 0 || !matchesAnyPattern(filePath, excludes, baseDir)
	if len(forceIncludes) > 0 && matchesAnyExactPattern(filePath, forceIncludes, baseDir) {
		enabled = true
	}
	if len(forceExcludes) > 0 && matchesAnyExactPattern(filePath, forceExcludes, baseDir) {
		enabled = false
	}
	return enabled
}

func applyPatterns(allPaths []string, patterns []string, baseDir string) map[string]bool {
	var includes, excludes, forceIncludes, forceExcludes []string
	for _, p := range patterns {
		switch {
		case strings.HasPrefix(p, "+"):
			forceIncludes = append(forceIncludes, p[1:])
		case strings.HasPrefix(p, "-"):
			forceExcludes = append(forceExcludes, p[1:])
		case strings.HasPrefix(p, "!"):
			excludes = append(excludes, p[1:])
		default:
			if p != "" {
				includes = append(includes, p)
			}
		}
	}
	var result []string
	if len(includes) == 0 {
		result = append(result, allPaths...)
	} else {
		for _, f := range allPaths {
			if matchesAnyPattern(f, includes, baseDir) {
				result = append(result, f)
			}
		}
	}
	if len(excludes) > 0 {
		var kept []string
		for _, f := range result {
			if !matchesAnyPattern(f, excludes, baseDir) {
				kept = append(kept, f)
			}
		}
		result = kept
	}
	if len(forceIncludes) > 0 {
		seen := map[string]bool{}
		for _, f := range result {
			seen[f] = true
		}
		for _, f := range allPaths {
			if !seen[f] && matchesAnyExactPattern(f, forceIncludes, baseDir) {
				result = append(result, f)
			}
		}
	}
	if len(forceExcludes) > 0 {
		var kept []string
		for _, f := range result {
			if !matchesAnyExactPattern(f, forceExcludes, baseDir) {
				kept = append(kept, f)
			}
		}
		result = kept
	}
	out := make(map[string]bool, len(allPaths))
	enabled := map[string]bool{}
	for _, f := range result {
		enabled[f] = true
	}
	for _, f := range allPaths {
		out[f] = enabled[f]
	}
	return out
}

func applyAutoloadDisabled(allPaths []string, patterns []string, baseDir string) map[string]bool {
	out := map[string]bool{}
	for _, pattern := range patterns {
		target := stripOverridePrefix(pattern)
		on := !strings.HasPrefix(pattern, "-") && !strings.HasPrefix(pattern, "!")
		exact := strings.HasPrefix(pattern, "+") || strings.HasPrefix(pattern, "-")
		for _, filePath := range allPaths {
			ok := false
			if exact {
				ok = matchesAnyExactPattern(filePath, []string{target}, baseDir)
			} else {
				ok = matchesAnyPattern(filePath, []string{target}, baseDir)
			}
			if ok {
				out[filePath] = on
			}
		}
	}
	return out
}

func replaceExactOverride(entries []string, pattern string, next string) []string {
	var out []string
	for _, p := range entries {
		stripped := stripOverridePrefix(p)
		if stripped == pattern && isOverridePattern(p) {
			continue
		}
		if stripped == pattern && next == "" {
			continue
		}
		out = append(out, p)
	}
	if next != "" {
		out = append(out, next)
	}
	return out
}

func nextOverrideState(cur OverrideState, inheritedEnabled bool) OverrideState {
	if cur == OverrideInherit {
		if inheritedEnabled {
			return OverrideUnload
		}
		return OverrideLoad
	}
	if cur == OverrideUnload {
		if inheritedEnabled {
			return OverrideLoad
		}
		return OverrideInherit
	}
	if inheritedEnabled {
		return OverrideInherit
	}
	return OverrideUnload
}

func resourcePattern(r Resource) string {
	base := r.BaseDir
	if base == "" {
		base = filepath.Dir(r.Path)
	}
	return relPosix(base, r.Path)
}

func packageResourcePattern(r Resource) string {
	base := r.BaseDir
	if base == "" {
		base = filepath.Dir(r.Path)
	}
	return relPosix(base, r.Path)
}

func (m *Manager) topLevelBase(scope string) string {
	if scope == "project" {
		return config.ProjectDir(m.Cwd)
	}
	return m.AgentDir
}

func (m *Manager) findPackageIndex(pkgs []config.PackageEntry, source, itemScope, targetScope string) int {
	for i, p := range pkgs {
		if packageSourceMatches(source, itemScope, p.Source, targetScope, m) {
			return i
		}
	}
	return -1
}

func packageSourceMatches(leftSource, leftScope, rightSource, rightScope string, m *Manager) bool {
	if leftSource == rightSource {
		return true
	}
	left, err1 := ParseSource(leftSource)
	right, err2 := ParseSource(rightSource)
	if err1 != nil || err2 != nil || left.Kind != KindLocal || right.Kind != KindLocal {
		return false
	}
	resolve := func(src Source, scope string) string {
		p := src.Path
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Clean(filepath.Join(m.topLevelBase(scope), p))
	}
	return resolve(left, leftScope) == resolve(right, rightScope)
}

func overrideStateFromEntries(entries []string, patterns map[string]bool, emptyIsUnload bool) OverrideState {
	if len(entries) == 0 && emptyIsUnload {
		return OverrideUnload
	}
	state := OverrideInherit
	for _, entry := range entries {
		target := stripOverridePrefix(entry)
		if !patterns[target] {
			continue
		}
		if strings.HasPrefix(entry, "!") || strings.HasPrefix(entry, "-") {
			state = OverrideUnload
		} else {
			state = OverrideLoad
		}
	}
	return state
}

func (m *Manager) topLevelOverridePatterns(r Resource, scope string) map[string]bool {
	base := m.topLevelBase(scope)
	out := map[string]bool{
		resourcePatternForScope(r, scope, m): true,
		r.Path:                               true,
		toPosix(r.Path):                      true,
		relPosix(base, r.Path):               true,
	}
	if r.BaseDir != "" {
		out[relPosix(r.BaseDir, r.Path)] = true
	}
	return out
}

func resourcePatternForScope(r Resource, scope string, m *Manager) string {
	if scope != r.Scope {
		return r.Path
	}
	base := r.BaseDir
	if base == "" {
		base = m.topLevelBase(r.Scope)
	}
	return relPosix(base, r.Path)
}

// ProjectOverrideState is inherit / load / unload for one resource in project settings.
func (m *Manager) ProjectOverrideState(r Resource) OverrideState {
	if r.Origin == "top-level" {
		return overrideStateFromEntries(m.Project.ResourcePaths(r.Type), m.topLevelOverridePatterns(r, "project"), false)
	}
	idx := m.findPackageIndex(m.Project.Packages, r.Source, r.Scope, "project")
	if idx < 0 {
		return OverrideInherit
	}
	pkg := m.Project.Packages[idx]
	entries := pkg.ResourceFilters(r.Type)
	if entries == nil && !pkg.Filtered() {
		return OverrideInherit
	}
	return overrideStateFromEntries(entries, map[string]bool{packageResourcePattern(r): true}, pkg.Autoload != nil && !*pkg.Autoload)
}

func boolFalse() *bool {
	v := false
	return &v
}

// ToggleResource applies Space: global enable/disable, or project inherit/load/unload.
func (m *Manager) ToggleResource(r Resource, writeProject bool, inheritedEnabled bool) error {
	if writeProject {
		if err := m.assertProject(true); err != nil {
			return err
		}
		next := nextOverrideState(m.ProjectOverrideState(r), inheritedEnabled)
		if r.Origin == "top-level" {
			m.setProjectTopLevelOverride(r, next)
		} else {
			m.setProjectPackageOverride(r, next)
		}
		return m.persist(true)
	}
	enabled := !r.Enabled
	if r.Origin == "top-level" {
		m.toggleTopLevel(r, enabled)
	} else {
		m.togglePackage(r, enabled)
	}
	local := r.Scope == "project"
	if local {
		if err := m.assertProject(true); err != nil {
			return err
		}
	}
	return m.persist(local)
}

func (m *Manager) toggleTopLevel(r Resource, enabled bool) {
	s := m.settings(r.Scope == "project")
	pattern := resourcePattern(r)
	next := "-" + pattern
	if enabled {
		next = "+" + pattern
	}
	s.SetResourcePaths(r.Type, replaceExactOverride(s.ResourcePaths(r.Type), pattern, next))
}

func (m *Manager) togglePackage(r Resource, enabled bool) {
	local := r.Scope == "project"
	s := m.settings(local)
	idx := m.findPackageIndex(s.Packages, r.Source, r.Scope, r.Scope)
	if idx < 0 {
		return
	}
	pkg := s.Packages[idx]
	pattern := packageResourcePattern(r)
	next := "-" + pattern
	if enabled {
		next = "+" + pattern
	}
	updated := replaceExactOverride(pkg.ResourceFilters(r.Type), pattern, next)
	if len(updated) == 0 {
		updated = nil
	}
	pkg.SetResourceFilters(r.Type, updated)
	if !pkg.HasAnyFilters() && pkg.Autoload == nil {
		pkg.AsStringPackage()
	}
	s.Packages[idx] = pkg
}

func (m *Manager) setProjectTopLevelOverride(r Resource, state OverrideState) {
	current := append([]string{}, m.Project.ResourcePaths(r.Type)...)
	pattern := resourcePatternForScope(r, "project", m)
	if r.Scope == "user" {
		pattern = r.Path
	}
	patterns := m.topLevelOverridePatterns(r, "project")
	var updated []string
	for _, entry := range current {
		target := stripOverridePrefix(entry)
		if isOverridePattern(entry) && patterns[target] {
			continue
		}
		if state == OverrideInherit && r.Scope == "user" && target == pattern {
			continue
		}
		updated = append(updated, entry)
	}
	if state != OverrideInherit {
		if r.Scope == "user" {
			found := false
			for _, e := range updated {
				if e == pattern {
					found = true
					break
				}
			}
			if !found {
				updated = append(updated, pattern)
			}
		}
		prefix := "-"
		if state == OverrideLoad {
			prefix = "+"
		}
		updated = append(updated, prefix+pattern)
	}
	if updated == nil {
		updated = []string{}
	}
	m.Project.SetResourcePaths(r.Type, updated)
}

func (m *Manager) setProjectPackageOverride(r Resource, state OverrideState) {
	pkgs := append([]config.PackageEntry{}, m.Project.Packages...)
	idx := m.findPackageIndex(pkgs, r.Source, r.Scope, "project")
	if idx < 0 {
		if state == OverrideInherit {
			return
		}
		src, err := ParseSource(r.Source)
		entry := config.ObjectPackage(r.Source, boolFalse())
		if err == nil && src.Kind == KindLocal {
			rel := relPosix(m.topLevelBase("project"), src.Path)
			if rel == "" {
				rel = "."
			}
			if !filepath.IsAbs(src.Path) {
				abs := src.Path
				if !filepath.IsAbs(abs) {
					abs = filepath.Join(m.topLevelBase(r.Scope), src.Path)
				}
				rel = relPosix(m.topLevelBase("project"), abs)
			}
			entry = config.ObjectPackage(rel, boolFalse())
		}
		pkgs = append(pkgs, entry)
		idx = len(pkgs) - 1
	}
	pkg := pkgs[idx]
	pattern := packageResourcePattern(r)
	updated := replaceExactOverride(pkg.ResourceFilters(r.Type), pattern, "")
	if state != OverrideInherit {
		prefix := "-"
		if state == OverrideLoad {
			prefix = "+"
		}
		updated = append(updated, prefix+pattern)
	}
	pkg.SetResourceFilters(r.Type, updated)
	if !pkg.HasAnyFilters() {
		if pkg.AutoloadOff() {
			pkgs = append(pkgs[:idx], pkgs[idx+1:]...)
			m.Project.Packages = pkgs
			return
		}
		pkg.AsStringPackage()
	}
	pkgs[idx] = pkg
	m.Project.Packages = pkgs
}
