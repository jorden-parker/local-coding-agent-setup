# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`setup.sh` is the single install-and-update command: it fast-forwards this checkout (re-executing itself when commits arrive), installs or upgrades llama.cpp, hf, Qwen Code and Go with Homebrew, downloads a Qwen GGUF model to `~/models` unless config.env already points at one that exists, seeds `~/.config/llama-coder/config.env`, installs the two static launchers from `launchers/` to `~/.local/bin` (`local-harness` plus `qwen-local` and `pi-local` symlinks to it), builds the Go tool `lca` (`cmd/lca`, `internal/`) there too, and runs `lca sync` to write the harness provider entries, then `lca doctor` (a doctor failure makes setup.sh exit non-zero). There is no separate updater; re-running setup.sh is the update. `lca` edits config.env, syncs the harness, and summarises response times (CLI subcommands and a Charm TUI). `README.md` documents it. `.claude/` holds the check hook and the `/sync-readme` skill. macOS on Apple Silicon only.

Two agent harnesses are supported: Qwen Code (installed by setup.sh) and pi (https://pi.dev, **never** installed by setup.sh — no Homebrew formula, it self-updates with `pi update`, and the work machine has only Qwen Code). Nothing may fail or even report a check when pi is absent unless `HARNESS=pi`.

## Never run setup.sh or the installed launchers

setup.sh runs `git pull --ff-only` on this checkout, downloads ~22 GB, runs `brew install`/`brew upgrade`, and overwrites `~/.local/bin/{llama-coder,local-harness,qwen-local,pi-local,lca}` without asking. `lca config set`, `lca config edit`, `lca sync`, and the TUI's Config tab write `config.env` and `~/.qwen/settings.json` for the real user when run against the real HOME. Verify changes statically only:

```bash
bash -n setup.sh && shellcheck setup.sh launchers/llama-coder launchers/local-harness .claude/hooks/check.sh
gofmt -l . && go vet ./... && go test ./...
```

All must pass clean before you are done. A hook runs the relevant checks after every edit to a `.sh`, `launchers/*`, or `.go` file. `go build`, `go test`, and `go vet` are safe. For manual checks of `lca` or the launchers, set `HOME`, `XDG_CONFIG_HOME`, and `XDG_STATE_HOME` to a temp dir first, build the binary before changing `HOME` (a `go run` under a temp HOME downloads a module cache there), and put stub `llama-server`/`llama-coder`/`qwen`/`pi`/`curl` scripts on PATH (the launcher starts `llama-coder` itself; see the stubs in `internal/cli/launchers_test.go`).

To check a Homebrew package, use `brew info <formula>` (read-only). Never `brew install` from here.

## Launchers and config.env

`launchers/llama-coder` and `launchers/local-harness` are plain bash, installed verbatim with `install -m 755`; `qwen-local` and `pi-local` are `ln -sfn` symlinks to the installed `local-harness`. Nothing is baked in at setup time; both scripts `source` config.env at every start.

- `local-harness` picks the harness from the name it was invoked under (`${0##*/}`: `qwen-local` or `pi-local`), then `HARNESS=` in the environment, then config.env's `HARNESS`, then `qwen`; anything else is an error. Everything up to and including port selection is harness-agnostic; `prepare_qwen`/`prepare_pi` and `exec_qwen`/`exec_pi` hold the rest, dispatched by an explicit `case` (shellcheck's SC2329 flags `"prepare_$harness"` as an uninvoked function). `prepare_pi` requires `pi` on PATH and otherwise exits with the `pi.dev/install.sh` hint before any sync. The harness runs as a foreground child so the EXIT trap still stops a server this session started.

