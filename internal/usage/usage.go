package qwen

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Record is one line of token-usage-YYYY-MM.jsonl.
type Record struct {
	SchemaVersion int       `json:"schemaVersion"`
	ID            string    `json:"id"`
	Timestamp     time.Time `json:"timestamp"`
	LocalDate     string    `json:"localDate"`
	SessionID     string    `json:"sessionId"`
	Model         string    `json:"model"`
	AuthType      string    `json:"authType"`
	Source        string    `json:"source"`
	InputTokens   int       `json:"inputTokens"`
	OutputTokens  int       `json:"outputTokens"`
	CachedTokens  int       `json:"cachedTokens"`
	ThoughtTokens int       `json:"thoughtsTokens"`
	TotalTokens   int       `json:"totalTokens"`
	APIDurationMs float64   `json:"apiDurationMs"`
}

// Read returns every schemaVersion 1 record under dir with a timestamp at or
// after since, oldest first. Malformed lines are skipped. The number of
// skipped lines is returned for diagnostics.
func Read(dir string, since time.Time) ([]Record, int, error) {
	files, err := filepath.Glob(filepath.Join(dir, "token-usage-*.jsonl"))
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(files)
	var out []Record
	skipped := 0
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			return nil, skipped, err
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var r Record
			if err := json.Unmarshal(line, &r); err != nil || r.SchemaVersion != 1 || r.Timestamp.IsZero() {
				skipped++
				continue
			}
			if r.Timestamp.Before(since) {
				continue
			}
			out = append(out, r)
		}
		_ = fh.Close()
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, skipped, nil
}

// ReadUsageDirs merges managed and legacy usage, preferring the first directory
// for duplicate IDs. Records without IDs remain distinct.
func ReadUsageDirs(since time.Time, dirs ...string) ([]Record, int, error) {
	var out []Record
	seen := map[string]bool{}
	skipped := 0
	for _, dir := range dirs {
		records, n, err := Read(dir, since)
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
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out, skipped, nil
}
