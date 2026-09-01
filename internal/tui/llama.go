package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/llama"
	"github.com/Lowpower/pigo/internal/models"
)

const llamaDownloadID = "\x00download"

type llamaPhase int

const (
	llamaIdle llamaPhase = iota
	llamaCatalog
	llamaSelect
	llamaSearch
	llamaProgress
)

type llamaSelectKind string

const (
	llamaSelNone   llamaSelectKind = ""
	llamaSelUnload llamaSelectKind = "unload"
	llamaSelLoad   llamaSelectKind = "load"
	llamaSelGated  llamaSelectKind = "gated"
	llamaSelQuant  llamaSelectKind = "quant"
	llamaSelConn   llamaSelectKind = "conn"
	llamaSelStop   llamaSelectKind = "stop"
)

type llamaOverlay struct {
	active         bool
	phase          llamaPhase
	list           listPicker
	client         *llama.Client
	catalog        []llama.ModelInfo
	serverURL      string
	searchCache    map[string][]llama.HFModel
	searchGen      int
	searchDebounce time.Duration
	hfBaseURL      string
	hfToken        string
	progress       llama.Progress
	progressTitle  string
	progressModel  string
	cancel         context.CancelFunc
	actionGen      int
	selectKind     llamaSelectKind
	selectRepo     string
	details        llama.HFDetails
	loadTarget     llama.ModelInfo
	loadedOthers   []llama.ModelInfo
	status         string
	notify         string
}

type llamaCatalogMsg struct {
	models []llama.ModelInfo
	err    error
}

type llamaSearchTickMsg struct {
	gen   int
	query string
}

type llamaSearchResultMsg struct {
	gen    int
	query  string
	models []llama.HFModel
	err    error
}

type llamaDetailsMsg struct {
	details llama.HFDetails
	err     error
	picked  string
}

type llamaActionDoneMsg struct {
	gen     int
	err     error
	text    string
	refresh bool
}

func (m Model) llamaActive() bool { return m.llama.active }

func (m Model) llamaClient() (*llama.Client, error) {
	url := strings.TrimSpace(os.Getenv("LLAMA_BASE_URL"))
	key := strings.TrimSpace(os.Getenv("LLAMA_API_KEY"))
	if m.engine != nil {
		if p, ok := auth.Lookup(llama.ProviderID); ok {
			if res, err := auth.Resolve(context.Background(), auth.Open(m.engine.Opts.AgentDir), p, auth.ResolveOpts{}); err == nil && res != nil {
				if u := strings.TrimSpace(res.Env["LLAMA_BASE_URL"]); u != "" {
					url = u
				}
				if res.Auth.APIKey != "" && res.Auth.APIKey != "local" {
					key = res.Auth.APIKey
				}
			}
		}
	}
	return llama.NewClient(url, key)
}

func (m Model) openLlamaManager() (tea.Model, tea.Cmd) {
	c, err := m.llamaClient()
	if err != nil {
		m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(err.Error())})
		return m, nil
	}
	debounce := m.llama.searchDebounce
	hf := m.llama.hfBaseURL
	m.llama = llamaOverlay{
		active:         true,
		phase:          llamaCatalog,
		client:         c,
		serverURL:      c.ServerURL,
		searchCache:    map[string][]llama.HFModel{},
		searchDebounce: debounce,
		hfBaseURL:      hf,
		hfToken:        llama.FindHuggingFaceToken(),
		status:         "Loading…",
	}
	m.llama.list.title = "llama.cpp models"
	m.llama.list.hint = "enter load/unload/download · esc close"
	m.llama.list.active = true
	m.llama.list.skipFilter = false
	return m, m.fetchLlamaCatalog()
}

func (m Model) fetchLlamaCatalog() tea.Cmd {
	c := m.llama.client
	return func() tea.Msg {
		list, err := c.List(false)
		return llamaCatalogMsg{models: list, err: err}
	}
}

