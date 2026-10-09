package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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

func qwenLauncherFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
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
		"curl": "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE.health\"\nexit ${HEALTH_EXIT:-0}\n",
		"lca":  "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE.sync\"\nexit ${SYNC_EXIT:-0}\n",
		"qwen": "#!/bin/bash\nprintf '%s\\n' \"$@\" > \"$CAPTURE.args\"\nprintf '%s\\n' \"$QWEN_HOME\" \"$OPENAI_MODEL\" \"$OPENAI_BASE_URL\" \"$OPENAI_API_KEY\" \"${QWEN_CODE_ENABLE_WORKFLOWS-unset}\" \"${QWEN_CODE_DISABLE_WORKFLOWS-unset}\" \"${QWEN_RUNTIME_DIR-unset}\" > \"$CAPTURE.env\"\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../../launchers/qwen-local"), filepath.Join(dir, "capture")
}

func TestQwenLauncherPinsSelectionAndForwardsArguments(t *testing.T) {
	launcher, capture := qwenLauncherFixture(t)
	if out, err := exec.Command("/bin/bash", launcher, "-p", "hello world", "--output-format", "json").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	args, _ := os.ReadFile(capture + ".args")
	want := "--auth-type\nopenai\n--model\nlocal\n--openai-base-url\nhttp://127.0.0.1:9000/v1\n--openai-api-key\nlocal\n-p\nhello world\n--output-format\njson\n"
	if string(args) != want {
		t.Fatalf("args: %q", args)
	}
	env, _ := os.ReadFile(capture + ".env")
	want = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "llama-coder", "qwen") + "\nlocal\nhttp://127.0.0.1:9000/v1\nlocal\nunset\nunset\nunset\n"
	if string(env) != want {
		t.Fatalf("env: %q", env)
	}
	sync, _ := os.ReadFile(capture + ".sync")
	if string(sync) != "sync\n--port\n9000\n" {
		t.Fatalf("sync: %q", sync)
	}
	health, _ := os.ReadFile(capture + ".health")
	if !strings.Contains(string(health), "http://127.0.0.1:9000/health") {
		t.Fatal("health uses wrong port")
	}
}

func TestQwenLauncherStopsOnConflictsAndFailures(t *testing.T) {
	for _, arg := range []string{"-m", "-mremote", "--model=remote", "--auth-type", "--openai-base-url=https://remote.invalid", "--openai-api-key", "--authType=anthropic", "--openaiBaseUrl=https://remote.invalid", "--openaiApiKey=remote"} {
		t.Run(arg, func(t *testing.T) {
			launcher, capture := qwenLauncherFixture(t)
			out, err := exec.Command("/bin/bash", launcher, arg).CombinedOutput()
			if err == nil || !strings.Contains(string(out), "lca config set") {
				t.Fatalf("conflict: %v %s", err, out)
			}
			if _, err := os.Stat(capture + ".args"); !os.IsNotExist(err) {
				t.Fatal("Qwen launched")
			}
		})
	}
	for _, env := range []string{"HEALTH_EXIT", "SYNC_EXIT"} {
		t.Run(env, func(t *testing.T) {
			launcher, capture := qwenLauncherFixture(t)
			t.Setenv(env, "1")
			if out, err := exec.Command("/bin/bash", launcher).CombinedOutput(); err == nil {
				t.Fatalf("failure ignored: %s", out)
			}
			if _, err := os.Stat(capture + ".args"); !os.IsNotExist(err) {
				t.Fatal("Qwen launched after failure")
			}
		})
	}
}
