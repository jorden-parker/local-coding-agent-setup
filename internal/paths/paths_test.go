package paths

import (
	"path/filepath"
	"testing"
)

// setHome points every path helper at a temporary directory.
func setHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("QWEN_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	return home
}

func TestHarnessDirsDefaultToTheHarnessOwnDirectories(t *testing.T) {
	home := setHome(t)
	cases := map[string]struct{ got, want string }{
		"qwen dir":      {QwenDir(), filepath.Join(home, ".qwen")},
		"qwen settings": {QwenSettings(), filepath.Join(home, ".qwen", "settings.json")},
		"qwen usage":    {QwenUsageDir(), filepath.Join(home, ".qwen", "usage")},
		"qwen skills":   {QwenSkillsDir(), filepath.Join(home, ".qwen", "skills")},
		"pi dir":        {PiDir(), filepath.Join(home, ".pi", "agent")},
		"pi models":     {PiModels(), filepath.Join(home, ".pi", "agent", "models.json")},
		"pi sessions":   {PiSessionDir(), filepath.Join(home, ".pi", "agent", "sessions")},
		"pi skills":     {PiSkillsDir(), filepath.Join(home, ".pi", "agent", "skills")},
		"agents skills": {AgentsSkillsDir(), filepath.Join(home, ".agents", "skills")},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", name, c.got, c.want)
		}
	}
}

// An exported QWEN_HOME or PI_CODING_AGENT_DIR is where the harness itself
// reads its configuration, so it is the file lca must patch.
func TestHarnessDirsFollowAnAbsoluteOverride(t *testing.T) {
	setHome(t)
	elsewhere := t.TempDir()
	t.Setenv("QWEN_HOME", elsewhere)
	t.Setenv("PI_CODING_AGENT_DIR", elsewhere)
	cases := map[string]struct{ got, want string }{
		"qwen dir":      {QwenDir(), elsewhere},
		"qwen settings": {QwenSettings(), filepath.Join(elsewhere, "settings.json")},
		"qwen usage":    {QwenUsageDir(), filepath.Join(elsewhere, "usage")},
		"pi dir":        {PiDir(), elsewhere},
		"pi models":     {PiModels(), filepath.Join(elsewhere, "models.json")},
		"pi sessions":   {PiSessionDir(), filepath.Join(elsewhere, "sessions")},
	}
	for name, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", name, c.got, c.want)
		}
	}
}

// A relative override would resolve against each process's working directory,
// so lca and the harness could disagree about which file is theirs.
func TestHarnessDirsIgnoreARelativeOverride(t *testing.T) {
	home := setHome(t)
	t.Setenv("QWEN_HOME", "relative/qwen")
	t.Setenv("PI_CODING_AGENT_DIR", "relative/pi")
	if got, want := QwenDir(), filepath.Join(home, ".qwen"); got != want {
		t.Errorf("QwenDir() = %s, want %s", got, want)
	}
	if got, want := PiDir(), filepath.Join(home, ".pi", "agent"); got != want {
		t.Errorf("PiDir() = %s, want %s", got, want)
	}
}

// AgentsSkillsDir is read from HOME by both harnesses, so no override moves it.
func TestAgentsSkillsDirIgnoresHarnessOverrides(t *testing.T) {
	home := setHome(t)
	t.Setenv("QWEN_HOME", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	if got, want := AgentsSkillsDir(), filepath.Join(home, ".agents", "skills"); got != want {
		t.Errorf("AgentsSkillsDir() = %s, want %s", got, want)
	}
}

func TestTildifyShortensOnlyUnderHome(t *testing.T) {
	home := setHome(t)
	cases := map[string]struct{ in, want string }{
		"settings":    {QwenSettings(), "~/.qwen/settings.json"},
		"pi models":   {PiModels(), "~/.pi/agent/models.json"},
		"home itself": {home, "~"},
		"outside":     {"/etc/hosts", "/etc/hosts"},
		"prefix only": {home + "-other/x", home + "-other/x"},
	}
	for name, c := range cases {
		if got := Tildify(c.in); got != c.want {
			t.Errorf("%s: Tildify(%s) = %s, want %s", name, c.in, got, c.want)
		}
	}
}

// lca writes config.env and its own state under XDG, not in the harness dirs.
func TestLcaOwnDirsStayUnderXdg(t *testing.T) {
	home := setHome(t)
	if got, want := Config(), filepath.Join(home, ".config", "llama-coder", "config.env"); got != want {
		t.Errorf("Config() = %s, want %s", got, want)
	}
	if got, want := Timings(), filepath.Join(home, ".state", "llama-coder", "timings.jsonl"); got != want {
		t.Errorf("Timings() = %s, want %s", got, want)
	}
}
