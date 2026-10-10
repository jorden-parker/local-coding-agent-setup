package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// lca must never write pi's settings.json: defaultProvider and defaultModel
// are the user's own selection, and pi-local passes both as flags.
func TestPrepareModelsNeverTouchesSettings(t *testing.T) {
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.json")
	existing := `{"defaultProvider":"anthropic","defaultModel":"opus"}`
	if err := os.WriteFile(settings, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareModels(filepath.Join(dir, "models.json"), ProviderFor("local", 8080, 65536, false)); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(settings); string(after) != existing {
		t.Fatalf("settings.json rewritten: %s", after)
	}
}

func TestHasProviderDistinguishesAdoptedConfigs(t *testing.T) {
	dir := t.TempDir()
	if got, err := HasProvider(filepath.Join(dir, "absent.json"), ProviderID); err != nil || got {
		t.Fatalf("missing file: %v %v", got, err)
	}
	theirs := filepath.Join(dir, "theirs.json")
	if err := os.WriteFile(theirs, []byte(`{"providers":{"anthropic":{"apiKey":"k"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := HasProvider(theirs, ProviderID); err != nil || got {
		t.Fatalf("unadopted file: %v %v", got, err)
	}
	if _, err := PrepareModels(theirs, ProviderFor("local", 8080, 65536, false)); err != nil {
		t.Fatal(err)
	}
	if got, err := HasProvider(theirs, ProviderID); err != nil || !got {
		t.Fatalf("adopted file: %v %v", got, err)
	}
}

func TestUnprepareModelsEmptiesAFileLcaCreated(t *testing.T) {
	p := ProviderFor("local", 8080, 65536, false)
	path := filepath.Join(t.TempDir(), "models.json")
	if _, err := PrepareModels(path, p); err != nil {
		t.Fatal(err)
	}
	r, changed, err := UnprepareModels(path, p, false)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if !r.Empty {
		t.Fatalf("not empty: %+v", read(t, path))
	}
	if len(r.Removed) != 2 || len(r.Left) != 0 {
		t.Fatalf("removal = %+v", r)
	}
}

func TestUnprepareModelsKeepsAHandAddedModel(t *testing.T) {
	p := ProviderFor("local", 8080, 65536, false)
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"llama-local":{"models":[{"id":"other"}]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareModels(path, p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := UnprepareModels(path, p, false); err != nil {
		t.Fatal(err)
	}
	models := read(t, path)["providers"].(map[string]any)[ProviderID].(map[string]any)["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["id"] != "other" {
		t.Fatalf("models = %+v", models)
	}
}

// An edited contextWindow may mean the entry is now the user's, so it stays.
func TestUnprepareModelsLeavesAnEditedModel(t *testing.T) {
	p := ProviderFor("local", 8080, 65536, false)
	path := filepath.Join(t.TempDir(), "models.json")
	if _, err := PrepareModels(path, p); err != nil {
		t.Fatal(err)
	}
	m := read(t, path)
	model := m["providers"].(map[string]any)[ProviderID].(map[string]any)["models"].([]any)[0].(map[string]any)
	model["contextWindow"] = float64(4096)
	b, _ := json.Marshal(m)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	r, _, err := UnprepareModels(path, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Left) != 1 || len(r.Removed) != 0 {
		t.Fatalf("removal = %+v", r)
	}
	if _, ok := read(t, path)["providers"].(map[string]any)[ProviderID]; !ok {
		t.Fatal("edited provider removed")
	}
}

func TestUnprepareModelsPreservesEverythingElse(t *testing.T) {
	p := ProviderFor("local", 8080, 65536, false)
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"anthropic":{"apiKey":"secret"}},"unrelated":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareModels(path, p); err != nil {
		t.Fatal(err)
	}
	r, _, err := UnprepareModels(path, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Empty {
		t.Fatal("file holding the user's own provider reported empty")
	}
	m := read(t, path)
	if m["unrelated"] != true {
		t.Fatal("unrelated key lost")
	}
	if m["providers"].(map[string]any)["anthropic"].(map[string]any)["apiKey"] != "secret" {
		t.Fatal("other provider lost")
	}
	if _, ok := m["providers"].(map[string]any)[ProviderID]; ok {
		t.Fatal("lca provider not removed")
	}
}

// A provider the user has added their own keys to is never deleted wholesale.
func TestUnprepareModelsKeepsAProviderWithHandWrittenSettings(t *testing.T) {
	p := ProviderFor("local", 8080, 65536, false)
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"providers":{"llama-local":{"headers":{"X-Trace":"1"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareModels(path, p); err != nil {
		t.Fatal(err)
	}
	r, _, err := UnprepareModels(path, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Left) != 1 {
		t.Fatalf("removal = %+v", r)
	}
	ours := read(t, path)["providers"].(map[string]any)[ProviderID].(map[string]any)
	if ours["headers"].(map[string]any)["X-Trace"] != "1" {
		t.Fatal("hand-written header lost")
	}
}
