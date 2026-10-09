# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`setup.sh` is the single install-and-update command: it fast-forwards this checkout (re-executing itself when commits arrive), installs or upgrades llama.cpp, hf, Qwen Code and Go with Homebrew, downloads a Qwen GGUF model to `~/models` unless config.env already points at one that exists, seeds `~/.config/llama-coder/config.env`, installs the two static launchers from `launchers/` to `~/.local/bin`, builds the Go tool `lca` (`cmd/lca`, `internal/`) there too, and runs `lca sync` to write Qwen Code's provider entry, then `lca doctor` (a doctor failure makes setup.sh exit non-zero). There is no separate updater; re-running setup.sh is the update. `lca` edits config.env, syncs Qwen, and summarises response times (CLI subcommands and a Charm TUI). `README.md` documents it. `.claude/` holds the check hook and the `/sync-readme` skill. macOS on Apple Silicon only.

## Never run setup.sh or the installed launchers

setup.sh runs `git pull --ff-only` on this checkout, downloads ~22 GB, runs `brew install`/`brew upgrade`, and overwrites `~/.local/bin/{llama-coder,qwen-local,lca}` without asking. `lca config set`, `lca config edit`, `lca sync`, and the TUI's Config tab write `config.env` and `~/.qwen/settings.json` for the real user when run against the real HOME. Verify changes statically only:

```bash
bash -n setup.sh && shellcheck setup.sh launchers/llama-coder launchers/qwen-local .claude/hooks/check.sh
gofmt -l . && go vet ./... && go test ./...
```

All must pass clean before you are done. A hook runs the relevant checks after every edit to a `.sh`, `launchers/*`, or `.go` file. `go build`, `go test`, and `go vet` are safe. For manual checks of `lca` or the launchers, set `HOME`, `XDG_CONFIG_HOME`, and `XDG_STATE_HOME` to a temp dir first, build the binary before changing `HOME` (a `go run` under a temp HOME downloads a module cache there), and put stub `llama-server`/`llama-coder`/`qwen`/`curl` scripts on PATH (`qwen-local` starts `llama-coder` itself; see the stubs in `internal/cli/launchers_test.go`).

To check a Homebrew package, use `brew info <formula>` (read-only). Never `brew install` from here.

## Launchers and config.env

`launchers/llama-coder` and `launchers/qwen-local` are plain bash, installed verbatim with `install -m 755`. Nothing is baked in at setup time; both `source` config.env at every start.

- `CTX=` and `PORT=` in the environment win over the file for that run (the launchers save them before sourcing). `qwen-local` manages its own server: without `PORT=` it claims the first port from config.env's `PORT` upward not held by another live `qwen-local` (claim = `$XDG_STATE_HOME/llama-coder/instances/<port>/owner.pid`, created by hard-linking a temp file so concurrent launches cannot both win), then adopts a server answering `/health` (waiting on 503), starts `PORT=<port> llama-coder` in the background when nothing listens (`server.pid`, `launcher.log`), and its EXIT trap stops only a server it started or adopted from a dead claim. `PORT=` pins the port and shares it if another live session holds it (`sharers/<pid>`). `qwen` runs as a foreground child, not `exec`, so the trap runs; `&` children would have SIGINT ignored. A port that differs from config.env's own value is an isolated instance: `qwen-local` exports `QWEN_HOME` to a `qwen-<port>` sibling directory and `lca sync --port` writes that port's settings there (`internal/paths.QwenInstanceDir`/`QwenSettingsFor`, `internal/app.SyncQwenAt`), so it never races the default port's shared `qwen/settings.json`. `lca doctor` and `lca config` only look at the default (config.env) port; `lca stats` reads every `qwen-*/usage` dir (`internal/paths.QwenUsageDirs`).
- `qwen-local` always calls `lca sync --port "$PORT"`, so in `syncCmd` `cmd.Flags().Changed("port")` is always true; detect a real override by comparing that port against config.env's on-disk `PORT` (read before any `f.Set`), not by the flag being set. `internal/cli/launchers_test.go` and `sync_test.go` assert literal `qwen`/`qwen-<port>` paths — update both if the naming scheme changes.
- setup.sh writes config.env only when absent and never overwrites it. The memory-profile defaults (`MEM_GB < 24` → Qwen3.5-9B, `CTX=65536`; otherwise Qwen3.6-35B-A3B, `CTX=131072`) only seed that first file; memory detection must stay above the defaults block. Moving to a new machine with an existing config.env keeps the old values, and setup.sh prints the diff.
- `qwen-local` selects Qwen Code's OpenAI-compatible provider via `OPENAI_API_KEY` (any value), `OPENAI_BASE_URL` (must end in `/v1`), and `OPENAI_MODEL` (must equal `ALIAS`). Do not reintroduce `ANTHROPIC_*` or `CLAUDE_CODE_*` vars.
- macOS `/bin/bash` is 3.2: no `mapfile`, no `declare -A`, and empty arrays need `${arr[@]+"${arr[@]}"}` under `set -u`.
- The key list, ranges, and which keys sync to Qwen live in `internal/config/catalog.go`. Adding a key means: catalogue entry, the config.env heredoc in setup.sh, the launcher flag, the README key table.

## Keep README.md in sync with setup.sh

Model repo/file/alias, default `CTX` and `PORT`, model file size, both memory profiles and the threshold that selects them, the config.env defaults and key table, the `llama-server` flags in `launchers/llama-coder`, the install list (now including Go and the three binaries), the update steps (pull, package upgrade, model check, doctor), the `lca` command list, the Qwen settings keys `lca sync` writes, and the reasons for them appear in both files. Change both. Run `/sync-readme` to check.

## Conventions

- Comments on non-obvious flags cite the source (llama.cpp issue, Unsloth guide). Keep that when adding flags.
- Overridable settings use `${VAR:-default}` at the top of setup.sh and are documented in the header comment and README.
- Go: gofmt and `go vet` clean; Charm v2 imports (`charm.land/bubbletea/v2`, `bubbles/v2`, `huh/v2`, `lipgloss/v2`); every user-file write goes through `internal/atomicfile`; everything under `internal/` so nothing is importable; paths resolve through `internal/paths` at call time so tests can `t.Setenv`. `internal/app` is the layer shared by `cli` and `tui`; do not import `cli` from `tui`.
- Tests that touch files set `HOME`, `XDG_CONFIG_HOME`, and `XDG_STATE_HOME` to `t.TempDir()`.
