package pi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// PrepareModels owns providers.llama-local in pi's models.json: the
// OpenAI-compatible endpoint of the local llama-server and the one model it
// serves. Other providers, other models of this provider and every unrelated
// key are left alone — this is the user's own models.json, so nothing that
// lca does not need is written. It reports whether the file changed.
//
// pi's settings.json is deliberately not touched. defaultProvider and
// defaultModel are the user's own selection, and pi-local does not need them:
// it passes --provider and --model explicitly.
func PrepareModels(path string, p Provider) (bool, error) {
	return apply(path, func(m map[string]any) error { return mergeProvider(m, p) })
}

// CheckModels reports drift between models.json and config.env.
func CheckModels(path string, p Provider) error {
	return check(path, func(m map[string]any) error { return mergeProvider(m, p) },
		fmt.Sprintf("provider %q or model %q is missing or differs from config.env", p.ID, p.Model))
}

// HasProvider reports whether path already carries a provider entry with id.
// lca uses it to tell a pi configuration it has adopted from one it has never
// written: only an adopted one is resynced when ALIAS, PORT or CTX change, so
// installing pi for other work never invites lca into its configuration. A
// missing file is not an error.
func HasProvider(path, id string) (bool, error) {
	m, exists, err := load(path)
	if err != nil || !exists {
		return false, err
	}
	providers, ok := m["providers"].(map[string]any)
	if !ok {
		return false, nil
	}
	_, ok = providers[id].(map[string]any)
	return ok, nil
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
	for k, v := range providerFields(p) {
		entry[k] = v
	}

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
	for k, v := range modelFields(p) {
		model[k] = v
	}
	entry["models"] = list
	return nil
}

// providerFields is every provider key lca owns, and nothing else.
func providerFields(p Provider) map[string]any {
	return map[string]any{
		"name":    p.Name,
		"baseUrl": p.BaseURL,
		// pi has no llama.cpp single-model API of its own; llama-server's /v1
		// is OpenAI chat completions.
		"api":    "openai-completions",
		"apiKey": p.APIKey,
	}
}

// modelFields is every model key lca owns, and nothing else.
func modelFields(p Provider) map[string]any {
	return map[string]any{
		"id":    p.Model,
		"name":  p.Name,
		"input": []any{"text"},
		// pi compacts against contextWindow the way Qwen Code does against
		// generationConfig.contextWindowSize; without it the default is far
		// too large and compaction never triggers before llama-server
		// truncates.
		"contextWindow": float64(p.ContextWindow),
		"reasoning":     p.Reasoning,
		// A local model is free. Zeroed costs keep pi's footer and session
		// statistics from inventing a price.
		"cost": map[string]any{"input": float64(0), "output": float64(0), "cacheRead": float64(0), "cacheWrite": float64(0)},
	}
}

// Removal reports what UnprepareModels did to one file.
type Removal struct {
	// Removed names each thing taken out, for the command's output.
	Removed []string
	// Left names what was recognisably lca's but had been edited since, and
	// so was kept rather than guessed at.
	Left []string
	// Empty is true when nothing but the document's own scaffolding remains,
	// so a file lca created can be deleted outright.
	Empty bool
}

// UnprepareModels takes lca's provider entry back out of pi's models.json. It
// removes only what it can still recognise as its own: the model whose id is
// p.Model and whose every owned field still matches, and the provider itself
// once that leaves it with no models. An entry the user has edited since is
// reported in Left and kept, because an edited baseUrl may mean the entry is
// now theirs. dryRun computes the result without writing anything.
func UnprepareModels(path string, p Provider, dryRun bool) (Removal, bool, error) {
	var r Removal
	m, exists, err := load(path)
	if err != nil {
		return r, false, err
	}
	if !exists {
		return r, false, nil
	}
	before, _ := json.Marshal(m)
	r = unprepareModels(m, p)
	after, _ := json.Marshal(m)
	r.Empty = len(m) == 0
	if string(before) == string(after) {
		return r, false, nil
	}
	if dryRun {
		return r, true, nil
	}
	return r, true, write(path, m)
}

func unprepareModels(m map[string]any, p Provider) Removal {
	var r Removal
	providers, ok := m["providers"].(map[string]any)
	if !ok {
		return r
	}
	entry, ok := providers[p.ID].(map[string]any)
	if !ok {
		return r
	}
	list, _ := entry["models"].([]any)
	kept := make([]any, 0, len(list))
	for _, e := range list {
		em, ok := e.(map[string]any)
		if !ok {
			kept = append(kept, e)
			continue
		}
		if em["id"] != p.Model {
			kept = append(kept, e)
			continue
		}
		if edited := editedFields(em, modelFields(p)); len(edited) > 0 {
			r.Left = append(r.Left, fmt.Sprintf("providers.%s model %q (edited: %v)", p.ID, p.Model, edited))
			kept = append(kept, e)
			continue
		}
		r.Removed = append(r.Removed, fmt.Sprintf("providers.%s model %q", p.ID, p.Model))
	}
	if len(kept) > 0 {
		entry["models"] = kept
		return r
	}
	// No models left: the provider exists only to carry lca's model.
	delete(entry, "models")
	if edited := editedFields(entry, providerFields(p)); len(edited) > 0 {
		r.Left = append(r.Left, fmt.Sprintf("providers.%s (edited: %v)", p.ID, edited))
		entry["models"] = []any{}
		return r
	}
	if extra := extraKeys(entry, providerFields(p)); len(extra) > 0 {
		r.Left = append(r.Left, fmt.Sprintf("providers.%s (has %v)", p.ID, extra))
		entry["models"] = []any{}
		return r
	}
	delete(providers, p.ID)
	r.Removed = append(r.Removed, "providers."+p.ID)
	if len(providers) == 0 {
		delete(m, "providers")
	}
	return r
}

// editedFields names the keys of want that obj has but with another value.
// A key obj does not have at all is not an edit: lca's own write is simply
// already gone.
func editedFields(obj, want map[string]any) []string {
	var out []string
	for k, v := range want {
		got, exists := obj[k]
		if !exists {
			continue
		}
		if !equalJSON(got, v) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// extraKeys names the keys obj carries that lca does not own, so a provider
// the user has added settings to is never deleted wholesale.
func extraKeys(obj, want map[string]any) []string {
	var out []string
	for k := range obj {
		if _, ok := want[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// equalJSON compares two decoded JSON values. Values lca writes go through a
// marshal/unmarshal round trip, so comparing their encodings avoids having to
// care whether a number arrived as int or float64.
func equalJSON(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
