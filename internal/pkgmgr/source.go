package pkgmgr

import (
	"net/url"
	"regexp"
	"strings"
)

// Kind is the parsed install source type.
type Kind string

const (
	// KindNPM is an npm:name or npm:name@version source.
	KindNPM Kind = "npm"
	// KindGit is a git clone URL or git:host/path source.
	KindGit Kind = "git"
	// KindLocal is a filesystem path.
	KindLocal Kind = "local"
)

// Source is a parsed package source string.
type Source struct {
	Raw      string
	Kind     Kind
	Spec     string // npm spec (name or name@version)
	Name     string // npm package name
	Version  string // npm version / range
	Host     string // git host
	RepoPath string // git user/repo
	Repo     string // git clone URL
	Ref      string // git ref
	Path     string // local path
}

// Identity is stable across version/ref so the same package in user and
// project settings can be deduped (project wins).
func (s Source) Identity() string {
	switch s.Kind {
	case KindNPM:
		return "npm:" + s.Name
	case KindGit:
		return "git:" + s.Host + "/" + s.RepoPath
	default:
		return "local:" + s.Path
	}
}

var npmSpecRe = regexp.MustCompile(`^(@?[^@]+(?:/[^@]+)?)(?:@(.+))?$`)

// ParseSource classifies an install/remove source string.
func ParseSource(source string) (Source, error) {
	source = strings.TrimSpace(source)
	out := Source{Raw: source}
	if strings.HasPrefix(source, "npm:") {
		spec := strings.TrimSpace(source[len("npm:"):])
		name, version := parseNpmSpec(spec)
		out.Kind = KindNPM
		out.Spec = spec
		out.Name = name
		out.Version = version
		return out, nil
	}
	if isLocalPath(source) {
		out.Kind = KindLocal
		out.Path = source
		return out, nil
	}
	if g, ok := parseGitURL(source); ok {
		return g, nil
	}
	out.Kind = KindLocal
	out.Path = source
	return out, nil
}

func parseNpmSpec(spec string) (name, version string) {
	m := npmSpecRe.FindStringSubmatch(spec)
	if m == nil {
		return spec, ""
	}
	return m[1], m[2]
}

func isLocalPath(value string) bool {
	trimmed := strings.TrimSpace(value)
	for _, p := range []string{"npm:", "git:", "github:", "http:", "https:", "ssh:"} {
		if strings.HasPrefix(trimmed, p) {
			return false
		}
	}
	return true
}

func parseGitURL(source string) (Source, bool) {
	trimmed := strings.TrimSpace(source)
	hasGitPrefix := strings.HasPrefix(trimmed, "git:")
	u := trimmed
	if hasGitPrefix {
		u = strings.TrimSpace(trimmed[4:])
	}
	if !hasGitPrefix && !hasGitProtocol(u) {
		return Source{}, false
	}
	repo, ref := splitGitRef(u)
	host, repoPath, cloneURL, ok := gitHostPath(repo)
	if !ok {
		return Source{}, false
	}
	repoPath = strings.TrimSuffix(repoPath, ".git")
	repoPath = strings.TrimPrefix(repoPath, "/")
	if host == "" || repoPath == "" || !strings.Contains(repoPath, "/") {
		return Source{}, false
	}
	if cloneURL == "" {
		if strings.HasPrefix(repo, "git@") || hasGitProtocol(repo) {
			cloneURL = repo
		} else {
			cloneURL = "https://" + repo
		}
	}
	return Source{
		Raw:      source,
		Kind:     KindGit,
		Host:     host,
		RepoPath: repoPath,
		Repo:     cloneURL,
		Ref:      ref,
	}, true
}

func hasGitProtocol(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") ||
		strings.HasPrefix(l, "ssh://") || strings.HasPrefix(l, "git://")
}

func splitGitRef(u string) (repo, ref string) {
	if strings.HasPrefix(u, "git@") {
		colon := strings.Index(u, ":")
		if colon < 0 {
			return u, ""
		}
		rest := u[colon+1:]
		if i := strings.Index(rest, "@"); i >= 0 {
			pathPart, r := rest[:i], rest[i+1:]
			if pathPart != "" && r != "" {
				return u[:colon+1] + pathPart, r
			}
		}
		return u, ""
	}
	if strings.Contains(u, "://") {
		parsed, err := url.Parse(u)
		if err != nil {
			return u, ""
		}
		p := strings.TrimPrefix(parsed.Path, "/")
		if i := strings.Index(p, "@"); i >= 0 {
			repoPath, r := p[:i], p[i+1:]
			if repoPath != "" && r != "" {
				parsed.Path = "/" + repoPath
				s := strings.TrimSuffix(parsed.String(), "/")
				return s, r
			}
		}
		return u, ""
	}
	slash := strings.Index(u, "/")
	if slash < 0 {
		return u, ""
	}
	rest := u[slash+1:]
	if i := strings.Index(rest, "@"); i >= 0 {
		return u[:slash+1] + rest[:i], rest[i+1:]
	}
	return u, ""
}

func gitHostPath(repo string) (host, repoPath, cloneURL string, ok bool) {
	if strings.HasPrefix(repo, "git@") {
		colon := strings.Index(repo, ":")
		if colon < 0 {
			return "", "", "", false
		}
		return repo[4:colon], repo[colon+1:], repo, true
	}
	if hasGitProtocol(repo) {
		parsed, err := url.Parse(repo)
		if err != nil {
			return "", "", "", false
		}
		return parsed.Hostname(), strings.TrimPrefix(parsed.Path, "/"), repo, true
	}
	slash := strings.Index(repo, "/")
	if slash < 0 {
		return "", "", "", false
	}
	host = repo[:slash]
	repoPath = repo[slash+1:]
	if !strings.Contains(host, ".") && host != "localhost" {
		return "", "", "", false
	}
	return host, repoPath, "https://" + repo, true
}
