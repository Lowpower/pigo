package auth

import (
	"html"
	"strings"
)

const oauthLogoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 800" aria-hidden="true"><path fill="#fff" fill-rule="evenodd" d="M165.29 165.29 H517.36 V400 H400 V517.36 H282.65 V634.72 H165.29 Z M282.65 282.65 V400 H400 V282.65 Z"/><path fill="#fff" d="M517.36 400 H634.72 V634.72 H517.36 Z"/></svg>`

func oauthSuccessHTML(message string) string {
	return renderOAuthPage("Authentication successful", "Authentication successful", message, "")
}

func oauthErrorHTML(message, details string) string {
	return renderOAuthPage("Authentication failed", "Authentication failed", message, details)
}

func renderOAuthPage(title, heading, message, details string) string {
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\"/>")
	b.WriteString("<title>" + esc(title) + "</title></head><body>")
	b.WriteString(oauthLogoSVG)
	b.WriteString("<h1>" + esc(heading) + "</h1><p>" + esc(message) + "</p>")
	if details != "" {
		b.WriteString("<pre>" + esc(details) + "</pre>")
	}
	b.WriteString("</body></html>")
	return b.String()
}