func (m *Model) setLlamaCatalog(list []llama.ModelInfo) {
	sorted := append([]llama.ModelInfo(nil), list...)
	sort.Slice(sorted, func(i, j int) bool {
		li := llama.ModelIsLoaded(sorted[i])
		lj := llama.ModelIsLoaded(sorted[j])
		if li != lj {
			return li
		}
		return sorted[i].ID < sorted[j].ID
	})
	m.llama.catalog = sorted
	items := make([]pickerItem, 0, len(sorted)+1)
	for _, model := range sorted {
		items = append(items, pickerItem{
			ID:    model.ID,
			Label: model.ID,
			Meta:  llamaModelMeta(model),
		})
	}
	items = append(items, pickerItem{
		ID:    llamaDownloadID,
		Label: "Download model…",
		Meta:  "Hugging Face owner/repository[:quant]",
	})
	m.llama.list.title = "llama.cpp models\n" + m.llama.serverURL
	m.llama.list.hint = "enter load/unload/download · esc close"
	m.llama.list.skipFilter = false
	m.llama.list.query = ""
	m.llama.list.setItems(items)
	m.llama.phase = llamaCatalog
	m.llama.selectKind = llamaSelNone
	m.llama.status = ""
}

func llamaModelMeta(model llama.ModelInfo) string {
	var parts []string
	if llama.ModelIsLoaded(model) {
		parts = append(parts, "loaded")
	} else if model.Status.Value != "unloaded" {
		parts = append(parts, model.Status.Value)
	}
	if llama.ModelIsLoaded(model) {
		if ctx := llama.ContextLabel(model); ctx != "" {
			parts = append(parts, ctx+" context")
		}
	}
	return strings.Join(parts, " · ")
}

func (m Model) llamaSelect(title string, kind llamaSelectKind, options []string) Model {
	items := make([]pickerItem, 0, len(options))
	for _, o := range options {
		items = append(items, pickerItem{ID: o, Label: o})
	}
	m.llama.phase = llamaSelect
	m.llama.selectKind = kind
	m.llama.list.title = title
	m.llama.list.hint = "enter select · esc cancel"
	m.llama.list.query = ""
	m.llama.list.skipFilter = true
	m.llama.list.setItems(items)
	return m
}

func (m Model) llamaSearchView() Model {
	m.llama.phase = llamaSearch
	m.llama.selectKind = llamaSelNone
	m.llama.list.title = "Download model"
	m.llama.list.hint = "enter select · esc back"
	m.llama.list.query = ""
	m.llama.list.skipFilter = true
	m.llama.status = "Type at least 2 characters"
	m.llama.list.setItems(nil)
	return m
}

func (m Model) scheduleLlamaSearch() (Model, tea.Cmd) {
	query := strings.TrimSpace(m.llama.list.query)
	m.llama.searchGen++
	gen := m.llama.searchGen
	if len(query) < 2 {
		m.llama.status = "Type at least 2 characters"
		m.applyLlamaSearchFilter(nil, query)
		return m, nil
	}
	if cached, ok := m.llama.searchCache[strings.ToLower(query)]; ok {
		m.llama.status = ""
		if len(cached) == 0 {
			m.llama.status = "No GGUF models found"
		}
		m.applyLlamaSearchFilter(cached, query)
		return m, nil
	}
	m.llama.status = "Searching Hugging Face…"
	m.applyLlamaSearchFilter(nil, query)
	d := m.llama.searchDebounce
	if d == 0 {
		d = 500 * time.Millisecond
	}
	return m, func() tea.Msg {
		if d > 0 {
			timer := time.NewTimer(d)
			<-timer.C
			timer.Stop()
		}
		return llamaSearchTickMsg{gen: gen, query: query}
	}
}

func (m *Model) applyLlamaSearchFilter(results []llama.HFModel, query string) {
	filtered := results
	if query != "" && len(results) > 0 {
		filtered = fuzzyFilter(results, query, func(model llama.HFModel) string { return model.ID })
	}
	items := make([]pickerItem, 0, len(filtered))
	for _, model := range filtered {
		items = append(items, pickerItem{
			ID:    model.ID,
			Label: model.ID,
			Meta:  llama.CompactCount(model.Downloads) + " downloads",
		})
	}
	m.llama.list.items = items
	m.llama.list.filtered = items
	if m.llama.list.selected >= len(items) {
		m.llama.list.selected = max(0, len(items)-1)
	}
}

