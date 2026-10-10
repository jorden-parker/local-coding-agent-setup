// Package pi reads and writes the configuration of the pi coding agent
// (https://pi.dev) that pi-local runs against the local llama-server, and
// derives response times from pi's session files.
//
// pi is optional: nothing here installs it, and every caller is expected to
// stay quiet when the binary is absent.
package pi

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jorden-parker/local-coding-agent-setup/internal/atomicfile"
)

// ProviderID is the models.json key pi-local selects with --provider. One id
// per managed agent directory is enough: a second instance on another port
// gets its own directory, so the ids never collide.
const ProviderID = "llama-local"

// localAPIKey is the placeholder pi stores in models.json. llama-server
// ignores API keys, but pi hides a model whose provider has no credential.
const localAPIKey = "local"

// Provider is the subset of a models.json provider entry that lca owns.
type Provider struct {
	ID            string // provider id, always ProviderID
	Model         string // model id, the ALIAS llama-server reports
	Name          string
	BaseURL       string
	APIKey        string
	ContextWindow int
	Reasoning     bool
}

// ProviderFor builds the entry for a local llama-server with the given alias,
// port, context size and THINKING setting.
func ProviderFor(alias string, port, ctx int, thinking bool) Provider {
	return Provider{
		ID:            ProviderID,
		Model:         alias,
		Name:          alias + " (local llama.cpp)",
		BaseURL:       fmt.Sprintf("http://127.0.0.1:%d/v1", port),
		APIKey:        localAPIKey,
		ContextWindow: ctx,
		Reasoning:     thinking,
	}
}

// load reads a JSON object file. A missing file is an empty object.
func load(path string) (map[string]any, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, false, fmt.Errorf("%s: %w", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, true, nil
}

func write(path string, m map[string]any) error {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(out, '\n'))
}

// object returns m[key] as an object, creating it when absent.
func object(m map[string]any, key string) (map[string]any, error) {
	value, exists := m[key]
	if !exists {
		obj := map[string]any{}
		m[key] = obj
		return obj, nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	return obj, nil
}

// apply runs edit on the file at path and writes it back only when the result
// differs. It reports whether the file changed.
func apply(path string, edit func(map[string]any) error) (bool, error) {
	m, _, err := load(path)
	if err != nil {
		return false, err
	}
	before, _ := json.Marshal(m)
	if err := edit(m); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	after, _ := json.Marshal(m)
	if string(before) == string(after) {
		return false, nil
	}
	return true, write(path, m)
}

// check reports drift without modifying the file.
func check(path string, edit func(map[string]any) error, drift string) error {
	m, exists, err := load(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%s missing", path)
	}
	before, _ := json.Marshal(m)
	if err := edit(m); err != nil {
		return err
	}
	after, _ := json.Marshal(m)
	if string(before) != string(after) {
		return fmt.Errorf("%s", drift)
	}
	return nil
}
