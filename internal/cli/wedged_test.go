package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
)

// wedgedLog is what llama.cpp writes once a Metal command buffer has failed.
// The marker is the one internal/server and the launcher both grep for.
const wedgedLog = "I slot print_timing: ordinary chatter\n" +
	"E error: Insufficient Memory (00000008:kIOGPUCommandBufferCallbackErrorOutOfMemory)\n" +
	"E ggml_metal_graph_compute: " + server.WedgedMarker +
	" from a previous command buffer failure - recreate the backend to recover\n"

// writeLog puts a launcher.log in a port's claim directory.
func writeLog(t *testing.T, port, body string) {
	t.Helper()
	dir := filepath.Join(instancesDir(), port)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "launcher.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeServer starts a process named llama-server, so the launcher's
// is_llama_server check adopts it the way it would a real one. It returns the
// pid and records it as the given port's server.pid.
func fakeServer(t *testing.T, port string) (int, <-chan struct{}) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "llama-server")
	src, err := os.ReadFile("/bin/sleep")
	if err != nil {
		t.Skipf("cannot read /bin/sleep: %v", err)
	}
	if err := os.WriteFile(bin, src, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "300")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a copy of sleep: %v", err)
	}
	pid := cmd.Process.Pid
	// Reap it here rather than in Cleanup: a TERMed child nobody has waited
	// on is a zombie that still answers kill -0, so a liveness check alone
	// would report it running long after the launcher stopped it.
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-exited })
	dir := filepath.Join(instancesDir(), port)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.pid"), []byte(strconv.Itoa(pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return pid, exited
}

// A wedged server answers /health 200 like a healthy one, so without the log
// check the launcher would hand the harness a server that errors on every
// request. It must skip the port — and stop that server first, because on a
// 16 GB machine its memory is what the replacement needs.
func TestQwenLauncherSkipsAndStopsAWedgedServer(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("PORT", "") // walk from config.env's 8080
	writeClaim(t, "8080", 999999)
	writeLog(t, "8080", wedgedLog)
	_, exited := fakeServer(t, "8080")

	_, out, err := runLauncher(t, launcher)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if !strings.Contains(string(out), "unrecoverable Metal error") {
		t.Fatalf("no diagnosis: %s", out)
	}
	if env := readCapture(t, capture+".env"); !strings.Contains(env, "http://127.0.0.1:8081/v1") {
		t.Fatalf("did not move to the next port: %q", env)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("wedged server left running; its memory is what the replacement needs")
	}
	if exists(filepath.Join(instancesDir(), "8081")) {
		t.Fatal("claim left behind")
	}
}

// Same log, but no marker: nothing changes.
func TestQwenLauncherUsesAHealthyServerWithALog(t *testing.T) {
	cases := map[string]string{
		"healthy log": "I slot print_timing: id 0 | task 1 | total time = 1200.00 ms\n",
		"no log":      "",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			launcher, capture := qwenLauncherFixture(t)
			t.Setenv("PORT", "")
			writeClaim(t, "8080", 999999)
			if body != "" {
				writeLog(t, "8080", body)
			}
			if _, out, err := runLauncher(t, launcher); err != nil {
				t.Fatalf("%v %s", err, out)
			}
			if env := readCapture(t, capture+".env"); !strings.Contains(env, "http://127.0.0.1:8080/v1") {
				t.Fatalf("skipped a healthy port: %q", env)
			}
		})
	}
}

// A pinned port has nowhere to walk to, so the launcher says what is wrong
// instead of starting the harness against a dead server.
func TestQwenLauncherRefusesAWedgedPinnedPort(t *testing.T) {
	cases := map[string]struct {
		owner int
		want  string
	}{
		"dead owner": {999999, "unset PORT="},
		"live owner": {os.Getpid(), "quit the session that owns it"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			launcher, capture := qwenLauncherFixture(t) // fixture pins PORT=9000
			writeClaim(t, "9000", c.owner)
			writeLog(t, "9000", wedgedLog)

			_, out, err := runLauncher(t, launcher)
			if err == nil {
				t.Fatalf("launched against a wedged server: %s", out)
			}
			if !strings.Contains(string(out), "unrecoverable Metal error") || !strings.Contains(string(out), c.want) {
				t.Fatalf("message missing %q: %s", c.want, out)
			}
			if exists(capture + ".args") {
				t.Fatal("started the harness anyway")
			}
		})
	}
}

// A server can be healthy at launch and wedge hours later, which is how the
// failure this guards against actually happened. The watcher has to say so.
func TestQwenLauncherWarnsWhenTheServerWedgesMidSession(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	t.Setenv("PORT", "")
	t.Setenv("HEALTH_CODE", "000") // nothing there, so the launcher starts one
	t.Setenv("LOCAL_HARNESS_WATCH_INTERVAL", "1")
	// The harness stub must outlive one poll, so hold it until the log is
	// wedged. The qwen stub waits for this file to appear.
	gate := filepath.Join(t.TempDir(), "gate")
	t.Setenv("QWEN_WAIT_FOR", gate)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Wait for the server's log to exist, wedge it, then release the
		// harness two polls later.
		log := filepath.Join(instancesDir(), "8080", "launcher.log")
		for i := 0; i < 100 && !exists(log); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		f, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		_, _ = f.WriteString(wedgedLog)
		_ = f.Close()
		time.Sleep(2500 * time.Millisecond)
		_ = os.WriteFile(gate, []byte("go\n"), 0o600)
	}()

	_, out, err := runLauncher(t, launcher)
	<-done
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if n := strings.Count(string(out), "unrecoverable error"); n != 1 {
		t.Fatalf("warned %d times, want exactly 1: %s", n, out)
	}
	if !strings.Contains(string(out), "Compute error") {
		t.Fatalf("warning does not name the symptom: %s", out)
	}
	_ = capture
}

// The watcher must not outlive the session that started it.
func TestQwenLauncherReapsTheWatcher(t *testing.T) {
	launcher, _ := qwenLauncherFixture(t)
	t.Setenv("PORT", "")
	t.Setenv("HEALTH_CODE", "000")
	t.Setenv("LOCAL_HARNESS_WATCH_INTERVAL", "1")
	pid, out, err := runLauncher(t, launcher)
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	// Any surviving watcher is a child of the launcher, which has exited.
	for i := 0; i < 20; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	b, _ := exec.Command("/bin/ps", "-eo", "ppid=,command=").Output()
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, "watch_wedge") || strings.Contains(line, strconv.Itoa(pid)+" ") {
			if strings.Contains(line, "local-harness") {
				t.Fatalf("launcher child survived: %s", line)
			}
		}
	}
}
