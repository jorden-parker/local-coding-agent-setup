package qwen

import (
	"encoding/json"
	"fmt"
	"strings"
)

// localAPIKey is the placeholder stored in settings.env for the shared
// provider entry. llama-server ignores API keys, but Qwen Code refuses an
// OpenAI-compatible provider without one. It reads the key from the process
// environment first and falls back to settings.env, so a real key exported
// in the shell still wins.
const localAPIKey = "local"

// ShareLocal mirrors p into ordinary Qwen's settings so plain qwen and the
// VS Code companion's chat view, which runs a bundled CLI against
// ~/.qwen/settings.json, can pick the local model. It merges the
// modelProviders.openai[] entry, stores the placeholder API key under
// env.<EnvKey> unless a value is already there, and selects the local model
// only when no auth type is chosen yet. Every other key is left alone.
func ShareLocal(path string, p Provider) (bool, error) {
	m, _, err := loadSettings(path)
	if err != nil {
		return false, err
	}
	before, _ := json.Marshal(m)
	if err := shareLocal(m, p); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	after, _ := json.Marshal(m)
	if string(before) == string(after) {
		return false, nil
	}
	return true, writeJSON(path, m)
}

// CheckShared reports whether ordinary Qwen's settings carry the shared
// provider entry and its key, without modifying them.
func CheckShared(path string, p Provider) error {
	m, exists, err := loadSettings(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s missing", path)
	}
	before, _ := json.Marshal(m)
	if err := shareLocal(m, p); err != nil {
		return err
	}
	after, _ := json.Marshal(m)
	if string(before) != string(after) {
		return fmt.Errorf("local provider %q or its env.%s is missing or differs from config.env", p.ID, p.EnvKey)
	}
	return nil
}

func shareLocal(m map[string]any, p Provider) error {
	mergeProvider(m, p)
	env, err := settingsObject(m, "env")
	if err != nil {
		return err
	}
	if s, _ := env[p.EnvKey].(string); strings.TrimSpace(s) == "" {
		env[p.EnvKey] = localAPIKey
	}
	selected, err := stateAt(m, "security.auth")
	if err != nil {
		return err
	}
	if auth, ok := selected.Value.(map[string]any); ok {
		if s, _ := auth["selectedType"].(string); s != "" {
			return nil
		}
	} else if selected.Present {
		return fmt.Errorf("settings.security.auth must be an object")
	}
	security, err := settingsObject(m, "security")
	if err != nil {
		return err
	}
	auth, err := settingsObject(security, "auth")
	if err != nil {
		return err
	}
	model, err := settingsObject(m, "model")
	if err != nil {
		return err
	}
	auth["selectedType"] = "openai"
	model["name"] = p.ID
	return nil
}