func (m Model) handleLlamaMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case llamaCatalogMsg:
		if msg.err != nil {
			return m.llamaSelect("llama.cpp unavailable\n"+m.llama.serverURL+"\n\n"+llamaConnErr(msg.err), llamaSelConn, []string{"Retry", "Close"}), nil
		}
		m.setLlamaCatalog(msg.models)
		return m, nil
	case llamaSearchTickMsg:
		if msg.gen != m.llama.searchGen || strings.TrimSpace(m.llama.list.query) != msg.query {
			return m, nil
		}
		token := m.llama.hfToken
		base := m.llama.hfBaseURL
		return m, func() tea.Msg {
			found, err := llama.SearchHuggingFace(context.Background(), msg.query, token, base)
			return llamaSearchResultMsg{gen: msg.gen, query: msg.query, models: found, err: err}
		}
	case llamaSearchResultMsg:
		if msg.gen != m.llama.searchGen || strings.TrimSpace(m.llama.list.query) != msg.query {
			return m, nil
		}
		if msg.err != nil {
			m.llama.status = msg.err.Error()
			m.applyLlamaSearchFilter(nil, msg.query)
			return m, nil
		}
		m.llama.searchCache[strings.ToLower(msg.query)] = msg.models
		m.llama.status = ""
		if len(msg.models) == 0 {
			m.llama.status = "No GGUF models found"
		}
		m.applyLlamaSearchFilter(msg.models, msg.query)
		return m, nil
	case llamaDetailsMsg:
		if msg.err != nil {
			m.llama.notify = msg.err.Error()
			return m, m.fetchLlamaCatalog()
		}
		m.llama.details = msg.details
		if msg.details.Gated != "" {
			access := "Manual approval is required"
			if msg.details.Gated == "auto" {
				access = "Accept the access terms"
			}
			title := "Hugging Face access required\n" + msg.details.ID + "\n\n" + access + " at:\nhttps://huggingface.co/" + msg.details.ID + "\n\nThe llama.cpp server needs HF_TOKEN with access."
			m.llama.selectRepo = msg.picked
			return m.llamaSelect(title, llamaSelGated, []string{"Continue", "Back"}), nil
		}
		return m.afterLlamaDetails(msg.picked)
	case llamaActionDoneMsg:
		if msg.gen != m.llama.actionGen {
			return m, nil
		}
		m.llama.cancel = nil
		m.llama.phase = llamaCatalog
		if msg.err != nil {
			m.llama.notify = msg.err.Error()
		} else if msg.text != "" {
			m.llama.notify = msg.text
		}
		if msg.refresh || msg.err == nil {
			_ = models.ApplyLlamaCatalog(m.llama.client, nil)
			return m, m.fetchLlamaCatalog()
		}
		return m, m.fetchLlamaCatalog()
	}
	return m, nil
}

func llamaConnErr(err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "connection refused") || strings.Contains(msg, "network") {
		return "Could not connect to the server."
	}
	return err.Error()
}

func (m Model) afterLlamaDetails(picked string) (tea.Model, tea.Cmd) {
	repo, quant := llama.ParseHuggingFaceModel(picked)
	if repo == "" {
		repo = m.llama.details.ID
	}
	if quant == "" && len(m.llama.details.Quantizations) > 0 {
		options := make([]string, 0, len(m.llama.details.Quantizations))
		for _, q := range m.llama.details.Quantizations {
			label := q.Name
			var extra []string
			if q.Size != nil {
				extra = append(extra, llama.FormatBytes(float64(*q.Size)))
			}
			if q.Name == "Q4_K_M" {
				extra = append(extra, "recommended")
			}
			if len(extra) > 0 {
				label += " · " + strings.Join(extra, " · ")
			}
			options = append(options, label)
		}
		m.llama.selectRepo = repo
		return m.llamaSelect("Select quantization\n"+m.llama.details.ID, llamaSelQuant, options), nil
	}
	id := m.llama.details.ID
	if quant != "" {
		id += ":" + quant
	}
	return m.startLlamaDownload(id)
}

func (m Model) startLlamaDownload(id string) (tea.Model, tea.Cmd) {
	m.llama.actionGen++
	gen := m.llama.actionGen
	ctx, cancel := context.WithCancel(context.Background())
	m.llama.cancel = cancel
	m.llama.phase = llamaProgress
	m.llama.progressTitle = "Downloading model"
	m.llama.progressModel = id
	m.llama.progress = llama.Progress{Message: "Starting…", Ratio: -1}
	c := m.llama.client
	return m, func() tea.Msg {
		_, err := c.DownloadAndWaitProgress(ctx, id, func(p llama.Progress) {
			// progress is sampled on the next poll; the TUI reads llama.progress via done/progress msgs
			_ = p
		})
		if err != nil {
			return llamaActionDoneMsg{gen: gen, err: err, refresh: true}
		}
		return llamaActionDoneMsg{gen: gen, text: "Downloaded " + id, refresh: true}
	}
}

