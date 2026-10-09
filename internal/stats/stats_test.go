package stats

import (
	"testing"
	"time"

	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
)

func TestPercentile(t *testing.T) {
	cases := []struct {
		xs   []float64
		p    float64
		want float64
	}{
		{nil, 50, 0},
		{[]float64{5}, 50, 5},
		{[]float64{5}, 95, 5},
		{[]float64{3, 1, 2}, 50, 2},
		{[]float64{3, 1, 2}, 0, 1},
		{[]float64{3, 1, 2}, 100, 3},
		{[]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 95, 10},
		{[]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 50, 5},
	}
	for _, c := range cases {
		if got := Percentile(c.xs, c.p); got != c.want {
			t.Errorf("P%g(%v) = %g want %g", c.p, c.xs, got, c.want)
		}
	}
}

func TestFromServerWeightsTokens(t *testing.T) {
	day := time.Date(2026, 10, 8, 12, 0, 0, 0, time.Local)
	ts := []server.Timing{
		{Time: day, Model: "m", PromptMs: 1000, PromptN: 100, GenMs: 1000, GenN: 10, TotalMs: 2000},
		{Time: day.Add(time.Hour), Model: "m", PromptMs: 3000, PromptN: 100, GenMs: 1000, GenN: 30, TotalMs: 4000},
	}
	bs := FromServer(ts)
	if len(bs) != 1 {
		t.Fatalf("%d buckets", len(bs))
	}
	b := bs[0]
	if b.N != 2 || b.InTok != 200 || b.OutTok != 40 || b.P50 != 2000 || b.P95 != 4000 || b.Mean != 3000 {
		t.Errorf("%+v", b)
	}
	if b.PromptTPS != 50 || b.GenTPS != 20 {
		t.Errorf("tps %g %g", b.PromptTPS, b.GenTPS)
	}
}

func TestFromQwenBucketsByDayAndModel(t *testing.T) {
	recs := []qwen.Record{
		{LocalDate: "2026-10-08", Model: "a", APIDurationMs: 100, InputTokens: 1, OutputTokens: 2},
		{LocalDate: "2026-10-08", Model: "b", APIDurationMs: 200},
		{LocalDate: "2026-10-07", Model: "a", APIDurationMs: 300},
	}
	bs := FromQwen(recs)
	if len(bs) != 3 || bs[0].Day != "2026-10-07" || bs[1].Model != "a" || bs[2].Model != "b" {
		t.Fatalf("%+v", bs)
	}
	if bs[1].PromptTPS != 0 || bs[1].InTok != 1 {
		t.Errorf("%+v", bs[1])
	}
	days, vals := DailyP50(bs)
	if len(days) != 2 || vals[1] != 200 {
		t.Errorf("%v %v", days, vals)
	}
}

func TestSparkline(t *testing.T) {
	if got := Sparkline([]float64{0, 1, 2, 3, 4, 5, 6, 7}, 8); got != "▁▂▃▄▅▆▇█" {
		t.Errorf("got %q", got)
	}
	if got := Sparkline([]float64{1, 2, 3, 4}, 2); got != "▆█" {
		t.Errorf("width trims from the front: %q", got)
	}
	if got := Sparkline([]float64{0, 0}, 5); got != "▁▁" {
		t.Errorf("zeros: %q", got)
	}
	if Sparkline(nil, 5) != "" || Sparkline([]float64{1}, 0) != "" {
		t.Error("empty cases")
	}
}
