package qwen

import (
	"encoding/json"
	"fmt"
)

// localSkillsDir is added to skills.directories so qwen-local offers the same
// user skills as ordinary qwen, whose default skill directory this is. Qwen
// Code (0.21+) expands "~" itself; keeping the tilde makes settings.json
// portable across HOMEs. The default $QWEN_HOME/skills stays first and Qwen
// deduplicates skill names, so this never shadows a locally installed skill.
const localSkillsDir = "~/.qwen/skills"

// PrepareLocal owns model selection, lean defaults and the shared skills
// directory in the dedicated local configuration. Unlike ApplyLean, it needs
// no restoration snapshot.
func PrepareLocal(path string, p Provider) (bool, error) {
	m, _, err := loadSettings(path)
	if err != nil {
		return false, err
	}
	before, _ := json.Marshal(m)
	if err := prepareLocal(m, p); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	after, _ := json.Marshal(m)
	if string(before) == string(after) {
		return false, nil
	}
	return true, writeJSON(path, m)
}

// CheckLocal reports drift without modifying settings.
func CheckLocal(path string, p Provider) error {
	m, exists, err := loadSettings(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s missing", path)
	}
	before, _ := json.Marshal(m)
	if err := prepareLocal(m, p); err != nil {
		return err
	}
	after, _ := json.Marshal(m)
	if string(before) != string(after) {
		return fmt.Errorf("local model selection or lean settings differ from config.env")
	}
	return nil
}

func settingsObject(m map[string]any, key string) (map[string]any, error) {
	value, exists := m[key]
	if !exists {
		obj := map[string]any{}
		m[key] = obj
		return obj, nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("settings.%s must be an object", key)
	}
	return obj, nil
}

func prepareLocal(m map[string]any, p Provider) error {
	model, err := settingsObject(m, "model")
	if err != nil {
		return err
	}
	security, err := settingsObject(m, "security")
	if err != nil {
		return err
	}
	auth, err := settingsObject(security, "auth")
	if err != nil {
		return err
	}
	values, err := leanValues(m)
	if err != nil {
		return err
	}
	for path, value := range values {
		putState(m, path, settingState{Present: true, Value: value})
	}
	if err := addSkillsDir(m, localSkillsDir); err != nil {
		return err
	}

	model["name"] = p.ID
	auth["selectedType"] = "openai"
	mergeProvider(m, p)
	return nil
}

// addSkillsDir appends dir to skills.directories unless already listed,
// keeping existing entries and their order.
func addSkillsDir(m map[string]any, dir string) error {
	skills, err := settingsObject(m, "skills")
	if err != nil {
		return err
	}
	var list []any
	if value, exists := skills["directories"]; exists {
		var ok bool
		if list, ok = value.([]any); !ok {
			return fmt.Errorf("settings.skills.directories must be an array")
		}
	}
	for _, entry := range list {
		s, ok := entry.(string)
		if !ok {
			return fmt.Errorf("settings.skills.directories entries must be strings")
		}
		if s == dir {
			return nil
		}
	}
	skills["directories"] = append(append([]any{}, list...), dir)
	return nil
}
