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

	"github.com/jorden-parker/local-coding-agent-setup/internal/backup"
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

// SyncQwen merges the local provider into the user's own Qwen Code settings.
// It reports whether the file changed, the file it patched, and the backup it
// took if this was the first time lca wrote there.
func SyncQwen(f *config.File) (p qwen.Provider, changed bool, settingsPath, backedUp string, err error) {
	p, err = Provider(f)
	if err != nil {
		return p, false, "", "", err
	}
	settingsPath = paths.QwenSettings()
	if backedUp, err = backup.Once(settingsPath); err != nil {
		return p, false, settingsPath, "", err
	}
	changed, err = qwen.Prepare(settingsPath, p)
	return p, changed, settingsPath, backedUp, err
}

// SyncPi merges the local provider into the user's own pi models.json. pi's
// settings.json is deliberately left alone: defaultProvider and defaultModel
// are the user's own selection, and pi-local passes both as flags.
func SyncPi(f *config.File) (p pi.Provider, changed bool, modelsPath, backedUp string, err error) {
	p, err = PiProvider(f)
	if err != nil {
		return p, false, "", "", err
	}
	modelsPath = paths.PiModels()
	if backedUp, err = backup.Once(modelsPath); err != nil {
		return p, false, modelsPath, "", err
	}
	changed, err = pi.PrepareModels(modelsPath, p)
	return p, changed, modelsPath, backedUp, err
}

// Syncs reports whether lca manages harness h's configuration. Since lca
// patches the files the harnesses already own rather than keeping copies of
// its own, the test is deliberately narrow: h is the selected harness, or its
// configuration already carries lca's provider entry from an earlier sync.
// Having the binary on PATH is not enough — someone who installed a harness
// for other work has not invited lca into its configuration file.
func Syncs(f *config.File, h string) bool {
	if Harness(f) == h {
		return true
	}
	switch h {
	case "qwen":
		_, ok, err := qwen.Entry(paths.QwenSettings(), value(f, "ALIAS"))
		return err == nil && ok
	case "pi":
		ok, err := pi.HasProvider(paths.PiModels(), pi.ProviderID)
		return err == nil && ok
	}
	return false
}

// SyncsPi reports whether lca manages a pi configuration.
func SyncsPi(f *config.File) bool { return Syncs(f, "pi") }

// SyncsQwen reports whether lca manages a Qwen Code configuration.
func SyncsQwen(f *config.File) bool { return Syncs(f, "qwen") }

// Harnesses names every harness lca manages the configuration of, in a stable
// order. setup.sh installs neither, so neither is assumed to be present.
func Harnesses(f *config.File) []string {
	var out []string
	for _, h := range []string{"qwen", "pi"} {
		if Syncs(f, h) {
			out = append(out, h)
		}
	}
	return out
}

