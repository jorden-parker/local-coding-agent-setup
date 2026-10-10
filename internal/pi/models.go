package pi

import "fmt"

// LocalSkillsDir is added to settings.skills so pi-local offers the same user
// skills as ordinary pi, whose default skill directory this is. pi expands "~"
// itself in resource paths, so keeping the tilde makes settings.json portable
// across HOMEs.
const LocalSkillsDir = "~/.pi/agent/skills"

// PrepareModels owns providers.llama-local in pi's models.json: the
// OpenAI-compatible endpoint of the local llama-server and the one model it
// serves. Other providers, other models of this provider and every unrelated
// key are left alone. It reports whether the file changed.
func PrepareModels(path string, p Provider) (bool, error) {
	return apply(path, func(m map[string]any) error { return mergeProvider(m, p) })
}

// CheckModels reports drift between models.json and config.env.
func CheckModels(path string, p Provider) error {
	return check(path, func(m map[string]any) error { return mergeProvider(m, p) },
		fmt.Sprintf("provider %q or model %q is missing or differs from config.env", p.ID, p.Model))
}

// PrepareSettings owns the default model selection and the shared skills
// directory in pi's settings.json. Every other setting is left alone.
func PrepareSettings(path string, p Provider) (bool, error) {
	return apply(path, func(m map[string]any) error { return prepareSettings(m, p) })
}

// CheckSettings reports drift between settings.json and config.env.
func CheckSettings(path string, p Provider) error {
	return check(path, func(m map[string]any) error { return prepareSettings(m, p) },
		"default model selection or the shared skills directory differs from config.env")
}

// mergeProvider updates only the provider fields lca owns. models[] keeps any
// entry whose id differs, so a hand-added model survives a sync.
func mergeProvider(m map[string]any, p Provider) error {
	providers, err := object(m, "providers")
	if err != nil {
		return err
	}
	entry, err := object(providers, p.ID)
	if err != nil {
		return err
	}
	entry["name"] = p.Name
	entry["baseUrl"] = p.BaseURL
	// pi has no llama.cpp single-model API of its own; llama-server's /v1 is
	// OpenAI chat completions.
	entry["api"] = "openai-completions"
	entry["apiKey"] = p.APIKey

	list, _ := entry["models"].([]any)
	if list == nil {
		if _, exists := entry["models"]; exists {
			return fmt.Errorf("providers.%s.models must be an array", p.ID)
		}
	}
	var model map[string]any
	for _, e := range list {
		em, ok := e.(map[string]any)
		if !ok {
			return fmt.Errorf("providers.%s.models entries must be objects", p.ID)
		}
		if em["id"] == p.Model {
			model = em
			break
		}
	}
	if model == nil {
		model = map[string]any{}
		list = append(list, model)
	}
	model["id"] = p.Model
	model["name"] = p.Name
	model["input"] = []any{"text"}
	// pi compacts against contextWindow the way Qwen Code does against
	// generationConfig.contextWindowSize; without it the default is far too
	// large and compaction never triggers before llama-server truncates.
	model["contextWindow"] = float64(p.ContextWindow)
	model["reasoning"] = p.Reasoning
	// A local model is free. Zeroed costs keep pi's footer and session
	// statistics from inventing a price.
	model["cost"] = map[string]any{"input": float64(0), "output": float64(0), "cacheRead": float64(0), "cacheWrite": float64(0)}
	entry["models"] = list
	return nil
}

func prepareSettings(m map[string]any, p Provider) error {
	m["defaultProvider"] = p.ID
	m["defaultModel"] = p.Model
	return addSkillsDir(m, LocalSkillsDir)
}

// addSkillsDir appends dir to settings.skills unless already listed, keeping
// existing entries and their order.
func addSkillsDir(m map[string]any, dir string) error {
	var list []any
	if value, exists := m["skills"]; exists {
		var ok bool
		if list, ok = value.([]any); !ok {
			return fmt.Errorf("settings.skills must be an array")
		}
	}
	for _, entry := range list {
		s, ok := entry.(string)
		if !ok {
			return fmt.Errorf("settings.skills entries must be strings")
		}
		if s == dir {
			return nil
		}
	}
	m["skills"] = append(append([]any{}, list...), dir)
	return nil
}