- `CTX=` and `PORT=` in the environment win over the file for that run (the launchers save them before sourcing). `local-harness` manages its own server: without `PORT=` it claims the first port from config.env's `PORT` upward not held by another live session (claim = `$XDG_STATE_HOME/llama-coder/instances/<port>/owner.pid`, created by hard-linking a temp file so concurrent launches cannot both win), then adopts a server answering `/health` (waiting on 503), starts `PORT=<port> llama-coder` in the background when nothing listens (`server.pid`, `launcher.log`), and its EXIT trap stops only a server it started or adopted from a dead claim. `PORT=` pins the port and shares it if another live session holds it (`sharers/<pid>`). The harness runs as a foreground child, not `exec`, so the trap runs; `&` children would have SIGINT ignored. A port that differs from config.env's own value is an isolated instance: `qwen-local` exports `QWEN_HOME` to a `qwen-<port>` sibling directory and `lca sync --harness qwen --port` writes that port's settings there (`internal/paths.QwenInstanceDir`/`QwenSettingsFor`, `internal/app.SyncQwenAt`), so it never races the default port's shared `qwen/settings.json`. `lca doctor` and `lca config` only look at the default (config.env) port; `lca stats` reads every `qwen-*/usage` dir (`internal/paths.QwenUsageDirs`) and every `pi-*/sessions` dir (`internal/paths.PiSessionDirs`).
- `pi-local` exports `PI_CODING_AGENT_DIR` to `~/.config/llama-coder/pi` (or `pi-<port>`) and runs `pi --provider llama-local --model "$ALIAS"`. That provider id is `internal/pi.ProviderID` and the two must stay equal. The managed agent directory deliberately isolates the user's `~/.pi/agent` (credentials, model choice, sessions); only skills are shared, through `settings.json`'s `skills: ["~/.pi/agent/skills"]`. An inherited `PI_CODING_AGENT_SESSION_DIR` is unset so `lca stats` can still find the sessions. Do not switch to pi's own llama.cpp integration (`LLAMA_BASE_URL`, `/llama`): it needs llama-server in router mode, which `llama-coder` does not start.
- `qwen-local` symlinks `$QWEN_HOME/ide` (in every `qwen`/`qwen-<port>` directory) to `~/.qwen/ide`, or to `ide/` under a `QWEN_HOME` inherited from the environment. Qwen Code reads the VS Code companion's `<port>.lock` (port plus bearer token) from `$QWEN_HOME/ide`, while the extension writes it to the global Qwen dir; without the link the CLI connects tokenless and the extension answers 401 Unauthorized. Keep the link when changing `QWEN_HOME` handling.
- The launcher always calls `lca sync --harness <harness> --port "$PORT"`, so in `syncCmd` both `cmd.Flags().Changed("port")` and `Changed("harness")` are always true; detect a real port override by comparing that port against config.env's on-disk `PORT` (read before any `f.Set`), not by the flag being set. Without `--harness`, `lca sync` prepares Qwen Code and, when `app.SyncsPi` says so, pi — at the active port either way. `internal/cli/launchers_test.go` and `sync_test.go` assert literal `qwen`/`qwen-<port>` and `pi`/`pi-<port>` paths, and the launcher tests run through a symlink named `qwen-local`/`pi-local` so the `$0` dispatch is exercised — update them if the naming scheme changes.
- setup.sh writes config.env only when absent and never overwrites it. The memory-profile defaults (`MEM_GB < 24` → Qwen3.5-9B, `CTX=65536`; otherwise Qwen3.6-35B-A3B, `CTX=131072`) only seed that first file; memory detection must stay above the defaults block. Moving to a new machine with an existing config.env keeps the old values, and setup.sh prints the diff.
- `qwen-local` selects Qwen Code's OpenAI-compatible provider via `OPENAI_API_KEY` (any value), `OPENAI_BASE_URL` (must end in `/v1`), and `OPENAI_MODEL` (must equal `ALIAS`). Do not reintroduce `ANTHROPIC_*` or `CLAUDE_CODE_*` vars.
- macOS `/bin/bash` is 3.2: no `mapfile`, no `declare -A`, and empty arrays need `${arr[@]+"${arr[@]}"}` under `set -u`.
- The key list, ranges, and which keys sync to a harness live in `internal/config/catalog.go`. Adding a key means: catalogue entry, the config.env heredoc in setup.sh, the launcher flag, the README key table. `HARNESS` is a `KindEnum` key with `Choices`; it has a `Default` so older config.env files without it still validate.

## Keep README.md in sync with setup.sh

Model repo/file/alias, default `CTX` and `PORT`, model file size, both memory profiles and the threshold that selects them, the config.env defaults and key table, the `llama-server` flags in `launchers/llama-coder`, the install list (Go, the launchers and their symlinks, and that pi is *not* installed), the update steps (pull, package upgrade, model check, doctor), the `lca` command list, the Qwen settings and pi `models.json` keys `lca sync` writes, and the reasons for them appear in both files. Change both. Run `/sync-readme` to check.

## Conventions

- Comments on non-obvious flags cite the source (llama.cpp issue, Unsloth guide). Keep that when adding flags.
- Overridable settings use `${VAR:-default}` at the top of setup.sh and are documented in the header comment and README.
- Go: one package per harness (`internal/qwen`, `internal/pi`), both writing `internal/usage.Record` values that `internal/stats.FromUsage` buckets; `internal/app` chooses between them from `HARNESS` and `app.Source`. gofmt and `go vet` clean; Charm v2 imports (`charm.land/bubbletea/v2`, `bubbles/v2`, `huh/v2`, `lipgloss/v2`); every user-file write goes through `internal/atomicfile`; everything under `internal/` so nothing is importable; paths resolve through `internal/paths` at call time so tests can `t.Setenv`. `internal/app` is the layer shared by `cli` and `tui`; do not import `cli` from `tui`.
- Tests that touch files set `HOME`, `XDG_CONFIG_HOME`, and `XDG_STATE_HOME` to `t.TempDir()`.
