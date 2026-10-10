package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/pi"
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
	// Nor is an isolated port's server offered to ordinary Qwen.
	if after, _ := os.ReadFile(paths.LegacyQwenSettings()); string(after) != string(ordinary) {
		t.Fatal("port override changed ordinary Qwen")
	}

	root = Root()
	root.SetArgs([]string{"sync"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	// The configured port still uses the original, shared settings file.
	want := qwen.ProviderFor("local", 8080, 65536)
	if err := qwen.CheckLocal(paths.QwenSettings(), want); err != nil {
		t.Fatal(err)
	}
	// Ordinary Qwen gains the provider entry and placeholder key for the
	// configured port, but keeps its own model and auth choice.
	if err := qwen.CheckShared(paths.LegacyQwenSettings(), want); err != nil {
		t.Fatal(err)
	}
	var shared map[string]any
	b, _ := os.ReadFile(paths.LegacyQwenSettings())
	if err := json.Unmarshal(b, &shared); err != nil {
		t.Fatal(err)
	}
	if shared["model"].(map[string]any)["name"] != "other" || shared["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "qwen-oauth" {
		t.Fatalf("ordinary Qwen's selection changed: %s", b)
	}
	if shared["env"].(map[string]any)["OPENAI_API_KEY"] != "local" {
		t.Fatalf("placeholder key missing: %s", b)
	}
	if _, lean := shared["tools"]; lean {
		t.Fatalf("lean profile applied to ordinary Qwen: %s", b)
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
	if after, _ := os.ReadFile(paths.Config()); string(after) != config {
		t.Fatal("runtime override persisted")
	}
	if _, err := os.Stat(filepath.Join(home, "other-qwen")); !os.IsNotExist(err) {
		t.Fatal("inherited QWEN_HOME used")
	}
}

func TestSyncPiIsolatesInstancesAndLeavesTheUsersOwnPiAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(home, "inherited"))
	if err := os.MkdirAll(paths.ConfigDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config(), []byte("ALIAS=local\nPORT=8080\nCTX=65536\nTHINKING=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The user's own pi configuration is never a sync target.
	ownModels := []byte(`{"providers":{"anthropic":{"apiKey":"secret"}}}`)
	if err := os.MkdirAll(paths.LegacyPiDir(), 0755); err != nil {
		t.Fatal(err)
	}
	ownPath := filepath.Join(paths.LegacyPiDir(), "models.json")
	if err := os.WriteFile(ownPath, ownModels, 0600); err != nil {
		t.Fatal(err)
	}

	root := Root()
	root.SetArgs([]string{"sync", "--harness", "pi", "--port", "9000", "--ctx", "32768"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	want := pi.ProviderFor("local", 9000, 32768, true)
	if err := pi.CheckModels(paths.PiModelsFor("9000", "8080"), want); err != nil {
		t.Fatal(err)
	}
	if err := pi.CheckSettings(paths.PiSettingsFor("9000", "8080"), want); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.PiModels()); !os.IsNotExist(err) {
		t.Fatal("port override wrote the default instance")
	}
	if _, err := os.Stat(paths.QwenSettings()); !os.IsNotExist(err) {
		t.Fatal("--harness pi touched Qwen Code")
	}
	if after, _ := os.ReadFile(ownPath); string(after) != string(ownModels) {
		t.Fatal("the user's own ~/.pi/agent/models.json changed")
	}
	if _, err := os.Stat(filepath.Join(home, "inherited")); !os.IsNotExist(err) {
		t.Fatal("inherited PI_CODING_AGENT_DIR used")
	}

	// Once the managed directory exists, a plain sync keeps both harnesses in
	// step, still on config.env's own port.
	root = Root()
	root.SetArgs([]string{"sync", "--harness", "pi"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	root = Root()
	root.SetArgs([]string{"sync"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := pi.CheckModels(paths.PiModels(), pi.ProviderFor("local", 8080, 65536, true)); err != nil {
		t.Fatal(err)
	}
	if err := qwen.CheckLocal(paths.QwenSettings(), qwen.ProviderFor("local", 8080, 65536)); err != nil {
		t.Fatal(err)
	}

	root = Root()
	root.SetArgs([]string{"sync", "--harness", "codex"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "qwen or pi") {
		t.Fatalf("unknown harness: %v", err)
	}
}
