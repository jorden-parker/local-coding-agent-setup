package app

import (
	"os"
	"path/filepath"
	"testing"
)

// skill creates a <dir>/<name>/SKILL.md, the shape both harnesses discover.
func skill(t *testing.T, dir, name string) string {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\nname: "+name+"\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestCountSkillsFollowsSymlinksAndSkipsStrays(t *testing.T) {
	shared := t.TempDir()
	farm := t.TempDir()
	skill(t, shared, "cmux-browser")
	// A harness skill directory is commonly a symlink farm into ~/.agents/skills.
	if err := os.Symlink(filepath.Join(shared, "cmux-browser"), filepath.Join(farm, "cmux-browser")); err != nil {
		t.Fatal(err)
	}
	// A directory without a SKILL.md, and a stray file, are not skills.
	if err := os.MkdirAll(filepath.Join(farm, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(farm, "SKILL.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countSkills(farm); got != 1 {
		t.Fatalf("symlinked skill not counted: got %d want 1", got)
	}
	if got := countSkills(filepath.Join(farm, "missing")); got != 0 {
		t.Fatalf("missing root: got %d want 0", got)
	}
}

func TestSkillRootsListsEveryRootInOrder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	managed := filepath.Join(home, "managed")
	agents := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	skill(t, agents, "cmux")
	skill(t, agents, "cmux-browser")

	// An empty managed directory is normal and must still be reported, tildified.
	detail, total := skillRoots(managed, agents)
	want := "0 in ~/managed, 2 in ~/.agents/skills"
	if detail != want {
		t.Fatalf("detail: got %q want %q", detail, want)
	}
	if total != 2 {
		t.Fatalf("total: got %d want 2", total)
	}
	if _, total := skillRoots(managed); total != 0 {
		t.Fatalf("empty roots must total 0, got %d", total)
	}
}
