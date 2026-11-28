package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"

	"github.com/Lowpower/pigo/internal/ai"
	"github.com/Lowpower/pigo/internal/theme"
)

type entryKind uint8

const (
	entryStatic entryKind = iota
	entryMeta
	entryError
	entryMarkdownMeta
	entryBugReport
	entryUser
	entryAssistant
	entryThinking
	entryToolCustom
	entryToolResult
	entryToolPreview
	entryBang
)

type bangEntry struct {
	header string
	body   string
	status string
}

func (m Model) markdownWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.layoutWidth() - m.cfg.OutputPadN()
}

func newThemeRenderer(width int, th theme.Theme) *glamour.TermRenderer {
	wrap := width - 2
	if wrap < 20 {
		wrap = 20
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyleConfig(th)),
		glamour.WithColorProfile(lipgloss.ColorProfile()),
		glamour.WithWordWrap(wrap),
	)
	if err != nil {
		return nil
	}
	return r
}

func markdownStyleConfig(th theme.Theme) ansi.StyleConfig {
	light := th.Appearance == "light" || (th.Appearance == "" && !lipgloss.HasDarkBackground())
	var cfg ansi.StyleConfig
	if light {
		cfg = styles.LightStyleConfig
	} else {
		cfg = styles.DarkStyleConfig
	}

	var chroma ansi.Chroma
	if cfg.CodeBlock.Chroma != nil {
		chroma = *cfg.CodeBlock.Chroma
	}
	cfg.CodeBlock.Chroma = &chroma

	colorOf := func(token, fallback string) *string {
		c := ""
		if th.Colors != nil {
			c = strings.TrimSpace(th.Colors[token])
		}
		if c == "" {
			c = strings.TrimSpace(fallback)
		}
		if c == "" {
			return nil
		}
		return &c
	}
	chromaColorOf := func(token, fallback string) *string {
		p := colorOf(token, fallback)
		if p == nil {
			return nil
		}
		hex := theme.Hex(*p)
		if hex == "" {
			return nil
		}
		return &hex
	}

	cfg.Document.Color = colorOf("text", th.Assistant)
	cfg.BlockQuote.Color = colorOf("mdQuote", th.Muted)
	cfg.Heading.Color = colorOf("mdHeading", th.Accent)
	cfg.H1.Color = nil
	cfg.H1.BackgroundColor = nil
	cfg.H1.Prefix = "# "
	cfg.H1.Suffix = ""
	cfg.H6.Color = nil
	cfg.HorizontalRule.Color = colorOf("mdHr", th.Muted)
	cfg.Item.Color = colorOf("mdListBullet", th.Accent)
	cfg.Enumeration.Color = colorOf("mdListBullet", th.Accent)
	cfg.LinkText.Color = colorOf("mdLink", th.Accent)
	cfg.Link.Color = colorOf("mdLinkUrl", th.Muted)
	cfg.Code.Color = colorOf("mdCode", th.Accent)
	cfg.Code.BackgroundColor = nil
	cfg.CodeBlock.Color = colorOf("mdCodeBlock", th.Tool)

	chroma.Text.Color = chromaColorOf("mdCodeBlock", th.Tool)
	chroma.Error.Color = chromaColorOf("error", th.Error)
	chroma.Error.BackgroundColor = nil
	chroma.Comment.Color = chromaColorOf("syntaxComment", th.Muted)
	chroma.CommentPreproc.Color = chromaColorOf("syntaxComment", th.Muted)
	chroma.Keyword.Color = chromaColorOf("syntaxKeyword", th.Accent)
	chroma.KeywordReserved.Color = chromaColorOf("syntaxKeyword", th.Accent)
	chroma.KeywordNamespace.Color = chromaColorOf("syntaxKeyword", th.Accent)
	chroma.KeywordType.Color = chromaColorOf("syntaxType", th.Accent)
	chroma.Operator.Color = chromaColorOf("syntaxOperator", th.Muted)
	chroma.Punctuation.Color = chromaColorOf("syntaxPunctuation", th.Muted)
	chroma.Name.Color = chromaColorOf("syntaxVariable", th.Assistant)
	chroma.NameBuiltin.Color = chromaColorOf("syntaxFunction", th.Accent)
	chroma.NameTag.Color = chromaColorOf("syntaxKeyword", th.Accent)
	chroma.NameAttribute.Color = chromaColorOf("syntaxVariable", th.Assistant)
	chroma.NameClass.Color = chromaColorOf("syntaxType", th.Accent)
	chroma.NameDecorator.Color = chromaColorOf("syntaxFunction", th.Accent)
	chroma.NameFunction.Color = chromaColorOf("syntaxFunction", th.Accent)
	chroma.LiteralNumber.Color = chromaColorOf("syntaxNumber", th.Tool)
	chroma.LiteralString.Color = chromaColorOf("syntaxString", th.Tool)
	chroma.LiteralStringEscape.Color = chromaColorOf("syntaxString", th.Tool)
	chroma.GenericDeleted.Color = chromaColorOf("toolDiffRemoved", th.Error)
	chroma.GenericInserted.Color = chromaColorOf("toolDiffAdded", th.Tool)
	chroma.GenericSubheading.Color = chromaColorOf("mdHeading", th.Accent)
	chroma.Background.BackgroundColor = nil

	return cfg
}

