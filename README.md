# Local coding agent setup (Apple Silicon)

One-shot setup for running a local coding model with llama.cpp and Qwen Code on a Mac.
Target machine: MacBook Pro, M4 Pro, 48 GB.

## Run

```bash
git clone https://github.com/jorden-parker/local-coding-agent-setup.git
cd local-coding-agent-setup
./setup.sh
```

Then in one terminal `llama-coder`, in another `qwen-local` inside your project.

## What it installs

| Item | Source |
|---|---|
| llama.cpp | Homebrew formula `llama.cpp` |
| hf (HuggingFace CLI) | Homebrew formula `hf` |
| Qwen Code | Homebrew formula `qwen-code` (skipped if already installed) |
| Model | `unsloth/Qwen3.6-35B-A3B-GGUF`, file `Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf` (22.4 GB) into `~/models` |

## Why this model

Qwen3.6-35B-A3B is a mixture-of-experts model: 35B parameters total, 3B used per token.
On an M4 Pro it should run roughly 3x faster than the dense 27B models, which is what matters
for agent loops. Thinking is turned off in the launcher for the same reason.

Model card scores (Qwen's own numbers): SWE-bench Verified 73.4, Terminal-Bench 2.0 51.5.

## Why Qwen Code

[harness-bench](https://neuralnoise.com/2026/harness-bench-wip/) is the only independent
comparison of coding harnesses on this exact model running under llama.cpp (16 SWE-style tasks,
M3 Max). Averaged over its 10 local models:

| Harness | Tasks solved | Avg time per task |
|---|---|---|
| Pi | 76.9% | 191 s |
| Qwen Code | 75.0% | 191 s |
| Claude Code | 66.2% | 306 s |
| OpenCode | 63.8% | - |
| Aider | 62.5% | - |

For Qwen3.6-35B-A3B at the quant this script downloads (UD-Q4_K_XL), Qwen Code solved 15/16 tasks
at about 108 s per task. Claude Code only reached 15/16 with the Q8_0 quant, at 244 s per task.

The practical reason: Qwen Code is Alibaba's own harness for Qwen models. It talks to llama-server's
native OpenAI `/v1` endpoint and needs none of the Claude-specific workarounds (attribution header,
`--bare`, trimmed system prompt) that keep the server's KV cache from being reused.

Pi scored slightly higher overall and is the alternative if you don't mind a curl/npm install
instead of Homebrew.

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

## Tuning

- Memory tight? `CTX=65536 llama-coder`
- Compaction triggers early? Qwen Code sizes its context from its own
  `modelProviders.openai[].generationConfig.contextWindowSize` in `~/.qwen/settings.json`.
  Set it to match `CTX`. The script does not write that file.
- Tool calls failing? Add `--jinja` to the `llama-server` line in `~/.local/bin/llama-coder`. Recent
  llama.cpp builds enable it by default; harness-bench passed it explicitly.
- Previously used `claude-local`? setup.sh no longer writes it; delete `~/.local/bin/claude-local` by hand.
- Measure speed: `llama-bench -m ~/models/Qwen3.6-35B-A3B-GGUF/Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf -ngl 99 -p 512 -n 128`
