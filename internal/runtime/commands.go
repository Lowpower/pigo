package runtime

import (
	"fmt"

	"github.com/Lowpower/pigo/internal/ext"
	"github.com/Lowpower/pigo/internal/slash"
)

type extCommand struct {
	Name        string
	Orig        string
	Description string
	Host        *ext.Host
}

func (e *Engine) rebuildCommands() {
	used := map[string]int{}
	for _, c := range slash.Builtins() {
		used[c.Name] = 1
		for _, a := range c.Aliases {
			used[a] = 1
		}
	}
	var cmds []extCommand
	for _, h := range e.Hosts {
		if h == nil {
			continue
		}
		for _, c := range h.Commands() {
			n := used[c.Name]
			name := c.Name
			if n > 0 {
				name = fmt.Sprintf("%s:%d", c.Name, n+1)
			}
			used[c.Name] = n + 1
			cmds = append(cmds, extCommand{Name: name, Orig: c.Name, Description: c.Description, Host: h})
		}
	}
	e.mu.Lock()
	e.extCommands = cmds
	e.mu.Unlock()
}

// SlashCommands are extension commands with collision names applied.
func (e *Engine) SlashCommands() []slash.Command {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]slash.Command, 0, len(e.extCommands))
	for _, c := range e.extCommands {
		out = append(out, slash.Command{Name: c.Name, Description: c.Description})
	}
	return out
}

// DispatchCommand sends a command frame when name is a registered extension command.
func (e *Engine) DispatchCommand(name, args string) bool {
	e.mu.Lock()
	var host *ext.Host
	orig := name
	for _, c := range e.extCommands {
		if c.Name == name {
			host = c.Host
			orig = c.Orig
			break
		}
	}
	e.mu.Unlock()
	if host == nil {
		return false
	}
	_ = host.SendCommand(orig, args)
	return true
}

func (e *Engine) hasCommand(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.extCommands {
		if c.Name == name {
			return true
		}
	}
	return false
}

// DispatchShortcut sends a shortcut frame. Later-registered hosts win.
func (e *Engine) DispatchShortcut(key string) bool {
	var host *ext.Host
	for _, h := range e.Hosts {
		if h != nil && h.HasShortcut(key) {
			host = h
		}
	}
	if host == nil {
		return false
	}
	_ = host.SendShortcut(key)
	return true
}
