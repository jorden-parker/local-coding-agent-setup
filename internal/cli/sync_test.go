package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
)

func TestSyncIsolatesLocalAndPortOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("QWEN_HOME", filepath.Join(home, "other-qwen"))
	if err := os.MkdirAll(paths.ConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	config := "ALIAS=local\nPORT=8080\nCTX=65536\n"
	if err := os.WriteFile(paths.Config(), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	ordinary := []byte(`{"model":{"name":"other"},"security":{"auth":{"selectedType":"qwen-oauth"}}}`)
	if err := os.MkdirAll(paths.LegacyQwenDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyQwenSettings(), ordinary, 0600); err != nil {
		t.Fatal(err)
	}
	root := Root()
	root.SetArgs([]string{"sync", "--port", "9000", "--ctx", "32768"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	// A port that differs from config.env's own PORT is isolated: it must not
	// touch the shared default settings file. The one-off CTX reaches Qwen's
	// contextWindowSize for that instance.
	if err := qwen.CheckLocal(paths.QwenSettingsFor("9000", "8080"), qwen.ProviderFor("local", 9000, 32768)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.QwenSettings()); !os.IsNotExist(err) {
		t.Fatal("port override wrote the shared default settings file")
	}

	root = Root()
	root.SetArgs([]string{"sync"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	// The configured port still uses the original, shared settings file.
	if err := qwen.CheckLocal(paths.QwenSettings(), qwen.ProviderFor("local", 8080, 65536)); err != nil {
		t.Fatal(err)
	}

	root = Root()
	root.SetArgs([]string{"sync", "--port", "bad"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("invalid port: %v", err)
	}
	root = Root()
	root.SetArgs([]string{"sync", "--ctx", "100"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "CTX") {
		t.Fatalf("invalid ctx: %v", err)
	}
	after, _ := os.ReadFile(paths.LegacyQwenSettings())
	if string(after) != string(ordinary) {
		t.Fatal("ordinary Qwen changed")
	}
	after, _ = os.ReadFile(paths.Config())
	if string(after) != config {
		t.Fatal("runtime override persisted")
	}
	if _, err := os.Stat(filepath.Join(home, "other-qwen")); !os.IsNotExist(err) {
		t.Fatal("inherited QWEN_HOME used")
	}
}
