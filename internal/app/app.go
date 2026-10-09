// Package app holds the operations shared by the CLI and the TUI: loading
// config.env, mirroring it into Qwen Code's settings, health checks and
// collecting response-time statistics.
package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jorden-parker/local-coding-agent-setup/internal/config"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
	"github.com/jorden-parker/local-coding-agent-setup/internal/stats"
)

// LoadConfig reads config.env. A missing file is an error that names it.
func LoadConfig() (*config.File, error) {
	f, err := config.Load(paths.Config())
	if err != nil {
		return nil, err
	}
	if !f.Exists {
		return f, fmt.Errorf("%s does not exist; run setup.sh first", paths.Tildify(f.Path))
	}
	return f, nil
}

// Provider derives the Qwen provider entry from a config file.
func Provider(f *config.File) (qwen.Provider, error) {
	alias, _ := f.Get("ALIAS")
	port, _ := f.Get("PORT")
	ctx, _ := f.Get("CTX")
	for _, k := range []string{"ALIAS", "PORT", "CTX"} {
		v, _ := f.Get(k)
		if _, err := config.Validate(k, v); err != nil {
			return qwen.Provider{}, err
		}
	}
	p, _ := strconv.Atoi(port)
	c, _ := strconv.Atoi(ctx)
	return qwen.ProviderFor(alias, p, c), nil
}

// SyncQwen prepares the managed local provider and lean settings for f.
func SyncQwen(f *config.File) (qwen.Provider, bool, error) {
	p, err := Provider(f)
	if err != nil {
		return p, false, err
	}
	changed, err := qwen.PrepareLocal(paths.QwenSettings(), p)
	return p, changed, err
}

// SyncQwenAt prepares the local provider and lean settings for f, isolated
// per paths.QwenInstanceDir(port, cfgPort). cfgPort is config.env's own PORT,
// captured before any runtime override; port is the port actually in effect.
// A port matching cfgPort still uses the shared default settings file, so
// ordinary single-instance use is unaffected.
func SyncQwenAt(f *config.File, port, cfgPort string) (qwen.Provider, bool, string, error) {
	p, err := Provider(f)
	if err != nil {
		return p, false, "", err
	}
	settingsPath := paths.QwenSettingsFor(port, cfgPort)
	changed, err := qwen.PrepareLocal(settingsPath, p)
	return p, changed, settingsPath, err
}

// Set validates and writes one key, syncing Qwen when the key requires it.
// It returns the warning (if any) and a restart hint.
func Set(f *config.File, key, value string) (warn, hint string, err error) {
	k, ok := config.Lookup(key)
	if !ok {
		return "", "", fmt.Errorf("unknown key %s (known: %s)", key, strings.Join(config.Names(), ", "))
	}
	warn, err = config.Validate(key, value)
	if err != nil {
		return warn, "", err
	}
	f.Set(key, strings.TrimSpace(value))
	if err := f.Save(); err != nil {
		return warn, "", err
	}
	hint = "Restart " + k.Restart + " to apply."
	if k.Syncs {
		if _, _, err := SyncQwen(f); err != nil {
			return warn, hint, fmt.Errorf("config.env saved but Qwen settings not updated: %w", err)
		}
		hint += " " + paths.Tildify(paths.QwenSettings()) + " updated."
	}
	return warn, hint, nil
}

// Check is one doctor result.
type Check struct {
	Name   string
	OK     bool
	Warn   bool // true: a warning, not a failure
	Detail string
}

