package pi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jorden-parker/local-coding-agent-setup/internal/usage"
)

// entry is the subset of one session JSONL line lca reads. Sessions are a
// tree: every line carries its own id, its parent's and the time it was
// appended.
type entry struct {
	Type      string    `json:"type"`
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Message   *struct {
		Role     string `json:"role"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Usage    *struct {
			Input       int `json:"input"`
			Output      int `json:"output"`
			CacheRead   int `json:"cacheRead"`
			CacheWrite  int `json:"cacheWrite"`
			TotalTokens int `json:"totalTokens"`
		} `json:"usage"`
		StopReason string `json:"stopReason"`
	} `json:"message"`
}

// ReadFile turns one session file into usage records. pi logs no per-call
// duration, so the response time of an assistant message is the time between
// the previous line being appended (the request that provoked it) and its own.
// Only the local provider's messages count; an aborted or failed turn has no
// usable duration and is skipped.
func ReadFile(path string, since time.Time) ([]usage.Record, int, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = fh.Close() }()

	var out []usage.Record
	skipped := 0
	sessionID := ""
	var prev time.Time
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e entry
		if err := json.Unmarshal(line, &e); err != nil || e.Timestamp.IsZero() {
			skipped++
			continue
		}
		at := e.Timestamp
		previous := prev
		prev = at
		if e.Type == "session" && sessionID == "" {
			sessionID = e.ID
		}
		if e.Type != "message" || e.Message == nil || e.Message.Role != "assistant" {
			continue
		}
		if e.Message.Provider != ProviderID {
			continue
		}
		if e.Message.Usage == nil || e.Message.StopReason == "error" || e.Message.StopReason == "aborted" {
			skipped++
			continue
		}
		if at.Before(since) || previous.IsZero() {
			continue
		}
		out = append(out, usage.Record{
			SchemaVersion: 1,
			ID:            e.ID,
			Timestamp:     at,
			LocalDate:     at.Local().Format("2006-01-02"),
			SessionID:     sessionID,
			Model:         e.Message.Model,
			AuthType:      "openai",
			Source:        "pi",
			InputTokens:   e.Message.Usage.Input,
			OutputTokens:  e.Message.Usage.Output,
			CachedTokens:  e.Message.Usage.CacheRead,
			TotalTokens:   e.Message.Usage.TotalTokens,
			APIDurationMs: float64(at.Sub(previous)) / float64(time.Millisecond),
		})
	}
	if err := sc.Err(); err != nil {
		return out, skipped, err
	}
	return out, skipped, nil
}

// ReadSessions merges the sessions under every directory, oldest first,
// preferring the first directory for duplicate entry IDs. A missing directory
// contributes nothing. Sessions are grouped one directory per working
// directory, so the files are <dir>/<cwd-slug>/<session>.jsonl.
func ReadSessions(since time.Time, dirs ...string) ([]usage.Record, int, error) {
	var out []usage.Record
	seen := map[string]bool{}
	skipped := 0
	for _, dir := range dirs {
		files, err := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
		if err != nil {
			return nil, skipped, err
		}
		sort.Strings(files)
		for _, f := range files {
			records, n, err := ReadFile(f, since)
			skipped += n
			if err != nil {
				return nil, skipped, err
			}
			for _, r := range records {
				if r.ID != "" && seen[r.ID] {
					continue
				}
				if r.ID != "" {
					seen[r.ID] = true
				}
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, skipped, nil
}
