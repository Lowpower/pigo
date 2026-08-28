package sandbox

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

func wrapArgv(command, cwd string, cfg Config, br netBridge) []string {
	switch runtime.GOOS {
	case "windows":
		return nil
	case "darwin":
		return wrapDarwin(command, cwd, cfg, br)
	default:
		return wrapLinux(command, cwd, cfg, br)
	}
}

func wrapLinux(command, cwd string, cfg Config, br netBridge) []string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	args := []string{
		"bwrap",
		"--die-with-parent",
		"--unshare-pid",
		"--new-session",
		"--unshare-net",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
	}
	if br.HTTPSock != "" {
		args = append(args, "--bind", br.HTTPSock, br.HTTPSock)
		if br.SOCKSSock != "" && br.SOCKSSock != br.HTTPSock {
			args = append(args, "--bind", br.SOCKSSock, br.SOCKSSock)
		}
	}
	seen := map[string]bool{}
	for _, p := range expandPatterns(cfg.Filesystem.AllowWrite, cwd) {
		if seen[p] {
			continue
		}
		seen[p] = true
		args = append(args, "--bind", p, p)
	}
	for _, p := range expandPatterns(cfg.Filesystem.DenyRead, cwd) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		args = append(args, "--tmpfs", p)
	}
	for _, p := range expandPatterns(cfg.Filesystem.DenyWrite, cwd) {
		if _, err := os.Stat(p); err != nil {
			continue
		}
		args = append(args, "--ro-bind", p, p)
	}
	if cwd != "" {
		args = append(args, "--chdir", cwd)
	}
	inner := command
	if br.HTTPSock != "" {
		inner = linuxProxyScript(command, br)
	}
	args = append(args, "--", "bash", "-c", inner)
	return args
}

func linuxProxyScript(command string, br netBridge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "socat TCP-LISTEN:%d,bind=127.0.0.1,fork,reuseaddr UNIX-CONNECT:%s &\n", br.HTTPPort, br.HTTPSock)
	if br.SOCKSSock != "" {
		fmt.Fprintf(&b, "socat TCP-LISTEN:%d,bind=127.0.0.1,fork,reuseaddr UNIX-CONNECT:%s &\n", br.SOCKSPort, br.SOCKSSock)
	}
	fmt.Fprintf(&b, "export http_proxy=http://127.0.0.1:%d https_proxy=http://127.0.0.1:%d HTTP_PROXY=http://127.0.0.1:%d HTTPS_PROXY=http://127.0.0.1:%d\n", br.HTTPPort, br.HTTPPort, br.HTTPPort, br.HTTPPort)
	if br.SOCKSPort != 0 {
		fmt.Fprintf(&b, "export ALL_PROXY=socks5h://127.0.0.1:%d all_proxy=socks5h://127.0.0.1:%d\n", br.SOCKSPort, br.SOCKSPort)
	}
	b.WriteString("exec bash -c " + strconv.Quote(command) + "\n")
	return b.String()
}

func wrapDarwin(command, cwd string, cfg Config, br netBridge) []string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	profile := seatbeltProfile(cwd, cfg, br)
	inner := command
	if br.HostTCP && br.HTTPPort != 0 {
		inner = darwinProxyScript(command, br)
	}
	return []string{"sandbox-exec", "-p", profile, "bash", "-c", inner}
}

func darwinProxyScript(command string, br netBridge) string {
	var b strings.Builder
	fmt.Fprintf(&b, "export http_proxy=http://127.0.0.1:%d https_proxy=http://127.0.0.1:%d HTTP_PROXY=http://127.0.0.1:%d HTTPS_PROXY=http://127.0.0.1:%d\n", br.HTTPPort, br.HTTPPort, br.HTTPPort, br.HTTPPort)
	if br.SOCKSPort != 0 {
		fmt.Fprintf(&b, "export ALL_PROXY=socks5h://127.0.0.1:%d all_proxy=socks5h://127.0.0.1:%d\n", br.SOCKSPort, br.SOCKSPort)
	}
	b.WriteString("exec bash -c " + strconv.Quote(command) + "\n")
	return b.String()
}

func seatbeltProfile(cwd string, cfg Config, br netBridge) string {
	var b strings.Builder
	b.WriteString("(version 1)\n(deny default)\n")
	b.WriteString("(allow process*)\n(allow signal)\n(allow sysctl-read)\n")
	b.WriteString("(allow mach-lookup)\n(allow file-read*)\n(allow file-ioctl)\n")
	b.WriteString("(allow file-write-data (literal \"/dev/null\") (literal \"/dev/zero\"))\n")
	for _, p := range expandPatterns(cfg.Filesystem.AllowWrite, cwd) {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplPath(p))
	}
	if cwd != "" {
		fmt.Fprintf(&b, "(allow file-write* (subpath %s))\n", sbplPath(cwd))
	}
	for _, p := range expandPatterns(cfg.Filesystem.DenyWrite, cwd) {
		fmt.Fprintf(&b, "(deny file-write* (subpath %s))\n", sbplPath(p))
	}
	for _, p := range cfg.Filesystem.DenyWrite {
		if strings.ContainsAny(p, "*") {
			re := globToSBPL(p)
			if re != "" {
				fmt.Fprintf(&b, "(deny file-write* (regex %s))\n", strconv.Quote(re))
			}
		}
	}
	for _, p := range expandPatterns(cfg.Filesystem.DenyRead, cwd) {
		fmt.Fprintf(&b, "(deny file-read* (subpath %s))\n", sbplPath(p))
	}
	if br.HTTPPort != 0 || br.SOCKSPort != 0 {
		if br.HTTPPort != 0 {
			fmt.Fprintf(&b, "(allow network-outbound (remote ip \"localhost:%d\"))\n", br.HTTPPort)
		}
		if br.SOCKSPort != 0 {
			fmt.Fprintf(&b, "(allow network-outbound (remote ip \"localhost:%d\"))\n", br.SOCKSPort)
		}
		b.WriteString("(allow network-inbound (local ip \"localhost:*\"))\n")
	}
	return b.String()
}

func sbplPath(p string) string {
	return strconv.Quote(p)
}

func globToSBPL(pat string) string {
	pat = strings.TrimSpace(pat)
	if pat == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(".*/")
	for _, r := range pat {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		case '.', '+', '(', ')', '[', ']', '{', '}', '|', '^', '$':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('$')
	return b.String()
}
