package oauth

import (
	"os/exec"
	"runtime"

	"github.com/Lowpower/pigo/internal/auth"
)

func openBrowser(target string) {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{target}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", target}
	default:
		name, args = "xdg-open", []string{target}
	}
	cmd := exec.Command(name, args...)
	_ = cmd.Start()
}

func (p Prompt) show(rawURL string) {
	if p.Notify != nil {
		p.Notify(auth.Event{
			Type:         auth.EventAuthURL,
			URL:          rawURL,
			Instructions: "Open this URL to sign in to the MCP server.",
		})
	}
	if p.Open != nil {
		p.Open(rawURL)
		return
	}
	openBrowser(rawURL)
}
