package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLauncherCacheArguments(t *testing.T) {
	for _, tc := range []struct{ name, config, cache, checkpoints string }{
		{"old config", "", "8192", "32"},
		{"explicit values", "CACHE_RAM=2048\nCTX_CHECKPOINTS=32\n", "2048", "32"},
		{"disabled checkpoints", "CACHE_RAM=0\nCTX_CHECKPOINTS=0\n", "0", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
			t.Setenv("PATH", dir+":/usr/bin:/bin")
			t.Setenv("CTX", "32768")
			t.Setenv("PORT", "8888")
			t.Setenv("CAPTURE", filepath.Join(dir, "args"))
			model := filepath.Join(dir, "model with spaces.gguf")
			if err := os.WriteFile(model, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			configDir := filepath.Join(dir, "llama-coder")
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf("MODEL_PATH='%s'\nALIAS=test\nCTX=65536\nPORT=8080\n%s", model, tc.config)
			if err := os.WriteFile(filepath.Join(configDir, "config.env"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{
				"lca":          "#!/bin/bash\nexit 0\n",
				"llama-server": "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE\"\n",
			} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			_, file, _, _ := runtime.Caller(0)
			launcher := filepath.Join(filepath.Dir(file), "../../launchers/llama-coder")
			if out, err := exec.Command("/bin/bash", launcher).CombinedOutput(); err != nil {
				t.Fatalf("launcher: %v %s", err, out)
			}
			b, err := os.ReadFile(filepath.Join(dir, "args"))
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Split(strings.TrimSpace(string(b)), "\n")
			for flag, value := range map[string]string{"--cache-ram": tc.cache, "--ctx-checkpoints": tc.checkpoints, "-c": "32768", "--port": "8888", "-m": model} {
				count := 0
				for i, arg := range args {
					if arg == flag {
						count++
						if i+1 >= len(args) || args[i+1] != value {
							t.Errorf("%s: %v", flag, args)
						}
					}
				}
				if count != 1 {
					t.Errorf("%s occurs %d times", flag, count)
				}
			}
		})
	}
}

func TestLauncherRejectsDuplicateCacheFlags(t *testing.T) {
	for _, extra := range []string{"--cache-ram=0", "-cram 1024", "--ctx-checkpoints 8", "-ctxcp=8", "--swa-checkpoints 8"} {
		t.Run(extra, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HOME", dir)
			t.Setenv("XDG_CONFIG_HOME", dir)
			t.Setenv("XDG_STATE_HOME", dir)
			t.Setenv("PATH", "/usr/bin:/bin")
			model := filepath.Join(dir, "model.gguf")
			if err := os.WriteFile(model, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			configDir := filepath.Join(dir, "llama-coder")
			if err := os.MkdirAll(configDir, 0o755); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf("MODEL_PATH='%s'\nALIAS=test\nCTX=65536\nPORT=8080\nEXTRA_ARGS='%s'\n", model, extra)
			if err := os.WriteFile(filepath.Join(configDir, "config.env"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, file, _, _ := runtime.Caller(0)
			out, err := exec.Command("/bin/bash", filepath.Join(filepath.Dir(file), "../../launchers/llama-coder")).CombinedOutput()
			if err == nil || !strings.Contains(string(out), "EXTRA_ARGS must not contain") {
				t.Fatalf("duplicate accepted: %v %s", err, out)
			}
		})
	}
}

// Stubs for qwen-local. curl answers /health with HEALTH_CODE (default 200),
// then HEALTH_THEN for every probe after the first, and 200 once the
// llama-coder stub has started. llama-coder records PORT, its pid and CTX,
// exits with LLAMA_EXIT when set, and otherwise execs sleep so TERM stops it
// like the real exec'd llama-server. qwen records its arguments, environment
// and the claim files present while it runs.
const (
	curlStub = `#!/bin/bash
printf '%s\n' "$@" >> "$CAPTURE.health"
code=${HEALTH_CODE:-200}
if [[ -n "${HEALTH_THEN:-}" && $(grep -c -- /health "$CAPTURE.health") -gt 1 ]]; then code=$HEALTH_THEN; fi
[[ -f "$CAPTURE.server-started" ]] && code=200
printf '%s' "$code"
exit 0
`
	llamaCoderStub = `#!/bin/bash
printf '%s\n' "$PORT" "$$" "${CTX:-}" > "$CAPTURE.llama"
echo "llama-server: stub port $PORT" >&2
[[ "${LLAMA_EXIT:-0}" == 0 ]] || { echo boom >&2; exit "$LLAMA_EXIT"; }
: > "$CAPTURE.server-started"
exec sleep 300
`
	qwenStub = `#!/bin/bash
printf '%s\n' "$@" > "$CAPTURE.args"
printf '%s\n' "$QWEN_HOME" "$OPENAI_MODEL" "$OPENAI_BASE_URL" "$OPENAI_API_KEY" "${QWEN_CODE_ENABLE_WORKFLOWS-unset}" "${QWEN_CODE_DISABLE_WORKFLOWS-unset}" "${QWEN_RUNTIME_DIR-unset}" > "$CAPTURE.env"
inst="$XDG_STATE_HOME/llama-coder/instances"
for f in "$inst"/*/*.pid "$inst"/*/sharers/*; do
  [[ -f "$f" ]] && printf '%s=%s\n' "${f#"$inst/"}" "$(cat "$f")"
done > "$CAPTURE.claims"
exit 0
`
)

func qwenLauncherFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("QWEN_HOME", filepath.Join(dir, "remote"))
	t.Setenv("QWEN_RUNTIME_DIR", filepath.Join(dir, "remote-runtime"))
	t.Setenv("QWEN_CODE_ENABLE_WORKFLOWS", "true")
	t.Setenv("QWEN_CODE_DISABLE_WORKFLOWS", "false")
	t.Setenv("PORT", "9000")
	t.Setenv("OPENAI_MODEL", "remote")
	t.Setenv("OPENAI_BASE_URL", "https://remote.invalid/v1")
	t.Setenv("CAPTURE", filepath.Join(dir, "capture"))
	configDir := filepath.Join(dir, "config", "llama-coder")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.env"), []byte("ALIAS=local\nPORT=8080\nCTX=65536\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"curl":        curlStub,
		"lca":         "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE.sync\"\nexit ${SYNC_EXIT:-0}\n",
		"llama-coder": llamaCoderStub,
		"qwen":        qwenStub,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../../launchers/qwen-local"), filepath.Join(dir, "capture")
}

func instancesDir() string {
	return filepath.Join(os.Getenv("XDG_STATE_HOME"), "llama-coder", "instances")
}

// writeClaim records pid as the owner of port, as a running qwen-local would.
func writeClaim(t *testing.T, port string, pid int) string {
	t.Helper()
	dir := filepath.Join(instancesDir(), port)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "owner.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// runLauncher runs qwen-local and returns its pid, combined output and error.
func runLauncher(t *testing.T, launcher string, args ...string) (int, []byte, error) {
	t.Helper()
	cmd := exec.Command("/bin/bash", append([]string{launcher}, args...)...)
	out, err := cmd.CombinedOutput()
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	return pid, out, err
}

func processGone(pid int) bool {
	// A killed child that nobody has reaped is a zombie and still answers
	// kill -0; the launcher waits for its server, so this is only a guard.
	for i := 0; i < 20; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func readCapture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestQwenLauncherPinsSelectionAndForwardsArguments(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	_, out, err := runLauncher(t, launcher, "-p", "hello world", "--output-format", "json")
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	want := "--auth-type\nopenai\n--model\nlocal\n--openai-base-url\nhttp://127.0.0.1:9000/v1\n--openai-api-key\nlocal\n-p\nhello world\n--output-format\njson\n"
	if args := readCapture(t, capture+".args"); args != want {
		t.Fatalf("args: %q", args)
	}
	// config.env's own PORT is 8080; the 9000 override isolates QWEN_HOME.
	want = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "llama-coder", "qwen-9000") + "\nlocal\nhttp://127.0.0.1:9000/v1\nlocal\nunset\nunset\nunset\n"
	if env := readCapture(t, capture+".env"); env != want {
		t.Fatalf("env: %q", env)
	}
	if sync := readCapture(t, capture+".sync"); sync != "sync\n--port\n9000\n" {
		t.Fatalf("sync: %q", sync)
	}
	if health := readCapture(t, capture+".health"); !strings.Contains(health, "http://127.0.0.1:9000/health") {
		t.Fatal("health uses wrong port")
	}
	if !strings.Contains(string(out), "using the llama-server already running") {
		t.Fatalf("banner: %s", out)
	}
	if exists(capture + ".llama") {
		t.Fatal("started a server although one was running")
	}
	if exists(filepath.Join(instancesDir(), "9000")) {
		t.Fatal("claim left behind")
	}
}

func TestQwenLauncherSharesDefaultDirWhenPortMatchesConfig(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("PORT", "8080") // matches config.env's own PORT: no isolation needed.
	if _, out, err := runLauncher(t, launcher); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "llama-coder", "qwen") + "\nlocal\nhttp://127.0.0.1:8080/v1\nlocal\nunset\nunset\nunset\n"
	if env := readCapture(t, capture+".env"); env != want {
		t.Fatalf("env: %q", env)
	}
}

func TestQwenLauncherStopsOnConflictsAndFailures(t *testing.T) {
	for _, arg := range []string{"-m", "-mremote", "--model=remote", "--auth-type", "--openai-base-url=https://remote.invalid", "--openai-api-key", "--authType=anthropic", "--openaiBaseUrl=https://remote.invalid", "--openaiApiKey=remote"} {
		t.Run(arg, func(t *testing.T) {
			launcher, capture := qwenLauncherFixture(t)
			_, out, err := runLauncher(t, launcher, arg)
			if err == nil || !strings.Contains(string(out), "lca config set") {
				t.Fatalf("conflict: %v %s", err, out)
			}
			if exists(capture + ".args") {
				t.Fatal("Qwen launched")
			}
		})
	}
	for _, tc := range []struct {
		name string
		env  map[string]string
		msg  string
	}{
		{"sync fails", map[string]string{"SYNC_EXIT": "1"}, ""},
		{"foreign service", map[string]string{"HEALTH_CODE": "404"}, "something other than llama-server"},
		{"server dies", map[string]string{"HEALTH_CODE": "000", "LLAMA_EXIT": "1"}, "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			launcher, capture := qwenLauncherFixture(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, out, err := runLauncher(t, launcher)
			if err == nil {
				t.Fatalf("failure ignored: %s", out)
			}
			if !strings.Contains(string(out), tc.msg) {
				t.Fatalf("message: %s", out)
			}
			if exists(capture + ".args") {
				t.Fatal("Qwen launched after failure")
			}
			if exists(filepath.Join(instancesDir(), "9000")) {
				t.Fatal("claim left behind")
			}
		})
	}
}

func TestQwenLauncherStartsAndStopsServer(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("HEALTH_CODE", "000")
	t.Setenv("CTX", "4096")
	pid, out, err := runLauncher(t, launcher)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	llama := strings.Split(strings.TrimSpace(readCapture(t, capture+".llama")), "\n")
	if len(llama) != 3 || llama[0] != "9000" || llama[2] != "4096" {
		t.Fatalf("llama-coder env: %q", llama)
	}
	serverPid, err := strconv.Atoi(llama[1])
	if err != nil {
		t.Fatal(err)
	}
	claims := readCapture(t, capture+".claims")
	for _, want := range []string{fmt.Sprintf("9000/owner.pid=%d\n", pid), fmt.Sprintf("9000/server.pid=%d\n", serverPid)} {
		if !strings.Contains(claims, want) {
			t.Errorf("claims %q lack %q", claims, want)
		}
	}
	if env := readCapture(t, capture+".env"); !strings.Contains(env, "http://127.0.0.1:9000/v1") {
		t.Fatalf("env: %q", env)
	}
	if sync := readCapture(t, capture+".sync"); sync != "sync\n--port\n9000\n--ctx\n4096\n" {
		t.Fatalf("sync: %q", sync)
	}
	if !strings.Contains(string(out), "started llama-server pid "+llama[1]) || !strings.Contains(string(out), "llama-server: stub port 9000") {
		t.Fatalf("banner: %s", out)
	}
	if !processGone(serverPid) {
		_ = syscall.Kill(serverPid, syscall.SIGKILL)
		t.Fatal("server still running after qwen exited")
	}
	if exists(filepath.Join(instancesDir(), "9000")) {
		t.Fatal("claim left behind")
	}
}

func TestQwenLauncherAutoSkipsLiveClaimAndClearsStaleOne(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("PORT", "")
	writeClaim(t, "8080", os.Getpid()) // this test process is alive
	writeClaim(t, "8081", 999999)      // nobody
	pid, out, err := runLauncher(t, launcher)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "llama-coder", "qwen-8081") + "\nlocal\nhttp://127.0.0.1:8081/v1\nlocal\nunset\nunset\nunset\n"
	if env := readCapture(t, capture+".env"); env != want {
		t.Fatalf("env: %q", env)
	}
	if sync := readCapture(t, capture+".sync"); sync != "sync\n--port\n8081\n" {
		t.Fatalf("sync: %q", sync)
	}
	claims := readCapture(t, capture+".claims")
	if !strings.Contains(claims, fmt.Sprintf("8081/owner.pid=%d\n", pid)) || strings.Contains(claims, "8081/server.pid") {
		t.Fatalf("claims: %q", claims)
	}
	if health := readCapture(t, capture+".health"); strings.Contains(health, ":8080/") {
		t.Fatalf("probed a port held by another session: %q", health)
	}
	if owner := readCapture(t, filepath.Join(instancesDir(), "8080", "owner.pid")); strings.TrimSpace(owner) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("live claim changed: %q", owner)
	}
	if exists(filepath.Join(instancesDir(), "8081")) {
		t.Fatal("claim left behind")
	}
}

