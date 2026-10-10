// Package config describes the keys in config.env, validates values, and
// reads and writes the file without disturbing comments or unknown keys.
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Kind is the value type of a key.
type Kind string

// Kinds.
const (
	KindPath   Kind = "path"
	KindString Kind = "string"
	KindInt    Kind = "int"
	KindFloat  Kind = "float"
	KindBool   Kind = "bool"
	KindArgs   Kind = "args"
	KindEnum   Kind = "enum"
)

// Key describes one config.env entry.
type Key struct {
	Name    string
	Kind    Kind
	Default string   // fallback for optional keys absent from older config files
	Choices []string // the allowed values of a KindEnum key
	Min     float64
	Max     float64
	Help    string
	Flag    string // llama-server flag the key feeds, "" if none
	Syncs   bool   // changing it must also update the harness provider entry
	Restart string // which process must restart for the change to apply, "" if none
}

// Keys is the catalogue in file order.
var Keys = []Key{
	{Name: "HARNESS", Kind: KindEnum, Choices: []string{"qwen", "pi"}, Default: "qwen", Help: "Agent harness lca syncs and reports on by default: qwen (Qwen Code) or pi. qwen-local and pi-local always use their own.", Syncs: true},
	{Name: "MODEL_PATH", Kind: KindPath, Help: "Absolute path to the GGUF model file.", Flag: "-m", Restart: "llama-coder"},
	{Name: "ALIAS", Kind: KindString, Help: "Model id llama-server reports and the harness requests. Also the Qwen provider id.", Flag: "--alias", Syncs: true, Restart: "llama-coder and qwen-local/pi-local"},
	{Name: "CTX", Kind: KindInt, Min: 2048, Max: 1048576, Help: "Context window in tokens, multiple of 256. Mirrored to the harness settings.", Flag: "-c", Syncs: true, Restart: "llama-coder and qwen-local/pi-local"},
	{Name: "PORT", Kind: KindInt, Min: 1024, Max: 65535, Help: "Port llama-server listens on and qwen-local or pi-local connects to.", Flag: "--port", Syncs: true, Restart: "llama-coder and qwen-local/pi-local"},
	{Name: "TEMP", Kind: KindFloat, Min: 0, Max: 2, Default: "0.7", Help: "Sampling temperature.", Flag: "--temp", Restart: "llama-coder"},
	{Name: "TOP_P", Kind: KindFloat, Min: 0, Max: 1, Default: "0.8", Help: "Nucleus sampling cutoff.", Flag: "--top-p", Restart: "llama-coder"},
	{Name: "TOP_K", Kind: KindInt, Min: 0, Max: 1000, Default: "20", Help: "Top-k sampling, 0 disables.", Flag: "--top-k", Restart: "llama-coder"},
	{Name: "MIN_P", Kind: KindFloat, Min: 0, Max: 1, Default: "0.0", Help: "Min-p sampling cutoff.", Flag: "--min-p", Restart: "llama-coder"},
	{Name: "PRESENCE_PENALTY", Kind: KindFloat, Min: -2, Max: 2, Default: "1.5", Help: "Presence penalty; Unsloth recommends 1.5 for Qwen3.x non-thinking.", Flag: "--presence-penalty", Restart: "llama-coder"},
	{Name: "THINKING", Kind: KindBool, Default: "false", Help: "true enables reasoning via --chat-template-kwargs enable_thinking (slower agent turns).", Flag: "--chat-template-kwargs", Restart: "llama-coder"},
	{Name: "CACHE_RAM", Kind: KindInt, Min: 0, Max: 1048576, Default: "8192", Help: "Host prompt-cache limit in MiB, 0 disables it. Active slot prefix caching remains enabled.", Flag: "--cache-ram", Restart: "llama-coder"},
	{Name: "CTX_CHECKPOINTS", Kind: KindInt, Min: 0, Max: 1024, Default: "32", Help: "Maximum recurrent/window state checkpoints per slot, 0 disables them.", Flag: "--ctx-checkpoints", Restart: "llama-coder"},
	{Name: "EXTRA_ARGS", Kind: KindArgs, Help: "Extra llama-server flags, whitespace-separated, no quoting (e.g. --jinja).", Restart: "llama-coder"},
}

// Lookup returns the catalogue entry for name.
func Lookup(name string) (Key, bool) {
	for _, k := range Keys {
		if k.Name == name {
			return k, true
		}
	}
	return Key{}, false
}

// Names lists the catalogue keys in order.
func Names() []string {
	out := make([]string, len(Keys))
	for i, k := range Keys {
		out[i] = k.Name
	}
	return out
}

