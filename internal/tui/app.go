// Package tui is the interactive lca screen: stats, config and doctor tabs.
package tui

import (
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
)

type tab int

const (
	tabStats tab = iota
	tabConfig
	tabDoctor
)

const livePeriod = 2 * time.Second

type model struct {
	tab           tab
	width, height int
	isDark        bool
	st            styles
	stats         statsScreen
	cfg           *configScreen
	doctor        doctorScreen
	status        string
	statusErr     bool
	quitting      bool
}

// Run starts the interactive UI.
func Run() error {
	_, err := tea.NewProgram(newModel()).Run()
	return err
}

func newModel() *model {
	m := &model{isDark: true, st: newStyles(true), stats: newStatsScreen()}
	m.stats.setStyles(m.st)
	if f, err := app.LoadConfig(); err == nil {
		if p, _ := f.Get("PORT"); p != "" {
			m.stats.port, _ = strconv.Atoi(p)
		}
	}
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, loadStats, m.tick())
}

func (m *model) tick() tea.Cmd {
	if m.stats.port == 0 {
		return nil
	}
	return tea.Tick(livePeriod, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.isDark = msg.IsDark()
		m.st = newStyles(m.isDark)
		m.stats.setStyles(m.st)
		if m.cfg != nil && m.cfg.form != nil {
			m.cfg.form = m.cfg.form.WithTheme(formTheme(m.isDark))
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.stats.layout(m.width, m.height)
		if m.cfg != nil && m.cfg.form != nil {
			m.cfg.form = m.cfg.form.WithWidth(max(20, m.width-4)).WithHeight(max(5, m.height-6))
		}
		return m, nil
	case statsLoadedMsg:
		m.stats.loaded = true
		m.stats.err = msg.err
		if msg.err == nil {
			m.stats.data = msg.s
			m.stats.refill()
		}
		return m, nil
	case tickMsg:
		if m.tab != tabStats {
			return m, m.tick()
		}
		return m, tea.Batch(scrape(m.stats.port), m.tick())
	case liveMsg:
		if msg.err != nil {
			m.stats.live = m.st.subtle.Render("server not running on port " + strconv.Itoa(m.stats.port))
		} else {
			m.stats.live = msg.m.Summary()
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.tab == tabConfig && m.cfg != nil && m.cfg.form != nil {
			return m.updateForm(msg)
		}
		return m.updateKeys(msg)
	}
	if m.tab == tabConfig && m.cfg != nil && m.cfg.form != nil {
		return m.updateForm(msg)
	}
	return m, nil
}

func (m *model) updateKeys(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "1":
		return m, m.switchTo(tabStats)
	case "2":
		return m, m.switchTo(tabConfig)
	case "3":
		return m, m.switchTo(tabDoctor)
	case "tab":
		return m, m.switchTo((m.tab + 1) % 3)
	case "shift+tab":
		return m, m.switchTo((m.tab + 2) % 3)
	}
	switch m.tab {
	case tabStats:
		switch k.String() {
		case "s":
			m.stats.toggle()
			return m, nil
		case "r":
			m.stats.loaded = false
			return m, loadStats
		}
		t, cmd := m.stats.table.Update(k)
		m.stats.table = t
		return m, cmd
	case tabDoctor:
		if k.String() == "r" {
			m.doctor.run()
		}
	}
	return m, nil
}

func (m *model) switchTo(t tab) tea.Cmd {
	m.tab = t
	m.status, m.statusErr = "", false
	switch t {
	case tabConfig:
		c := newConfigScreen(m.isDark, m.width, m.height)
		m.cfg = &c
		if c.err != nil {
			m.status, m.statusErr = c.err.Error(), true
			return nil
		}
		return c.form.Init()
	case tabDoctor:
		m.doctor.run()
	}
	return nil
}

func (m *model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	f, cmd := m.cfg.form.Update(msg)
	if form, ok := f.(*huh.Form); ok {
		m.cfg.form = form
	}
	switch m.cfg.form.State {
	case huh.StateAborted:
		m.cfg = nil
		m.tab = tabStats
		m.status = "Edit cancelled. Nothing written."
		return m, nil
	case huh.StateCompleted:
		status, isErr := m.cfg.commit()
		m.cfg = nil
		m.tab = tabStats
		m.status, m.statusErr = status, isErr
		return m, tea.Batch(loadStats)
	}
	return m, cmd
}

func (m *model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	if m.quitting {
		return v
	}
	if m.width == 0 {
		v.SetContent("Opening lca…")
		return v
	}
	if m.width < 40 || m.height < 12 {
		v.SetContent(lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render("A little more room?\nResize to at least 40 × 12.\nq quits"))
		return v
	}
	var body string
	switch m.tab {
	case tabStats:
		body = m.stats.view(m.st, m.width)
	case tabConfig:
		if m.cfg != nil && m.cfg.form != nil {
			body = m.cfg.form.View()
		} else {
			body = m.st.err.Render(m.status)
		}
	case tabDoctor:
		body = m.doctor.view(m.st)
	}
	help := map[tab]string{
		tabStats:  "s source · r reload · ↑↓ scroll · 1/2/3 tab · q quit",
		tabConfig: "enter next · esc cancel · tab next field",
		tabDoctor: "r re-run · 1/2/3 tab · q quit",
	}[m.tab]
	status := m.status
	if status != "" {
		if m.statusErr {
			status = m.st.err.Render(status)
		} else {
			status = m.st.ok.Render(status)
		}
	}
	content := m.tabs() + "\n" + body + "\n" + status + "\n" + m.st.subtle.Render(help)
	v.SetContent(lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(content))
	return v
}

func (m *model) tabs() string {
	names := []string{"1 Stats", "2 Config", "3 Doctor"}
	var parts []string
	for i, n := range names {
		if tab(i) == m.tab {
			parts = append(parts, m.st.tabOn.Render(n))
		} else {
			parts = append(parts, m.st.tab.Render(n))
		}
	}
	return m.st.title.Render("lca") + "  " + strings.Join(parts, " ")
}
