// Package app holds the operations shared by the CLI and the TUI: loading
// config.env, mirroring it into the agent harness's settings, health checks
// and collecting response-time statistics.
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
	"github.com/jorden-parker/local-coding-agent-setup/internal/pi"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
	"github.com/jorden-parker/local-coding-agent-setup/internal/stats"
	"github.com/jorden-parker/local-coding-agent-setup/internal/usage"
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

// Harness names the agent harness lca syncs and reports on by default.
func Harness(f *config.File) string { return value(f, "HARNESS") }

// value reads a key, falling back to the catalogue default.
func value(f *config.File, key string) string {
	if v, ok := f.Get(key); ok && v != "" {
		return v
	}
	k, _ := config.Lookup(key)
	return k.Default
}

// requireProviderKeys validates the keys both harnesses build their provider
// entry from.
func requireProviderKeys(f *config.File) error {
	for _, k := range []string{"ALIAS", "PORT", "CTX"} {
		v, _ := f.Get(k)
		if _, err := config.Validate(k, v); err != nil {
			return err
		}
	}
	return nil
}

// Provider derives the Qwen provider entry from a config file.
func Provider(f *config.File) (qwen.Provider, error) {
	if err := requireProviderKeys(f); err != nil {
		return qwen.Provider{}, err
	}
	alias, _ := f.Get("ALIAS")
	port, _ := f.Get("PORT")
	ctx, _ := f.Get("CTX")
	p, _ := strconv.Atoi(port)
	c, _ := strconv.Atoi(ctx)
	return qwen.ProviderFor(alias, p, c), nil
}

// PiProvider derives pi's models.json provider entry from a config file.
func PiProvider(f *config.File) (pi.Provider, error) {
	if err := requireProviderKeys(f); err != nil {
		return pi.Provider{}, err
	}
	alias, _ := f.Get("ALIAS")
	port, _ := f.Get("PORT")
	ctx, _ := f.Get("CTX")
	p, _ := strconv.Atoi(port)
	c, _ := strconv.Atoi(ctx)
	return pi.ProviderFor(alias, p, c, value(f, "THINKING") == "true"), nil
}

// SyncQwen prepares the managed local provider and lean settings for f and
// shares the provider with ordinary Qwen. It reports whether either file
// changed.
func SyncQwen(f *config.File) (qwen.Provider, bool, error) {
	p, changed, shared, _, err := SyncQwenAt(f, "", "")
	return p, changed || shared, err
}

// SyncQwenAt prepares the local provider and lean settings for f, isolated
// per paths.QwenInstanceDir(port, cfgPort). cfgPort is config.env's own PORT,
// captured before any runtime override; port is the port actually in effect.
// A port matching cfgPort still uses the shared default settings file, so
// ordinary single-instance use is unaffected, and only then is the provider
// also mirrored into ordinary Qwen's settings (qwen.ShareLocal), since that
// is the server the VS Code companion's chat and plain qwen can reach.
// changed reports the managed file, shared the ordinary one.
func SyncQwenAt(f *config.File, port, cfgPort string) (p qwen.Provider, changed, shared bool, settingsPath string, err error) {
	p, err = Provider(f)
	if err != nil {
		return p, false, false, "", err
	}
	settingsPath = paths.QwenSettingsFor(port, cfgPort)
	changed, err = qwen.PrepareLocal(settingsPath, p)
	if err != nil || port != cfgPort {
		return p, changed, false, settingsPath, err
	}
	shared, err = qwen.ShareLocal(paths.LegacyQwenSettings(), p)
	return p, changed, shared, settingsPath, err
}

// SyncPi writes pi's managed models.json and settings.json for the default
// instance. It reports whether either file changed.
func SyncPi(f *config.File) (pi.Provider, bool, error) {
	p, changed, _, err := SyncPiAt(f, "", "")
	return p, changed, err
}

