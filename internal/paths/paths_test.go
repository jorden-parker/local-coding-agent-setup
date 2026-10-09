package paths

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestQwenUsageDirsIncludesInstances(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	// qwen-8082 has no usage directory yet and must be skipped.
	for _, d := range []string{"qwen/usage", "qwen-9000/usage", "qwen-8081/usage", "qwen-8082"} {
		if err := os.MkdirAll(filepath.Join(ConfigDir(), d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		QwenUsageDir(),
		filepath.Join(ConfigDir(), "qwen-8081", "usage"),
		filepath.Join(ConfigDir(), "qwen-9000", "usage"),
		LegacyQwenUsageDir(),
	}
	if got := QwenUsageDirs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
