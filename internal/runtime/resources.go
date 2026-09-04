package runtime

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Lowpower/pigo/internal/pkgmgr"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/skills"
)

func resolvePackageResources(ctx context.Context, opts Options) []pkgmgr.Resource {
	m, err := pkgmgr.Open(opts.Cwd, opts.AgentDir, opts.ProjectTrusted)
	if err != nil {
		return nil
	}
	rs, err := m.Resolve(ctx)
	if err != nil {
		return nil
	}
	return rs
}

func collectExtensionSpecs(opts Options, rs []pkgmgr.Resource) []string {
	var specs []string
	if !opts.NoExtensions {
		for _, argv := range pkgmgr.SpawnArgv(rs) {
			specs = append(specs, strings.Join(argv, " "))
		}
	}
	specs = append(specs, opts.CLIExtensions...)
	return specs
}

func (e *Engine) applyResolved(rs []pkgmgr.Resource) {
	e.Skills = loadSkills(e.Opts, rs)
	e.Templates = loadTemplates(e.Opts, rs)
	e.ThemeFiles = loadThemeFiles(e.Opts, rs)
}

func loadSkills(opts Options, rs []pkgmgr.Resource) []skills.Skill {
	paths := append([]string{}, opts.SkillPaths...)
	if !opts.NoSkills {
		paths = append(paths, enabledPaths(rs, pkgmgr.KindSkills)...)
	}
	sk, _ := skills.Discover("", "", paths, false, false)
	return sk
}

func loadTemplates(opts Options, rs []pkgmgr.Resource) []prompt.Template {
	paths := append([]string{}, opts.PromptPaths...)
	if !opts.NoPromptTpls {
		paths = append(paths, enabledPaths(rs, pkgmgr.KindPrompts)...)
	}
	return prompt.DiscoverTemplates("", "", paths, false, false)
}

func loadThemeFiles(opts Options, rs []pkgmgr.Resource) []string {
	if opts.NoThemes {
		return nil
	}
	return enabledPaths(rs, pkgmgr.KindThemes)
}

func enabledPaths(rs []pkgmgr.Resource, kind string) []string {
	var hits []pkgmgr.Resource
	for _, r := range rs {
		if r.Enabled && r.Type == kind {
			hits = append(hits, r)
		}
	}
	slices.SortStableFunc(hits, func(a, b pkgmgr.Resource) int {
		return resourceLoadRank(a) - resourceLoadRank(b)
	})
	seen := map[string]bool{}
	var out []string
	for _, r := range hits {
		key := r.Path
		if abs, err := filepath.Abs(r.Path); err == nil {
			key = abs
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r.Path)
	}
	return out
}

func resourceLoadRank(r pkgmgr.Resource) int {
	if r.Origin == "package" {
		return 4
	}
	rank := 2
	if r.Scope == "project" {
		rank = 0
	}
	if r.Source != "local" {
		rank++
	}
	return rank
}
