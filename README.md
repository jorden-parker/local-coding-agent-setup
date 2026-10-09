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

`./setup.sh` is the only command, for the first install and for every update after it: re-run it
and it fast-forwards this checkout, upgrades the Homebrew packages, rebuilds `lca`, reinstalls
the launchers, re-syncs Qwen Code's settings and runs `lca doctor`. config.env and downloaded
models are kept (see [Update](#update)).

Then `qwen-local` inside your project. It takes the first free port from config.env's `PORT`
upward, starts `llama-coder` there itself when nothing is listening, waits for the model to load,
and stops that server when it exits. To watch the
server in its own terminal, run `llama-coder` first; `qwen-local` then uses it and leaves it
running. `lca` (no arguments) opens a small terminal UI for settings and response times;
`lca doctor` checks the install.

## What it installs

| Item | Source |
|---|---|
| Homebrew | official install script (skipped if already installed) |
| llama.cpp | Homebrew formula `llama.cpp` (upgraded on every run; Qwen3.x needs a recent build) |
| hf (HuggingFace CLI) | Homebrew formula `hf` (upgraded on every run) |
| Qwen Code | Homebrew formula `qwen-code` (upgraded on every run; left alone if `qwen` was installed another way) |
| Go | Homebrew formula `go` (upgraded on every run; left alone if `go` was installed another way); builds `lca` |
| `~/.local/bin/llama-coder` | launcher for llama-server, copied from `launchers/llama-coder` |
| `~/.local/bin/qwen-local` | launcher for Qwen Code, copied from `launchers/qwen-local` |
| `~/.local/bin/lca` | Go tool built from `cmd/lca`: config editor, Qwen sync, response-time stats |
| `~/.config/llama-coder/config.env` | model, context, port and sampling settings; written once, never overwritten |
| `~/.config/llama-coder/qwen/settings.json` | dedicated local model/provider configuration with automatic lean defaults |
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
  `MODEL_FILE=Qwen3.5-9B-UD-Q6_K_XL.gguf ./setup.sh`. On an existing install that only downloads
  the file, because config.env is kept; setup.sh then prints the switch, which is
  `lca config set MODEL_PATH ~/models/Qwen3.5-9B-GGUF/Qwen3.5-9B-UD-Q6_K_XL.gguf`.
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
several times slower on Apple Silicon. It does not fit a 16 GB machine. On an existing install
setup.sh only downloads the model, because config.env is kept, and prints the two lines that
switch to it: `lca config set MODEL_PATH ~/models/Qwen3.8-27B-GGUF/Qwen3.8-27B-UD-Q4_K_XL.gguf`
and `lca config set ALIAS qwen3.8-27b`.

## Configure without re-running setup.sh

The launchers read `~/.config/llama-coder/config.env` (or `$XDG_CONFIG_HOME/llama-coder/config.env`)
every time they start. setup.sh seeds it once from the memory profile and never touches it again, so
`CTX=65536 ./setup.sh` only matters on the first run. After that:

```bash
lca config show                      # every key and its value
lca config set CTX 65536             # validated; ALIAS, PORT and CTX also prepare the local Qwen settings
lca config set EXTRA_ARGS --jinja    # extra llama-server flags
lca config edit                      # $VISUAL or $EDITOR (else vi), then validate + sync
lca                                  # the same as a form in the terminal UI (tab 2)
CTX=32768 llama-coder                # one-off override; PORT works the same way for both launchers
```

| Key | llama-server flag | Default | Notes |
|---|---|---|---|
| `MODEL_PATH` | `-m` | from the memory profile | absolute path to an existing `.gguf` |
| `ALIAS` | `--alias` | `qwen3.5-9b` / `qwen3.6-35b-a3b` | also the Qwen Code provider id and `OPENAI_MODEL` |
| `CTX` | `-c` | `65536` / `131072` | 2048 to 1048576, multiple of 256; mirrored to Qwen's `contextWindowSize` |
| `PORT` | `--port` | `8080` | 1024 to 65535; first port `qwen-local` tries, `PORT=` in the environment pins it for both launchers |
| `TEMP` | `--temp` | `0.7` | 0 to 2; Unsloth non-thinking recommendation for Qwen3.5/3.6 |
| `TOP_P` | `--top-p` | `0.8` | 0 to 1 |
| `TOP_K` | `--top-k` | `20` | 0 to 1000, 0 disables |
| `MIN_P` | `--min-p` | `0.0` | 0 to 1 |
| `PRESENCE_PENALTY` | `--presence-penalty` | `1.5` | -2 to 2; Unsloth recommends 1.5 for Qwen3.x non-thinking |
| `THINKING` | `--chat-template-kwargs` | `false` | `true` enables reasoning; slower agent turns |
| `CACHE_RAM` | `--cache-ram` | `8192` | host prompt-cache limit in MiB, 0 disables it; range 0–1048576 |
| `CTX_CHECKPOINTS` | `--ctx-checkpoints` | `32` | recurrent/window state checkpoints per slot; range 0–1024 |
| `EXTRA_ARGS` | appended | empty | whitespace-separated, no quoting; `lca` rejects flags the keys above own and the launcher's fixed `-fa`, `-ngl`, `-np`, `--host`, `--log-file`, `--log-timestamps`, `--metrics` |

Restart rules: every key needs a restart of `llama-coder`. `ALIAS`, `PORT` and `CTX` also need a
restart of `qwen-local`, because Qwen Code reads `modelProviders` at startup. `lca config set`
prints the hint. setup.sh, `lca sync`, and every `qwen-local` launch on config.env's own port
prepare `${XDG_CONFIG_HOME:-~/.config}/llama-coder/qwen/settings.json`; a launch on any other port
prepares `qwen-<port>/settings.json` next to it and points `QWEN_HOME` there. The file's
provider entry is under `modelProviders.openai[]` with `id` = `ALIAS`, `name` =
`<ALIAS> (local llama.cpp)`, `baseUrl` =
`http://127.0.0.1:PORT/v1`, `envKey` = `OPENAI_API_KEY` and `generationConfig.contextWindowSize`
= `CTX`. Preparation also selects the local model, OpenAI authentication, and lean defaults,
preserving unrelated settings, and adds `~/.qwen/skills` to `skills.directories` (Qwen Code
0.21+) so `qwen-local` offers the same user skills as ordinary `qwen`, including anything
linked there from `~/.agents/skills`; existing entries in that list are kept, and the lean
profile for ordinary `qwen` never touches it. `qwen-local` sets `QWEN_HOME` to this dedicated directory and
passes explicit model, auth, base URL, and API key flags, so saved choices from ordinary `qwen`
cannot select another model. Model/auth/endpoint flags passed to `qwen-local` are rejected;
change `ALIAS` or `PORT` through `lca config set`. `lca doctor` reports provider or lean-setting
drift for the configured port. If an older config has cache/checkpoint flags in `EXTRA_ARGS`,
move their values into `CACHE_RAM` / `CTX_CHECKPOINTS` and remove those flags from `EXTRA_ARGS`.
`lca config set` rejects every owned or fixed flag; the launcher itself only checks the
cache/checkpoint duplicates, including `-cram`, `-ctxcp`, `--swa-checkpoints`, and `--flag=value`
forms, for configs edited by hand.

### Running more than one instance

Every `qwen-local` session gets its own `llama-server`. Without `PORT=` in the environment it
takes the first port from config.env's `PORT` upward that no other running `qwen-local` holds, so
a second session in another project lands on `8081`, a third on `8082`, and so on (a port where
something other than `llama-server` answers is skipped too). On that port it uses a server that is
already listening (waiting while one is still loading the model), or starts `llama-coder` in the
background and stops it again when Qwen Code exits. A server you started yourself with
`llama-coder` is used as is and never stopped. Each session records its claim in
`${XDG_STATE_HOME:-~/.local/state}/llama-coder/instances/<port>/` (`owner.pid`, `server.pid` for a
server it started, `launcher.log` with that server's console output, `sharers/<pid>` for sessions
sharing a pinned port); claims whose owner has died are cleared, and a server left behind by a
crashed session is adopted and stopped by the next session on that port. `CTX=` passes through to
a server started this way and to that instance's Qwen `contextWindowSize`;
`QWEN_LOCAL_START_TIMEOUT` (seconds, default 600) bounds the wait for the model to load.

`PORT=9000 qwen-local` pins the port instead: same rules, except that a port another running
`qwen-local` holds is shared. The owner then leaves the server running when it exits, and it keeps
running after the sharer exits until the next session on that port adopts and stops it. A
one-off `PORT=9000 llama-coder` still works for starting a server by hand. Because `9000` differs
from config.env's own `PORT`, that session's Qwen settings land in
`.../llama-coder/qwen-9000/settings.json` rather than the shared `qwen/settings.json` the default
port uses, so `lca sync` never races or overwrites the default instance's provider entry. Every
`llama-server` loads a full copy of the model and serves a single request slot (`-np 1`), so RAM
bounds how many sessions you can run. `lca doctor` only covers the port configured in config.env;
`lca stats` reads the usage logs of every instance.

## Response times

Two sources are captured, and `lca stats` shows both as one table per source (p50, p95, mean,
token counts per day and model) plus a sparkline of the daily p50. `lca stats --raw` lists every
request, `--json` is for scripts, `--source qwen|server|both` (default `both`), `--days N` and
`--model ALIAS` filter.
The terminal UI (`lca`, tab 1) shows the same table and, while the server runs, a live line from
its `/metrics` endpoint every two seconds (`lca metrics` prints one scrape).

- **Qwen Code** writes one JSON line per API call to
  `~/.config/llama-coder/qwen/usage/token-usage-YYYY-MM.jsonl` for `qwen-local`
  (under `XDG_CONFIG_HOME` when set), or `qwen-<port>/usage/` for a session on another port.
  Stats read every instance's directory, plus legacy `~/.qwen/usage` logs, and
  deduplicate records by ID, keeping historical calls visible. Each record includes
  `apiDurationMs`, input/output tokens, model and caller (`main` or a subagent). This is the
  end-to-end time an agent turn waits for. It is on unless `privacy.usageStatisticsEnabled` is false
  in the managed Qwen settings; `lca doctor` warns if it is.
- **llama-server** logs `prompt eval time`, `eval time` and `total time` with tokens per second
  for every request. `llama-coder` passes `--log-file --log-timestamps --metrics`, writing one
  log per launch to `~/.local/state/llama-coder/server-<UTC time>-<ALIAS>.log`. The timestamps are
  relative to process start, so the file name supplies the wall clock. On each launch (and on every
  `lca stats`) the previous logs are folded into `timings.jsonl` in the same directory and logs
  older than 14 days are deleted, except the newest.

Qwen's output tokens divided by API duration includes prompt processing and queue time; it is
not a measurement of generation speed. Use server timings to separate those costs.

## Lean Qwen Code profile

`qwen-local` automatically uses lean defaults in its dedicated configuration. Run `qwen-local`
inside your project; it starts the server if needed, and no model picker or profile command is needed.
Ordinary `qwen` retains its own models, credentials, and settings. Local sessions and runtime
files use the dedicated Qwen directory; existing ordinary Qwen sessions are not migrated.
Project instructions, permissions, and deliberate project tool settings retain Qwen’s normal
precedence. Inherited workflow enable/disable variables and `QWEN_RUNTIME_DIR` are cleared by `qwen-local`.

For **ordinary `qwen` only**, the optional reversible profile commands remain available:

```bash
lca qwen-profile lean
# Restart ordinary qwen to apply. qwen-local is already lean.
lca qwen-profile restore
```

Lean disables automatic memory extraction, dreaming, automatic skill review, workflows, and
the `agent` subagent tool. File reading, searching, editing and shell tools remain eager; other
tools remain discoverable through `tool_search` and `tool_call`. Manual memory commands remain
available. This reduces background requests and avoids switching a single server slot between
agent prompts. Local preparation reapplies lean defaults on each launch. The reversible commands above
apply only to ordinary Qwen settings.

Only affected settings are saved in `~/.qwen/settings.json.lca-lean.json`, before the settings
file is changed. Existing disabled tools and unrelated settings are preserved. Reapplying lean
is idempotent. Restore merges only the saved settings, preserving provider and unrelated edits;
it refuses to overwrite edits made to profile settings while lean was active and keeps the
snapshot so the conflict can be resolved. Compare JSON values in the snapshot's `applied`
fields with the named conflicting settings, restore those applied values, then retry restore.
Do not edit the snapshot. A snapshot is retained if settings writing fails, and retry recovers.

Settings were verified against Qwen Code 0.25.0's installed tool names and its
[settings reference](https://qwenlm.github.io/qwen-code-docs/en/users/configuration/settings/).
On the disposable M1 coding fixture, lean reduced median session time from 343 to 182 seconds
(47%), with all six sessions passing shell, discovery and correctness checks. This is a small-task
measurement; M4 and long-task results remain pending. See the [measured report](docs/performance/2026-10-08.md).

## Update

```bash
./setup.sh
```

The same command updates an existing install. Each run, in order:

1. Fast-forwards this checkout with `git pull --ff-only`. Skipped when the checkout has local
   changes or no upstream branch; a failed pull (offline, diverged) is a warning. When new
   commits arrive, setup.sh restarts itself so the rest of the run uses them.
2. Installs Homebrew if missing, then installs any of `llama.cpp`, `hf`, `go`, `qwen-code` whose
   command is absent and upgrades the ones Homebrew already manages. A failed upgrade is a
   warning. A `go` or `qwen` installed some other way is left alone.
3. Checks the model: if config.env points at a file that exists, nothing is downloaded, so
   updates work offline. If config.env is missing or points at the memory profile's default
   path, the default model is downloaded (skipped when present). If config.env points at a
   custom model that is missing, setup.sh stops and says how to download one.
   `MODEL_REPO`/`MODEL_FILE`/`ALIAS` always download that model (see Swap above).
4. Seeds config.env if absent, otherwise keeps it and prints how it differs from the defaults.
5. Reinstalls both launchers and rebuilds `lca` from the checkout.
6. Runs `lca sync`, then `lca doctor`; setup.sh exits non-zero if doctor reports a failure.

A running `llama-server` is never stopped; restart `llama-coder` or `qwen-local` to pick up a
new llama.cpp build or launcher.

## Measure before selecting performance defaults

```bash
python3 tools/benchmark.py \
  --model "$HOME/models/Qwen3.5-9B-GGUF/Qwen3.5-9B-UD-Q4_K_XL.gguf" \
  --profile m1 --output /tmp/lca-bench-m1
```

For the M4 Pro, use its Qwen3.6-35B-A3B model path and `--profile m4`. The output directory must
not exist. Run outside a sandbox that blocks Metal; the runner verifies full GPU offload. It
launches one isolated localhost server at a time on a free port and stops only its own child
processes. Installed launchers and settings are untouched. Allow approximately 15 minutes for
the M1 matrix with the default 2048-token fixture; larger `--prompt-tokens` take longer.

The matrix compares baseline cache/checkpoints (8192 MiB / 32), 0 MiB / 8, 0 MiB / 32,
1024 MiB / 8 (a bounded-cache experiment that retains caching), and
physical batch sizes 256 and 1024 against baseline 512. The M4 adds 2048 MiB host cache with
unified KV. Logical batch size is 2048 throughout. Each variant repeats cold, growing,
subagent-like branch, and return-to-main requests three times, then verifies a structured
`read_file` tool call without executing tools. Requests use temperature 0 and seed 42 to reduce
sampling noise; production sampling stays unchanged. Use `--variants baseline bounded-8` for
a subset and `--prompt-tokens 8192` or `16384` for longer contexts.

`requests.jsonl` records time to first token, total duration, prompt and generation timings,
and cached tokens. `summary.json` includes medians, exact arguments, model/runtime information,
RSS, memory pressure snapshots, swap growth, and the tool-call check. Compare a candidate only
against a baseline from the same run and machine. Adopt a changed default only for at least
10% lower median request duration, successful tool calls, and no greater swap growth. Also
check the per-scenario medians for cache regressions; combining improvements requires another
run. An 8 GiB cache limit is a ceiling, not an immediate 8 GiB allocation.

The installed build 11429 uses `--checkpoint-min-step` (8192 tokens by default); older advice
using `--checkpoint-every-n-tokens` does not apply. Disabling the host prompt cache does not
disable active-slot prefix reuse, but can affect returning to an earlier branch. Flag meanings
are from the [matching llama.cpp server reference](https://github.com/ggml-org/llama.cpp/blob/d81235049/tools/server/README.md).

Measured results: [local performance report](docs/performance/2026-10-08.md).

For a normal-versus-lean coding comparison, build `lca`, start an isolated server on a different
port (for example 8081), and run:

```bash
go build -o /tmp/lca-benchmark ./cmd/lca
python3 tools/benchmark_qwen.py --base-url http://127.0.0.1:8081 \
  --alias qwen3.5-9b --ctx 65536 --lca /tmp/lca-benchmark \
  --output /tmp/lca-qwen-bench
```

The runner alternates normal/lean sessions three times, using temporary HOME/XDG directories
and disposable coding projects. Each session fixes an addition function, creates three tests,
and runs them. File edits are auto-approved in those fixtures; only the specific unittest shell
command is explicitly allowed. A fixture-only core-tool allowlist registers shell execution
in Qwen’s headless mode; normal user permissions and core-tool settings are untouched.
Model calls use the supplied local OpenAI-compatible endpoint. Logs, usage sources, resulting
fixtures, recorded shell/discovery calls, and independent behaviour/test checks are retained
in the output directory. It never changes the user's Qwen settings. A fresh short headless session
may not trigger background memory extraction, so report observed sources rather than assuming
every normal session incurs that work. Allow up to 15 minutes per session on the M1; the runner
stops a session at that limit. Do not run two model servers concurrently on the 16 GB Mac.

## Mac-specific choices in the launcher

The server flags live in `launchers/llama-coder`, installed verbatim to `~/.local/bin/llama-coder`.

- `-ngl 99` offloads every layer to the GPU (unified memory on Apple Silicon, so there is no
  reason to keep layers on the CPU) and `-fa on` enables flash attention, which llama.cpp supports
  on Metal.
- `-np 1`: one request slot, so the whole context budget and KV cache serve the single Qwen Code
  session; `qwen-local` starts a separate server per session instead of sharing slots.
- `--host 127.0.0.1`: loopback only, so the unauthenticated server is never reachable from the
  network.
- No `--cache-type-k/v` (quantised KV cache). Not optimised for Metal per the llama.cpp
  maintainer in [issue #23011](https://github.com/ggml-org/llama.cpp/issues/23011).
- No MTP speculative decoding. Reported slower than baseline on an M4 Pro 48 GB in the same issue.

## Tuning

- On 32 GB+ with memory pressure: `lca config set CTX 65536` (already the default on 16 GB), or
  `CTX=65536 llama-coder` for one run
- Compaction triggers early? Qwen Code sizes its context from
  `modelProviders.openai[].generationConfig.contextWindowSize` in the managed
  `~/.config/llama-coder/qwen/settings.json`.
  setup.sh and `lca config set CTX` keep it equal to `CTX`; `lca doctor` reports drift, `lca sync` fixes it.
- Tool calls failing? `lca config set EXTRA_ARGS --jinja`. Recent llama.cpp builds enable it by
  default; harness-bench passed it explicitly.
- Previously used `claude-local`? setup.sh no longer writes it; delete `~/.local/bin/claude-local` by hand.
- Measure speed: `llama-bench -m ~/models/<repo-name>/<file>.gguf -ngl 99 -p 512 -n 128`, where
  `<repo-name>` is the Hugging Face repo without its owner, e.g. `Qwen3.6-35B-A3B-GGUF`
  (setup.sh prints the exact path at the end)
