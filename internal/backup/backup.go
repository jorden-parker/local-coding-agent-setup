// Package backup preserves a harness configuration file the first time lca is
// about to patch it.
//
// lca merges its provider entry into files the user owns (~/.qwen/settings.json,
// ~/.pi/agent/models.json). That merge round-trips the document through a Go
// map, which sorts keys and drops any formatting, so even a change lca can undo
// exactly leaves the file looking different. One copy of the original, taken
// before the first write and never overwritten afterwards, is what makes that
// recoverable.
package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jorden-parker/local-coding-agent-setup/internal/atomicfile"
)

// suffix is the middle of a backup's name: <file>.lca-backup-<stamp><ext>.
const suffix = ".lca-backup-"

// stampFormat is a filename-safe UTC timestamp.
const stampFormat = "20060102T150405Z"

// Name is the backup path Once would write for path at t.
func Name(path string, t time.Time) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)] + suffix + t.UTC().Format(stampFormat) + ext
}

// glob matches every backup already taken for path.
func glob(path string) string {
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)] + suffix + "*" + ext
}

// Existing lists the backups taken for path, oldest name first.
func Existing(path string) ([]string, error) {
	target, _, err := atomicfile.Target(path)
	if err != nil {
		// No resolvable target means nothing has been backed up.
		return nil, nil
	}
	return filepath.Glob(glob(target))
}

// Once copies path aside before lca patches it for the first time, and
// reports the backup it wrote. It returns "" when there is nothing to do:
// either path does not exist yet — lca is creating it, so there is no earlier
// state to keep — or a backup is already there, because "first time" is
// derived from that file's existence rather than from a marker lca maintains.
// Deleting a backup therefore re-arms this for the next write.
func Once(path string) (string, error) {
	target, _, err := atomicfile.Target(path)
	if err != nil {
		return "", err
	}
	found, err := filepath.Glob(glob(target))
	if err != nil {
		return "", err
	}
	if len(found) > 0 {
		return "", nil
	}
	b, err := os.ReadFile(target)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	dst := Name(target, time.Now())
	if err := atomicfile.Write(dst, b); err != nil {
		return "", fmt.Errorf("backing up %s: %w", target, err)
	}
	return dst, nil
}
