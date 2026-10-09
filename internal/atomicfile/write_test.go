package atomicfile

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"syscall"
	"testing"
)

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func TestWriteCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "s.json")
	if err := Write(path, []byte("new\n")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new\n" {
		t.Fatalf("got %q", got)
	}
	fi, _ := os.Stat(path)
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

func TestWritePreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	fi, _ := os.Stat(path)
	if string(got) != "new" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("got %q mode %v", got, fi.Mode().Perm())
	}
}

func TestWritePreservesOriginalOnFailure(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]struct {
		mod  func(o *ops)
		want error
	}{
		"short write": {func(o *ops) {
			o.write = func(f *os.File, b []byte) (int, error) { return f.Write(b[:len(b)/2]) }
		}, io.ErrShortWrite},
		"write error": {func(o *ops) {
			o.write = func(f *os.File, b []byte) (int, error) { return 0, boom }
		}, boom},
		"sync error": {func(o *ops) {
			o.sync = func(f *os.File) error { return boom }
		}, boom},
		"close error": {func(o *ops) {
			o.close = func(f *os.File) error { _ = f.Close(); return boom }
		}, boom},
		"rename error": {func(o *ops) {
			o.rename = func(a, b string) error { return boom }
		}, boom},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "s.json")
			orig := []byte("original\n")
			if err := os.WriteFile(path, orig, 0o600); err != nil {
				t.Fatal(err)
			}
			before := names(t, dir)
			o := defaultOps()
			c.mod(&o)
			err := write(path, []byte("replacement document\n"), o)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			got, _ := os.ReadFile(path)
			if !bytes.Equal(got, orig) {
				t.Fatalf("original changed: %q", got)
			}
			if after := names(t, dir); !slices.Equal(before, after) {
				t.Fatalf("dir %v -> %v", before, after)
			}
		})
	}
}

func TestWriteCleansTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	for _, body := range []string{"one", "two"} {
		if err := Write(path, []byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if got := names(t, dir); !slices.Equal(got, []string{"s.json"}) {
		t.Fatalf("dir = %v", got)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "two" {
		t.Fatalf("got %q", got)
	}
}

func TestWritePreservesSymlink(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(old, link string) {
		t.Helper()
		if err := os.Symlink(old, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	cases := map[string]func() (path, referent string){
		"absolute": func() (string, string) {
			ref := filepath.Join(real, "abs.json")
			_ = os.WriteFile(ref, []byte("old"), 0o600)
			link := filepath.Join(root, "abs-link.json")
			mk(ref, link)
			return link, ref
		},
		"relative": func() (string, string) {
			ref := filepath.Join(real, "rel.json")
			_ = os.WriteFile(ref, []byte("old"), 0o600)
			link := filepath.Join(root, "rel-link.json")
			mk(filepath.Join("real", "rel.json"), link)
			return link, ref
		},
		"symlinked parent": func() (string, string) {
			ref := filepath.Join(real, "par.json")
			_ = os.WriteFile(ref, []byte("old"), 0o600)
			mk("real", filepath.Join(root, "linkdir"))
			return filepath.Join(root, "linkdir", "par.json"), ref
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			path, ref := setup()
			if err := Write(path, []byte("brand new content")); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(ref)
			if string(got) != "brand new content" {
				t.Fatalf("referent = %q", got)
			}
			if name != "symlinked parent" {
				if fi, err := os.Lstat(path); err != nil || fi.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("link was replaced: %v %v", fi, err)
				}
			}
		})
	}
	for _, d := range []string{root, real} {
		for _, n := range names(t, d) {
			if len(n) > 1 && n[0] == '.' {
				t.Fatalf("stage left behind in %s: %s", d, n)
			}
		}
	}
}

func TestWriteRejectsInvalidDestination(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"directory": dir}
	dangling := filepath.Join(root, "dangling")
	if os.Symlink(filepath.Join(root, "missing"), dangling) == nil {
		cases["dangling link"] = dangling
	}
	if runtime.GOOS != "windows" {
		fifo := filepath.Join(root, "fifo")
		if err := syscall.Mkfifo(fifo, 0o600); err == nil {
			cases["fifo"] = fifo
		}
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			before := names(t, root)
			if err := Write(path, []byte("x")); err == nil {
				t.Fatal("expected error")
			}
			if after := names(t, root); !slices.Equal(before, after) {
				t.Fatalf("dir %v -> %v", before, after)
			}
		})
	}
}