// SyncPiAt writes pi's managed configuration for f, isolated per
// paths.PiInstanceDir(port, cfgPort) the way SyncQwenAt isolates Qwen's. pi
// needs no equivalent of qwen.ShareLocal: the managed agent directory is the
// only one pi-local ever points PI_CODING_AGENT_DIR at.
func SyncPiAt(f *config.File, port, cfgPort string) (p pi.Provider, changed bool, modelsPath string, err error) {
	p, err = PiProvider(f)
	if err != nil {
		return p, false, "", err
	}
	modelsPath = paths.PiModelsFor(port, cfgPort)
	changed, err = pi.PrepareModels(modelsPath, p)
	if err != nil {
		return p, changed, modelsPath, err
	}
	settings, err := pi.PrepareSettings(paths.PiSettingsFor(port, cfgPort), p)
	return p, changed || settings, modelsPath, err
}

// SyncsPi reports whether lca manages a pi configuration: either pi is the
// selected harness, or pi-local has already created its managed directory.
func SyncsPi(f *config.File) bool {
	if Harness(f) == "pi" {
		return true
	}
	_, err := os.Stat(paths.PiModels())
	return err == nil
}

// SyncAll prepares the default instance of every harness lca manages: Qwen
// Code always, since setup.sh installs it, and pi when SyncsPi says so. It
// returns the settings files it owns, whether or not they changed.
func SyncAll(f *config.File) ([]string, error) {
	synced := []string{paths.QwenSettings()}
	if _, _, err := SyncQwen(f); err != nil {
		return nil, err
	}
	if SyncsPi(f) {
		_, _, models, err := SyncPiAt(f, "", "")
		if err != nil {
			return nil, err
		}
		synced = append(synced, models)
	}
	return synced, nil
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
	hint = "No restart needed."
	if k.Restart != "" {
		hint = "Restart " + k.Restart + " to apply."
	}
	if k.Syncs {
		synced, err := SyncAll(f)
		if err != nil {
			return warn, hint, fmt.Errorf("config.env saved but harness settings not updated: %w", err)
		}
		for _, path := range synced {
			hint += " " + paths.Tildify(path) + " updated."
		}
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

	for _, name := range []string{"llama-coder", "local-harness"} {
		p := filepath.Join(paths.BinDir(), name)
		b, err := os.ReadFile(p)
		pinned := []string{"QWEN_HOME", "--auth-type openai --model", "PI_CODING_AGENT_DIR", "--provider"}
		missing := ""
		if name == "local-harness" {
			for _, want := range pinned {
				if !strings.Contains(string(b), want) {
					missing = want
				}
			}
		}
		switch {
		case err != nil:
			add(name, false, paths.Tildify(p)+" missing; run setup.sh")
		case !strings.Contains(string(b), "config.env"):
			add(name, false, paths.Tildify(p)+" is the old setup-time version; re-run setup.sh")
		case missing != "":
			add(name, false, paths.Tildify(p)+" does not pin the local model ("+missing+" absent); re-run setup.sh")
		default:
			add(name, true, paths.Tildify(p))
		}
	}
	// qwen-local and pi-local are symlinks to local-harness; the name they are
	// invoked under is what selects the harness.
	for _, name := range []string{"qwen-local", "pi-local"} {
		p := filepath.Join(paths.BinDir(), name)
		switch target, err := os.Readlink(p); {
		case err != nil && os.IsNotExist(err):
			add(name, false, paths.Tildify(p)+" missing; run setup.sh")
		case err != nil:
			add(name, false, paths.Tildify(p)+" is not a symlink to local-harness; re-run setup.sh")
		case filepath.Base(target) != "local-harness":
			add(name, false, paths.Tildify(p)+" points at "+target+", not local-harness; re-run setup.sh")
		default:
			add(name, true, paths.Tildify(p)+" → "+target)
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
		if err := qwen.CheckShared(paths.LegacyQwenSettings(), want); err != nil {
			add("qwen shared provider", false, err.Error()+"; run lca sync")
		} else {
			add("qwen shared provider", true, want.ID+" listed for ordinary qwen and the VS Code companion in "+paths.Tildify(paths.LegacyQwenSettings()))
		}
	}
	if on, err := qwen.UsageStatsEnabled(paths.QwenSettings()); err != nil {
		add("qwen usage stats", false, err.Error())
	} else if !on {
		warn("qwen usage stats", "privacy.usageStatisticsEnabled is false; lca stats --source qwen will be empty")
	} else {
		add("qwen usage stats", true, "enabled")
	}

	addPiChecks(f, values, add, warn)

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

// addPiChecks reports on the optional pi harness. pi is never installed by
// setup.sh, so a machine without it stays silent unless HARNESS says pi is the
// one that should be running.
func addPiChecks(f *config.File, values map[string]string, add func(string, bool, string), warn func(string, string)) {
	selected := values["HARNESS"] == "pi"
	_, lookErr := exec.LookPath("pi")
	if lookErr != nil {
		if selected {
			add("pi on PATH", false, "HARNESS=pi but pi is not installed: curl -fsSL https://pi.dev/install.sh | sh")
		}
		return
	}
	add("pi on PATH", true, "")
	want, err := PiProvider(f)
	if err != nil {
		return
	}
	if _, err := os.Stat(paths.PiModels()); err != nil && os.IsNotExist(err) && !selected {
		// pi is installed but has never been used against the local server.
		warn("pi provider", paths.Tildify(paths.PiModels())+" not written yet; run pi-local or lca sync --harness pi")
		return
	}
	if err := pi.CheckModels(paths.PiModels(), want); err != nil {
		add("pi provider", false, err.Error()+"; run lca sync --harness pi")
	} else {
		add("pi provider", true, fmt.Sprintf("%s → %s ctx %d in %s", want.Model, want.BaseURL, want.ContextWindow, paths.Tildify(paths.PiModels())))
	}
	if err := pi.CheckSettings(paths.PiSettings(), want); err != nil {
		add("pi local profile", false, err.Error()+"; run lca sync --harness pi")
	} else {
		add("pi local profile", true, want.ID+", skills "+paths.Tildify(paths.LegacyPiDir())+"/skills in "+paths.Tildify(paths.PiSettings()))
	}
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

// Sources. SourceBoth pairs the server with whichever harness config.env
// selects.
const (
	SourceQwen   Source = "qwen"
	SourcePi     Source = "pi"
	SourceServer Source = "server"
	SourceBoth   Source = "both"
)

// ParseSource validates a --source flag.
func ParseSource(s string) (Source, error) {
	switch Source(s) {
	case SourceQwen, SourcePi, SourceServer, SourceBoth:
		return Source(s), nil
	}
	return "", errors.New("--source must be qwen, pi, server or both")
}

// HarnessSource is the stats source of the harness config.env selects. A
// missing or unreadable config.env falls back to Qwen Code.
func HarnessSource() Source {
	f, err := LoadConfig()
	if err != nil {
		return SourceQwen
	}
	if Harness(f) == "pi" {
		return SourcePi
	}
	return SourceQwen
}

// Label names a source in a heading.
func (s Source) Label() string {
	if s == SourcePi {
		return "pi"
	}
	return "Qwen Code"
}

// Stats is the collected data for the stats command and screen. Harness holds
// whichever harness Source names.
type Stats struct {
	Source     Source
	Harness    []stats.Bucket
	Server     []stats.Bucket
	HarnessRaw []usage.Record
	ServerRaw  []server.Timing
	Skipped    int
	Notes      []string
}

// UsageSources names the directories CollectStats reads src's usage from.
func UsageSources(src Source) string {
	dirs := paths.QwenUsageDirs()
	if src == SourcePi {
		dirs = paths.PiSessionDirs()
	}
	var parts []string
	for _, d := range dirs {
		parts = append(parts, paths.Tildify(d))
	}
	return strings.Join(parts, ", ")
}

// CollectStats compacts server logs, then reads both sources since days ago,
// keeping only model when non-empty.
func CollectStats(days int, model string, src Source) (Stats, error) {
	var s Stats
	s.Source = src
	if src == SourceBoth {
		s.Source = HarnessSource()
	}
	since := time.Now().AddDate(0, 0, -days)
	if src == SourceServer || src == SourceBoth {
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
		var recs []usage.Record
		var skipped int
		var err error
		if s.Source == SourcePi {
			recs, skipped, err = pi.ReadSessions(since, paths.PiSessionDirs()...)
		} else {
			recs, skipped, err = usage.ReadUsageDirs(since, paths.QwenUsageDirs()...)
		}
		if err != nil {
			return s, err
		}
		s.Skipped = skipped
		s.HarnessRaw = filterRecords(recs, model)
		s.Harness = stats.FromUsage(s.HarnessRaw)
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

func filterRecords(rs []usage.Record, model string) []usage.Record {
	if model == "" {
		return rs
	}
	var out []usage.Record
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
