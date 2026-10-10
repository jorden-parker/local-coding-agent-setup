package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestQwenUsageDirsIncludesInstances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	// qwen-8082 has no usage directory yet and must be skipped.
	for _, d := range []string{"qwen/usage", "qwen-9000/usage", "qwen-8081/usage", "qwen-8082"} {
		if err := os.MkdirAll(filepath.Join(ConfigDir(), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		QwenUsageDir(),
		filepath.Join(ConfigDir(), "qwen-8081", "usage"),
		filepath.Join(ConfigDir(), "qwen-9000", "usage"),
		LegacyQwenUsageDir(),
	}
	if got := QwenUsageDirs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPiSessionDirsIncludesInstances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	// pi-8082 has no sessions directory yet and must be skipped.
	for _, d := range []string{"pi/sessions", "pi-9000/sessions", "pi-8081/sessions", "pi-8082"} {
		if err := os.MkdirAll(filepath.Join(ConfigDir(), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		filepath.Join(PiDir(), "sessions"),
		filepath.Join(ConfigDir(), "pi-8081", "sessions"),
		filepath.Join(ConfigDir(), "pi-9000", "sessions"),
		LegacyPiSessionDir(),
	}
	if got := PiSessionDirs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPiInstanceDirIsolatesOtherPorts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	for _, tc := range []struct{ port, cfgPort, want string }{
		{"", "8080", PiDir()},
		{"8080", "8080", PiDir()},
		{"9000", "8080", filepath.Join(ConfigDir(), "pi-9000")},
	} {
		if got := PiInstanceDir(tc.port, tc.cfgPort); got != tc.want {
			t.Errorf("PiInstanceDir(%q, %q) = %q want %q", tc.port, tc.cfgPort, got, tc.want)
		}
	}
	if got := PiModelsFor("9000", "8080"); got != filepath.Join(ConfigDir(), "pi-9000", "models.json") {
		t.Errorf("PiModelsFor: %q", got)
	}
	if got := PiSettings(); got != filepath.Join(PiDir(), "settings.json") {
		t.Errorf("PiSettings: %q", got)
	}
}

func TestSkillDirs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	for _, c := range []struct{ got, want string }{
		{QwenSkillsDir(), filepath.Join(dir, "config", "llama-coder", "qwen", "skills")},
		{PiSkillsDir(), filepath.Join(dir, "config", "llama-coder", "pi", "skills")},
		{LegacyQwenSkillsDir(), filepath.Join(dir, ".qwen", "skills")},
		{LegacyPiSkillsDir(), filepath.Join(dir, ".pi", "agent", "skills")},
		// ~/.agents/skills follows HOME, not XDG_CONFIG_HOME: that is why
		// moving QWEN_HOME or PI_CODING_AGENT_DIR never hides it.
		{AgentsSkillsDir(), filepath.Join(dir, ".agents", "skills")},
	} {
		if c.got != c.want {
			t.Errorf("got %s want %s", c.got, c.want)
		}
	}
	// The tilde literals the harness settings carry must name these same dirs.
	for _, c := range []struct{ literal, dir string }{
		{"~/.qwen/skills", LegacyQwenSkillsDir()},
		{"~/.pi/agent/skills", LegacyPiSkillsDir()},
	} {
		if got := Tildify(c.dir); got != c.literal {
			t.Errorf("Tildify(%s) = %s, want %s", c.dir, got, c.literal)
		}
	}
}
