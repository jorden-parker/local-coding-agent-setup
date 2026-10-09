package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
	"github.com/jorden-parker/local-coding-agent-setup/internal/stats"
)

const statsDays = 30

// statsLoadedMsg carries freshly collected statistics.
type statsLoadedMsg struct {
	s   app.Stats
	err error
}

// liveMsg carries one /metrics scrape; err is set when the server is down.
type liveMsg struct {
	m   server.Metrics
	err error
}

type tickMsg time.Time

type statsScreen struct {
	src    app.Source
	data   app.Stats
	err    error
	table  table.Model
	live   string
	port   int
	loaded bool
}

func newStatsScreen() statsScreen {
	t := table.New(table.WithFocused(true))
	return statsScreen{src: app.SourceQwen, table: t}
}

func loadStats() tea.Msg {
	s, err := app.CollectStats(statsDays, "", app.SourceBoth)
	return statsLoadedMsg{s: s, err: err}
}

func scrape(port int) tea.Cmd {
	return func() tea.Msg {
		m, err := server.Scrape(port)
		return liveMsg{m: m, err: err}
	}
}

func (s *statsScreen) buckets() []stats.Bucket {
	if s.src == app.SourceServer {
		return s.data.Server
	}
	return s.data.Qwen
}

func (s *statsScreen) toggle() {
	if s.src == app.SourceServer {
		s.src = app.SourceQwen
	} else {
		s.src = app.SourceServer
	}
	s.refill()
}

func (s *statsScreen) refill() {
	cols := []table.Column{{Title: "day", Width: 10}, {Title: "model", Width: 16}, {Title: "n", Width: 5}, {Title: "p50", Width: 8}, {Title: "p95", Width: 8}, {Title: "mean", Width: 8}, {Title: "in tok", Width: 8}, {Title: "out tok", Width: 8}}
	if s.src == app.SourceServer {
		cols = append(cols, table.Column{Title: "prompt t/s", Width: 10}, table.Column{Title: "gen t/s", Width: 8})
	}
	var rows []table.Row
	for _, b := range s.buckets() {
		r := table.Row{b.Day, b.Model, fmt.Sprint(b.N), app.FormatMs(b.P50), app.FormatMs(b.P95), app.FormatMs(b.Mean), fmt.Sprint(b.InTok), fmt.Sprint(b.OutTok)}
		if s.src == app.SourceServer {
			r = append(r, fmt.Sprintf("%.1f", b.PromptTPS), fmt.Sprintf("%.1f", b.GenTPS))
		}
		rows = append(rows, r)
	}
	s.table.SetRows(nil)
	s.table.SetColumns(cols)
	s.table.SetRows(rows)
	s.table.GotoBottom()
}

func (s *statsScreen) layout(width, height int) {
	s.table.SetWidth(max(10, width-4))
	s.table.SetHeight(max(3, height-9))
}

func (s *statsScreen) setStyles(st styles) {
	ts := table.DefaultStyles()
	ts.Header = st.header
	ts.Selected = st.selected
	ts.Cell = st.kv
	s.table.SetStyles(ts)
}

func (s *statsScreen) view(st styles, width int) string {
	var b strings.Builder
	src := "Qwen Code API calls (apiDurationMs) from " + paths.Tildify(paths.QwenUsageDir())
	if s.src == app.SourceServer {
		src = "llama-server requests (total time) from " + paths.Tildify(paths.Timings())
	}
	b.WriteString(st.subtle.Render(fmt.Sprintf("Last %d days · %s", statsDays, src)) + "\n")
	switch {
	case s.err != nil:
		b.WriteString(st.err.Render(s.err.Error()) + "\n")
	case !s.loaded:
		b.WriteString(st.subtle.Render("loading…") + "\n")
	case len(s.buckets()) == 0:
		b.WriteString(st.subtle.Render("no data yet. Run llama-coder and qwen-local, then press r.") + "\n")
	default:
		b.WriteString(s.table.View() + "\n")
		days, vals := stats.DailyP50(s.buckets())
		b.WriteString(st.label.Render("daily p50 ") + st.title.Render(stats.Sparkline(vals, max(8, min(40, width-24)))) + st.subtle.Render("  "+days[0]+" .. "+days[len(days)-1]) + "\n")
		b.WriteString(st.subtle.Render(s.summary()) + "\n")
	}
	if s.live != "" {
		b.WriteString(st.subtle.Render("live  ") + s.live + "\n")
	}
	return b.String()
}

func (s *statsScreen) summary() string {
	bs := s.buckets()
	n, in, out := 0, 0, 0
	var all []float64
	for _, b := range bs {
		n += b.N
		in += b.InTok
		out += b.OutTok
		all = append(all, b.P50)
	}
	return fmt.Sprintf("%d requests · %d prompt tokens · %d generated · median of daily p50 %s", n, in, out, app.FormatMs(stats.Percentile(all, 50)))
}