// Validate checks value for key name. It returns a warning (non-fatal) and an
// error. Unknown keys are an error.
func Validate(name, value string) (warn string, err error) {
	k, ok := Lookup(name)
	if !ok {
		return "", fmt.Errorf("unknown key %s (known: %s)", name, strings.Join(Names(), ", "))
	}
	value = strings.TrimSpace(value)
	switch k.Kind {
	case KindPath:
		if value == "" {
			return "", fmt.Errorf("%s must not be empty", name)
		}
		if !filepath.IsAbs(value) {
			return "", fmt.Errorf("%s must be an absolute path", name)
		}
		fi, err := os.Stat(value)
		if err != nil {
			return "", fmt.Errorf("%s: %v", name, err)
		}
		if !fi.Mode().IsRegular() {
			return "", fmt.Errorf("%s: %s is not a regular file", name, value)
		}
		if !strings.HasSuffix(strings.ToLower(value), ".gguf") {
			return fmt.Sprintf("%s does not end in .gguf", name), nil
		}
		return "", nil
	case KindString:
		if value == "" {
			return "", fmt.Errorf("%s must not be empty", name)
		}
		if strings.ContainsAny(value, " \t\n'\"") {
			return "", fmt.Errorf("%s must not contain whitespace or quotes", name)
		}
		return "", nil
	case KindInt:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", fmt.Errorf("%s must be an integer", name)
		}
		if float64(n) < k.Min || float64(n) > k.Max {
			return "", fmt.Errorf("%s must be between %d and %d", name, int64(k.Min), int64(k.Max))
		}
		if name == "CTX" && n%256 != 0 {
			return "", fmt.Errorf("CTX must be a multiple of 256")
		}
		return "", nil
	case KindFloat:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("%s must be a number", name)
		}
		if f < k.Min || f > k.Max {
			return "", fmt.Errorf("%s must be between %g and %g", name, k.Min, k.Max)
		}
		return "", nil
	case KindBool:
		if value != "true" && value != "false" {
			return "", fmt.Errorf("%s must be true or false", name)
		}
		return "", nil
	case KindEnum:
		for _, c := range k.Choices {
			if value == c {
				return "", nil
			}
		}
		return "", fmt.Errorf("%s must be one of %s", name, strings.Join(k.Choices, ", "))
	case KindArgs:
		return validateArgs(name, value)
	}
	return "", fmt.Errorf("%s: unhandled kind %s", name, k.Kind)
}

// fixedFlags are llama-server flags the launcher always passes and that
// EXTRA_ARGS must not repeat.
var fixedFlags = map[string]string{
	"-fa": "", "--flash-attn": "", "-ngl": "", "--n-gpu-layers": "", "-np": "", "--parallel": "",
	"--host": "", "--log-file": "", "--log-timestamps": "", "--metrics": "",
}

func validateArgs(name, value string) (string, error) {
	if strings.ContainsAny(value, "\n\r'\"") {
		return "", fmt.Errorf("%s must not contain newlines or quotes", name)
	}
	owned := map[string]string{}
	for _, k := range Keys {
		if k.Flag != "" {
			owned[k.Flag] = k.Name
		}
	}
	owned["--ctx-size"] = "CTX"
	owned["--model"] = "MODEL_PATH"
	owned["-a"] = "ALIAS"
	owned["-cram"] = "CACHE_RAM"
	owned["-ctxcp"] = "CTX_CHECKPOINTS"
	owned["--swa-checkpoints"] = "CTX_CHECKPOINTS"
	for _, tok := range strings.Fields(value) {
		flag := tok
		if i := strings.IndexByte(tok, '='); i > 0 {
			flag = tok[:i]
		}
		if key, ok := owned[flag]; ok {
			return "", fmt.Errorf("%s must not contain %s; set %s instead", name, flag, key)
		}
		if _, ok := fixedFlags[flag]; ok {
			return "", fmt.Errorf("%s must not contain %s; the launcher sets it", name, flag)
		}
	}
	return "", nil
}

// ValidateAll checks every catalogue key present in values. Missing keys
// without a default are reported as errors.
func ValidateAll(values map[string]string) (warns []string, errs []error) {
	for _, k := range Keys {
		v, ok := values[k.Name]
		if !ok {
			if k.Default == "" && k.Kind != KindArgs {
				errs = append(errs, fmt.Errorf("%s is not set", k.Name))
			}
			continue
		}
		w, err := Validate(k.Name, v)
		if w != "" {
			warns = append(warns, w)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return warns, errs
}
