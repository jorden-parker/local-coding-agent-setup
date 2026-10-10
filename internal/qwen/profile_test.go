package qwen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func profilePaths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	p := filepath.Join(dir, "settings.json")
	return p, p + ".lca-lean.json"
}

func TestLeanRoundTrip(t *testing.T) {
	for _, body := range []string{
		`{}`, `{"memory":{},"tools":{}}`,
		`{"tools":{"disabled":["web_fetch"],"eager":null},"memory":{"enableAutoSkill":true},"custom":42}`,
		`{"tools":{"disabled":["agent"]},"memory":{"enableManagedAutoDream":false}}`,
	} {
		t.Run(body, func(t *testing.T) {
			p, backup := profilePaths(t)
			if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			original, _, _ := loadSettings(p)
			if changed, err := ApplyLean(p, backup); err != nil || !changed {
				t.Fatalf("apply: %v %v", changed, err)
			}
			saved, _ := os.ReadFile(backup)
			if changed, err := ApplyLean(p, backup); err != nil || changed {
				t.Fatalf("repeat: %v %v", changed, err)
			}
			again, _ := os.ReadFile(backup)
			if string(saved) != string(again) {
				t.Fatal("snapshot replaced")
			}
			if changed, err := RestoreLean(p, backup); err != nil || !changed {
				t.Fatalf("restore: %v %v", changed, err)
			}
			got, _, _ := loadSettings(p)
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("round trip: %#v != %#v", got, original)
			}
			if changed, err := RestoreLean(p, backup); err != nil || changed {
				t.Fatalf("repeat restore: %v %v", changed, err)
			}
		})
	}
}

func TestLeanPreservesProviderAndUnrelatedEdits(t *testing.T) {
	p, backup := profilePaths(t)
	if _, err := ApplyLean(p, backup); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(p, ProviderFor("local", 8080, 65536)); err != nil {
		t.Fatal(err)
	}
	m, _, _ := loadSettings(p)
	m["tools"].(map[string]any)["custom"] = "keep"
	if err := writeJSON(p, m); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreLean(p, backup); err != nil {
		t.Fatal(err)
	}
	m, _, _ = loadSettings(p)
	if m["tools"].(map[string]any)["custom"] != "keep" {
		t.Fatal("unrelated setting lost")
	}
	if _, ok, err := Entry(p, "local"); err != nil || !ok {
		t.Fatalf("provider lost: %v", err)
	}
}

func TestLeanConflictsDoNotWrite(t *testing.T) {
	p, backup := profilePaths(t)
	if _, err := ApplyLean(p, backup); err != nil {
		t.Fatal(err)
	}
	m, _, _ := loadSettings(p)
	m["memory"].(map[string]any)["enableAutoSkill"] = true
	if err := writeJSON(p, m); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(p)
	for _, op := range []func(string, string) (bool, error){ApplyLean, RestoreLean} {
		if _, err := op(p, backup); err == nil {
			t.Fatal("expected conflict")
		}
		after, _ := os.ReadFile(p)
		if string(before) != string(after) {
			t.Fatal("conflict changed settings")
		}
		if _, err := os.Stat(backup); err != nil {
			t.Fatal("snapshot lost")
		}
	}
}

func TestLeanRejectsMalformedSettings(t *testing.T) {
	for _, body := range []string{`{"tools":false}`, `{"memory":null}`, `{"tools":{"disabled":"agent"}}`, `{"tools":{"disabled":[42]}}`, `{"tools":{"disabled":["tool_search"]}}`, `{"tools":{"toolSearch":{"enabled":false}}}`, `{`} {
		p, backup := profilePaths(t)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyLean(p, backup); err == nil {
			t.Fatalf("accepted %s", body)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatal("created snapshot on invalid input")
		}
	}
}

func TestLeanRetryAfterSettingsWriteFailure(t *testing.T) {
	p, backup := profilePaths(t)
	if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyLean(p, backup); err != nil {
		t.Fatal(err)
	}
	// Simulate a persisted snapshot followed by a failed settings write.
	if err := os.WriteFile(p, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyLean(p, backup); err != nil || !changed {
		t.Fatalf("retry: %v %v", changed, err)
	}
	if _, err := RestoreLean(p, backup); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &m); err != nil || len(m) != 0 {
		t.Fatalf("restored %s: %v", b, err)
	}
}
