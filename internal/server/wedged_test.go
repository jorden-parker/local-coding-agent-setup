package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWedgedReadsTheMarker(t *testing.T) {
	// The real lines llama.cpp writes, in order, when Metal runs out of
	// wired GPU memory.
	fatal := `E ggml_metal_synchronize: error: command buffer 0 failed with status 5
E error: Insufficient Memory (00000008:kIOGPUCommandBufferCallbackErrorOutOfMemory)
E ggml_metal_graph_compute: backend is in error state from a previous command buffer failure - recreate the backend to recover
E srv        decode: Compute error. off = 0, n_batch = 2048, ret = -3
`
	healthy := "I slot print_timing: id  0 | task 1 | total time = 1200.00 ms / 580 tokens\n"

	cases := map[string]struct {
		body string
		want bool
	}{
		"wedged":              {fatal, true},
		"healthy":             {healthy, false},
		"empty":               {"", false},
		"healthy then wedged": {healthy + fatal, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launcher.log")
			if err := os.WriteFile(path, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := Wedged(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("Wedged() = %v, want %v", got, c.want)
			}
		})
	}
}

// No log means no server was started under that claim, which is not an error.
func TestWedgedIgnoresAMissingLog(t *testing.T) {
	got, err := Wedged(filepath.Join(t.TempDir(), "absent.log"))
	if err != nil || got {
		t.Fatalf("Wedged() = %v, %v, want false, nil", got, err)
	}
}

// A server that ran for a day writes a large log; only its tail is read, and
// the marker repeats after the first failure so the tail always carries it.
func TestWedgedReadsOnlyTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "launcher.log")
	bulk := strings.Repeat("I slot print_timing: ordinary chatter\n", 20000)
	if err := os.WriteFile(path, []byte(bulk+WedgedMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Size() <= tailBytes {
		t.Fatalf("fixture is only %d bytes; it must exceed the %d-byte tail", fi.Size(), tailBytes)
	}
	got, err := Wedged(path)
	if err != nil || !got {
		t.Fatalf("Wedged() = %v, %v, want true", got, err)
	}

	// The same size of log with the marker only at the very start is a
	// server that wedged and was restarted into the same file; the tail
	// read deliberately does not look that far back.
	if err := os.WriteFile(path, []byte(WedgedMarker+"\n"+bulk), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := Wedged(path); err != nil || got {
		t.Fatalf("Wedged() = %v, %v, want false for a marker beyond the tail", got, err)
	}
}
