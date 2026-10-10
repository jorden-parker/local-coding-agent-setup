package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorden-parker/local-coding-agent-setup/internal/backup"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/pi"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
)

// syncHome points lca at a temporary HOME holding config.env, and clears any
// harness override so the default ~/.qwen and ~/.pi/agent are the targets.
func syncHome(t *testing.T, config string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("QWEN_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if err := os.MkdirAll(paths.ConfigDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Config(), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

func run(t *testing.T, args ...string) {
	t.Helper()
	root := Root()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return m
}

// lca patches the configuration Qwen Code already owns, writing only the
// provider entry and the placeholder key, and keeping every other setting.
func TestSyncPatchesTheUsersOwnQwenSettings(t *testing.T) {
	syncHome(t, "ALIAS=local\nPORT=8080\nCTX=65536\n")
	theirs := []byte(`{"model":{"name":"other"},"security":{"auth":{"selectedType":"qwen-oauth"}},"ui":{"theme":"mine"}}`)
	if err := os.MkdirAll(paths.QwenDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.QwenSettings(), theirs, 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, "sync", "--harness", "qwen")

	if err := qwen.Check(paths.QwenSettings(), qwen.ProviderFor("local", 8080, 65536)); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, paths.QwenSettings())
	// Their own selection and unrelated settings are untouched.
	if m["model"].(map[string]any)["name"] != "other" {
		t.Fatalf("model selection changed: %+v", m["model"])
	}
	if m["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "qwen-oauth" {
		t.Fatalf("auth changed: %+v", m["security"])
	}
	if m["ui"].(map[string]any)["theme"] != "mine" {
		t.Fatal("unrelated setting lost")
	}
	if m["env"].(map[string]any)["OPENAI_API_KEY"] != "local" {
		t.Fatalf("placeholder key missing: %+v", m["env"])
	}
	// Lean belongs to lca qwen-profile, never to a sync that runs on every launch.
	for _, key := range []string{"tools", "memory", "skills"} {
		if _, exists := m[key]; exists {
			t.Fatalf("sync wrote %s; lean must stay opt-in", key)
		}
	}
	// The original is kept once, not once per sync.
	found, err := backup.Existing(paths.QwenSettings())
	if err != nil || len(found) != 1 {
		t.Fatalf("backups = %v, %v, want one", found, err)
	}
	if b, _ := os.ReadFile(found[0]); string(b) != string(theirs) {
		t.Fatalf("backup = %s, want the original", b)
	}
	run(t, "sync", "--harness", "qwen")
	if found, _ := backup.Existing(paths.QwenSettings()); len(found) != 1 {
		t.Fatalf("second sync added a backup: %v", found)
	}
}

// pi learns the port from models.json alone — it has no base-URL flag — so a
// runtime --port must reach the provider entry, without saving to config.env.
func TestSyncPatchesPiModelsAndHonoursRuntimeOverrides(t *testing.T) {
	syncHome(t, "ALIAS=local\nPORT=8080\nCTX=65536\nHARNESS=pi\n")
	theirs := []byte(`{"providers":{"anthropic":{"apiKey":"secret"}}}`)
	if err := os.MkdirAll(paths.PiDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.PiModels(), theirs, 0o600); err != nil {
		t.Fatal(err)
	}
	piSettings := filepath.Join(paths.PiDir(), "settings.json")
	ownSettings := []byte(`{"defaultProvider":"anthropic","defaultModel":"opus"}`)
	if err := os.WriteFile(piSettings, ownSettings, 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, "sync", "--harness", "pi", "--port", "9000", "--ctx", "32768")

	if err := pi.CheckModels(paths.PiModels(), pi.ProviderFor("local", 9000, 32768, false)); err != nil {
		t.Fatal(err)
	}
	if m := readJSON(t, paths.PiModels()); m["providers"].(map[string]any)["anthropic"].(map[string]any)["apiKey"] != "secret" {
		t.Fatal("the user's own provider lost")
	}
	// pi's settings.json is the user's selection; lca never writes it.
	if after, _ := os.ReadFile(piSettings); string(after) != string(ownSettings) {
		t.Fatalf("pi settings.json rewritten: %s", after)
	}
	// The override is for this run only.
	b, _ := os.ReadFile(paths.Config())
	if !strings.Contains(string(b), "PORT=8080") || !strings.Contains(string(b), "CTX=65536") {
		t.Fatalf("config.env changed: %s", b)
	}
}

// An exported QWEN_HOME or PI_CODING_AGENT_DIR is where the harness reads its
// configuration, so it is the file lca must patch.
func TestSyncFollowsAnExportedHarnessDir(t *testing.T) {
	home := syncHome(t, "ALIAS=local\nPORT=8080\nCTX=65536\n")
	elsewhere := filepath.Join(home, "work-profile")
	t.Setenv("QWEN_HOME", elsewhere)

	run(t, "sync", "--harness", "qwen")

	if err := qwen.Check(filepath.Join(elsewhere, "settings.json"), qwen.ProviderFor("local", 8080, 65536)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".qwen", "settings.json")); !os.IsNotExist(err) {
		t.Fatal("~/.qwen was patched instead of QWEN_HOME")
	}
}

