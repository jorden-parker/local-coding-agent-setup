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

// instanceDir is <ConfigDir>/<prefix> when port matches cfgPort (config.env's
// own PORT), or an isolated "<prefix>-<port>" sibling otherwise. A second
// llama-coder/harness pair started on a different port then never shares
// mutable harness state, and races on the same settings file, with the default
// instance.
func instanceDir(prefix, port, cfgPort string) string {
	if port == "" || port == cfgPort {
		return filepath.Join(ConfigDir(), prefix)
	}
	return filepath.Join(ConfigDir(), prefix+"-"+port)
}

// instanceDirs lists the default instance directory, every <prefix>-<port>
// sibling (sorted by name) and finally legacy, each suffixed with leaf.
func instanceDirs(prefix, leaf, legacy string) []string {
	dirs := []string{filepath.Join(ConfigDir(), prefix, leaf)}
	extra, _ := filepath.Glob(filepath.Join(ConfigDir(), prefix+"-*", leaf))
	dirs = append(dirs, extra...)
	return append(dirs, legacy)
}

// QwenDir holds the managed qwen-local configuration, independent of QWEN_HOME.
func QwenDir() string { return filepath.Join(ConfigDir(), "qwen") }

// QwenInstanceDir is the managed Qwen directory for port.
func QwenInstanceDir(port, cfgPort string) string { return instanceDir("qwen", port, cfgPort) }

// QwenSettings is the managed qwen-local settings file.
func QwenSettings() string { return filepath.Join(QwenDir(), "settings.json") }

// QwenSettingsFor is the managed settings file for QwenInstanceDir(port, cfgPort).
func QwenSettingsFor(port, cfgPort string) string {
	return filepath.Join(QwenInstanceDir(port, cfgPort), "settings.json")
}

// QwenSkillsDir is the managed instance's own skill directory, which Qwen Code
// reads as $QWEN_HOME/skills. It starts empty: qwen-local's skills come from
// AgentsSkillsDir and the LegacyQwenSkillsDir entry lca sync writes into
// settings.json.
func QwenSkillsDir() string { return filepath.Join(QwenDir(), "skills") }

// QwenUsageDir holds Qwen Code's token-usage-YYYY-MM.jsonl files.
func QwenUsageDir() string { return filepath.Join(QwenDir(), "usage") }

// QwenUsageDirs lists every directory lca stats reads Qwen usage from: the
// default instance's, every qwen-<port> instance qwen-local has created
// (sorted by name), then the legacy ~/.qwen one. Earlier entries win for
// duplicate record IDs.
func QwenUsageDirs() []string { return instanceDirs("qwen", "usage", LegacyQwenUsageDir()) }

// LegacyQwenDir holds ordinary Qwen settings and historical usage.
func LegacyQwenDir() string { return filepath.Join(home(), ".qwen") }

func LegacyQwenSettings() string { return filepath.Join(LegacyQwenDir(), "settings.json") }

func LegacyQwenUsageDir() string { return filepath.Join(LegacyQwenDir(), "usage") }

// LegacyQwenSkillsDir is ordinary qwen's own skill directory, the one
// qwen.LocalSkillsDir names in settings.json.
func LegacyQwenSkillsDir() string { return filepath.Join(LegacyQwenDir(), "skills") }

// PiDir holds the managed pi-local agent directory, which pi-local points
// PI_CODING_AGENT_DIR at. It is separate from the user's own ~/.pi/agent.
func PiDir() string { return filepath.Join(ConfigDir(), "pi") }

// PiInstanceDir is the managed pi agent directory for port.
func PiInstanceDir(port, cfgPort string) string { return instanceDir("pi", port, cfgPort) }

// PiModels is pi's models.json in the default instance.
func PiModels() string { return filepath.Join(PiDir(), "models.json") }

// PiModelsFor is pi's models.json in PiInstanceDir(port, cfgPort).
func PiModelsFor(port, cfgPort string) string {
	return filepath.Join(PiInstanceDir(port, cfgPort), "models.json")
}

// PiSettings is pi's settings.json in the default instance.
func PiSettings() string { return filepath.Join(PiDir(), "settings.json") }

// PiSettingsFor is pi's settings.json in PiInstanceDir(port, cfgPort).
func PiSettingsFor(port, cfgPort string) string {
	return filepath.Join(PiInstanceDir(port, cfgPort), "settings.json")
}

// PiSkillsDir is the managed instance's own skill directory, which pi reads as
// $PI_CODING_AGENT_DIR/skills. It starts empty: pi-local's skills come from
// AgentsSkillsDir and the LegacyPiSkillsDir entry lca sync writes into
// settings.json.
func PiSkillsDir() string { return filepath.Join(PiDir(), "skills") }

// PiSessionDirs lists every directory lca stats reads pi sessions from: the
// default instance's, every pi-<port> instance pi-local has created (sorted by
// name), then the user's own ~/.pi/agent one. Earlier entries win for
// duplicate entry IDs.
func PiSessionDirs() []string { return instanceDirs("pi", "sessions", LegacyPiSessionDir()) }

// LegacyPiDir is pi's own agent directory, used when pi runs outside pi-local.
func LegacyPiDir() string { return filepath.Join(home(), ".pi", "agent") }

// LegacyPiSessionDir holds the sessions of pi runs outside pi-local.
func LegacyPiSessionDir() string { return filepath.Join(LegacyPiDir(), "sessions") }

// LegacyPiSkillsDir is ordinary pi's own skill directory, the one
// pi.LocalSkillsDir names in settings.json.
func LegacyPiSkillsDir() string { return filepath.Join(LegacyPiDir(), "skills") }

// AgentsSkillsDir is the harness-neutral ~/.agents/skills, which both Qwen Code
// and pi read at user level straight from HOME. Pointing QWEN_HOME or
// PI_CODING_AGENT_DIR at a managed instance directory does not move it, so
// skills installed there reach qwen-local and pi-local without any settings
// entry.
func AgentsSkillsDir() string { return filepath.Join(home(), ".agents", "skills") }

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
