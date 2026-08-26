package pkgmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Lowpower/pigo/internal/config"
)

// Configured is one packages[] entry as shown by `pigo list`.
type Configured struct {
	Source        string
	Scope         string
	Filtered      bool
	InstalledPath string
}

// Manager installs and discovers packages for one cwd + agent dir.
type Manager struct {
	Cwd, AgentDir string
	Trusted       bool
	User          config.Config
	Project       config.Config
	AutoInstall   bool
	Run           func(ctx context.Context, name string, args []string, dir string) error
}

// Open loads user (and trusted project) settings.
func Open(cwd, agentDir string, trusted bool) (*Manager, error) {
	user, err := config.Load(agentDir)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		Cwd:         cwd,
		AgentDir:    agentDir,
		Trusted:     trusted,
		User:        user,
		AutoInstall: true,
		Run:         defaultRun,
	}
	if trusted {
		m.Project, _ = config.Load(config.ProjectDir(cwd))
	}
	return m, nil
}

func defaultRun(ctx context.Context, name string, args []string, dir string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *Manager) assertProject(local bool) error {
	if local && !m.Trusted {
		return fmt.Errorf("project is not trusted; use --approve to modify local package config")
	}
	return nil
}

func (m *Manager) npmRoot(local bool) string {
	if local {
		return filepath.Join(config.ProjectDir(m.Cwd), "npm")
	}
	return filepath.Join(m.AgentDir, "npm")
}

func (m *Manager) gitRoot(local bool) string {
	if local {
		return filepath.Join(config.ProjectDir(m.Cwd), "git")
	}
	return filepath.Join(m.AgentDir, "git")
}

func (m *Manager) npmPath(src Source, local bool) string {
	return filepath.Join(m.npmRoot(local), "node_modules", filepath.FromSlash(src.Name))
}

func (m *Manager) gitPath(src Source, local bool) string {
	return managedJoin(m.gitRoot(local), src.Host, filepath.FromSlash(src.RepoPath))
}

func managedJoin(root string, parts ...string) string {
	resolved := filepath.Join(append([]string{root}, parts...)...)
	rel, err := filepath.Rel(root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.Join(root, "unsafe")
	}
	return resolved
}

