// Copied verbatim from github.com/jorden-parker/claude-config-cli (internal/atomicfile).
//

// Package atomicfile replaces a file's contents without ever truncating the
// original. The new bytes go to a temporary file in the same directory, and
// that file is renamed over the destination only after it was written,
// synced and closed.
//
// Limits: a rename gives readers either the complete old file or the complete
// new one on local macOS and Linux filesystems. It does not promise survival
// of power loss right after the rename, behavior on network filesystems, or
// preservation of hard links and extended metadata (ACLs, xattrs, owner) on
// the replaced file.
package atomicfile

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ops holds the filesystem calls that can fail, so tests can inject faults.
type ops struct {
	createTemp func(dir, pattern string) (*os.File, error)
	write      func(f *os.File, b []byte) (int, error)
	sync       func(f *os.File) error
	close      func(f *os.File) error
	rename     func(oldpath, newpath string) error
}

func defaultOps() ops {
	return ops{
		createTemp: os.CreateTemp,
		write:      func(f *os.File, b []byte) (int, error) { return f.Write(b) },
		sync:       func(f *os.File) error { return f.Sync() },
		close:      func(f *os.File) error { return f.Close() },
		rename:     os.Rename,
	}
}

// Write replaces path with data. Missing parent directories are created
// (0755). A new file is created with mode 0600; an existing regular file
// keeps its permission bits. If path is a symlink, the file it points to is
// replaced and the link is left alone. Dangling symlinks and destinations
// that are not regular files are refused.
func Write(path string, data []byte) error {
	return write(path, data, defaultOps())
}

// Target reports the real file a Write to path would replace, following a
// symlinked file or parent directory, and the mode the replacement would get.
// Callers that need to copy a file aside before lca patches it use this so the
// copy lands next to the file itself rather than next to a link to it.
func Target(path string) (string, fs.FileMode, error) { return resolve(path) }

func write(path string, data []byte, o ops) error {
	target, mode, err := resolve(path)
	if err != nil {
		return err
	}
	tmp, err := stage(target, mode, data, o)
	if err != nil {
		return err
	}
	return commit(tmp, target, o)
}

// resolve returns the real file to replace and the mode to give the new one.
func resolve(path string) (string, fs.FileMode, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", 0, err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", 0, err
	}
	target := filepath.Join(dir, filepath.Base(path))
	li, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return target, 0o600, nil
	}
	if err != nil {
		return "", 0, err
	}
	if li.Mode()&fs.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			return "", 0, fmt.Errorf("%s is a symlink that points nowhere; fix or remove the link first: %w", path, err)
		}
		target = resolved
	}
	fi, err := os.Stat(target)
	if err != nil {
		return "", 0, err
	}
	if !fi.Mode().IsRegular() {
		return "", 0, fmt.Errorf("%s is not a regular file (%s); refusing to replace it", target, fi.Mode().Type())
	}
	return target, fi.Mode().Perm(), nil
}

// stage writes data to a synced temporary file next to target and returns its
// path. On any error the temporary file is removed.
func stage(target string, mode fs.FileMode, data []byte, o ops) (string, error) {
	f, err := o.createTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	fail := func(err error) (string, error) {
		_ = o.close(f) // may already be closed; the first error wins
		_ = os.Remove(name)
		return "", err
	}
	n, err := o.write(f, data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fail(err)
	}
	if err := f.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := o.sync(f); err != nil {
		return fail(err)
	}
	if err := o.close(f); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// commit renames the staged file over target. The original is never removed
// as a fallback; on failure it is untouched and the staged file is removed.
func commit(tmp, target string, o ops) error {
	if err := o.rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
