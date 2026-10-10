package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	return home
}

func seed(t *testing.T, home string) {
	t.Helper()
	model := filepath.Join(home, "m.gguf")
	_ = os.WriteFile(model, nil, 0o600)
	cfg := filepath.Join(home, ".config", "llama-coder", "config.env")
	_ = os.MkdirAll(filepath.Dir(cfg), 0o755)
	_ = os.WriteFile(cfg, []byte("# seeded\nMODEL_PATH="+model+"\nALIAS=qwen3.5-9b\nCTX=65536\nPORT=8080\nTHINKING=false\nEXTRA_ARGS=\n"), 0o600)
	usage := filepath.Join(home, ".qwen", "usage")
	_ = os.MkdirAll(usage, 0o755)
	_ = os.WriteFile(filepath.Join(usage, "token-usage-2026-10.jsonl"), []byte(`{"schemaVersion":1,"id":"a","timestamp":"2026-10-08T13:06:07.876Z","localDate":"2026-10-08","model":"qwen3.5-9b","source":"main","inputTokens":21154,"outputTokens":10,"apiDurationMs":234698}`+"\n"), 0o600)
	state := filepath.Join(home, ".local", "state", "llama-coder")
	_ = os.MkdirAll(state, 0o755)
	_ = os.WriteFile(filepath.Join(state, "server-20261008T120000Z-qwen3.5-9b.log"), []byte(
		"0.00.500.000 slot print_timing: id  0 | task 1 | prompt eval time =     500.00 ms /    10 tokens (   50.00 ms per token,    20.00 tokens per second)\n"+
			"0.00.500.000 slot print_timing: id  0 | task 1 |        eval time =     250.00 ms /     5 tokens (   50.00 ms per token,    20.00 tokens per second)\n"+
			"0.00.500.000 slot print_timing: id  0 | task 1 |       total time =     750.00 ms /    15 tokens\n"), 0o600)
}

func press(m *model, s string) {
	var k tea.KeyPressMsg
	switch s {
	case "esc":
		k = tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		k = tea.KeyPressMsg{Code: tea.KeyTab}
	default:
		k = tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	m.Update(k)
}

func TestEmptyHomeRenders(t *testing.T) {
	isolate(t)
	m := newModel()
	if got := m.View().Content; !strings.Contains(got, "Opening") {
		t.Errorf("pre-size view: %q", got)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(loadStats())
	for _, tab := range []string{"1", "2", "3"} {
		press(m, tab)
		v := m.View().Content
		if v == "" {
			t.Errorf("tab %s rendered nothing", tab)
		}
		if lines := strings.Split(v, "\n"); len(lines) > 24 {
			t.Errorf("tab %s: %d lines > 24", tab, len(lines))
		}
	}
	if !strings.Contains(m.View().Content, "FAIL") {
		t.Error("doctor on empty HOME should fail a check")
	}
}

func TestConfigEscWritesNothing(t *testing.T) {
	home := isolate(t)
	seed(t, home)
	cfg := filepath.Join(home, ".config", "llama-coder", "config.env")
	before, _ := os.ReadFile(cfg)
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	press(m, "2")
	if m.cfg == nil || m.cfg.form == nil {
		t.Fatal("config form not opened")
	}
	press(m, "esc")
	if m.cfg != nil || m.tab != tabStats {
		t.Errorf("esc did not leave the form: tab=%d", m.tab)
	}
	after, _ := os.ReadFile(cfg)
	if string(before) != string(after) {
		t.Error("config.env changed")
	}
	// Leaving the form without saving must not patch the user's own harness
	// configuration either.
	for _, f := range []string{
		filepath.Join(home, ".qwen", "settings.json"),
		filepath.Join(home, ".pi", "agent", "models.json"),
	} {
		if _, err := os.Stat(f); err == nil {
			t.Error("harness configuration was written: " + f)
		}
	}
}

func TestSeededStatsShowRows(t *testing.T) {
	home := isolate(t)
	seed(t, home)
	m := newModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(loadStats())
	v := m.View().Content
	if !strings.Contains(v, "2026-10-08") || !strings.Contains(v, "qwen3.5-9b") || !strings.Contains(v, "3m54s") {
		t.Errorf("qwen row missing:\n%s", v)
	}
	press(m, "s")
	v = m.View().Content
	if !strings.Contains(v, "llama-server") || !strings.Contains(v, "750 ms") {
		t.Errorf("server row missing:\n%s", v)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "llama-coder", "timings.jsonl")); err != nil {
		t.Error("compaction did not write timings.jsonl")
	}
	press(m, "tab")
	if m.tab != tabConfig {
		t.Errorf("tab key: %d", m.tab)
	}
}
