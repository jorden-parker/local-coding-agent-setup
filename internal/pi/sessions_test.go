package pi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// One session: a user turn answered by the local model, a turn answered by
// another provider, a failed local turn and a line pi never wrote.
const sessionSample = `{"type":"session","id":"sess-1","timestamp":"2026-10-08T13:00:00.000Z","cwd":"/work","version":"0.87.1"}
{"type":"model_change","id":"c1","parentId":"sess-1","timestamp":"2026-10-08T13:00:00.100Z","provider":"llama-local","modelId":"local"}
{"type":"message","id":"m1","parentId":"c1","timestamp":"2026-10-08T13:00:01.000Z","message":{"role":"user","timestamp":1791550801000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-10-08T13:00:04.500Z","message":{"role":"assistant","api":"openai-completions","provider":"llama-local","model":"local","usage":{"input":100,"output":20,"cacheRead":5,"cacheWrite":0,"totalTokens":125},"stopReason":"stop","timestamp":1791550801200}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-10-08T13:00:06.000Z","message":{"role":"assistant","provider":"anthropic","model":"claude-opus-4-8","usage":{"input":1,"output":1,"totalTokens":2},"stopReason":"stop"}}
{"type":"message","id":"m4","parentId":"m3","timestamp":"2026-10-08T13:00:09.000Z","message":{"role":"assistant","provider":"llama-local","model":"local","stopReason":"error","errorMessage":"connection refused"}}
not json at all
`

func writeSession(t *testing.T, dir, slug, name, body string) string {
	t.Helper()
	full := filepath.Join(dir, slug)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(full, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSessionsDerivesDurationsAndSkipsUnusableTurns(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "--work--", "2026-10-08T13-00-00-000Z_sess-1.jsonl", sessionSample)
	recs, skipped, err := ReadSessions(time.Time{}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records: %+v", len(recs), recs)
	}
	// The malformed line and the failed turn are skipped; another provider's
	// turn is simply not ours.
	if skipped != 2 {
		t.Fatalf("skipped %d", skipped)
	}
	r := recs[0]
	// pi logs no duration: 13:00:01.000 asked, 13:00:04.500 answered.
	if r.APIDurationMs != 3500 {
		t.Errorf("duration %v", r.APIDurationMs)
	}
	if r.ID != "m2" || r.SessionID != "sess-1" || r.Model != "local" || r.Source != "pi" {
		t.Errorf("identity: %+v", r)
	}
	if r.InputTokens != 100 || r.OutputTokens != 20 || r.CachedTokens != 5 || r.TotalTokens != 125 {
		t.Errorf("tokens: %+v", r)
	}
	if r.LocalDate != r.Timestamp.Local().Format("2006-01-02") {
		t.Errorf("localDate %q for %s", r.LocalDate, r.Timestamp)
	}
	// The since filter is applied to the record, not the file.
	later, _, _ := ReadSessions(time.Date(2026, 10, 8, 13, 0, 5, 0, time.UTC), dir)
	if len(later) != 0 {
		t.Errorf("since filter: %+v", later)
	}
}

func TestReadSessionsMergesDirectoriesAndDeduplicates(t *testing.T) {
	managed, legacy := t.TempDir(), t.TempDir()
	for _, dir := range []string{managed, legacy} {
		writeSession(t, dir, "--work--", "a.jsonl", sessionSample)
	}
	only := `{"type":"session","id":"sess-2","timestamp":"2026-10-07T09:00:00.000Z","cwd":"/other"}
{"type":"message","id":"n1","timestamp":"2026-10-07T09:00:01.000Z","message":{"role":"user"}}
{"type":"message","id":"n2","timestamp":"2026-10-07T09:00:02.000Z","message":{"role":"assistant","provider":"llama-local","model":"local","usage":{"input":1,"output":1,"totalTokens":2},"stopReason":"stop"}}
`
	writeSession(t, legacy, "--other--", "b.jsonl", only)
	recs, _, err := ReadSessions(time.Time{}, managed, legacy, filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records: %+v", len(recs), recs)
	}
	// Oldest first, and the duplicate from the second directory is dropped.
	if recs[0].ID != "n2" || recs[1].ID != "m2" {
		t.Fatalf("order or dedup: %+v", recs)
	}
}
