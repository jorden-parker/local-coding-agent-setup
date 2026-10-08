# Local coding agent setup (Apple Silicon)

One-shot setup for running a local coding model with llama.cpp and Qwen Code on a Mac.
Tested machines: MacBook Pro M4 Pro 48 GB and MacBook Air M1 16 GB. setup.sh picks the model
from detected memory: under 24 GB it downloads Qwen3.5-9B, otherwise Qwen3.6-35B-A3B.

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
| Model (32 GB+) | `unsloth/Qwen3.6-35B-A3B-GGUF`, file `Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf` (22.4 GB) into `~/models` |
| Model (16 GB) | `unsloth/Qwen3.5-9B-GGUF`, file `Qwen3.5-9B-UD-Q4_K_XL.gguf` (6.0 GB) into `~/models` |

## Why this model

Qwen3.6-35B-A3B is a mixture-of-experts model: 35B parameters total, 3B used per token.
On an M4 Pro it should run roughly 3x faster than the dense 27B models, which is what matters
for agent loops. Thinking is turned off in the launcher for the same reason.

Model card scores (Qwen's own numbers): SWE-bench Verified 73.4, Terminal-Bench 2.0 51.5.

## 16 GB machines (MacBook Air M1)

The 35B file does not fit. macOS caps Metal's wired GPU memory at roughly 10.7 GB on a 16 GB
machine, so the 22.4 GB UD-Q4_K_XL file cannot load, and even the 12.3 GB UD-Q2_K_XL quant is
over the limit before any KV cache. Qwen3.6 and Qwen3.8 ship nothing smaller than 27B / 35B-A3B.

setup.sh therefore selects `Qwen3.5-9B-UD-Q4_K_XL.gguf` (6.0 GB) when it detects under 24 GB:

- Qwen3.5 is the newest Qwen line with a 9B dense model, and it uses the same Unsloth
  UD-Q4_K_XL quant family as the Pro config.
- Qwen3.5 small models (0.8B to 9B) ship with thinking off by default, so the launcher's
  `enable_thinking:false` stays correct.
- Qwen3.5's non-thinking sampling recommendation (temp 0.7, top_p 0.8, top_k 20, min_p 0.0,
  presence_penalty 1.5) is identical to the Qwen3.6 values in the launcher, so the launcher
  is unchanged.
- Default `CTX=65536`. Qwen3.5-9B is hybrid attention (8 of 32 layers full attention, 4 KV heads
  x 256 dim), so the f16 KV cache is about 32 KB per token: roughly 2 GB at 64k and 4 GB at 128k.
  6 GB model + 4 GB KV sits right at the Metal limit, hence the smaller default.
  Unsloth's hardware table for the 9B: 4-bit about 6.5 GB, 6-bit about 9 GB, 8-bit about 13 GB total.

Model card scores (Qwen's own numbers): LiveCodeBench v6 65.6, BFCL-V4 66.1, TAU2-Bench 79.1.
No SWE-bench figure is published for the 9B, and harness-bench did not test any model under 20B,
so there is no comparable agent benchmark for this profile.

Options on this profile:

- Better quality, slower: swap to `Qwen3.5-9B-UD-Q6_K_XL.gguf` (8.8 GB) with
  `MODEL_FILE=Qwen3.5-9B-UD-Q6_K_XL.gguf ./setup.sh`.
- Try the full context: `CTX=131072 llama-coder`. Expect memory pressure.

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
several times slower on Apple Silicon. It does not fit a 16 GB machine.

## Mac-specific choices in the launcher

- No `--cache-type-k/v` (quantised KV cache). Not optimised for Metal per the llama.cpp
  maintainer in [issue #23011](https://github.com/ggml-org/llama.cpp/issues/23011).
- No MTP speculative decoding. Reported slower than baseline on an M4 Pro 48 GB in the same issue.

## Tuning

- On 32 GB+ with memory pressure: `CTX=65536 llama-coder` (already the default on 16 GB)
- Compaction triggers early? Qwen Code sizes its context from its own
  `modelProviders.openai[].generationConfig.contextWindowSize` in `~/.qwen/settings.json`.
  Set it to match `CTX`. The script does not write that file.
- Tool calls failing? Add `--jinja` to the `llama-server` line in `~/.local/bin/llama-coder`. Recent
  llama.cpp builds enable it by default; harness-bench passed it explicitly.
- Previously used `claude-local`? setup.sh no longer writes it; delete `~/.local/bin/claude-local` by hand.
- Measure speed: `llama-bench -m ~/models/<repo>/<file>.gguf -ngl 99 -p 512 -n 128` (setup.sh prints the exact path at the end)