// Doctor runs every health check. Failing checks have OK false and Warn
// false.
func Doctor() []Check {
	var out []Check
	add := func(name string, ok bool, detail string) {
		out = append(out, Check{Name: name, OK: ok, Detail: detail})
	}
	warn := func(name, detail string) { out = append(out, Check{Name: name, OK: false, Warn: true, Detail: detail}) }

	f, err := LoadConfig()
	if err != nil {
		add("config.env", false, err.Error())
		return out
	}
	values := f.Values()
	warns, errs := config.ValidateAll(values)
	switch {
	case len(errs) > 0:
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		add("config.env", false, strings.Join(msgs, "; "))
	case len(warns) > 0:
		warn("config.env", strings.Join(warns, "; "))
	default:
		add("config.env", true, paths.Tildify(f.Path))
	}
	if mp := values["MODEL_PATH"]; mp != "" {
		if fi, err := os.Stat(mp); err == nil && fi.Mode().IsRegular() {
			add("model file", true, fmt.Sprintf("%s (%.1f GB)", paths.Tildify(mp), float64(fi.Size())/1e9))
		} else {
			add("model file", false, mp+" missing")
		}
	}

	for _, name := range []string{"llama-coder", "qwen-local"} {
		p := filepath.Join(paths.BinDir(), name)
		b, err := os.ReadFile(p)
		switch {
		case err != nil:
			add(name, false, paths.Tildify(p)+" missing; run setup.sh")
		case !strings.Contains(string(b), "config.env"):
			add(name, false, paths.Tildify(p)+" is the old setup-time version; re-run setup.sh")
		case name == "qwen-local" && (!strings.Contains(string(b), "QWEN_HOME") || !strings.Contains(string(b), "--auth-type openai --model")):
			add(name, false, paths.Tildify(p)+" does not pin the local model; re-run setup.sh")
		default:
			add(name, true, paths.Tildify(p))
		}
	}
	if _, err := exec.LookPath("lca"); err != nil {
		warn("lca on PATH", "not found; llama-coder will not compact logs. Add ~/.local/bin to PATH")
	} else {
		add("lca on PATH", true, "")
	}

	want, err := Provider(f)
	if err == nil {
		got, ok, err := qwen.Entry(paths.QwenSettings(), want.ID)
		switch {
		case err != nil:
			add("qwen provider", false, err.Error())
		case !ok:
			add("qwen provider", false, fmt.Sprintf("no modelProviders.openai entry %q; run lca sync", want.ID))
		case got.BaseURL != want.BaseURL || got.ContextWindow != want.ContextWindow || got.EnvKey != want.EnvKey:
			add("qwen provider", false, fmt.Sprintf("entry %q has %s ctx %d, config.env says %s ctx %d; run lca sync", want.ID, got.BaseURL, got.ContextWindow, want.BaseURL, want.ContextWindow))
		default:
			add("qwen provider", true, fmt.Sprintf("%s ctx %d", want.ID, want.ContextWindow))
		}
	}
	if err == nil {
		if err := qwen.CheckLocal(paths.QwenSettings(), want); err != nil {
			add("qwen local profile", false, err.Error()+"; run lca sync")
		} else {
			add("qwen local profile", true, "lean, openai, "+want.ID+", skills ~/.qwen/skills in "+paths.Tildify(paths.QwenSettings()))
		}
	}
	if on, err := qwen.UsageStatsEnabled(paths.QwenSettings()); err != nil {
		add("qwen usage stats", false, err.Error())
	} else if !on {
		warn("qwen usage stats", "privacy.usageStatisticsEnabled is false; lca stats --source qwen will be empty")
	} else {
		add("qwen usage stats", true, "enabled")
	}

	sd := paths.StateDir()
	if err := os.MkdirAll(sd, 0o755); err != nil {
		add("state dir", false, err.Error())
	} else if probe, err := os.CreateTemp(sd, ".probe-*"); err != nil {
		add("state dir", false, err.Error())
	} else {
		_ = probe.Close()
		_ = os.Remove(probe.Name())
		add("state dir", true, paths.Tildify(sd))
	}

	if _, err := exec.LookPath("llama-server"); err != nil {
		add("llama-server", false, "not on PATH; brew install llama.cpp")
	} else {
		add("llama-server", true, "")
	}
	if port, err := strconv.Atoi(values["PORT"]); err == nil {
		if server.Health(port) {
			add("server /health", true, fmt.Sprintf("port %d", port))
		} else {
			warn("server /health", fmt.Sprintf("nothing on port %d; qwen-local starts one, or run llama-coder", port))
		}
	}
	return out
}

// Failed reports whether any check failed outright.
func Failed(cs []Check) bool {
	for _, c := range cs {
		if !c.OK && !c.Warn {
			return true
		}
	}
	return false
}

// Source selects which latency source stats come from.
type Source string

// Sources.
const (
	SourceQwen   Source = "qwen"
	SourceServer Source = "server"
	SourceBoth   Source = "both"
)

// ParseSource validates a --source flag.
func ParseSource(s string) (Source, error) {
	switch Source(s) {
	case SourceQwen, SourceServer, SourceBoth:
		return Source(s), nil
	}
	return "", errors.New("--source must be qwen, server or both")
}

// Stats is the collected data for the stats command and screen.
type Stats struct {
	Qwen      []stats.Bucket
	Server    []stats.Bucket
	QwenRaw   []qwen.Record
	ServerRaw []server.Timing
	Skipped   int
	Notes     []string
}

// UsageSources names the directories CollectStats reads Qwen usage from.
func UsageSources() string {
	var parts []string
	for _, d := range paths.QwenUsageDirs() {
		parts = append(parts, paths.Tildify(d))
	}
	return strings.Join(parts, ", ")
}

// CollectStats compacts server logs, then reads both sources since days ago,
// keeping only model when non-empty.
func CollectStats(days int, model string, src Source) (Stats, error) {
	var s Stats
	since := time.Now().AddDate(0, 0, -days)
	if src != SourceQwen {
		if _, _, err := server.Compact(paths.StateDir()); err != nil {
			s.Notes = append(s.Notes, "compact: "+err.Error())
		}
		ts, err := server.ReadTimings(paths.StateDir(), since)
		if err != nil {
			return s, err
		}
		s.ServerRaw = filterTimings(ts, model)
		s.Server = stats.FromServer(s.ServerRaw)
	}
	if src != SourceServer {
		recs, skipped, err := qwen.ReadUsageDirs(since, paths.QwenUsageDirs()...)
		if err != nil {
			return s, err
		}
		s.Skipped = skipped
		s.QwenRaw = filterRecords(recs, model)
		s.Qwen = stats.FromQwen(s.QwenRaw)
	}
	return s, nil
}

func filterTimings(ts []server.Timing, model string) []server.Timing {
	if model == "" {
		return ts
	}
	var out []server.Timing
	for _, t := range ts {
		if t.Model == model {
			out = append(out, t)
		}
	}
	return out
}

func filterRecords(rs []qwen.Record, model string) []qwen.Record {
	if model == "" {
		return rs
	}
	var out []qwen.Record
	for _, r := range rs {
		if r.Model == model {
			out = append(out, r)
		}
	}
	return out
}

// FormatMs renders milliseconds compactly (e.g. 850 ms, 12.3 s, 3m54s).
func FormatMs(ms float64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%.0f ms", ms)
	case ms < 60000:
		return fmt.Sprintf("%.1f s", ms/1000)
	default:
		d := time.Duration(ms) * time.Millisecond
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}