// Without --harness, lca patches the selected harness and any whose config it
// has already adopted — never one merely installed on the machine.
func TestSyncPatchesOnlyAdoptedHarnesses(t *testing.T) {
	syncHome(t, "ALIAS=local\nPORT=8080\nCTX=65536\n")

	run(t, "sync")

	if err := qwen.Check(paths.QwenSettings(), qwen.ProviderFor("local", 8080, 65536)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.PiModels()); !os.IsNotExist(err) {
		t.Fatal("pi was synced without being selected or adopted")
	}

	// Once pi carries the entry, a later sync keeps it in step.
	run(t, "sync", "--harness", "pi")
	run(t, "sync")
	if err := pi.CheckModels(paths.PiModels(), pi.ProviderFor("local", 8080, 65536, false)); err != nil {
		t.Fatal(err)
	}
}

func TestSyncReportsBadConfigAndUnknownHarness(t *testing.T) {
	cases := map[string]struct{ config, harness, want string }{
		"bad port":        {"ALIAS=local\nPORT=x\nCTX=65536\n", "qwen", "PORT"},
		"bad ctx":         {"ALIAS=local\nPORT=8080\nCTX=x\n", "qwen", "CTX"},
		"unknown harness": {"ALIAS=local\nPORT=8080\nCTX=65536\n", "zed", "qwen or pi"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			syncHome(t, c.config)
			root := Root()
			root.SetArgs([]string{"sync", "--harness", c.harness})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one naming %s", err, c.want)
			}
		})
	}
}

// unsync is the way back out of a file lca patched.
func TestUnsyncRemovesOnlyLcasOwnEntries(t *testing.T) {
	syncHome(t, "ALIAS=local\nPORT=8080\nCTX=65536\n")
	theirs := []byte(`{"model":{"name":"other"},"security":{"auth":{"selectedType":"qwen-oauth"}},"ui":{"theme":"mine"}}`)
	if err := os.MkdirAll(paths.QwenDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.QwenSettings(), theirs, 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, "sync", "--harness", "qwen")

	// A dry run reports without writing.
	before, _ := os.ReadFile(paths.QwenSettings())
	run(t, "unsync", "--harness", "qwen", "--dry-run")
	if after, _ := os.ReadFile(paths.QwenSettings()); string(after) != string(before) {
		t.Fatal("--dry-run wrote the file")
	}

	run(t, "unsync", "--harness", "qwen")
	if _, ok, err := qwen.Entry(paths.QwenSettings(), "local"); err != nil || ok {
		t.Fatalf("provider still listed: %v", err)
	}
	m := readJSON(t, paths.QwenSettings())
	if m["model"].(map[string]any)["name"] != "other" || m["ui"].(map[string]any)["theme"] != "mine" {
		t.Fatalf("the user's own settings changed: %+v", m)
	}
	if _, exists := m["env"]; exists {
		t.Fatalf("placeholder key left behind: %+v", m["env"])
	}
}

// A models.json lca created, holding nothing else, is removed outright. The
// absence of a backup is what proves lca wrote it from nothing.
func TestUnsyncDeletesAConfigLcaCreated(t *testing.T) {
	syncHome(t, "ALIAS=local\nPORT=8080\nCTX=65536\nHARNESS=pi\n")
	run(t, "sync", "--harness", "pi")
	if _, err := os.Stat(paths.PiModels()); err != nil {
		t.Fatal(err)
	}
	run(t, "unsync", "--harness", "pi")
	if _, err := os.Stat(paths.PiModels()); !os.IsNotExist(err) {
		t.Fatalf("models.json kept: %v", err)
	}
}
