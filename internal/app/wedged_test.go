package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
)

// doctorHome seeds a temporary HOME with config.env and, when body is
// non-empty, a launcher.log for the configured port.
func doctorHome(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("QWEN_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if err := os.MkdirAll(paths.ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	config := "HARNESS=qwen\nMODEL_PATH=/models/m.gguf\nALIAS=local\nCTX=65536\nPORT=8080\n"
	if err := os.WriteFile(paths.Config(), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if body == "" {
		return
	}
	log := paths.LauncherLog("8080")
	if err := os.MkdirAll(filepath.Dir(log), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(log, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func find(cs []Check, name string) (Check, bool) {
	for _, c := range cs {
		if c.Name == name {
			return c, true
		}
	}
	return Check{}, false
}

// A wedged server still answers /health, so the only way doctor can report it
// is from the log.
func TestDoctorReportsAWedgedBackend(t *testing.T) {
	doctorHome(t, "chatter\n"+server.WedgedMarker+" - recreate the backend to recover\n")
	c, ok := find(Doctor(), "server backend")
	if !ok {
		t.Fatal("no server backend check")
	}
	// A warning, never a failure: setup.sh gates its exit code on doctor, and
	// a server that died at runtime is not a broken install.
	if !c.Warn || c.OK {
		t.Fatalf("check = %+v, want a warning", c)
	}
	for _, want := range []string{"8080", "Metal"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("detail %q does not mention %q", c.Detail, want)
		}
	}
}

func TestDoctorStaysQuietWithoutAWedge(t *testing.T) {
	cases := map[string]string{
		"healthy log": "I slot print_timing: id 0 | task 1 | total time = 1200.00 ms\n",
		"no log":      "",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			doctorHome(t, body)
			if c, ok := find(Doctor(), "server backend"); ok {
				t.Fatalf("unexpected check: %+v", c)
			}
		})
	}
}

// The Go and shell sides grep for the same string; the launcher is what
// actually skips a wedged port, so a drift here would silently disarm it.
func TestWedgedMarkerMatchesTheLauncher(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "launchers", "local-harness"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "WEDGED_MARKER='"+server.WedgedMarker+"'") {
		t.Fatalf("local-harness does not set WEDGED_MARKER to %q", server.WedgedMarker)
	}
}
