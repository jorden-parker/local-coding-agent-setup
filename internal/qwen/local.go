package qwen

import (
	"encoding/json"
	"fmt"
)

// PrepareLocal owns model selection and lean defaults in the dedicated local
// configuration. Unlike ApplyLean, it needs no restoration snapshot.
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

	model["name"] = p.ID
	auth["selectedType"] = "openai"
	mergeProvider(m, p)
	return nil
}
