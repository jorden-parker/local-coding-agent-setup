package backup

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNameKeepsTheExtension(t *testing.T) {
	at := time.Date(2026, 10, 10, 14, 22, 33, 0, time.UTC)
	cases := map[string]struct{ in, want string }{
		"json":         {"/h/.qwen/settings.json", "/h/.qwen/settings.lca-backup-20261010T142233Z.json"},
		"no extension": {"/h/.qwen/settings", "/h/.qwen/settings.lca-backup-20261010T142233Z"},
	}
	for name, c := range cases {
		if got := Name(c.in, at); got != c.want {
			t.Errorf("%s: Name(%s) = %s, want %s", name, c.in, got, c.want)
		}
	}
}

func TestOnceCopiesTheOriginalOnlyTheFirstTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{\n  // a comment\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Once(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("no backup taken")
	}
	got, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{\n  // a comment\n}\n" {
		t.Fatalf("backup = %q", got)
	}

	// lca now patches the file; a second sync must not bury the original.
	if err := os.WriteFile(path, []byte("{\"patched\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Once(path)
	if err != nil {
		t.Fatal(err)
	}
	if second != "" {
		t.Fatalf("second backup = %s, want none", second)
	}
	if got, _ := os.ReadFile(first); string(got) != "{\n  // a comment\n}\n" {
		t.Fatalf("original backup changed: %q", got)
	}
	if found, err := Existing(path); err != nil || len(found) != 1 {
		t.Fatalf("Existing() = %v, %v, want one entry", found, err)
	}
}

// A missing file means lca is creating it, so there is no earlier state to
// keep. unsync relies on that absence to know the file is lca's own.
func TestOnceSkipsAMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	got, err := Once(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("backup = %s, want none", got)
	}
	if found, err := Existing(path); err != nil || len(found) != 0 {
		t.Fatalf("Existing() = %v, %v, want empty", found, err)
	}
}

// A settings.json symlinked into a dotfiles repo must be backed up next to the
// file it points at, not next to the link.
func TestOnceFollowsASymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "dotfiles", "settings.json")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	got, err := Once(link)
	if err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks also resolves /var -> /private/var on macOS, so compare
	// the resolved directories rather than the literal paths.
	realDir, err := filepath.EvalSymlinks(filepath.Dir(real))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(got) != realDir {
		t.Fatalf("backup = %s, want it beside %s", got, real)
	}
	if b, _ := os.ReadFile(got); string(b) != "original\n" {
		t.Fatalf("backup = %q", b)
	}
}
