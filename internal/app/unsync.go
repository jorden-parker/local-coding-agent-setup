package app

import (
	"fmt"
	"os"

	"github.com/jorden-parker/local-coding-agent-setup/internal/backup"
	"github.com/jorden-parker/local-coding-agent-setup/internal/config"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/pi"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
)

// Unsynced reports what UnsyncQwen or UnsyncPi did to one harness.
type Unsynced struct {
	Harness string
	// Path is the configuration file that was edited.
	Path string
	// Removed names each thing taken out of it.
	Removed []string
	// Left names what was recognisably lca's but had been edited since, and
	// so was kept rather than guessed at.
	Left []string
	// Changed reports whether the file was rewritten.
	Changed bool
	// Deleted reports that the file was removed outright, which happens only
	// when nothing of the user's was left in it and lca had created it.
	Deleted bool
	// Backup names the copy lca took before its first write, if it is still
	// there. It is the only way back from the reordering that first sync did.
	Backup string
	// LeanApplied reports that the Qwen Code lean profile is still in place;
	// unsync deliberately does not revert it.
	LeanApplied bool
}

// UnsyncQwen takes lca's provider entry, placeholder API key and — only while
// it is still exactly what lca would have written — the local model selection
// back out of the user's Qwen Code settings. dryRun reports what would change
// without writing.
func UnsyncQwen(f *config.File, dryRun bool) (Unsynced, error) {
	u := Unsynced{Harness: "qwen", Path: paths.QwenSettings()}
	p, err := Provider(f)
	if err != nil {
		return u, err
	}
	u.Backup = newestBackup(u.Path)
	if _, err := os.Stat(qwen.SnapshotFor(u.Path)); err == nil {
		u.LeanApplied = true
	}
	r, changed, err := qwen.Unprepare(u.Path, p, dryRun)
	u.Removed, u.Left, u.Changed = r.Removed, r.Left, changed
	if err != nil {
		return u, err
	}
	u.Deleted, err = deleteIfOurs(u.Path, r.Empty, u.Backup, dryRun)
	return u, err
}

// UnsyncPi takes lca's provider entry back out of the user's pi models.json.
func UnsyncPi(f *config.File, dryRun bool) (Unsynced, error) {
	u := Unsynced{Harness: "pi", Path: paths.PiModels()}
	p, err := PiProvider(f)
	if err != nil {
		return u, err
	}
	u.Backup = newestBackup(u.Path)
	r, changed, err := pi.UnprepareModels(u.Path, p, dryRun)
	u.Removed, u.Left, u.Changed = r.Removed, r.Left, changed
	if err != nil {
		return u, err
	}
	u.Deleted, err = deleteIfOurs(u.Path, r.Empty, u.Backup, dryRun)
	return u, err
}

// UnsyncAll undoes every harness lca manages, in Harnesses order, or just the
// one named.
func UnsyncAll(f *config.File, harness string, dryRun bool) ([]Unsynced, error) {
	harnesses := []string{harness}
	if harness == "" {
		harnesses = Harnesses(f)
	}
	var out []Unsynced
	for _, h := range harnesses {
		var u Unsynced
		var err error
		switch h {
		case "qwen":
			u, err = UnsyncQwen(f, dryRun)
		case "pi":
			u, err = UnsyncPi(f, dryRun)
		default:
			return nil, fmt.Errorf("--harness must be qwen or pi, not %q", h)
		}
		if err != nil {
			return out, err
		}
		out = append(out, u)
	}
	return out, nil
}

// deleteIfOurs removes a configuration file that lca created and that now
// holds nothing of the user's. The absence of a backup is the proof it was
// lca's: backup.Once takes one whenever the file already existed, so nothing
// there means lca wrote the file from nothing.
func deleteIfOurs(path string, empty bool, backedUp string, dryRun bool) (bool, error) {
	if !empty || backedUp != "" {
		return false, nil
	}
	if dryRun {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

// newestBackup is the latest copy lca took of path, or "".
func newestBackup(path string) string {
	found, err := backup.Existing(path)
	if err != nil || len(found) == 0 {
		return ""
	}
	return found[len(found)-1]
}
