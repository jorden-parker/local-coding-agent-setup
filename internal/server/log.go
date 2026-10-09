// Package server parses llama-server logs and its /metrics endpoint.
package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jorden-parker/local-coding-agent-setup/internal/atomicfile"
)

// Timing is one request's three print_timing lines folded together.
type Timing struct {
	Time      time.Time `json:"ts"`
	File      string    `json:"file"`
	Model     string    `json:"model"`
	Slot      int       `json:"slot"`
	Task      int       `json:"task"`
	PromptMs  float64   `json:"prompt_ms"`
	PromptN   int       `json:"prompt_n"`
	PromptTPS float64   `json:"prompt_tps"`
	GenMs     float64   `json:"gen_ms"`
	GenN      int       `json:"gen_n"`
	GenTPS    float64   `json:"gen_tps"`
	TotalMs   float64   `json:"total_ms"`
	TotalN    int       `json:"total_n"`
}

var (
	ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// timing matches the three slot print_timing lines. The timestamp group
	// is --log-timestamps' M.ss.mmm.uuu offset from process start.
	timing = regexp.MustCompile(`^(?:(\d+)\.(\d{2})\.(\d{3})\.(\d{3}) )?(?:[A-Z] )?slot print_timings?: id\s+(\d+) \| task (\d+) \|\s+(prompt eval time|eval time|total time) =\s+([\d.]+) ms /\s+(\d+) tokens(?: \(\s*([\d.]+) ms per token,\s*([\d.]+) tokens per second\))?`)
	// logName matches server-<UTC stamp>-<alias>.log as written by llama-coder.
	logName = regexp.MustCompile(`^server-(\d{8}T\d{6}Z)-(.+)\.log$`)
)

// ParseFile parses one log file. The wall-clock anchor is the timestamp in
// the file name when present, otherwise the file's modification time minus
// the last offset seen.
func ParseFile(path string) ([]Timing, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	base := filepath.Base(path)
	var start time.Time
	model := ""
	if m := logName.FindStringSubmatch(base); m != nil {
		start, _ = time.Parse("20060102T150405Z", m[1])
		model = m[2]
	}
	ts, maxOff := parse(bufio.NewScanner(f), base, model)
	if start.IsZero() {
		start = fi.ModTime().Add(-maxOff)
	}
	for i := range ts {
		ts[i].Time = start.Add(time.Duration(ts[i].Time.UnixNano()))
	}
	return ts, nil
}

// parse folds timing lines into Timings keyed by (slot, task). Time holds
// the offset from process start until the caller anchors it.
func parse(sc *bufio.Scanner, file, model string) ([]Timing, time.Duration) {
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	type key struct{ slot, task int }
	idx := map[key]int{}
	var out []Timing
	var maxOff time.Duration
	for sc.Scan() {
		line := ansi.ReplaceAllString(sc.Text(), "")
		m := timing.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		var off time.Duration
		if m[1] != "" {
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.Atoi(m[2])
			ms, _ := strconv.Atoi(m[3])
			us, _ := strconv.Atoi(m[4])
			off = time.Duration(min)*time.Minute + time.Duration(sec)*time.Second + time.Duration(ms)*time.Millisecond + time.Duration(us)*time.Microsecond
			if off > maxOff {
				maxOff = off
			}
		}
		slot, _ := strconv.Atoi(m[5])
		task, _ := strconv.Atoi(m[6])
		k := key{slot, task}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, Timing{File: file, Model: model, Slot: slot, Task: task})
		}
		t := &out[i]
		if off > 0 {
			t.Time = time.Unix(0, int64(off))
		}
		ms, _ := strconv.ParseFloat(m[8], 64)
		n, _ := strconv.Atoi(m[9])
		tps, _ := strconv.ParseFloat(m[11], 64)
		switch m[7] {
		case "prompt eval time":
			t.PromptMs, t.PromptN, t.PromptTPS = ms, n, tps
		case "eval time":
			t.GenMs, t.GenN, t.GenTPS = ms, n, tps
		case "total time":
			t.TotalMs, t.TotalN = ms, n
		}
	}
	// Offsets are stored as time since the zero Unix time; keep as is.
	return out, maxOff
}

// ReadTimings returns the compacted timings in stateDir at or after since.
func ReadTimings(stateDir string, since time.Time) ([]Timing, error) {
	f, err := os.Open(filepath.Join(stateDir, "timings.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Timing
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var t Timing
		if err := json.Unmarshal(sc.Bytes(), &t); err != nil {
			continue
		}
		if t.Time.Before(since) {
			continue
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// PruneAge is how old a compacted log must be before Compact deletes it.
const PruneAge = 14 * 24 * time.Hour

// Compact appends timings from every server-*.log in stateDir that are not
// yet in timings.jsonl (keyed by file and task), then deletes logs older
// than PruneAge except the newest, which may belong to a running server. It
// returns the number of new timings and the number of logs removed.
func Compact(stateDir string) (added, pruned int, err error) {
	logs, err := filepath.Glob(filepath.Join(stateDir, "server-*.log"))
	if err != nil {
		return 0, 0, err
	}
	if len(logs) == 0 {
		return 0, 0, nil
	}
	sort.Strings(logs)
	existing, err := ReadTimings(stateDir, time.Time{})
	if err != nil {
		return 0, 0, err
	}
	type key struct {
		file string
		task int
	}
	seen := map[key]bool{}
	for _, t := range existing {
		seen[key{t.File, t.Task}] = true
	}
	var buf strings.Builder
	for _, l := range logs {
		ts, err := ParseFile(l)
		if err != nil {
			return added, 0, err
		}
		for _, t := range ts {
			if t.TotalMs == 0 || seen[key{t.File, t.Task}] {
				continue
			}
			seen[key{t.File, t.Task}] = true
			b, _ := json.Marshal(t)
			buf.Write(b)
			buf.WriteByte('\n')
			added++
		}
	}
	if added > 0 {
		path := filepath.Join(stateDir, "timings.jsonl")
		old, _ := os.ReadFile(path)
		if err := atomicfile.Write(path, append(old, buf.String()...)); err != nil {
			return 0, 0, fmt.Errorf("write %s: %w", path, err)
		}
	}
	newest := logs[len(logs)-1]
	cutoff := time.Now().Add(-PruneAge)
	for _, l := range logs[:len(logs)-1] {
		fi, err := os.Stat(l)
		if err != nil || fi.ModTime().After(cutoff) || l == newest {
			continue
		}
		if os.Remove(l) == nil {
			pruned++
		}
	}
	return added, pruned, nil
}
