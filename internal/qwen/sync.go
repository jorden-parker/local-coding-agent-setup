package qwen

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// localAPIKey is the placeholder stored in settings.env for the local
// provider entry. llama-server ignores API keys, but Qwen Code refuses an
// OpenAI-compatible provider without one. It reads the key from the process
// environment first and falls back to settings.env, so a real key exported
// in the shell still wins.
const localAPIKey = "local"

// Prepare merges the local provider into the user's own Qwen Code settings.
// This is their file, so it writes as little as it can: the
// modelProviders.openai[] entry, the placeholder API key under env.<EnvKey>
// unless a value is already there, and the model selection only when no auth
// type has been chosen yet. qwen-local does not depend on that selection — it
// passes --auth-type, --model, --openai-base-url and --openai-api-key
// explicitly — so it exists only to make plain qwen, and the VS Code
// companion's chat view, able to reach the local server on a fresh install.
//
// The lean profile is deliberately not applied here. It changes how Qwen Code
// behaves for every model, so it belongs to the opt-in lca qwen-profile
// command and its restoration snapshot, not to a sync that runs on every
// launch.
func Prepare(path string, p Provider) (bool, error) {
	m, _, err := loadSettings(path)
	if err != nil {
		return false, err
	}
	before, _ := json.Marshal(m)
	if err := prepare(m, p); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	after, _ := json.Marshal(m)
	if string(before) == string(after) {
		return false, nil
	}
	return true, writeJSON(path, m)
}

// Check reports whether the settings carry the local provider entry and its
// key, without modifying them.
func Check(path string, p Provider) error {
	m, exists, err := loadSettings(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s missing", path)
	}
	before, _ := json.Marshal(m)
	if err := prepare(m, p); err != nil {
		return err
	}
	after, _ := json.Marshal(m)
	if string(before) != string(after) {
		return fmt.Errorf("local provider %q or its env.%s is missing or differs from config.env", p.ID, p.EnvKey)
	}
	return nil
}

func prepare(m map[string]any, p Provider) error {
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

// settingsObject returns m[key] as an object, creating it when absent.
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

// Removal reports what Unprepare did to one settings file.
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

// Unprepare takes lca's additions back out of the user's Qwen Code settings.
// It removes only what it can still recognise as its own:
//
//   - the modelProviders.openai[] entry whose id is p.ID, when every field lca
//     owns still matches and nothing has been added to it;
//   - env.<EnvKey>, only while it is still the placeholder — any other value
//     is the user's real key;
//   - security.auth.selectedType and model.name, only while they are still
//     exactly what Prepare would have written. That last one is a judgement
//     call: a user who had independently chosen openai and this model is
//     indistinguishable from lca's own write, so the selection is cleared in
//     that case too.
//
// The lean profile is out of scope; lca qwen-profile restore reverts it.
// dryRun computes the result without writing anything.
func Unprepare(path string, p Provider, dryRun bool) (Removal, bool, error) {
	var r Removal
	m, exists, err := loadSettings(path)
	if err != nil {
		return r, false, err
	}
	if !exists {
		return r, false, nil
	}
	before, _ := json.Marshal(m)
	r = unprepare(m, p)
	after, _ := json.Marshal(m)
	// $version is scaffolding loadSettings seeds, not content.
	r.Empty = len(m) == 0 || (len(m) == 1 && m["$version"] != nil)
	if string(before) == string(after) {
		return r, false, nil
	}
	if dryRun {
		return r, true, nil
	}
	return r, true, writeJSON(path, m)
}

func unprepare(m map[string]any, p Provider) Removal {
	var r Removal
	r.removeProvider(m, p)
	r.removeEnvKey(m, p)
	r.removeSelection(m, p)
	return r
}

// removeProvider drops lca's entry from modelProviders.openai[].
func (r *Removal) removeProvider(m map[string]any, p Provider) {
	mp, ok := m["modelProviders"].(map[string]any)
	if !ok {
		return
	}
	list, _ := mp["openai"].([]any)
	kept := make([]any, 0, len(list))
	for _, e := range list {
		em, ok := e.(map[string]any)
		if !ok || em["id"] != p.ID {
			kept = append(kept, e)
			continue
		}
		want := providerFields(p)
		if edited := editedFields(em, want); len(edited) > 0 {
			r.Left = append(r.Left, fmt.Sprintf("modelProviders.openai %q (edited: %v)", p.ID, edited))
			kept = append(kept, e)
			continue
		}
		if extra := extraKeys(em, want); len(extra) > 0 {
			r.Left = append(r.Left, fmt.Sprintf("modelProviders.openai %q (has %v)", p.ID, extra))
			kept = append(kept, e)
			continue
		}
		r.Removed = append(r.Removed, fmt.Sprintf("modelProviders.openai %q", p.ID))
	}
	if len(kept) > 0 {
		mp["openai"] = kept
		return
	}
	delete(mp, "openai")
	if len(mp) == 0 {
		delete(m, "modelProviders")
	}
}

// removeEnvKey drops the placeholder API key, never a real one.
func (r *Removal) removeEnvKey(m map[string]any, p Provider) {
	env, ok := m["env"].(map[string]any)
	if !ok {
		return
	}
	s, _ := env[p.EnvKey].(string)
	if s != localAPIKey {
		if s != "" {
			r.Left = append(r.Left, "env."+p.EnvKey+" (not the placeholder)")
		}
		return
	}
	delete(env, p.EnvKey)
	r.Removed = append(r.Removed, "env."+p.EnvKey)
	if len(env) == 0 {
		delete(m, "env")
	}
}

// removeSelection clears the model selection Prepare makes on a fresh config.
func (r *Removal) removeSelection(m map[string]any, p Provider) {
	security, _ := m["security"].(map[string]any)
	auth, _ := security["auth"].(map[string]any)
	model, _ := m["model"].(map[string]any)
	selected, _ := auth["selectedType"].(string)
	named, _ := model["name"].(string)
	if selected != "openai" || named != p.ID {
		return
	}
	delete(auth, "selectedType")
	delete(model, "name")
	r.Removed = append(r.Removed, "security.auth.selectedType", "model.name")
	if len(auth) == 0 {
		delete(security, "auth")
	}
	if len(security) == 0 {
		delete(m, "security")
	}
	if len(model) == 0 {
		delete(m, "model")
	}
}

// providerFields is every key mergeProvider writes into an openai entry.
func providerFields(p Provider) map[string]any {
	return map[string]any{
		"id":               p.ID,
		"name":             p.Name,
		"baseUrl":          p.BaseURL,
		"envKey":           p.EnvKey,
		"generationConfig": map[string]any{"contextWindowSize": float64(p.ContextWindow)},
	}
}

// editedFields names the keys of want that obj has but with another value. A
// key obj does not have at all is not an edit: lca's write is simply gone.
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

// extraKeys names the keys obj carries that lca does not own, so an entry the
// user has added settings to is never deleted wholesale.
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

// equalJSON compares two decoded JSON values by their encodings, so a number
// that arrived as int and one built as float64 still match.
func equalJSON(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}
