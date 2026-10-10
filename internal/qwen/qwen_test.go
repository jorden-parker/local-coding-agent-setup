package qwen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareCreatesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".qwen", "settings.json")
	changed, err := Prepare(p, ProviderFor("qwen3.5-9b", 8080, 65536))
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	b, _ := os.ReadFile(p)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["$version"] != float64(4) {
		t.Errorf("$version = %v", m["$version"])
	}
	e, ok, err := Entry(p, "qwen3.5-9b")
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	want := Provider{ID: "qwen3.5-9b", Name: "qwen3.5-9b (local llama.cpp)", BaseURL: "http://127.0.0.1:8080/v1", EnvKey: "OPENAI_API_KEY", ContextWindow: 65536}
	if e != want {
		t.Errorf("entry %+v want %+v", e, want)
	}
	changed, err = Prepare(p, want)
	if err != nil || changed {
		t.Errorf("second sync changed=%v err=%v", changed, err)
	}
}

func TestPrepareMergesAndKeepsOtherKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"ui":{"autoModeAcknowledged":true},"$version":7,"privacy":{"usageStatisticsEnabled":false},
"modelProviders":{"openai":[
  {"id":"other","name":"Other","baseUrl":"http://x/v1"},
  {"id":"qwen3.5-9b","name":"old","baseUrl":"http://127.0.0.1:8080/v1","custom":"mine","generationConfig":{"contextWindowSize":1,"temperature":0.1}}
],"anthropic":[{"id":"a"}]}}`
	_ = os.WriteFile(p, []byte(orig), 0o600)
	if _, err := Prepare(p, ProviderFor("qwen3.5-9b", 9000, 4096)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["$version"] != float64(7) {
		t.Errorf("$version changed: %v", m["$version"])
	}
	if m["ui"].(map[string]any)["autoModeAcknowledged"] != true {
		t.Error("ui lost")
	}
	mp := m["modelProviders"].(map[string]any)
	if len(mp["anthropic"].([]any)) != 1 {
		t.Error("anthropic lost")
	}
	list := mp["openai"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["id"] != "other" {
		t.Fatalf("openai list %v", list)
	}
	e := list[1].(map[string]any)
	if e["custom"] != "mine" || e["name"] != "qwen3.5-9b (local llama.cpp)" || e["baseUrl"] != "http://127.0.0.1:9000/v1" || e["envKey"] != "OPENAI_API_KEY" {
		t.Errorf("entry %v", e)
	}
	gc := e["generationConfig"].(map[string]any)
	if gc["contextWindowSize"] != float64(4096) || gc["temperature"] != 0.1 {
		t.Errorf("generationConfig %v", gc)
	}
	on, err := UsageStatsEnabled(p)
	if err != nil || on {
		t.Errorf("usage stats %v %v", on, err)
	}
	if on, _ := UsageStatsEnabled(filepath.Join(t.TempDir(), "missing.json")); !on {
		t.Error("default should be enabled")
	}
}
