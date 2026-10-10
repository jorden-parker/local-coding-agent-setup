package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `# llama-coder configuration. Created by setup.sh.
# Edit with lca config.
MODEL_PATH=/tmp/x.gguf
export ALIAS=qwen3.5-9b

CTX=65536   # inline comment
PORT="8080"
TEMP='0.7'
UNKNOWN=keep me
EXTRA_ARGS=
`

func TestCacheSettings(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"CACHE_RAM", "0", true}, {"CACHE_RAM", "2048", true}, {"CACHE_RAM", "-1", false},
		{"CACHE_RAM", "1048577", false}, {"CACHE_RAM", "2.5", false},
		{"CTX_CHECKPOINTS", "0", true}, {"CTX_CHECKPOINTS", "8", true},
		{"CTX_CHECKPOINTS", "1024", true}, {"CTX_CHECKPOINTS", "1025", false},
		{"CTX_CHECKPOINTS", "-1", false},
	} {
		_, err := Validate(tc.key, tc.value)
		if (err == nil) != tc.valid {
			t.Errorf("%s=%s: %v", tc.key, tc.value, err)
		}
	}
	for _, flag := range []string{"--cache-ram", "-cram", "--ctx-checkpoints", "-ctxcp", "--swa-checkpoints"} {
		for _, args := range []string{flag + " 8", flag + "=8"} {
			if _, err := Validate("EXTRA_ARGS", args); err == nil {
				t.Errorf("accepted duplicate %s", args)
			}
		}
	}
}

func load(t *testing.T, body string) *File {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.env")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRoundTripIsByteIdentical(t *testing.T) {
	f := load(t, sample)
	if got := string(f.Bytes()); got != sample {
		t.Fatalf("round trip changed file:\n%s", got)
	}
	noEOL := strings.TrimSuffix(sample, "\n")
	f = load(t, noEOL)
	if got := string(f.Bytes()); got != noEOL {
		t.Fatalf("round trip added newline:\n%q", got)
	}
}

func TestGetUnquotes(t *testing.T) {
	f := load(t, sample)
	want := map[string]string{"MODEL_PATH": "/tmp/x.gguf", "ALIAS": "qwen3.5-9b", "CTX": "65536", "PORT": "8080", "TEMP": "0.7", "UNKNOWN": "keep me", "EXTRA_ARGS": ""}
	for k, w := range want {
		if got, ok := f.Get(k); !ok || got != w {
			t.Errorf("Get(%s) = %q,%v want %q", k, got, ok, w)
		}
	}
	if _, ok := f.Get("MISSING"); ok {
		t.Error("MISSING found")
	}
	if len(f.Values()) != len(want) {
		t.Errorf("Values has %d entries", len(f.Values()))
	}
}

func TestSetKeepsCommentsOrderAndExport(t *testing.T) {
	f := load(t, sample)
	f.Set("ALIAS", "other")
	f.Set("CTX", "4096")
	f.Set("EXTRA_ARGS", "--jinja -t 8")
	f.Set("NEW", "it's new")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(f.Path)
	want := `# llama-coder configuration. Created by setup.sh.
# Edit with lca config.
MODEL_PATH=/tmp/x.gguf
export ALIAS=other

CTX=4096
PORT="8080"
TEMP='0.7'
UNKNOWN=keep me
EXTRA_ARGS='--jinja -t 8'
NEW='it'\''s new'
`
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	f2, _ := Load(f.Path)
	if v, _ := f2.Get("NEW"); v != "it's new" {
		t.Errorf("NEW = %q", v)
	}
	if v, _ := f2.Get("EXTRA_ARGS"); v != "--jinja -t 8" {
		t.Errorf("EXTRA_ARGS = %q", v)
	}
}

func TestLoadMissing(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope", "config.env"))
	if err != nil || f.Exists || len(f.Bytes()) != 0 {
		t.Fatalf("missing: %v %v %q", err, f.Exists, f.Bytes())
	}
	f.Set("A", "1")
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(f.Path)
	if string(got) != "A=1\n" {
		t.Fatalf("got %q", got)
	}
}

