// Package stats buckets response times by day and model.
package stats

import (
	"math"
	"sort"
	"strings"

	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
)

// Bucket summarises one day of one model.
type Bucket struct {
	Day       string  `json:"day"`
	Model     string  `json:"model"`
	N         int     `json:"n"`
	P50       float64 `json:"p50_ms"`
	P95       float64 `json:"p95_ms"`
	Mean      float64 `json:"mean_ms"`
	InTok     int     `json:"in_tokens"`
	OutTok    int     `json:"out_tokens"`
	PromptTPS float64 `json:"prompt_tps,omitempty"`
	GenTPS    float64 `json:"gen_tps,omitempty"`
}

type sample struct {
	ms          float64
	in, out     int
	promptMs    float64
	genMs       float64
	hasServerTP bool
}

// FromQwen buckets Qwen Code usage records by localDate and model. Latency
// is apiDurationMs; tokens are input and output.
func FromQwen(recs []qwen.Record) []Bucket {
	groups := map[[2]string][]sample{}
	for _, r := range recs {
		k := [2]string{r.LocalDate, r.Model}
		groups[k] = append(groups[k], sample{ms: r.APIDurationMs, in: r.InputTokens, out: r.OutputTokens})
	}
	return finish(groups)
}

// FromServer buckets llama-server timings by local day and model. Latency is
// the total time; tokens per second are token-weighted over the day.
func FromServer(ts []server.Timing) []Bucket {
	groups := map[[2]string][]sample{}
	for _, t := range ts {
		k := [2]string{t.Time.Local().Format("2006-01-02"), t.Model}
		groups[k] = append(groups[k], sample{ms: t.TotalMs, in: t.PromptN, out: t.GenN, promptMs: t.PromptMs, genMs: t.GenMs, hasServerTP: true})
	}
	return finish(groups)
}

func finish(groups map[[2]string][]sample) []Bucket {
	var out []Bucket
	for k, ss := range groups {
		b := Bucket{Day: k[0], Model: k[1], N: len(ss)}
		ms := make([]float64, len(ss))
		var sum, pMs, gMs float64
		tp := false
		for i, s := range ss {
			ms[i] = s.ms
			sum += s.ms
			b.InTok += s.in
			b.OutTok += s.out
			pMs += s.promptMs
			gMs += s.genMs
			tp = tp || s.hasServerTP
		}
		b.P50 = Percentile(ms, 50)
		b.P95 = Percentile(ms, 95)
		b.Mean = sum / float64(len(ss))
		if tp {
			if pMs > 0 {
				b.PromptTPS = float64(b.InTok) / pMs * 1000
			}
			if gMs > 0 {
				b.GenTPS = float64(b.OutTok) / gMs * 1000
			}
		}
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Day != out[j].Day {
			return out[i].Day < out[j].Day
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// Percentile returns the p-th percentile (0..100) by nearest-rank on a copy
// of xs. Empty input yields 0.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	if p <= 0 {
		return s[0]
	}
	if p >= 100 {
		return s[len(s)-1]
	}
	rank := int(math.Ceil(p / 100 * float64(len(s))))
	if rank < 1 {
		rank = 1
	}
	return s[rank-1]
}

var bars = []rune("▁▂▃▄▅▆▇█")

// Sparkline renders xs with block characters, scaled to the max. At most
// width values are shown, taking the most recent.
func Sparkline(xs []float64, width int) string {
	if width <= 0 || len(xs) == 0 {
		return ""
	}
	if len(xs) > width {
		xs = xs[len(xs)-width:]
	}
	maxV := 0.0
	for _, x := range xs {
		if x > maxV {
			maxV = x
		}
	}
	var b strings.Builder
	for _, x := range xs {
		i := 0
		if maxV > 0 {
			i = int(math.Round(x / maxV * float64(len(bars)-1)))
		}
		b.WriteRune(bars[i])
	}
	return b.String()
}

// DailyP50 returns one value per bucket day (pooled over models, by N-weighted
// p50 approximation: the max p50 of that day) for sparklines.
func DailyP50(bs []Bucket) (days []string, vals []float64) {
	idx := map[string]int{}
	for _, b := range bs {
		i, ok := idx[b.Day]
		if !ok {
			i = len(days)
			idx[b.Day] = i
			days = append(days, b.Day)
			vals = append(vals, 0)
		}
		if b.P50 > vals[i] {
			vals[i] = b.P50
		}
	}
	return days, vals
}