// InstalledPath returns the on-disk path for an installed source, or "".
func (m *Manager) InstalledPath(source string, local bool) string {
	src, err := ParseSource(source)
	if err != nil {
		return ""
	}
	var p string
	switch src.Kind {
	case KindNPM:
		p = m.npmPath(src, local)
	case KindGit:
		p = m.gitPath(src, local)
	case KindLocal:
		p = src.Path
		if !filepath.IsAbs(p) {
			base := m.AgentDir
			if local {
				base = config.ProjectDir(m.Cwd)
			}
			p = filepath.Join(base, p)
		}
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// ListConfigured returns user then project packages from settings.
func (m *Manager) ListConfigured() []Configured {
	var out []Configured
	for _, p := range m.User.Packages {
		out = append(out, Configured{
			Source: p.Source, Scope: "user", Filtered: p.Filtered(),
			InstalledPath: m.InstalledPath(p.Source, false),
		})
	}
	if m.Trusted {
		for _, p := range m.Project.Packages {
			out = append(out, Configured{
				Source: p.Source, Scope: "project", Filtered: p.Filtered(),
				InstalledPath: m.InstalledPath(p.Source, true),
			})
		}
	}
	return out
}

func (m *Manager) settings(local bool) *config.Config {
	if local {
		return &m.Project
	}
	return &m.User
}

func (m *Manager) persist(local bool) error {
	dir := m.AgentDir
	if local {
		dir = config.ProjectDir(m.Cwd)
	}
	onDisk, err := config.Load(dir)
	if err != nil {
		return err
	}
	s := m.settings(local)
	onDisk.Packages = s.Packages
	onDisk.Extensions = s.Extensions
	onDisk.Skills = s.Skills
	onDisk.Prompts = s.Prompts
	onDisk.Themes = s.Themes
	if s.NpmCommand != nil {
		onDisk.NpmCommand = s.NpmCommand
	}
	return config.Save(dir, onDisk)
}

func (m *Manager) addSource(source string, local bool) {
	s := m.settings(local)
	id := identityOf(source)
	for i, p := range s.Packages {
		if identityOf(p.Source) == id {
			s.Packages[i].Source = source
			return
		}
	}
	s.Packages = append(s.Packages, config.StringPackage(source))
}

func (m *Manager) removeSource(source string, local bool) bool {
	s := m.settings(local)
	id := identityOf(source)
	n := s.Packages[:0]
	removed := false
	for _, p := range s.Packages {
		if identityOf(p.Source) == id {
			removed = true
			continue
		}
		n = append(n, p)
	}
	s.Packages = n
	return removed
}

func identityOf(source string) string {
	src, err := ParseSource(source)
	if err != nil {
		return source
	}
	return src.Identity()
}

func (m *Manager) npmCommand() (string, []string) {
	cmd := m.User.NpmCommand
	if len(cmd) == 0 {
		return "npm", nil
	}
	return cmd[0], cmd[1:]
}

func (m *Manager) runNpm(ctx context.Context, extra []string, dir string) error {
	name, prefix := m.npmCommand()
	return m.Run(ctx, name, append(append([]string{}, prefix...), extra...), dir)
}

func (m *Manager) ensureNpmRoot(local bool) error {
	root := m.npmRoot(local)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	writeGitIgnore(root)
	pj := filepath.Join(root, "package.json")
	if _, err := os.Stat(pj); err == nil {
		return nil
	}
	body, _ := json.MarshalIndent(map[string]any{"name": "pigo-extensions", "private": true}, "", "  ")
	return os.WriteFile(pj, append(body, '\n'), 0o644)
}

func writeGitIgnore(dir string) {
	p := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(p); err == nil {
		return
	}
	_ = os.WriteFile(p, []byte("*\n!.gitignore\n"), 0o644)
}

func offline() bool {
	v := os.Getenv("PIGO_OFFLINE")
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// InstallAndPersist installs a source and records it in settings.
func (m *Manager) InstallAndPersist(ctx context.Context, source string, local bool) error {
	if err := m.assertProject(local); err != nil {
		return err
	}
	if err := m.install(ctx, source, local); err != nil {
		return err
	}
	m.addSource(source, local)
	return m.persist(local)
}

func (m *Manager) install(ctx context.Context, source string, local bool) error {
	src, err := ParseSource(source)
	if err != nil {
		return err
	}
	switch src.Kind {
	case KindNPM:
		if err := m.ensureNpmRoot(local); err != nil {
			return err
		}
		root := m.npmRoot(local)
		spec := src.Spec
		if spec == "" {
			spec = src.Name
		}
		return m.runNpm(ctx, []string{"install", spec, "--prefix", root, "--legacy-peer-deps"}, "")
	case KindGit:
		return m.installGit(ctx, src, local)
	case KindLocal:
		p := src.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(m.Cwd, p)
		}
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("path does not exist: %s", p)
		}
		return nil
	}
	return fmt.Errorf("unsupported install source: %s", source)
}

func (m *Manager) installGit(ctx context.Context, src Source, local bool) error {
	target := m.gitPath(src, local)
	root := m.gitRoot(local)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	writeGitIgnore(root)
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		if src.Ref != "" {
			_ = m.Run(ctx, "git", []string{"fetch", "origin", src.Ref}, target)
			return m.Run(ctx, "git", []string{"checkout", src.Ref}, target)
		}
		_ = m.Run(ctx, "git", []string{"fetch", "--all"}, target)
		return m.Run(ctx, "git", []string{"pull", "--ff-only"}, target)
	}
	if err := m.Run(ctx, "git", []string{"clone", src.Repo, target}, ""); err != nil {
		_ = os.RemoveAll(target)
		return err
	}
	if src.Ref != "" {
		if err := m.Run(ctx, "git", []string{"checkout", src.Ref}, target); err != nil {
			_ = os.RemoveAll(target)
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(target, "package.json")); err == nil {
		_ = m.runNpm(ctx, []string{"install", "--omit=dev"}, target)
	}
	return nil
}

// RemoveAndPersist uninstalls a source and drops it from settings.
func (m *Manager) RemoveAndPersist(ctx context.Context, source string, local bool) (bool, error) {
	if err := m.assertProject(local); err != nil {
		return false, err
	}
	src, err := ParseSource(source)
	if err != nil {
		return false, err
	}
	switch src.Kind {
	case KindNPM:
		root := m.npmRoot(local)
		if _, err := os.Stat(root); err == nil {
			_ = m.runNpm(ctx, []string{"uninstall", src.Name, "--prefix", root}, "")
		}
	case KindGit:
		_ = os.RemoveAll(m.gitPath(src, local))
	}
	removed := m.removeSource(source, local)
	if err := m.persist(local); err != nil {
		return removed, err
	}
	return removed, nil
}

// Update reinstalls configured npm/git packages. source may be empty (all) or one identity.
func (m *Manager) Update(ctx context.Context, source string) error {
	id := ""
	if source != "" {
		id = identityOf(source)
	}
	type item struct {
		src   string
		local bool
	}
	var items []item
	matched := false
	for _, p := range m.User.Packages {
		if id != "" && identityOf(p.Source) != id {
			continue
		}
		matched = true
		items = append(items, item{p.Source, false})
	}
	if m.Trusted {
		for _, p := range m.Project.Packages {
			if id != "" && identityOf(p.Source) != id {
				continue
			}
			matched = true
			items = append(items, item{p.Source, true})
		}
	}
	if source != "" && !matched {
		return fmt.Errorf("no matching package found for %s", source)
	}
	for _, it := range items {
		src, err := ParseSource(it.src)
		if err != nil {
			return err
		}
		switch src.Kind {
		case KindNPM:
			up := src.Name + "@latest"
			if src.Version != "" {
				up = src.Spec
			}
			if err := m.ensureNpmRoot(it.local); err != nil {
				return err
			}
			if err := m.runNpm(ctx, []string{"install", up, "--prefix", m.npmRoot(it.local), "--legacy-peer-deps"}, ""); err != nil {
				return err
			}
		case KindGit:
			if err := m.installGit(ctx, src, it.local); err != nil {
				return err
			}
		}
	}
	return nil
}

// Resolve returns discovered resources (packages, explicit paths, auto dirs).
func (m *Manager) Resolve(ctx context.Context) ([]Resource, error) {
	return m.resolve(ctx, m.Trusted)
}

// ResolveGlobal is Resolve with project files ignored (config TUI global mode).
func (m *Manager) ResolveGlobal(ctx context.Context) ([]Resource, error) {
	return m.resolve(ctx, false)
}

func (m *Manager) resolve(ctx context.Context, trusted bool) ([]Resource, error) {
	var out []Resource
	seen := map[string]int{}
	add := func(path, scope, origin, source, kind, baseDir string, enabled bool) {
		if path == "" {
			return
		}
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Clean(path)
		}
		key := kind + "\x00" + abs
		r := Resource{
			Path: abs, Enabled: enabled, Scope: scope, Origin: origin,
			Source: source, Type: kind, BaseDir: baseDir,
		}
		if i, ok := seen[key]; ok {
			out[i] = r
			return
		}
		seen[key] = len(out)
		out = append(out, r)
	}

	var pkgs []pkgRef
	if trusted {
		for _, p := range m.Project.Packages {
			pkgs = append(pkgs, pkgRef{p, true})
		}
	}
	for _, p := range m.User.Packages {
		pkgs = append(pkgs, pkgRef{p, false})
	}
	pkgs = dedupePackages(pkgs)

	for _, p := range pkgs {
		src, err := ParseSource(p.entry.Source)
		if err != nil {
			continue
		}
		root := m.InstalledPath(p.entry.Source, p.local)
		if root == "" && m.AutoInstall && !offline() && src.Kind != KindLocal {
			if err := m.install(ctx, p.entry.Source, p.local); err != nil {
				continue
			}
			root = m.InstalledPath(p.entry.Source, p.local)
		}
		if root == "" && src.Kind == KindLocal {
			root = src.Path
			if !filepath.IsAbs(root) {
				base := m.AgentDir
				if p.local {
					base = config.ProjectDir(m.Cwd)
				}
				root = filepath.Join(base, root)
			}
		}
		if root == "" {
			continue
		}
		scope := "user"
		if p.local {
			scope = "project"
		}
		for _, kind := range config.ResourceKinds {
			files := collectPackageFiles(root, kind)
			if kind == KindExtensions {
				if st, err := os.Stat(root); err == nil && st.Mode().IsRegular() && len(files) == 0 {
					files = []string{root}
				}
			}
			filters := p.entry.ResourceFilters(kind)
			enabledFor := map[string]bool{}
			switch {
			case p.entry.AutoloadOff():
				enabledFor = applyAutoloadDisabled(files, filters, root)
				var matched []string
				for _, f := range files {
					if _, ok := enabledFor[f]; ok {
						matched = append(matched, f)
					}
				}
				files = matched
			case len(filters) > 0:
				enabledFor = applyPatterns(files, filters, root)
			default:
				for _, f := range files {
					enabledFor[f] = true
				}
			}
			if len(files) == 0 && kind == KindExtensions && !p.entry.AutoloadOff() {
				add(root, scope, "package", p.entry.Source, kind, root, true)
				continue
			}
			for _, f := range files {
				add(f, scope, "package", p.entry.Source, kind, root, enabledFor[f])
			}
		}
	}

	addExplicit := func(paths []string, kind, scope, base string) {
		var plain []string
		for _, p := range paths {
			if isOverridePattern(p) {
				continue
			}
			plain = append(plain, p)
		}
		var files []string
		for _, p := range plain {
			fp := p
			if !filepath.IsAbs(fp) {
				fp = filepath.Join(base, p)
			}
			st, err := os.Stat(fp)
			if err != nil {
				continue
			}
			if st.IsDir() {
				files = append(files, collectResourceFiles(fp, kind)...)
				continue
			}
			files = append(files, fp)
		}
		enabledFor := applyPatterns(files, paths, base)
		for _, f := range files {
			add(f, scope, "top-level", "local", kind, base, enabledFor[f])
		}
	}
	if trusted {
		for _, kind := range config.ResourceKinds {
			addExplicit(m.Project.ResourcePaths(kind), kind, "project", config.ProjectDir(m.Cwd))
		}
	}
	for _, kind := range config.ResourceKinds {
		addExplicit(m.User.ResourcePaths(kind), kind, "user", m.AgentDir)
	}

	addAuto := func(dir, kind, scope, base string, overrides []string) {
		var files []string
		if kind == KindExtensions {
			files = collectAutoExtensionEntries(dir)
		} else {
			files = collectResourceFiles(dir, kind)
		}
		for _, f := range files {
			add(f, scope, "top-level", "auto", kind, base, IsEnabledByOverrides(f, overrides, base))
		}
	}
	for _, kind := range config.ResourceKinds {
		addAuto(filepath.Join(m.AgentDir, kind), kind, "user", m.AgentDir, m.User.ResourcePaths(kind))
	}
	if trusted {
		proj := config.ProjectDir(m.Cwd)
		for _, kind := range config.ResourceKinds {
			addAuto(filepath.Join(proj, kind), kind, "project", proj, m.Project.ResourcePaths(kind))
		}
	}
	return out, nil
}

type pkgRef struct {
	entry config.PackageEntry
	local bool
}

func dedupePackages(in []pkgRef) []pkgRef {
	seen := map[string]int{}
	var out []pkgRef
	for _, p := range in {
		id := identityOf(p.entry.Source)
		if i, ok := seen[id]; ok {
			if p.local {
				out[i] = p
			}
			continue
		}
		seen[id] = len(out)
		out = append(out, p)
	}
	return out
}
