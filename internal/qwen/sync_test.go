package qwen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareSelectsOnlyWhenNothingChosen(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".qwen", "settings.json")
	p := ProviderFor("local", 8080, 65536)
	if changed, err := Prepare(path, p); err != nil || !changed {
		t.Fatalf("first sync: %v %v", changed, err)
	}
	if err := Check(path, p); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if changed, err := Prepare(path, p); err != nil || changed {
		t.Fatalf("repeat: %v %v", changed, err)
	}
	if repeated, _ := os.ReadFile(path); string(original) != string(repeated) {
		t.Fatal("repeat rewrote settings")
	}
	m, _, _ := loadSettings(path)
	if m["env"].(map[string]any)["OPENAI_API_KEY"] != localAPIKey {
		t.Fatalf("env: %#v", m["env"])
	}
	// With no auth chosen, the local model becomes the default.
	if m["model"].(map[string]any)["name"] != "local" || m["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "openai" {
		t.Fatal("local model not selected on a fresh file")
	}
	got, ok, err := Entry(path, p.ID)
	if err != nil || !ok || got != p {
		t.Fatalf("provider: %+v %v", got, err)
	}
	for _, key := range []string{"memory", "tools", "skills"} {
		if _, exists := m[key]; exists {
			t.Fatalf("sync must not write lean or skills settings: %s", key)
		}
	}
}

func TestPrepareKeepsExistingChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".qwen", "settings.json")
	m := map[string]any{
		"model":    map[string]any{"name": "other"},
		"security": map[string]any{"auth": map[string]any{"selectedType": "qwen-oauth"}},
		"env":      map[string]any{"OPENAI_API_KEY": "sk-real"},
		"ui":       map[string]any{"theme": "mine"},
	}
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	p := ProviderFor("local", 8080, 65536)
	if changed, err := Prepare(path, p); err != nil || !changed {
		t.Fatalf("sync: %v %v", changed, err)
	}
	if err := Check(path, p); err != nil {
		t.Fatal(err)
	}
	m, _, _ = loadSettings(path)
	if m["model"].(map[string]any)["name"] != "other" || m["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "qwen-oauth" {
		t.Fatal("the user's own selection changed")
	}
	if m["env"].(map[string]any)["OPENAI_API_KEY"] != "sk-real" || m["ui"].(map[string]any)["theme"] != "mine" {
		t.Fatal("existing key or unrelated setting lost")
	}
	if got, ok, err := Entry(path, p.ID); err != nil || !ok || got != p {
		t.Fatalf("provider: %+v %v", got, err)
	}

	// Drift is reported, never repaired, by the check.
	m["modelProviders"].(map[string]any)["openai"].([]any)[0].(map[string]any)["baseUrl"] = "http://127.0.0.1:9999/v1"
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	drifted, _ := os.ReadFile(path)
	if err := Check(path, p); err == nil {
		t.Fatal("drift not detected")
	}
	if checked, _ := os.ReadFile(path); string(drifted) != string(checked) {
		t.Fatal("diagnostic wrote settings")
	}
	if changed, err := Prepare(path, p); err != nil || !changed {
		t.Fatalf("repair: %v %v", changed, err)
	}
	if err := Check(path, p); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRejectsMalformedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := writeJSON(path, map[string]any{"env": "nope"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(path, ProviderFor("local", 8080, 65536)); err == nil {
		t.Fatal("env as a string accepted")
	}
	if err := writeJSON(path, map[string]any{"security": map[string]any{"auth": []any{}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(path, ProviderFor("local", 8080, 65536)); err == nil {
		t.Fatal("security.auth as an array accepted")
	}
	if err := Check(filepath.Join(t.TempDir(), "missing.json"), ProviderFor("local", 8080, 65536)); err == nil {
		t.Fatal("missing file passed the check")
	}
	// Patching the user's own file means refusing anything unparseable or
	// shaped unexpectedly rather than guessing, and never half-writing it.
	for _, body := range []string{`{`, `{"model":null}`, `{"security":{"auth":false}}`, `{"env":[]}`} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Prepare(path, ProviderFor("local", 8080, 65536)); err == nil {
				t.Fatal("invalid settings accepted")
			}
			if after, _ := os.ReadFile(path); string(after) != body {
				t.Fatal("invalid settings changed")
			}
		})
	}
}
