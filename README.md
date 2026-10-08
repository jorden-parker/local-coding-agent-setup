# Local coding agent setup (Apple Silicon)

One-shot setup for running a local coding model with llama.cpp and Claude Code on a Mac.
Target machine: MacBook Pro, M4 Pro, 48 GB.

## Run

```bash
git clone https://github.com/jorden-parker/local-coding-agent-setup.git
cd local-coding-agent-setup
./setup.sh
```

Then in one terminal `llama-coder`, in another `claude-local` inside your project.

## What it installs

| Item | Source |
|---|---|
| llama.cpp | Homebrew formula `llama.cpp` |
| hf (HuggingFace CLI) | Homebrew formula `hf` |
| Claude Code | Homebrew cask `claude-code` (skipped if already installed) |
| Model | `unsloth/Qwen3.6-35B-A3B-GGUF`, file `Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf` (22.4 GB) into `~/models` |

## Why this model

Qwen3.6-35B-A3B is a mixture-of-experts model: 35B parameters total, 3B used per token.
On an M4 Pro it should run roughly 3x faster than the dense 27B models, which is what matters
for agent loops. Thinking is turned off in the launcher for the same reason.

Model card scores (Qwen's own numbers): SWE-bench Verified 73.4, Terminal-Bench 2.0 51.5.

## Swap to the stronger, slower model

```bash
MODEL_REPO=unsloth/Qwen3.8-27B-GGUF \
MODEL_FILE=Qwen3.8-27B-UD-Q4_K_XL.gguf \
ALIAS=qwen3.8-27b ./setup.sh
```

Qwen3.8-27B is dense and scores higher (SWE-bench Pro 61.7 vs 49.5) but generates
several times slower on Apple Silicon.

## Mac-specific choices in the launcher

- No `--cache-type-k/v` (quantised KV cache). Not optimised for Metal per the llama.cpp
  maintainer in [issue #23011](https://github.com/ggml-org/llama.cpp/issues/23011).
- No MTP speculative decoding. Reported slower than baseline on an M4 Pro 48 GB in the same issue.
- `CLAUDE_CODE_ATTRIBUTION_HEADER=0` so the server can reuse its KV cache across turns
  ([Unsloth guide](https://unsloth.ai/docs/basics/claude-code)).

## Tuning

- Memory tight? `CTX=65536 llama-coder`
- Measure speed: `llama-bench -m ~/models/Qwen3.6-35B-A3B-GGUF/Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf -ngl 99 -p 512 -n 128`
