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

// InstancesDir holds one directory per port a harness launcher has claimed.
// local-harness owns the layout; lca only reads it.
func InstancesDir() string { return filepath.Join(StateDir(), "instances") }

// InstanceDir is the claim directory for port.
func InstanceDir(port string) string { return filepath.Join(InstancesDir(), port) }

// LauncherLog is the console output of the llama-server started for port. It
// is where a Metal backend failure shows up: the server keeps answering
// /health, /slots and /metrics as if healthy, so its log is the only signal.
func LauncherLog(port string) string { return filepath.Join(InstanceDir(port), "launcher.log") }

// QwenDir is the Qwen Code configuration directory lca patches: $QWEN_HOME
// when the shell exports an absolute one, otherwise ~/.qwen. Qwen Code itself
// honours QWEN_HOME, so ignoring it would let lca write a provider entry the
// harness never reads — and report it as present. A relative value is ignored
// the way xdg() ignores a relative XDG_CONFIG_HOME.
func QwenDir() string { return harnessDir("QWEN_HOME", ".qwen") }

// QwenSettings is the settings file lca merges the local provider into.
func QwenSettings() string { return filepath.Join(QwenDir(), "settings.json") }

// QwenUsageDir holds Qwen Code's token-usage-YYYY-MM.jsonl files.
func QwenUsageDir() string { return filepath.Join(QwenDir(), "usage") }

// QwenSkillsDir is Qwen Code's own skill directory.
func QwenSkillsDir() string { return filepath.Join(QwenDir(), "skills") }

// PiDir is the pi agent directory lca patches: $PI_CODING_AGENT_DIR when the
// shell exports an absolute one, otherwise ~/.pi/agent. pi honours that
// variable, so the same reasoning as QwenDir applies.
func PiDir() string { return harnessDir("PI_CODING_AGENT_DIR", filepath.Join(".pi", "agent")) }

// PiModels is the models.json lca merges the local provider into.
func PiModels() string { return filepath.Join(PiDir(), "models.json") }

// PiSessionDir holds pi's session files, which lca stats derives timings from.
func PiSessionDir() string { return filepath.Join(PiDir(), "sessions") }

// PiSkillsDir is pi's own skill directory.
func PiSkillsDir() string { return filepath.Join(PiDir(), "skills") }

// harnessDir returns an absolute override from the environment, else
// home()/fallback. Only an absolute value wins: a relative one would resolve
// against each process's working directory, so lca and the harness it
// configures could disagree about which file is theirs.
func harnessDir(env, fallback string) string {
	if v := os.Getenv(env); v != "" && filepath.IsAbs(v) {
		return v
	}
	return filepath.Join(home(), fallback)
}

// AgentsSkillsDir is the harness-neutral ~/.agents/skills, which both Qwen
// Code and pi read at user level straight from HOME. No settings entry names
// it, and neither QWEN_HOME nor PI_CODING_AGENT_DIR moves it.
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
