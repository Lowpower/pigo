package changelog

import (
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/version"
)

// StartupNotice records lastChangelogVersion and returns markdown to show on a
// new interactive session. First install (no lastChangelogVersion) records the
// current version and returns empty.
func StartupNotice(cfg *config.Config, configDir string) string {
	if cfg == nil {
		return ""
	}
	entries := Parse(embedded)
	if cfg.LastChangelogVersion == "" {
		cfg.LastChangelogVersion = version.Version
		_ = config.Save(configDir, *cfg)
		reportInstallTelemetry(*cfg)
		return ""
	}
	newer := NewEntries(entries, cfg.LastChangelogVersion)
	if len(newer) == 0 {
		return ""
	}
	cfg.LastChangelogVersion = version.Version
	_ = config.Save(configDir, *cfg)
	reportInstallTelemetry(*cfg)

	var parts []string
	for _, e := range newer {
		parts = append(parts, NormalizeLinks(e.Content, e.Version()))
	}
	markdown := strings.Join(parts, "\n\n")
	if cfg.CollapsedChangelog() {
		latest := version.Version
		if m := versionHeaderRe.FindStringSubmatch(markdown); m != nil {
			latest = m[1] + "." + m[2] + "." + m[3]
		} else if len(newer) > 0 {
			latest = newer[0].Version()
		}
		return "Updated to v" + latest + ". Use /changelog to view full changelog."
	}
	return "What's New\n\n" + strings.TrimSpace(markdown)
}

func reportInstallTelemetry(cfg config.Config) {
	if os.Getenv("PIGO_OFFLINE") != "" || os.Getenv("PI_OFFLINE") != "" {
		return
	}
	if !cfg.InstallTelemetryEnabled() {
		return
	}
	go func() {
		u := installTelemetryURL + "?version=" + url.QueryEscape(version.Version)
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", userAgent(version.Version))
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		_ = resp.Body.Close()
	}()
}

func userAgent(ver string) string {
	return "pigo/" + ver + " (" + runtime.GOOS + "; " + runtime.Version() + "; " + runtime.GOARCH + ")"
}
