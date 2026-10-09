package server

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const plain = `slot print_timing: id  0 | task 12 |
prompt eval time =    1234.56 ms /   100 tokens (   12.35 ms per token,    81.00 tokens per second)
slot print_timing: id  0 | task 12 |        eval time =    2000.00 ms /    50 tokens (   40.00 ms per token,    25.00 tokens per second)
slot print_timing: id  0 | task 12 |       total time =    3234.56 ms /   150 tokens
slot print_timing: id  0 | task 12 | prompt eval time =    1234.56 ms /   100 tokens (   12.35 ms per token,    81.00 tokens per second)
`

const stamped = `0.12.345.678 slot print_timing: id  0 | task 7 | prompt eval time =     500.00 ms /    10 tokens (   50.00 ms per token,    20.00 tokens per second)
0.12.345.700 slot print_timing: id  0 | task 7 |        eval time =     250.00 ms /     5 tokens (   50.00 ms per token,    20.00 tokens per second)
0.12.345.800 slot print_timing: id  0 | task 7 |       total time =     750.00 ms /    15 tokens
`

const levelled = `1.02.000.000 I slot print_timings: id  1 | task 9 | prompt eval time =     100.00 ms /    10 tokens (   10.00 ms per token,   100.00 tokens per second)
1.02.000.000 I slot print_timings: id  1 | task 9 |        eval time =     100.00 ms /    10 tokens (   10.00 ms per token,   100.00 tokens per second)
1.02.000.000 I slot print_timings: id  1 | task 9 |       total time =     200.00 ms /    20 tokens
`

const colored = "\x1b[32m0.00.100.000 \x1b[0mslot print_timing: id  0 | task 3 | prompt eval time =      10.00 ms /     1 tokens (   10.00 ms per token,   100.00 tokens per second)\x1b[0m\n" +
	"\x1b[32m0.00.100.000 \x1b[0mslot print_timing: id  0 | task 3 |        eval time =      20.00 ms /     2 tokens (   10.00 ms per token,   100.00 tokens per second)\n" +
	"0.00.100.000 slot print_timing: id  0 | task 3 |       total time =      30.00 ms /     3 tokens\n"

func TestParsePrefixForms(t *testing.T) {
	cases := map[string]struct {
		body     string
		task     int
		promptN  int
		genTPS   float64
		totalMs  float64
		offsetMs int64
	}{
		"plain":    {plain, 12, 100, 25, 3234.56, 0},
		"stamped":  {stamped, 7, 10, 20, 750, 12345},
		"levelled": {levelled, 9, 10, 100, 200, 62 * 1000},
		"colored":  {colored, 3, 1, 100, 30, 100},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ts, off := parse(bufio.NewScanner(strings.NewReader(c.body)), "f", "m")
			if len(ts) != 1 {
				t.Fatalf("got %d timings: %+v", len(ts), ts)
			}
			g := ts[0]
			if g.Task != c.task || g.PromptN != c.promptN || g.GenTPS != c.genTPS || g.TotalMs != c.totalMs {
				t.Errorf("got %+v", g)
			}
			if got := off.Milliseconds(); got != c.offsetMs {
				t.Errorf("offset %d want %d", got, c.offsetMs)
			}
		})
	}
}

func TestParseFileAnchorsOnName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "server-20261008T120000Z-qwen3.5-9b.log")
	_ = os.WriteFile(p, []byte(stamped), 0o600)
	ts, err := ParseFile(p)
	if err != nil || len(ts) != 1 {
		t.Fatal(err, ts)
	}
	want := time.Date(2026, 10, 8, 12, 0, 12, 345800*1e3, time.UTC)
	if !ts[0].Time.Equal(want) || ts[0].Model != "qwen3.5-9b" {
		t.Errorf("time %v model %q", ts[0].Time, ts[0].Model)
	}
	// Unnamed log falls back to mtime.
	q := filepath.Join(dir, "other.log")
	_ = os.WriteFile(q, []byte(stamped), 0o600)
	mt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	_ = os.Chtimes(q, mt, mt)
	ts, _ = ParseFile(q)
	if got := ts[0].Time; !got.Equal(mt) {
		t.Errorf("mtime anchor: got %v want %v", got, mt)
	}
}

func TestCompactIsIdempotentAndPrunes(t *testing.T) {
	dir := t.TempDir()
	oldLog := filepath.Join(dir, "server-20260901T000000Z-m.log")
	newLog := filepath.Join(dir, "server-20261008T120000Z-m.log")
	_ = os.WriteFile(oldLog, []byte(levelled), 0o600)
	_ = os.WriteFile(newLog, []byte(stamped+plain), 0o600)
	old := time.Now().Add(-30 * 24 * time.Hour)
	_ = os.Chtimes(oldLog, old, old)
	_ = os.Chtimes(newLog, old, old)
	added, pruned, err := Compact(dir)
	if err != nil || added != 3 || pruned != 1 {
		t.Fatalf("first: added=%d pruned=%d err=%v", added, pruned, err)
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Error("newest log was pruned")
	}
	if _, err := os.Stat(oldLog); err == nil {
		t.Error("old log kept")
	}
	added, pruned, err = Compact(dir)
	if err != nil || added != 0 || pruned != 0 {
		t.Fatalf("second: added=%d pruned=%d err=%v", added, pruned, err)
	}
	ts, _ := ReadTimings(dir, time.Time{})
	if len(ts) != 3 {
		t.Errorf("%d timings", len(ts))
	}
	// Appending to the live log adds only the new task.
	f, _ := os.OpenFile(newLog, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(strings.ReplaceAll(stamped, "task 7", "task 8"))
	_ = f.Close()
	added, _, _ = Compact(dir)
	if added != 1 {
		t.Errorf("append: added=%d", added)
	}
}