func TestValidate(t *testing.T) {
	model := filepath.Join(t.TempDir(), "m.gguf")
	_ = os.WriteFile(model, nil, 0o600)
	notGGUF := filepath.Join(t.TempDir(), "m.bin")
	_ = os.WriteFile(notGGUF, nil, 0o600)
	cases := []struct {
		key, val string
		wantErr  string
		wantWarn bool
	}{
		{"MODEL_PATH", model, "", false},
		{"MODEL_PATH", notGGUF, "", true},
		{"MODEL_PATH", "relative.gguf", "absolute", false},
		{"MODEL_PATH", "/nonexistent/x.gguf", "no such file", false},
		{"MODEL_PATH", t.TempDir(), "not a regular file", false},
		{"ALIAS", "qwen3.5-9b", "", false},
		{"ALIAS", "", "empty", false},
		{"ALIAS", "a b", "whitespace", false},
		{"CTX", "65536", "", false},
		{"CTX", "65537", "multiple of 256", false},
		{"CTX", "1024", "between", false},
		{"CTX", "abc", "integer", false},
		{"PORT", "8080", "", false},
		{"PORT", "80", "between", false},
		{"PORT", "70000", "between", false},
		{"TEMP", "0.7", "", false},
		{"TEMP", "2.5", "between", false},
		{"TEMP", "x", "number", false},
		{"TOP_K", "20", "", false},
		{"TOP_K", "-1", "between", false},
		{"PRESENCE_PENALTY", "-2", "", false},
		{"THINKING", "true", "", false},
		{"THINKING", "yes", "true or false", false},
		{"EXTRA_ARGS", "", "", false},
		{"EXTRA_ARGS", "--jinja -t 8", "", false},
		{"EXTRA_ARGS", "--jinja --port 9000", "set PORT instead", false},
		{"EXTRA_ARGS", "--ctx-size=4096", "set CTX instead", false},
		{"EXTRA_ARGS", "-ngl 50", "launcher sets it", false},
		{"EXTRA_ARGS", "--foo 'a b'", "quotes", false},
		{"EXTRA_ARGS", "--foo\nbar", "newlines", false},
		{"NOPE", "1", "unknown key", false},
	}
	for _, c := range cases {
		warn, err := Validate(c.key, c.val)
		if c.wantErr == "" && err != nil {
			t.Errorf("%s=%q: unexpected error %v", c.key, c.val, err)
		}
		if c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s=%q: err %v, want containing %q", c.key, c.val, err, c.wantErr)
		}
		if (warn != "") != c.wantWarn {
			t.Errorf("%s=%q: warn %q", c.key, c.val, warn)
		}
	}
}

func TestValidateAllReportsMissing(t *testing.T) {
	_, errs := ValidateAll(map[string]string{"TEMP": "0.7"})
	var names []string
	for _, e := range errs {
		names = append(names, e.Error())
	}
	joined := strings.Join(names, "; ")
	for _, k := range []string{"MODEL_PATH", "ALIAS", "CTX", "PORT"} {
		if !strings.Contains(joined, k+" is not set") {
			t.Errorf("missing %s not reported: %s", k, joined)
		}
	}
	if strings.Contains(joined, "EXTRA_ARGS") || strings.Contains(joined, "TOP_P") {
		t.Errorf("optional key reported: %s", joined)
	}
}

func TestHarnessKey(t *testing.T) {
	for _, v := range []string{"qwen", "pi"} {
		if warn, err := Validate("HARNESS", v); err != nil || warn != "" {
			t.Errorf("%s: %v %q", v, err, warn)
		}
	}
	for _, v := range []string{"", "codex", "Pi", "qwen pi"} {
		if _, err := Validate("HARNESS", v); err == nil {
			t.Errorf("%q accepted", v)
		}
	}
	// Older config.env files predate the key, so the default has to stand in.
	k, ok := Lookup("HARNESS")
	if !ok || k.Default != "qwen" {
		t.Fatalf("default: %+v", k)
	}
	_, errs := ValidateAll(map[string]string{"ALIAS": "a", "CTX": "65536", "PORT": "8080"})
	for _, e := range errs {
		if strings.Contains(e.Error(), "HARNESS") {
			t.Fatalf("an absent HARNESS must fall back to the default: %v", e)
		}
	}
}