func TestQwenLauncherAutoStartsServerOnConfigPort(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("PORT", "")
	t.Setenv("HEALTH_CODE", "000")
	if _, out, err := runLauncher(t, launcher); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	llama := strings.Split(strings.TrimSpace(readCapture(t, capture+".llama")), "\n")
	if len(llama) < 2 || llama[0] != "8080" { // CTX is unset here, so the third line is empty
		t.Fatalf("llama-coder env: %q", llama)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "llama-coder", "qwen") + "\nlocal\nhttp://127.0.0.1:8080/v1\nlocal\nunset\nunset\nunset\n"
	if env := readCapture(t, capture+".env"); env != want {
		t.Fatalf("env: %q", env)
	}
	serverPid, _ := strconv.Atoi(llama[1])
	if !processGone(serverPid) {
		_ = syscall.Kill(serverPid, syscall.SIGKILL)
		t.Fatal("server still running after qwen exited")
	}
}

func TestQwenLauncherAutoSkipsForeignService(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("PORT", "")
	t.Setenv("HEALTH_CODE", "404")
	t.Setenv("HEALTH_THEN", "200")
	if _, out, err := runLauncher(t, launcher); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if env := readCapture(t, capture+".env"); !strings.Contains(env, "http://127.0.0.1:8081/v1") {
		t.Fatalf("env: %q", env)
	}
	if exists(capture + ".llama") {
		t.Fatal("started a server on a port something else uses")
	}
	for _, port := range []string{"8080", "8081"} {
		if exists(filepath.Join(instancesDir(), port)) {
			t.Fatalf("claim for %s left behind", port)
		}
	}
}

