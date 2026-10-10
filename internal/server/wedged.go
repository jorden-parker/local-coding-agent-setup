package server

import (
	"bytes"
	"io"
	"os"
)

// WedgedMarker is ggml's own wording when a Metal command buffer has failed,
// usually after running out of wired GPU memory: "backend is in error state
// from a previous command buffer failure - recreate the backend to recover".
// There is no recovering in process, and every later request answers "Compute
// error". launchers/local-harness greps for the same string; keep the two in
// step, and in step with llama.cpp.
const WedgedMarker = "backend is in error state"

// tailBytes bounds the read. The marker repeats for every request after the
// first failure, so the end of the log is enough however long the server ran.
const tailBytes = 64 << 10

// Wedged reports whether the llama-server that wrote path has hit an
// unrecoverable backend error. It is the only way to tell: a wedged server
// keeps answering /health with {"status":"ok"}, keeps reporting an idle
// healthy slot on /slots, and has no error counter in /metrics.
//
// A missing log is not an error — it only means no server has been started
// under that port's claim, or one was started by hand with llama-coder.
func Wedged(path string) (bool, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	at := int64(0)
	if fi.Size() > tailBytes {
		at = fi.Size() - tailBytes
	}
	b, err := io.ReadAll(io.NewSectionReader(f, at, fi.Size()-at))
	if err != nil {
		return false, err
	}
	return bytes.Contains(b, []byte(WedgedMarker)), nil
}
