package qwen

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/jorden-parker/local-coding-agent-setup/internal/atomicfile"
)

// Only these leaves are owned by the opt-in profile. Qwen Code 0.25.0 uses
// "agent" as the registered tool name (ToolNames.AGENT), not the "task" alias.
var leanSettings = map[string]any{
	"memory.enableManagedAutoMemory": false,
	"memory.enableManagedAutoDream":  false,
	"memory.enableAutoSkill":         false,
	"tools.workflowsEnabled":         false,
	"tools.disabled":                 []any{"agent"},
	"tools.eager": []any{"read_file", "write_file", "edit", "glob",
		"grep_search", "run_shell_command"},
}

type settingState struct {
	Present bool `json:"present"`
	Value   any  `json:"value"`
}

type profileField struct {
	Before  settingState `json:"before"`
	Applied settingState `json:"applied"`
}

type profileSnapshot struct {
	Version       int                     `json:"version"`
	Fields        map[string]profileField `json:"fields"`
	MissingGroups []string                `json:"missing_groups"`
}

func stateAt(m map[string]any, path string) (settingState, error) {
	group, key, _ := strings.Cut(path, ".")
	v, ok := m[group]
	if !ok {
		return settingState{}, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return settingState{}, fmt.Errorf("settings.%s must be an object", group)
	}
	v, ok = obj[key]
	return settingState{Present: ok, Value: v}, nil
}

func putState(m map[string]any, path string, s settingState) {
	group, key, _ := strings.Cut(path, ".")
	obj, _ := m[group].(map[string]any)
	if obj == nil {
		if !s.Present {
			return
		}
		obj = map[string]any{}
		m[group] = obj
	}
	if s.Present {
		obj[key] = s.Value
	} else {
		delete(obj, key)
	}
}

func writeJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(b, '\n'))
}

func loadSnapshot(path string) (profileSnapshot, bool, error) {
	var s profileSnapshot
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, false, nil
	}
	if err != nil {
		return s, false, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, true, fmt.Errorf("%s: %w", path, err)
	}
	if s.Version != 1 || len(s.Fields) != len(leanSettings) {
		return s, true, fmt.Errorf("%s: invalid lean profile snapshot", path)
	}
	for key := range leanSettings {
		if field, ok := s.Fields[key]; !ok || !field.Applied.Present {
			return s, true, fmt.Errorf("%s: missing applied setting %s", path, key)
		}
	}
	for _, group := range s.MissingGroups {
		if group != "tools" && group != "memory" {
			return s, true, fmt.Errorf("%s: invalid group %s", path, group)
		}
	}
	return s, true, nil
}

// matchSnapshot refuses overwriting intervening edits, including an absent
// setting versus an explicit null. It also recognises an interrupted write
// where all settings still match the pre-application snapshot.
func matchSnapshot(m map[string]any, s profileSnapshot) (before, applied bool, err error) {
	before, applied = true, true
	var changed []string
	for path, field := range s.Fields {
		current, e := stateAt(m, path)
		if e != nil {
			return false, false, e
		}
		b, a := reflect.DeepEqual(current, field.Before), reflect.DeepEqual(current, field.Applied)
		before = before && b
		applied = applied && a
		if !a {
			changed = append(changed, path)
		}
	}
	if !before && !applied {
		sort.Strings(changed)
		return false, false, fmt.Errorf("lean profile conflict at %s; restore the applied values before retrying (snapshot retained)", strings.Join(changed, ", "))
	}
	return before, applied, nil
}

// ApplyLean snapshots affected values before writing settings. Repeated calls
// do not replace the original snapshot. Existing disabled tools are retained.
// Normal provider Sync neither applies nor restores this profile.
func ApplyLean(settingsPath, snapshotPath string) (bool, error) {
	m, _, err := loadSettings(settingsPath)
	if err != nil {
		return false, err
	}
	if tools, ok := m["tools"].(map[string]any); ok {
		if search, ok := tools["toolSearch"].(map[string]any); ok && search["enabled"] == false {
			return false, fmt.Errorf("lean profile needs tools.toolSearch.enabled for deferred tool discovery")
		}
	}
	s, exists, err := loadSnapshot(snapshotPath)
	if err != nil {
		return false, err
	}
	if exists {
		_, applied, err := matchSnapshot(m, s)
		if err != nil || applied {
			return false, err
		}
	} else {
		s = profileSnapshot{Version: 1, Fields: map[string]profileField{}}
		for _, group := range []string{"memory", "tools"} {
			if _, ok := m[group]; !ok {
				s.MissingGroups = append(s.MissingGroups, group)
			}
		}
		for path, value := range leanSettings {
			before, err := stateAt(m, path)
			if err != nil {
				return false, err
			}
			if path == "tools.disabled" && before.Present {
				list, ok := before.Value.([]any)
				if !ok {
					return false, fmt.Errorf("settings.tools.disabled must be an array")
				}
				merged := append([]any{}, list...)
				found := false
				for _, v := range list {
					if _, ok := v.(string); !ok {
						return false, fmt.Errorf("settings.tools.disabled entries must be strings")
					}
					if v == "tool_search" || v == "tool_call" {
						return false, fmt.Errorf("lean profile needs %s enabled for deferred tool discovery", v)
					}
					if v == "agent" {
						found = true
					}
				}
				if !found {
					merged = append(merged, "agent")
				}
				value = merged
			}
			s.Fields[path] = profileField{Before: before, Applied: settingState{Present: true, Value: value}}
		}
		if err := writeJSON(snapshotPath, s); err != nil {
			return false, err
		}
	}
	for path, field := range s.Fields {
		putState(m, path, field.Applied)
	}
	if err := writeJSON(settingsPath, m); err != nil {
		return false, fmt.Errorf("snapshot saved; settings not applied: %w", err)
	}
	return true, nil
}

// RestoreLean restores only owned leaves, leaving unrelated and provider edits
// intact. The snapshot is removed only after a successful settings write.
func RestoreLean(settingsPath, snapshotPath string) (bool, error) {
	s, exists, err := loadSnapshot(snapshotPath)
	if err != nil || !exists {
		return false, err
	}
	m, _, err := loadSettings(settingsPath)
	if err != nil {
		return false, err
	}
	before, _, err := matchSnapshot(m, s)
	if err != nil {
		return false, err
	}
	if !before {
		for path, field := range s.Fields {
			putState(m, path, field.Before)
		}
		for _, group := range s.MissingGroups {
			if obj, ok := m[group].(map[string]any); ok && len(obj) == 0 {
				delete(m, group)
			}
		}
		if err := writeJSON(settingsPath, m); err != nil {
			return false, err
		}
	}
	if err := os.Remove(snapshotPath); err != nil {
		return false, fmt.Errorf("settings restored; cannot remove snapshot: %w", err)
	}
	return true, nil
}
