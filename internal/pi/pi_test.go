package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func read(t *testing.T, path string) map[string]any {
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

func TestPrepareModelsCreatesProviderAndModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	p := ProviderFor("qwen3.5-9b", 8080, 65536, false)
	changed, err := PrepareModels(path, p)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	provider := read(t, path)["providers"].(map[string]any)[ProviderID].(map[string]any)
	if provider["baseUrl"] != "http://127.0.0.1:8080/v1" || provider["api"] != "openai-completions" || provider["apiKey"] != localAPIKey {
		t.Fatalf("provider: %+v", provider)
	}
	models := provider["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("models: %+v", models)
	}
	model := models[0].(map[string]any)
	if model["id"] != "qwen3.5-9b" || model["contextWindow"] != float64(65536) || model["reasoning"] != false {
		t.Fatalf("model: %+v", model)
	}
	if cost := model["cost"].(map[string]any); cost["input"] != float64(0) || cost["output"] != float64(0) {
		t.Fatalf("cost: %+v", cost)
	}
	// A second run is a no-op, and the check agrees.
	if changed, err := PrepareModels(path, p); err != nil || changed {
		t.Fatal(err, changed)
	}
	if err := CheckModels(path, p); err != nil {
		t.Fatal(err)
	}
	// Drift is reported without touching the file.
	before, _ := os.ReadFile(path)
	if err := CheckModels(path, ProviderFor("qwen3.5-9b", 8081, 65536, false)); err == nil {
		t.Fatal("port change not reported")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("CheckModels wrote the file")
	}
}

func TestPrepareModelsKeepsEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	existing := `{
	  "providers": {
	    "ollama": {"baseUrl": "http://localhost:11434/v1", "models": [{"id": "gemma"}]},
	    "llama-local": {"headers": {"X-Trace": "1"}, "models": [{"id": "other", "contextWindow": 4096}]}
	  },
	  "unrelated": true
	}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareModels(path, ProviderFor("local", 8080, 65536, true)); err != nil {
		t.Fatal(err)
	}
	m := read(t, path)
	if m["unrelated"] != true {
		t.Fatal("unrelated key lost")
	}
	providers := m["providers"].(map[string]any)
	if _, ok := providers["ollama"]; !ok {
		t.Fatal("other provider lost")
	}
	ours := providers[ProviderID].(map[string]any)
	if ours["headers"].(map[string]any)["X-Trace"] != "1" {
		t.Fatal("hand-written header lost")
	}
	models := ours["models"].([]any)
	if len(models) != 2 {
		t.Fatalf("hand-added model lost: %+v", models)
	}
	if models[0].(map[string]any)["contextWindow"] != float64(4096) {
		t.Fatalf("hand-added model changed: %+v", models[0])
	}
	if models[1].(map[string]any)["reasoning"] != true {
		t.Fatalf("THINKING not mirrored: %+v", models[1])
	}
}

func TestPrepareSettingsOwnsOnlySelectionAndSkills(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	existing := `{"theme": "dark", "skills": ["~/work/skills"]}`
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	p := ProviderFor("local", 8080, 65536, false)
	if _, err := PrepareSettings(path, p); err != nil {
		t.Fatal(err)
	}
	m := read(t, path)
	if m["theme"] != "dark" {
		t.Fatal("unrelated setting lost")
	}
	if m["defaultProvider"] != ProviderID || m["defaultModel"] != "local" {
		t.Fatalf("selection: %+v", m)
	}
	skills := m["skills"].([]any)
	if len(skills) != 2 || skills[0] != "~/work/skills" || skills[1] != LocalSkillsDir {
		t.Fatalf("skills: %+v", skills)
	}
	if changed, err := PrepareSettings(path, p); err != nil || changed {
		t.Fatal("second run changed the file", err, changed)
	}
	if err := CheckSettings(path, p); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareRejectsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	p := ProviderFor("local", 8080, 65536, false)
	for name, body := range map[string]string{
		"not-json.json":    "{",
		"providers.json":   `{"providers": []}`,
		"models-map.json":  `{"providers": {"llama-local": {"models": {}}}}`,
		"model-entry.json": `{"providers": {"llama-local": {"models": ["x"]}}}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PrepareModels(path, p); err == nil {
			t.Fatalf("%s: accepted", name)
		}
		if after, _ := os.ReadFile(path); string(after) != body {
			t.Fatalf("%s: rewritten", name)
		}
	}
	path := filepath.Join(dir, "skills.json")
	if err := os.WriteFile(path, []byte(`{"skills": "one"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareSettings(path, p); err == nil {
		t.Fatal("string skills accepted")
	}
	// A missing file is drift, not an error.
	if err := CheckModels(filepath.Join(dir, "absent.json"), p); err == nil {
		t.Fatal("missing file not reported")
	}
}
