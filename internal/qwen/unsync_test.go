package qwen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnprepareEmptiesAFileLcaCreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".qwen", "settings.json")
	p := ProviderFor("local", 8080, 65536)
	if _, err := Prepare(path, p); err != nil {
		t.Fatal(err)
	}
	r, changed, err := Unprepare(path, p, false)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	if !r.Empty {
		m, _, _ := loadSettings(path)
		t.Fatalf("not empty: %+v", m)
	}
	if len(r.Left) != 0 {
		t.Fatalf("left behind: %+v", r.Left)
	}
	if _, ok, err := Entry(path, p.ID); err != nil || ok {
		t.Fatalf("provider still listed: %v", err)
	}
}

// A real API key and the user's own selection are theirs, not lca's.
func TestUnprepareKeepsTheUsersOwnChoices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
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
	if _, err := Prepare(path, p); err != nil {
		t.Fatal(err)
	}
	r, _, err := Unprepare(path, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Empty {
		t.Fatal("a file with the user's own settings reported empty")
	}
	m, _, _ = loadSettings(path)
	if m["model"].(map[string]any)["name"] != "other" {
		t.Fatalf("selection changed: %+v", m["model"])
	}
	if m["security"].(map[string]any)["auth"].(map[string]any)["selectedType"] != "qwen-oauth" {
		t.Fatalf("auth changed: %+v", m["security"])
	}
	if m["env"].(map[string]any)["OPENAI_API_KEY"] != "sk-real" {
		t.Fatal("real API key removed")
	}
	if m["ui"].(map[string]any)["theme"] != "mine" {
		t.Fatal("unrelated setting lost")
	}
	if _, ok, _ := Entry(path, p.ID); ok {
		t.Fatal("provider not removed")
	}
	if len(r.Left) != 1 {
		t.Fatalf("the real key should be reported as left: %+v", r.Left)
	}
}

// Other providers survive, and the containers lca created are pruned.
func TestUnprepareKeepsOtherProviders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"modelProviders":{"openai":[{"id":"other","baseUrl":"http://x/v1"}],"anthropic":[{"id":"a"}]}}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	p := ProviderFor("local", 8080, 65536)
	if _, err := Prepare(path, p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Unprepare(path, p, false); err != nil {
		t.Fatal(err)
	}
	m, _, _ := loadSettings(path)
	mp := m["modelProviders"].(map[string]any)
	list := mp["openai"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != "other" {
		t.Fatalf("openai list = %+v", list)
	}
	if len(mp["anthropic"].([]any)) != 1 {
		t.Fatal("anthropic lost")
	}
}

// An entry the user has edited may now be theirs, so it is kept and named.
func TestUnprepareLeavesAnEditedEntry(t *testing.T) {
	cases := map[string]func(e map[string]any){
		"edited baseUrl": func(e map[string]any) { e["baseUrl"] = "http://127.0.0.1:9999/v1" },
		"added key":      func(e map[string]any) { e["custom"] = "mine" },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			p := ProviderFor("local", 8080, 65536)
			if _, err := Prepare(path, p); err != nil {
				t.Fatal(err)
			}
			m, _, _ := loadSettings(path)
			entry := m["modelProviders"].(map[string]any)["openai"].([]any)[0].(map[string]any)
			edit(entry)
			if err := writeJSON(path, m); err != nil {
				t.Fatal(err)
			}
			r, _, err := Unprepare(path, p, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Left) == 0 {
				t.Fatalf("edited entry not reported: %+v", r)
			}
			if _, ok, _ := Entry(path, p.ID); !ok {
				t.Fatal("edited entry removed")
			}
		})
	}
}

// The lean profile is reverted by lca qwen-profile restore, not by unsync.
func TestUnprepareDoesNotTouchLeanSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	snapshot := path + ".lca-lean.json"
	p := ProviderFor("local", 8080, 65536)
	if _, err := Prepare(path, p); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLean(path, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Unprepare(path, p, false); err != nil {
		t.Fatal(err)
	}
	m, _, _ := loadSettings(path)
	tools, ok := m["tools"].(map[string]any)
	if !ok || tools["workflowsEnabled"] != false {
		t.Fatalf("lean settings changed: %+v", m["tools"])
	}
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("snapshot gone: %v", err)
	}
	// Restore is still what reverts lean, and it still works afterwards.
	if changed, err := RestoreLean(path, snapshot); err != nil || !changed {
		t.Fatal(err, changed)
	}
}

func TestUnprepareIgnoresAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	r, changed, err := Unprepare(path, ProviderFor("local", 8080, 65536), false)
	if err != nil || changed {
		t.Fatal(err, changed)
	}
	if len(r.Removed) != 0 || len(r.Left) != 0 {
		t.Fatalf("removal = %+v", r)
	}
}
