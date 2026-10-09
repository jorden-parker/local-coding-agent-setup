package qwen

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPrepareLocalSelectionLeanAndDrift(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qwen", "settings.json")
	p := ProviderFor("local", 8080, 65536)
	if changed, err := PrepareLocal(path, p); err != nil || !changed {
		t.Fatalf("first prepare: %v %v", changed, err)
	}
	if err := CheckLocal(path, p); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	if changed, err := PrepareLocal(path, p); err != nil || changed {
		t.Fatalf("repeat: %v %v", changed, err)
	}
	repeated, _ := os.ReadFile(path)
	if string(original) != string(repeated) {
		t.Fatal("repeat rewrote settings")
	}
	m, _, _ := loadSettings(path)
	if m["model"].(map[string]any)["name"] != "local" || m["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "openai" {
		t.Fatal("selection missing")
	}
	for key, want := range leanSettings {
		got, err := stateAt(m, key)
		if err != nil || !reflect.DeepEqual(got.Value, want) {
			t.Fatalf("%s: %#v %v", key, got, err)
		}
	}
	m["model"].(map[string]any)["name"] = "remote"
	m["memory"].(map[string]any)["enableAutoSkill"] = true
	m["tools"].(map[string]any)["disabled"] = []any{"web_fetch"}
	m["ui"] = map[string]any{"theme": "mine"}
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	drifted, _ := os.ReadFile(path)
	if err := CheckLocal(path, p); err == nil {
		t.Fatal("drift not detected")
	}
	checked, _ := os.ReadFile(path)
	if string(drifted) != string(checked) {
		t.Fatal("diagnostic wrote settings")
	}
	p = ProviderFor("new-local", 9000, 32768)
	if changed, err := PrepareLocal(path, p); err != nil || !changed {
		t.Fatalf("repair: %v %v", changed, err)
	}
	if err := CheckLocal(path, p); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Entry(path, p.ID)
	if err != nil || !ok || got != p {
		t.Fatalf("provider: %+v %v", got, err)
	}
	m, _, _ = loadSettings(path)
	if m["ui"].(map[string]any)["theme"] != "mine" || !reflect.DeepEqual(m["tools"].(map[string]any)["disabled"], []any{"web_fetch", "agent"}) {
		t.Fatal("unrelated settings lost")
	}
}

func TestPrepareLocalInvalidSettingsDoNotWrite(t *testing.T) {
	for _, body := range []string{`{`, `{"model":null}`, `{"security":{"auth":false}}`, `{"tools":false}`, `{"memory":null}`, `{"tools":{"disabled":[42]}}`, `{"tools":{"disabled":"agent"}}`, `{"tools":{"disabled":["tool_search"]}}`, `{"tools":{"disabled":["tool_call"]}}`, `{"tools":{"toolSearch":{"enabled":false}}}`} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareLocal(path, ProviderFor("local", 8080, 65536)); err == nil {
				t.Fatal("invalid settings accepted")
			}
			after, _ := os.ReadFile(path)
			if string(after) != body {
				t.Fatal("invalid settings changed")
			}
		})
	}
}
