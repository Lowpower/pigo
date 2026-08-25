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
				return c, true
			}
		}
	}
	return Command{}, false
}

// Builtins matches pi BUILTIN_SLASH_COMMANDS, plus /help which pigo keeps as an alias of /hotkeys.
func Builtins() []Command {
	return []Command{
		{Name: "settings", Description: "show settings path"},
		{Name: "model", Description: "show or set the model"},
		{Name: "tree", Description: "show the session parentId tree"},
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
		{Name: "fork", Description: "create a new fork from a previous user message"},
		{Name: "clone", Description: "duplicate the current session at the current position"},
		{Name: "trust", Description: "not implemented"},
		{Name: "login", Description: "configure provider authentication"},
		{Name: "logout", Description: "remove provider authentication"},
		{Name: "new", Description: "start a new session"},
		{Name: "compact", Description: "manually compact the session context"},
		{Name: "resume", Description: "resume a different session"},
		{Name: "reload", Description: "reload skills, prompts, themes, and context files"},
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

// HotkeysText matches pi /hotkeys for the bindings pigo actually honours.
func HotkeysText() string {
	return `keybindings:
  enter                 send (steer while streaming)
  alt+enter             queue follow-up while streaming
  shift+enter / ctrl+j  newline
  escape                interrupt current turn
  ctrl+c                clear editor / interrupt / quit
  ctrl+d                exit when editor is empty
  ctrl+p                cycle model forward
  shift+ctrl+p          cycle model backward
  shift+tab             cycle thinking level
  /help                 list slash commands
`
}
