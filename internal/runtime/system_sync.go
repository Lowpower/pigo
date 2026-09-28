package runtime

import (
	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/prompt"
	"github.com/Lowpower/pigo/internal/session"
)

func (e *Engine) promptSections() prompt.SectionSet {
	var toolDefs []ai.Tool
	if e.Tools != nil {
		toolDefs = e.providerTools()
	}
	return prompt.Collect(prompt.Options{
		Cwd:              e.Opts.Cwd,
		AgentDir:         e.Opts.AgentDir,
		Custom:           e.Opts.SystemPrompt,
		Append:           e.Opts.AppendSystem,
		NoContextFiles:   e.Opts.NoContextFiles,
		ProjectTrusted:   e.Opts.ProjectTrusted,
		Skills:           e.Skills,
		Tools:            toolDefs,
		IncludeToolHints: true,
	})
}

// syncSystem writes the leading system declaration or a later patch.
// force, when set, becomes the preamble: part of the first declaration, or a
// preamble-only message once a system message already exists.
// The returned prompt is the replayed text. Engine.System is left unchanged.
func (e *Engine) syncSystem(force string) string {
	built := e.promptSections()
	var tools []ai.Tool
	if e.Tools != nil {
		tools = e.providerTools()
	}
	if e.Opts.Session == nil {
		if force != "" {
			built.Set(prompt.SectionPreamble, force)
		}
		return built.Render()
	}
	replay := session.ReplaySystem(e.Opts.Session.GetBranch(""))
	if !replay.Declared {
		desired := built.Map()
		if force != "" {
			desired[prompt.SectionPreamble] = force
		}
		msg := session.DeclareSystem(desired, tools)
		_, _ = e.Opts.Session.AppendMessage("system", msg)
		e.preambleAnchor = built.Get(prompt.SectionPreamble)
		e.sawPreamble = true
		return session.ReplaySystem(e.Opts.Session.GetBranch("")).Prompt
	}
	desired := mergeDesiredSections(replay, built)
	if !e.sawPreamble {
		e.preambleAnchor = built.Get(prompt.SectionPreamble)
		e.sawPreamble = true
		if p, ok := replay.Sections[prompt.SectionPreamble]; ok {
			desired[prompt.SectionPreamble] = p
		}
	} else if built.Get(prompt.SectionPreamble) == e.preambleAnchor {
		if p, ok := replay.Sections[prompt.SectionPreamble]; ok {
			desired[prompt.SectionPreamble] = p
		}
	} else {
		e.preambleAnchor = built.Get(prompt.SectionPreamble)
	}
	if force != "" {
		other := copySections(desired)
		if p, ok := replay.Sections[prompt.SectionPreamble]; ok {
			other[prompt.SectionPreamble] = p
		} else {
			delete(other, prompt.SectionPreamble)
		}
		if patch := session.DiffSystem(replay, other, tools); patch != nil {
			_, _ = e.Opts.Session.AppendMessage("system", *patch)
			replay = session.ReplaySystem(e.Opts.Session.GetBranch(""))
		}
		if replay.Sections[prompt.SectionPreamble] != force {
			_, _ = e.Opts.Session.AppendMessage("system", session.PreamblePatch(force))
		}
		e.preambleAnchor = built.Get(prompt.SectionPreamble)
		e.sawPreamble = true
		return session.ReplaySystem(e.Opts.Session.GetBranch("")).Prompt
	}
	if patch := session.DiffSystem(replay, desired, tools); patch != nil {
		_, _ = e.Opts.Session.AppendMessage("system", *patch)
	}
	return session.ReplaySystem(e.Opts.Session.GetBranch("")).Prompt
}

func (e *Engine) syncToolsIfDeclared() {
	if e.Opts.Session == nil {
		return
	}
	if !session.ReplaySystem(e.Opts.Session.GetBranch("")).Declared {
		return
	}
	_ = e.syncSystem("")
}

func (e *Engine) currentSystemCheckpoint() *session.SystemMessage {
	if e.Opts.Session != nil {
		st := session.ReplaySystem(e.Opts.Session.GetBranch(""))
		if st.Declared {
			msg := session.CheckpointSystem(st.Content, st.Sections, st.Tools)
			return &msg
		}
	}
	msg := session.CheckpointSystem("", e.promptSections().Map(), e.providerTools())
	return &msg
}

func (e *Engine) requestTools() ([]ai.Tool, bool) {
	if e.Opts.Session != nil {
		st := session.ReplaySystem(e.Opts.Session.GetBranch(""))
		if st.Declared {
			return st.Tools, true
		}
	}
	if e.Tools == nil {
		return nil, false
	}
	return e.providerTools(), false
}

func (e *Engine) restoreSystem(s *session.Manager) {
	if s == nil {
		return
	}
	st := session.ReplaySystem(s.GetBranch(""))
	if !st.Declared {
		return
	}
	e.System = st.Prompt
	names := make([]string, len(st.Tools))
	for i, t := range st.Tools {
		names[i] = t.Name
	}
	e.mu.Lock()
	e.activeToolNames = names
	e.mu.Unlock()
	e.sawPreamble = false
}

func mergeDesiredSections(replay session.ReplayResult, built prompt.SectionSet) map[string]string {
	out := map[string]string{}
	for k, v := range replay.Sections {
		if !prompt.IsKnownSection(k) {
			out[k] = v
		}
	}
	for k, v := range built.Map() {
		out[k] = v
	}
	return out
}

func copySections(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
