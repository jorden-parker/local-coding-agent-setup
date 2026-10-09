package qwen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSyncCreatesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".qwen", "settings.json")
	changed, err := Sync(p, ProviderFor("qwen3.5-9b", 8080, 65536))
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
	changed, err = Sync(p, want)
	if err != nil || changed {
		t.Errorf("second sync changed=%v err=%v", changed, err)
	}
}

func TestSyncMergesAndKeepsOtherKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.json")
	orig := `{"ui":{"autoModeAcknowledged":true},"$version":7,"privacy":{"usageStatisticsEnabled":false},
"modelProviders":{"openai":[
  {"id":"other","name":"Other","baseUrl":"http://x/v1"},
  {"id":"qwen3.5-9b","name":"old","baseUrl":"http://127.0.0.1:8080/v1","custom":"mine","generationConfig":{"contextWindowSize":1,"temperature":0.1}}
],"anthropic":[{"id":"a"}]}}`
	_ = os.WriteFile(p, []byte(orig), 0o600)
	if _, err := Sync(p, ProviderFor("qwen3.5-9b", 9000, 4096)); err != nil {
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

const usageSample = `{"schemaVersion":1,"id":"20a43195-4975-4447-b396-44ff3791a307","timestamp":"2026-10-08T13:06:07.876Z","localDate":"2026-10-08","localMonth":"2026-10","sessionId":"4028e0b8-dc3e-49e3-8a6b-9144eb1253cb","model":"qwen3.5-9b","authType":"openai","source":"main","inputTokens":21154,"outputTokens":10,"cachedTokens":0,"thoughtsTokens":0,"totalTokens":21164,"apiDurationMs":234698}
{"schemaVersion":1,"id":"8a93db8d-68d1-429c-ad28-ec043335a347","timestamp":"2026-10-08T13:07:28.320Z","localDate":"2026-10-08","localMonth":"2026-10","sessionId":"4028e0b8-dc3e-49e3-8a6b-9144eb1253cb","model":"qwen3.5-9b","authType":"openai","source":"managed-auto-memory-extractor","inputTokens":5907,"outputTokens":93,"cachedTokens":0,"thoughtsTokens":0,"totalTokens":6000,"apiDurationMs":80343}
{"schemaVersion":2,"id":"x","timestamp":"2026-10-08T13:08:00.000Z","localDate":"2026-10-08","model":"qwen3.5-9b","apiDurationMs":1}
not json at all
`

func TestReadUsage(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "token-usage-2026-10.jsonl"), []byte(usageSample), 0o600)
	recs, skipped, err := Read(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || skipped != 2 {
		t.Fatalf("got %d records, %d skipped", len(recs), skipped)
	}
	if recs[0].InputTokens != 21154 || recs[0].APIDurationMs != 234698 || recs[0].Source != "main" || recs[0].LocalDate != "2026-10-08" {
		t.Errorf("record 0: %+v", recs[0])
	}
	later, _, _ := Read(dir, time.Date(2026, 10, 8, 13, 7, 0, 0, time.UTC))
	if len(later) != 1 {
		t.Errorf("since filter: %d", len(later))
	}
}