// SyncAll patches the configuration of every harness lca manages and returns
// the files it wrote to, whether or not they changed.
func SyncAll(f *config.File) ([]string, error) {
	var synced []string
	for _, h := range Harnesses(f) {
		var path string
		var err error
		switch h {
		case "qwen":
			_, _, path, _, err = SyncQwen(f)
		case "pi":
			_, _, path, _, err = SyncPi(f)
		}
		if err != nil {
			return nil, err
		}
		synced = append(synced, path)
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
		// The launcher, not the settings file, is what pins model, auth and
		// endpoint for a local session, so these strings must survive any
		// edit to it.
		pinned := []string{"--auth-type openai --model", "--provider llama-local --model", "OPENAI_BASE_URL"}
		missing := ""
		if name == "local-harness" {
			for _, want := range pinned {
				if !strings.Contains(string(b), want) {
					missing = want
					break
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

	addQwenChecks(f, values, add, warn)
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

// qwenInstallHint and piInstallHint are the one place each harness's install
// command is written. local-harness prints the same two strings.
const (
	qwenInstallHint = "brew install qwen-code"
	piInstallHint   = "curl -fsSL https://pi.dev/install.sh | sh"
)

// addQwenChecks reports on Qwen Code. setup.sh installs neither harness, so a
// machine without this one stays silent unless HARNESS selects it — the same
// contract addPiChecks has always had.
func addQwenChecks(f *config.File, values map[string]string, add func(string, bool, string), warn func(string, string)) {
	selected := values["HARNESS"] == "qwen"
	if _, err := exec.LookPath("qwen"); err != nil {
		if selected {
			add("qwen on PATH", false, "HARNESS=qwen but Qwen Code is not installed: "+qwenInstallHint)
		}
		return
	}
	add("qwen on PATH", true, "")
	settings := paths.QwenSettings()
	want, err := Provider(f)
	if err != nil {
		return
	}
	if !selected {
		if _, ok, err := qwen.Entry(settings, want.ID); err == nil && !ok {
			// Qwen Code is installed but has never been pointed at the local
			// server, and HARNESS says it is not meant to be.
			warn("qwen provider", paths.Tildify(settings)+" carries no local provider yet; run qwen-local or lca sync --harness qwen")
			return
		}
	}
	got, ok, entryErr := qwen.Entry(settings, want.ID)
	switch {
	case entryErr != nil:
		add("qwen provider", false, entryErr.Error())
	case !ok:
		add("qwen provider", false, fmt.Sprintf("no modelProviders.openai entry %q in %s; run lca sync", want.ID, paths.Tildify(settings)))
	case got.BaseURL != want.BaseURL || got.ContextWindow != want.ContextWindow || got.EnvKey != want.EnvKey:
		add("qwen provider", false, fmt.Sprintf("entry %q has %s ctx %d, config.env says %s ctx %d; run lca sync", want.ID, got.BaseURL, got.ContextWindow, want.BaseURL, want.ContextWindow))
	default:
		add("qwen provider", true, fmt.Sprintf("%s → %s ctx %d in %s", want.ID, want.BaseURL, want.ContextWindow, paths.Tildify(settings)))
	}
	// Lean is opt-in and global, so its absence is never a failure: it only
	// says which profile ordinary qwen and qwen-local are both running under.
	if _, err := os.Stat(qwen.SnapshotFor(settings)); err == nil {
		add("qwen lean profile", true, "applied; lca qwen-profile restore reverts it")
	} else {
		warn("qwen lean profile", "not applied; lca qwen-profile lean trims background work and tool schemas for a local model")
	}
	if on, err := qwen.UsageStatsEnabled(settings); err != nil {
		add("qwen usage stats", false, err.Error())
	} else if !on {
		warn("qwen usage stats", "privacy.usageStatisticsEnabled is false; lca stats --source qwen will be empty")
	} else {
		add("qwen usage stats", true, "enabled")
	}
	addSkillsCheck("qwen skills", paths.QwenSkillsDir(), add, warn)
	addBackupCheck("qwen config backup", settings, add)
}

// addPiChecks reports on the optional pi harness. pi is never installed by
// setup.sh, so a machine without it stays silent unless HARNESS says pi is the
// one that should be running.
func addPiChecks(f *config.File, values map[string]string, add func(string, bool, string), warn func(string, string)) {
	selected := values["HARNESS"] == "pi"
	if _, err := exec.LookPath("pi"); err != nil {
		if selected {
			add("pi on PATH", false, "HARNESS=pi but pi is not installed: "+piInstallHint)
		}
		return
	}
	add("pi on PATH", true, "")
	models := paths.PiModels()
	want, err := PiProvider(f)
	if err != nil {
		return
	}
	if ok, err := pi.HasProvider(models, pi.ProviderID); err == nil && !ok && !selected {
		// pi is installed but has never been used against the local server.
		warn("pi provider", paths.Tildify(models)+" carries no local provider yet; run pi-local or lca sync --harness pi")
		return
	}
	if err := pi.CheckModels(models, want); err != nil {
		add("pi provider", false, err.Error()+"; run lca sync --harness pi")
	} else {
		add("pi provider", true, fmt.Sprintf("%s → %s ctx %d in %s", want.Model, want.BaseURL, want.ContextWindow, paths.Tildify(models)))
	}
	addSkillsCheck("pi skills", paths.PiSkillsDir(), add, warn)
	addBackupCheck("pi config backup", models, add)
}

// addBackupCheck names the copy lca took of a harness configuration file
// before it first patched it, so the way back is discoverable later.
func addBackupCheck(name, path string, add func(string, bool, string)) {
	found, err := backup.Existing(path)
	if err != nil || len(found) == 0 {
		return
	}
	add(name, true, paths.Tildify(found[len(found)-1]))
}

// addSkillsCheck reports the skills a harness can see, per root. A harness
// reads its own skills directory and the harness-neutral ~/.agents/skills, and
// lca names neither in any settings file: both are defaults the harness
// resolves from its own home. Reporting them separately is how a stale symlink
// farm in one tells itself apart from a real gap.
func addSkillsCheck(name, harnessDir string, add func(string, bool, string), warn func(string, string)) {
	if detail, total := skillRoots(harnessDir, paths.AgentsSkillsDir()); total == 0 {
		warn(name, "no SKILL.md in "+detail)
	} else {
		add(name, true, detail)
	}
}

// skillRoots describes the user-level skill directories a harness reads, in
// that order, with the number of skills each holds. The returned count only
// answers "anything anywhere?": the roots overlap, because a harness skill
// directory is commonly a symlink farm pointing into ~/.agents/skills.
func skillRoots(dirs ...string) (string, int) {
	parts := make([]string, 0, len(dirs))
	total := 0
	for _, dir := range dirs {
		n := countSkills(dir)
		total += n
		parts = append(parts, fmt.Sprintf("%d in %s", n, paths.Tildify(dir)))
	}
	return strings.Join(parts, ", "), total
}

// countSkills counts the immediate subdirectories of dir holding a SKILL.md,
// following symlinks. Both harnesses also look deeper, so this is a floor
// rather than a census: enough to tell an empty root from a populated one.
func countSkills(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil && info.Mode().IsRegular() {
			n++
		}
	}
	return n
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
	Source Source
	// Model is the alias the records were filtered to, or "" for every model.
	Model      string
	Harness    []stats.Bucket
	Server     []stats.Bucket
	HarnessRaw []usage.Record
	ServerRaw  []server.Timing
	Skipped    int
	Notes      []string
}

// UsageSources names the directory CollectStats reads src's usage from.
func UsageSources(src Source) string {
	if src == SourcePi {
		return paths.Tildify(paths.PiSessionDir())
	}
	return paths.Tildify(paths.QwenUsageDir())
}

// StatsFilter resolves which model's records CollectStats keeps. The records
// now live in the harness's own directory, mixed in with every other model
// the user runs it against, so the default is config.env's ALIAS: without it
// "response times" would silently average the local model together with a
// cloud one. An explicit model wins, and allModels drops the filter.
func StatsFilter(model string, allModels bool) string {
	if model != "" {
		return model
	}
	if allModels {
		return ""
	}
	f, err := LoadConfig()
	if err != nil {
		return ""
	}
	return value(f, "ALIAS")
}

// CollectStats compacts server logs, then reads both sources since days ago,
// keeping only the model StatsFilter resolves. Stats.Model reports it so the
// caller can say what was counted.
func CollectStats(days int, model string, allModels bool, src Source) (Stats, error) {
	var s Stats
	s.Source = src
	if src == SourceBoth {
		s.Source = HarnessSource()
	}
	s.Model = StatsFilter(model, allModels)
	since := time.Now().AddDate(0, 0, -days)
	if src == SourceServer || src == SourceBoth {
		if _, _, err := server.Compact(paths.StateDir()); err != nil {
			s.Notes = append(s.Notes, "compact: "+err.Error())
		}
		ts, err := server.ReadTimings(paths.StateDir(), since)
		if err != nil {
			return s, err
		}
		s.ServerRaw = filterTimings(ts, s.Model)
		s.Server = stats.FromServer(s.ServerRaw)
	}
	if src != SourceServer {
		var recs []usage.Record
		var skipped int
		var err error
		if s.Source == SourcePi {
			recs, skipped, err = pi.ReadSessions(since, paths.PiSessionDir())
		} else {
			recs, skipped, err = usage.ReadUsageDirs(since, paths.QwenUsageDir())
		}
		if err != nil {
			return s, err
		}
		s.Skipped = skipped
		s.HarnessRaw = filterRecords(recs, s.Model)
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
