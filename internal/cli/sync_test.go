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
	for _, tc := range []struct {
		args []string
		port int
	}{{[]string{"sync", "--port", "9000"}, 9000}, {[]string{"sync"}, 8080}} {
		root := Root()
		root.SetArgs(tc.args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if err := qwen.CheckLocal(paths.QwenSettings(), qwen.ProviderFor("local", tc.port, 65536)); err != nil {
			t.Fatal(err)
		}
	}
	root := Root()
	root.SetArgs([]string{"sync", "--port", "bad"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "PORT") {
		t.Fatalf("invalid port: %v", err)
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