func (m Model) startLlamaLoad(target llama.ModelInfo, replace bool) (tea.Model, tea.Cmd) {
	m.llama.actionGen++
	gen := m.llama.actionGen
	ctx, cancel := context.WithCancel(context.Background())
	m.llama.cancel = cancel
	m.llama.phase = llamaProgress
	m.llama.progressTitle = "Loading model"
	m.llama.progressModel = target.ID
	m.llama.progress = llama.Progress{Message: "Starting…", Ratio: -1}
	c := m.llama.client
	others := m.llama.loadedOthers
	return m, func() tea.Msg {
		if replace {
			for _, model := range others {
				_ = c.UnloadAndWait(ctx, model.ID)
			}
		}
		_, err := c.LoadAndWaitProgress(ctx, target.ID, nil)
		if err != nil {
			if replace {
				for _, model := range others {
					_, _ = c.LoadAndWait(context.Background(), model.ID)
				}
			}
			return llamaActionDoneMsg{gen: gen, err: err, refresh: true}
		}
		return llamaActionDoneMsg{gen: gen, text: "Loaded " + target.ID, refresh: true}
	}
}

func (m Model) handleLlamaKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.llama.phase == llamaProgress {
		if m.keys.Matches(msg.String(), "tui.select.cancel") {
			return m.llamaSelect("Stop "+strings.ToLower(m.llama.progressTitle)+"?\n"+m.llama.progressModel, llamaSelStop, []string{"Yes", "No"}), nil
		}
		return m, nil
	}
	switch m.llama.list.handleKey(msg, m.keys) {
	case "confirm":
		return m.confirmLlama()
	case "cancel":
		return m.cancelLlama()
	case "edit":
		if m.llama.phase == llamaSearch {
			return m.scheduleLlamaSearch()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) confirmLlama() (tea.Model, tea.Cmd) {
	it, ok := m.llama.list.current()
	switch m.llama.phase {
	case llamaCatalog:
		if !ok {
			return m, nil
		}
		if it.ID == llamaDownloadID {
			return m.llamaSearchView(), nil
		}
		var target llama.ModelInfo
		for _, model := range m.llama.catalog {
			if model.ID == it.ID {
				target = model
				break
			}
		}
		if target.ID == "" {
			return m, nil
		}
		if llama.ModelIsLoaded(target) {
			m.llama.loadTarget = target
			return m.llamaSelect("Unload model?\n"+target.ID, llamaSelUnload, []string{"Yes", "No"}), nil
		}
		if target.Status.Value != "unloaded" {
			m.llama.notify = target.ID + " is " + target.Status.Value
			return m, nil
		}
		var loaded []llama.ModelInfo
		for _, model := range m.llama.catalog {
			if model.ID != target.ID && llama.ModelIsLoaded(model) {
				loaded = append(loaded, model)
			}
		}
		m.llama.loadTarget = target
		m.llama.loadedOthers = loaded
		if len(loaded) == 0 {
			return m.startLlamaLoad(target, false)
		}
		noun := " models are"
		if len(loaded) == 1 {
			noun = " model is"
		}
		return m.llamaSelect(fmt.Sprintf("%d%s loaded", len(loaded), noun), llamaSelLoad, []string{"Unload all and load", "Keep loaded and load", "Cancel"}), nil
	case llamaSearch:
		query := strings.TrimSpace(m.llama.list.query)
		picked := ""
		if llama.ExactHuggingFaceID(query) {
			picked = query
		} else if ok {
			picked = it.ID
		}
		if picked == "" {
			return m, nil
		}
		token := m.llama.hfToken
		base := m.llama.hfBaseURL
		repo, _ := llama.ParseHuggingFaceModel(picked)
		m.llama.status = "Loading model details"
		return m, func() tea.Msg {
			details, err := llama.HuggingFaceDetails(context.Background(), repo, token, base)
			return llamaDetailsMsg{details: details, err: err, picked: picked}
		}
	case llamaSelect:
		if !ok {
			return m, nil
		}
		return m.confirmLlamaSelect(it.ID)
	}
	return m, nil
}

func (m Model) confirmLlamaSelect(id string) (tea.Model, tea.Cmd) {
	switch m.llama.selectKind {
	case llamaSelConn:
		if id == "Retry" {
			m.llama.phase = llamaCatalog
			m.llama.status = "Loading…"
			return m, m.fetchLlamaCatalog()
		}
		m.llama.active = false
		return m, nil
	case llamaSelUnload:
		if id != "Yes" {
			m.setLlamaCatalog(m.llama.catalog)
			return m, nil
		}
		target := m.llama.loadTarget
		m.llama.actionGen++
		gen := m.llama.actionGen
		c := m.llama.client
		m.llama.phase = llamaProgress
		m.llama.progressTitle = "Unloading model"
		m.llama.progressModel = target.ID
		m.llama.progress = llama.Progress{Message: "Unloading…", Ratio: -1}
		return m, func() tea.Msg {
			err := c.UnloadAndWait(context.Background(), target.ID)
			if err != nil {
				return llamaActionDoneMsg{gen: gen, err: err, refresh: true}
			}
			return llamaActionDoneMsg{gen: gen, text: "Unloaded " + target.ID, refresh: true}
		}
	case llamaSelLoad:
		if id == "Cancel" {
			m.setLlamaCatalog(m.llama.catalog)
			return m, nil
		}
		return m.startLlamaLoad(m.llama.loadTarget, id == "Unload all and load")
	case llamaSelGated:
		if id != "Continue" {
			return m.llamaSearchView(), nil
		}
		return m.afterLlamaDetails(m.llama.selectRepo)
	case llamaSelQuant:
		idx := m.llama.list.selected
		if idx < 0 || idx >= len(m.llama.details.Quantizations) {
			return m, nil
		}
		q := m.llama.details.Quantizations[idx]
		return m.startLlamaDownload(m.llama.details.ID + ":" + q.Name)
	case llamaSelStop:
		if id != "Yes" {
			m.llama.phase = llamaProgress
			return m, nil
		}
		if m.llama.cancel != nil {
			m.llama.cancel()
		}
		if m.llama.progressModel != "" && m.llama.client != nil {
			_ = m.llama.client.Unload(m.llama.progressModel)
		}
		m.llama.notify = "Cancelled"
		return m, m.fetchLlamaCatalog()
	}
	return m, nil
}

func (m Model) cancelLlama() (tea.Model, tea.Cmd) {
	switch m.llama.phase {
	case llamaSearch, llamaSelect:
		if m.llama.selectKind == llamaSelConn {
			m.llama.active = false
			return m, nil
		}
		if m.llama.selectKind == llamaSelStop {
			m.llama.phase = llamaProgress
			return m, nil
		}
		return m, m.fetchLlamaCatalog()
	default:
		m.llama.active = false
		if m.llama.notify != "" {
			m.transcript = append(m.transcript, entry{role: "meta", rendered: m.metaStyle.Render(m.llama.notify)})
		}
		return m, nil
	}
}

func (p llamaOverlay) view() string {
	if p.phase == llamaProgress {
		var b strings.Builder
		b.WriteString(p.progressTitle)
		b.WriteByte('\n')
		b.WriteString(p.progressModel)
		b.WriteString("\n\n")
		b.WriteString(p.progress.Message)
		b.WriteByte('\n')
		if p.progress.Ratio >= 0 {
			filled := int(p.progress.Ratio * 40)
			if filled > 40 {
				filled = 40
			}
			if filled < 0 {
				filled = 0
			}
			b.WriteString(strings.Repeat("█", filled))
			b.WriteString(strings.Repeat("─", 40-filled))
			b.WriteByte(' ')
			fmt.Fprintf(&b, "%d%%", int(p.progress.Ratio*100))
			b.WriteByte('\n')
		}
		if p.progress.Detail != "" {
			b.WriteString(p.progress.Detail)
			b.WriteByte('\n')
		}
		b.WriteString("\nesc stop")
		return b.String()
	}
	s := p.list.view()
	if p.status != "" && p.phase == llamaSearch {
		s += "\n  " + p.status
	}
	return s
}
