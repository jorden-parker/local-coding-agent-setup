// Package paths resolves where lca reads and writes. Every function consults
// the environment at call time so tests can point HOME and XDG_* at a
// temporary directory.
package paths

import (
	"os"
	"path/filepath"
)

func home() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func xdg(name, fallback string) string {
	if v := os.Getenv(name); v != "" && filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(home(), fallback)
}

// ConfigDir is ${XDG_CONFIG_HOME:-~/.config}/llama-coder.
func ConfigDir() string { return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "llama-coder") }

// Config is the config.env file the launchers source.
func Config() string { return filepath.Join(ConfigDir(), "config.env") }

// StateDir is ${XDG_STATE_HOME:-~/.local/state}/llama-coder, where server
// logs and compacted timings live.
func StateDir() string {
	return filepath.Join(xdg("XDG_STATE_HOME", filepath.Join(".local", "state")), "llama-coder")
}

// Timings is the compacted per-request timing file.
func Timings() string { return filepath.Join(StateDir(), "timings.jsonl") }

// QwenDir holds the managed qwen-local configuration, independent of QWEN_HOME.
func QwenDir() string { return filepath.Join(ConfigDir(), "qwen") }

// QwenInstanceDir is QwenDir() when port matches cfgPort (config.env's own
// PORT), or an isolated "qwen-<port>" sibling otherwise. A second
// llama-coder/qwen-local pair started on a different port then never shares
// mutable Qwen state, and races on the same settings file, with the default
// instance.
func QwenInstanceDir(port, cfgPort string) string {
	if port == "" || port == cfgPort {
		return QwenDir()
	}
	return filepath.Join(ConfigDir(), "qwen-"+port)
}

// QwenSettings is the managed qwen-local settings file.
func QwenSettings() string { return filepath.Join(QwenDir(), "settings.json") }

// QwenSettingsFor is the managed settings file for QwenInstanceDir(port, cfgPort).
func QwenSettingsFor(port, cfgPort string) string {
	return filepath.Join(QwenInstanceDir(port, cfgPort), "settings.json")
}

// QwenUsageDir holds Qwen Code's token-usage-YYYY-MM.jsonl files.
func QwenUsageDir() string { return filepath.Join(QwenDir(), "usage") }

// QwenUsageDirs lists every directory lca stats reads Qwen usage from: the
// default instance's, every qwen-<port> instance qwen-local has created
// (sorted by name), then the legacy ~/.qwen one. Earlier entries win for
// duplicate record IDs.
func QwenUsageDirs() []string {
	dirs := []string{QwenUsageDir()}
	extra, _ := filepath.Glob(filepath.Join(ConfigDir(), "qwen-*", "usage"))
	dirs = append(dirs, extra...)
	return append(dirs, LegacyQwenUsageDir())
}

// LegacyQwenDir holds ordinary Qwen settings and historical usage.
func LegacyQwenDir() string { return filepath.Join(home(), ".qwen") }

func LegacyQwenSettings() string { return filepath.Join(LegacyQwenDir(), "settings.json") }

func LegacyQwenUsageDir() string { return filepath.Join(LegacyQwenDir(), "usage") }

// BinDir is ~/.local/bin, where setup.sh installs the launchers and lca.
func BinDir() string { return filepath.Join(home(), ".local", "bin") }

// Tildify shortens a path under HOME for display.
func Tildify(p string) string {
	h := home()
	if h != "" && len(p) >= len(h) && p[:len(h)] == h && (len(p) == len(h) || p[len(h)] == '/') {
		return "~" + p[len(h):]
	}
	return p
}
