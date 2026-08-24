package slash

import (
	"strings"
)

// Command is a built-in or extension-registered slash command (pi core/slash-commands.ts).
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
				c.Name = c.Name // canonical
				return c, true
			}
		}
	}
	return Command{}, false
}

// Builtins is the pi built-in slash command list that pigo currently implements
// (interactive handlers live in the TUI / runtime). Commands we cannot honour
// yet are still listed so /help matches pi's surface.
func Builtins() []Command {
	return []Command{
		{Name: "help", Aliases: []string{"?"}, Description: "list slash commands"},
		{Name: "quit", Aliases: []string{"exit", "q"}, Description: "exit pigo"},
		{Name: "new", Description: "start a new session"},
		{Name: "resume", Description: "switch to another session"},
		{Name: "session", Description: "show current session info"},
		{Name: "compact", Description: "compact conversation history"},
		{Name: "model", Description: "show or set the model"},
		{Name: "provider", Description: "show or set the provider"},
		{Name: "theme", Description: "show or set the theme"},
		{Name: "thinking", Description: "show or set thinking level"},
		{Name: "skills", Description: "list discovered skills"},
		{Name: "tools", Description: "list available tools"},
		{Name: "export", Description: "export the session JSONL"},
		{Name: "fork", Description: "fork the session into a new file"},
		{Name: "clear", Description: "clear the on-screen transcript"},
		{Name: "login", Description: "store an API key (pi /login)"},
		{Name: "logout", Description: "remove a stored API key"},
		{Name: "reload", Description: "reload skills, themes, and context files"},
		{Name: "copy", Description: "copy the last assistant message"},
		{Name: "hotkeys", Description: "show keybindings"},
		{Name: "settings", Description: "show settings path"},
		{Name: "name", Description: "set a session display name"},
		{Name: "share", Description: "not implemented (see docs/parity-gaps.md)"},
		{Name: "changelog", Description: "not implemented (see docs/parity-gaps.md)"},
		{Name: "tree", Description: "not implemented (see docs/parity-gaps.md)"},
		{Name: "scoped-models", Description: "not implemented (see docs/parity-gaps.md)"},
		{Name: "import", Description: "not implemented (see docs/parity-gaps.md)"},
		{Name: "trust", Description: "not implemented (see docs/parity-gaps.md)"},
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