func (m Model) renderEntry(e entry) string {
	switch e.kind {
	case entryMeta:
		return m.metaStyle.Render(e.raw)
	case entryError:
		return m.errStyle.Render(e.raw)
	case entryMarkdownMeta:
		return m.renderMarkdown(e.raw)
	case entryBugReport:
		return m.metaStyle.Render(e.raw) + "\n" + e.suffix
	case entryUser:
		return m.userStyle.Render("› you") + "\n" + indent(e.raw)
	case entryAssistant:
		out := m.renderMarkdown(e.raw)
		for _, img := range e.images {
			out += "\n" + m.renderInlineImage(img)
		}
		return out
	case entryThinking:
		return m.metaStyle.Render(e.thinking)
	case entryToolCustom:
		return m.toolStyle.Render(e.raw)
	case entryToolResult:
		style := m.toolStyle
		mark := "→"
		if e.isError {
			style = m.errStyle
			mark = "✗"
		}
		return style.Render(fmt.Sprintf("  %s %s", mark, firstLine(e.toolOut)))
	case entryToolPreview:
		return m.toolStyle.Render(toolResultBodyLinesAt(e.toolOut, false, toolPreviewLines(e.toolName), m.toolPreviewWidth()))
	case entryBang:
		if e.bang == nil {
			return e.rendered
		}
		out := m.toolStyle.Render(e.bang.header)
		if e.bang.body != "" {
			out += "\n" + indent(e.bang.body)
		}
		if e.bang.status != "" {
			out += "\n" + m.errStyle.Render(e.bang.status)
		}
		return out
	default:
		return e.rendered
	}
}

func (m *Model) rerenderTranscript() {
	for i := range m.transcript {
		if m.transcript[i].kind == entryStatic {
			continue
		}
		m.transcript[i].rendered = m.renderEntry(m.transcript[i])
	}
}

func (m *Model) appendEntry(e entry) {
	if e.kind != entryStatic {
		e.rendered = m.renderEntry(e)
	}
	m.transcript = append(m.transcript, e)
}

func (m *Model) appendMeta(text string) {
	m.appendEntry(entry{role: "meta", kind: entryMeta, raw: text})
}

func (m *Model) appendError(text string) {
	m.appendEntry(entry{role: "meta", kind: entryError, raw: text})
}

func (m *Model) appendMarkdownMeta(md string) {
	m.appendEntry(entry{role: "meta", kind: entryMarkdownMeta, raw: md})
}

func (m *Model) appendBugReport(text, link string) {
	m.appendEntry(entry{role: "meta", kind: entryBugReport, raw: text, suffix: link})
}

func (m *Model) appendUser(text string) {
	m.appendEntry(m.makeUserEntry(text))
}

func (m *Model) appendAssistant(text string, images []ai.ImageContent) {
	m.appendEntry(m.makeAssistantEntry(text, images))
}

func (m *Model) appendThinking(text string) {
	m.appendEntry(entry{role: "thinking", kind: entryThinking, thinking: text})
}

func (m *Model) appendBang(header, body, status string) {
	m.appendEntry(entry{
		role: "tool",
		kind: entryBang,
		bang: &bangEntry{header: header, body: body, status: status},
	})
}

func (m Model) makeUserEntry(text string) entry {
	e := entry{role: "user", kind: entryUser, raw: text}
	e.rendered = m.renderEntry(e)
	return e
}

func (m Model) makeAssistantEntry(text string, images []ai.ImageContent) entry {
	e := entry{role: "assistant", kind: entryAssistant, raw: text, images: images}
	e.rendered = m.renderEntry(e)
	return e
}

func (m Model) makeToolPreviewEntry(toolName, toolOut string) entry {
	e := entry{role: "tool", kind: entryToolPreview, toolName: toolName, toolOut: toolOut}
	e.rendered = m.renderEntry(e)
	return e
}
