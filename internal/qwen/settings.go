// Package qwen reads and writes Qwen Code's settings.json. Its usage records
// are read by internal/usage, which both harnesses share.
package qwen

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jorden-parker/local-coding-agent-setup/internal/atomicfile"
)

// Provider is the subset of a modelProviders.openai[] entry that lca owns.
type Provider struct {
	ID            string
	Name          string
	BaseURL       string
	EnvKey        string
	ContextWindow int
}

// ProviderFor builds the entry for a local llama-server with the given
// alias, port and context size.
func ProviderFor(alias string, port, ctx int) Provider {
	return Provider{
		ID:            alias,
		Name:          alias + " (local llama.cpp)",
		BaseURL:       fmt.Sprintf("http://127.0.0.1:%d/v1", port),
		EnvKey:        "OPENAI_API_KEY",
		ContextWindow: ctx,
	}
}

func loadSettings(path string) (map[string]any, bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{"$version": float64(4)}, false, nil
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

// openaiList returns settings.modelProviders.openai, creating the containers
// when create is true.
func openaiList(m map[string]any, create bool) []any {
	mp, _ := m["modelProviders"].(map[string]any)
	if mp == nil {
		if !create {
			return nil
		}
		mp = map[string]any{}
		m["modelProviders"] = mp
	}
	list, _ := mp["openai"].([]any)
	if list == nil && create {
		list = []any{}
		mp["openai"] = list
	}
	return list
}

// Sync merges p into modelProviders.openai[] (matched by id) in the settings
// file at path, creating the file when absent. Every other key is left
// alone. It reports whether the file changed.
func Sync(path string, p Provider) (bool, error) {
	m, _, err := loadSettings(path)
	if err != nil {
		return false, err
	}
	before, _ := json.Marshal(m)
	mergeProvider(m, p)

	after, _ := json.Marshal(m)
	if string(before) == string(after) {
		return false, nil
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return false, err
	}
	return true, atomicfile.Write(path, append(out, '\n'))
}

// Entry returns the provider entry with the given id, or ok false.
func Entry(path, id string) (Provider, bool, error) {
	m, exists, err := loadSettings(path)
	if err != nil || !exists {
		return Provider{}, false, err
	}
	for _, e := range openaiList(m, false) {
		em, ok := e.(map[string]any)
		if !ok || em["id"] != id {
			continue
		}
		p := Provider{ID: id}
		p.Name, _ = em["name"].(string)
		p.BaseURL, _ = em["baseUrl"].(string)
		p.EnvKey, _ = em["envKey"].(string)
		if gc, ok := em["generationConfig"].(map[string]any); ok {
			if f, ok := gc["contextWindowSize"].(float64); ok {
				p.ContextWindow = int(f)
			}
		}
		return p, true, nil
	}
	return Provider{}, false, nil
}

// UsageStatsEnabled reports privacy.usageStatisticsEnabled (default true).
func UsageStatsEnabled(path string) (bool, error) {
	m, _, err := loadSettings(path)
	if err != nil {
		return false, err
	}
	if pr, ok := m["privacy"].(map[string]any); ok {
		if b, ok := pr["usageStatisticsEnabled"].(bool); ok {
			return b, nil
		}
	}
	return true, nil
}

// mergeProvider updates only the provider fields owned by lca.
func mergeProvider(m map[string]any, p Provider) {
	list := openaiList(m, true)
	var entry map[string]any
	for _, e := range list {
		if em, ok := e.(map[string]any); ok && em["id"] == p.ID {
			entry = em
			break
		}
	}
	if entry == nil {
		entry = map[string]any{}
		list = append(list, entry)
		m["modelProviders"].(map[string]any)["openai"] = list
	}
	entry["id"] = p.ID
	entry["name"] = p.Name
	entry["baseUrl"] = p.BaseURL
	entry["envKey"] = p.EnvKey
	gc, _ := entry["generationConfig"].(map[string]any)
	if gc == nil {
		gc = map[string]any{}
		entry["generationConfig"] = gc
	}
	gc["contextWindowSize"] = float64(p.ContextWindow)
}
