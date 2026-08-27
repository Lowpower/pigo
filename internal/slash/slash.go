package slash

import (
	"strings"

	"github.com/Lowpower/pigo/internal/keys"
)

// Command is a built-in or extension-registered slash command.
type Command struct {
	Name        string
	Aliases     []string
	Description string
	Rest        string // arguments after the command name
}

// Parse splits a line that starts with '/' into a command. ok is false if the
// line is not a slash command.
func Parse(line string) (Command, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "/") || strings.HasPrefix(line, "//") {
		return Command{}, false
	}
	body := strings.TrimSpace(line[1:])
	name, rest, _ := strings.Cut(body, " ")
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Command{}, false
	}
	c, ok := lookup(name)
	c.Rest = strings.TrimSpace(rest)
	if !ok {
		c.Name = name
		c.Description = "unknown command"
	}
	return c, true
}

func lookup(name string) (Command, bool) {
	for _, c := range Builtins() {
		if c.Name == name {
			return c, true
		}
		for _, a := range c.Aliases {
			if a == name {
				return c, true
			}
		}
	}
	return Command{}, false
}

// Builtins is the built-in slash command list.
func Builtins() []Command {
	return []Command{
		{Name: "settings", Description: "open settings"},
		{Name: "model", Description: "select model (opens selector UI)"},
		{Name: "tree", Description: "navigate session tree (switch branches)"},
		{Name: "thinking", Description: "set thinking level"},
		{Name: "scoped-models", Description: "not implemented"},
		{Name: "export", Description: "export the session (HTML default, or .html/.jsonl path)"},
		{Name: "import", Description: "import and resume a session from a JSONL file"},
		{Name: "share", Description: "not implemented"},
		{Name: "copy", Description: "copy last agent message to clipboard"},
		{Name: "name", Description: "set session display name"},
		{Name: "session", Description: "show session info and stats"},
		{Name: "changelog", Description: "not implemented"},
		{Name: "hotkeys", Description: "show keyboard shortcuts"},
		{Name: "help", Aliases: []string{"?"}, Description: "list slash commands"},
		{Name: "fork", Description: "open fork picker, or /fork <id>"},
		{Name: "clone", Description: "duplicate the current session at the current position"},
		{Name: "trust", Description: "not implemented"},
		{Name: "login", Description: "configure provider authentication"},
		{Name: "logout", Description: "remove provider authentication"},
		{Name: "new", Description: "start a new session"},
		{Name: "compact", Description: "manually compact the session context"},
		{Name: "resume", Description: "resume a session (opens selector UI)"},
		{Name: "reload", Description: "reload keybindings, skills, prompts, themes, and context files"},
		{Name: "quit", Aliases: []string{"exit", "q"}, Description: "quit pigo"},
		{Name: "provider", Description: "show or set the provider"},
		{Name: "theme", Description: "show or set the theme"},
		{Name: "skills", Description: "list discovered skills"},
		{Name: "tools", Description: "list available tools"},
		{Name: "clear", Description: "clear the on-screen transcript"},
	}
}

// HelpText formats /help output.
func HelpText() string {
	var b strings.Builder
	b.WriteString("slash commands:\n")
	for _, c := range Builtins() {
		b.WriteString("  /")
		b.WriteString(c.Name)
		b.WriteString(" — ")
		b.WriteString(c.Description)
		b.WriteByte('\n')
	}
	return b.String()
}

// HotkeysText is the /hotkeys help for the bindings this TUI honours.
func HotkeysText() string {
	return keys.NewManager("").HotkeysText()
}
