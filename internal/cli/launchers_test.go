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