func TestQwenLauncherSharesPinnedPort(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	dir := writeClaim(t, "9000", os.Getpid())
	pid, out, err := runLauncher(t, launcher)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if env := readCapture(t, capture+".env"); !strings.Contains(env, "http://127.0.0.1:9000/v1") {
		t.Fatalf("env: %q", env)
	}
	if claims := readCapture(t, capture+".claims"); !strings.Contains(claims, fmt.Sprintf("9000/sharers/%d=%d\n", pid, pid)) {
		t.Fatalf("claims: %q", claims)
	}
	if !strings.Contains(string(out), "sharing the llama-server held by qwen-local pid "+strconv.Itoa(os.Getpid())) {
		t.Fatalf("banner: %s", out)
	}
	if exists(capture + ".llama") {
		t.Fatal("a sharer started a server")
	}
	if owner := readCapture(t, filepath.Join(dir, "owner.pid")); strings.TrimSpace(owner) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("owner changed: %q", owner)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "sharers")); len(entries) != 0 {
		t.Fatalf("sharer marker left behind: %v", entries)
	}
}

func TestQwenLauncherWaitsForLoadingServer(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("HEALTH_CODE", "503")
	t.Setenv("HEALTH_THEN", "200")
	if _, out, err := runLauncher(t, launcher); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !exists(capture + ".args") {
		t.Fatal("Qwen not launched")
	}
	if exists(capture + ".llama") {
		t.Fatal("started a second server while one was loading")
	}
	if n := strings.Count(readCapture(t, capture+".health"), "/health"); n < 2 {
		t.Fatalf("probed %d times", n)
	}
}

func TestQwenLauncherAdoptsOnlyOrphanedLlamaServers(t *testing.T) {
	// A stale claim's server.pid is adopted (and stopped at exit) only when
	// the process is still llama-server; anything else is left alone.
	launcher, capture := qwenLauncherFixture(t)
	dir := writeClaim(t, "9000", 999999)
	sleeper := exec.Command("/bin/sleep", "300")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	if err := os.WriteFile(filepath.Join(dir, "server.pid"), []byte(strconv.Itoa(sleeper.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, out, err := runLauncher(t, launcher); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if claims := readCapture(t, capture+".claims"); strings.Contains(claims, "server.pid") {
		t.Fatalf("adopted a process that is not llama-server: %q", claims)
	}
	if err := syscall.Kill(sleeper.Process.Pid, 0); err != nil {
		t.Fatal("stopped a process that is not llama-server")
	}
	if exists(dir) {
		t.Fatal("claim left behind")
	}
}
