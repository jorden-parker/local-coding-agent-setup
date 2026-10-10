package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const usageSample = `{"schemaVersion":1,"id":"20a43195-4975-4447-b396-44ff3791a307","timestamp":"2026-10-08T13:06:07.876Z","localDate":"2026-10-08","localMonth":"2026-10","sessionId":"4028e0b8-dc3e-49e3-8a6b-9144eb1253cb","model":"qwen3.5-9b","authType":"openai","source":"main","inputTokens":21154,"outputTokens":10,"cachedTokens":0,"thoughtsTokens":0,"totalTokens":21164,"apiDurationMs":234698}
{"schemaVersion":1,"id":"8a93db8d-68d1-429c-ad28-ec043335a347","timestamp":"2026-10-08T13:07:28.320Z","localDate":"2026-10-08","localMonth":"2026-10","sessionId":"4028e0b8-dc3e-49e3-8a6b-9144eb1253cb","model":"qwen3.5-9b","authType":"openai","source":"managed-auto-memory-extractor","inputTokens":5907,"outputTokens":93,"cachedTokens":0,"thoughtsTokens":0,"totalTokens":6000,"apiDurationMs":80343}
{"schemaVersion":2,"id":"x","timestamp":"2026-10-08T13:08:00.000Z","localDate":"2026-10-08","model":"qwen3.5-9b","apiDurationMs":1}
not json at all
`

func TestReadUsage(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "token-usage-2026-10.jsonl"), []byte(usageSample), 0o600)
	recs, skipped, err := Read(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || skipped != 2 {
		t.Fatalf("got %d records, %d skipped", len(recs), skipped)
	}
	if recs[0].InputTokens != 21154 || recs[0].APIDurationMs != 234698 || recs[0].Source != "main" || recs[0].LocalDate != "2026-10-08" {
		t.Errorf("record 0: %+v", recs[0])
	}
	later, _, _ := Read(dir, time.Date(2026, 10, 8, 13, 7, 0, 0, time.UTC))
	if len(later) != 1 {
		t.Errorf("since filter: %d", len(later))
	}
}

func TestReadUsageDirsRetainsHistoryAndDeduplicates(t *testing.T) {
	managed, legacy := t.TempDir(), t.TempDir()
	for _, dir := range []string{managed, legacy} {
		if err := os.WriteFile(filepath.Join(dir, "token-usage-2026-10.jsonl"), []byte(usageSample), 0600); err != nil {
			t.Fatal(err)
		}
	}
	extra := `{"schemaVersion":1,"id":"legacy-only","timestamp":"2026-10-08T13:05:00Z","model":"old"}` + "\n"
	if err := os.WriteFile(filepath.Join(legacy, "token-usage-extra.jsonl"), []byte(extra), 0600); err != nil {
		t.Fatal(err)
	}
	records, skipped, err := ReadUsageDirs(time.Time{}, managed, legacy, filepath.Join(t.TempDir(), "missing"))
	if err != nil || len(records) != 3 || skipped != 4 {
		t.Fatalf("records=%d skipped=%d err=%v", len(records), skipped, err)
	}
	if records[0].ID != "legacy-only" {
		t.Fatal("history lost or ordering incorrect")
	}
}
