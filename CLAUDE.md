# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Two files. `setup.sh` installs llama.cpp and Qwen Code with Homebrew, downloads a Qwen GGUF model to `~/models`, and writes two launchers to `~/.local/bin` (`llama-coder`, `qwen-local`). `README.md` documents it. `.claude/` holds the shellcheck hook and the `/sync-readme` skill. macOS on Apple Silicon only.

## Never run setup.sh or the launchers

setup.sh downloads ~22 GB, runs `brew install`/`brew upgrade`, and overwrites `~/.local/bin/llama-coder` and `~/.local/bin/qwen-local` without asking. Verify changes statically only:

```bash
bash -n setup.sh && shellcheck setup.sh
```

Both must pass clean before you are done. A hook runs these after every edit to a `.sh` file.

To check a Homebrew package, use `brew info <formula>` (read-only). Never `brew install` from here.

## Launcher heredocs (setup.sh step 5)

The two `cat > ... <<LAUNCHER` heredocs are unquoted, so `$` expands at setup time unless escaped:

- Baked in at setup time: `${MODEL_PATH}`, `${ALIAS}`, and the defaults inside `\${CTX:-${CTX}}` / `\${PORT:-${PORT}}`.
- `qwen-local` selects Qwen Code's OpenAI-compatible provider via `OPENAI_API_KEY` (any value), `OPENAI_BASE_URL` (must end in `/v1`), and `OPENAI_MODEL` (must equal `ALIAS`). Do not reintroduce `ANTHROPIC_*` or `CLAUDE_CODE_*` vars.
- Resolved when the launcher runs: `\${CTX...}`, `\${PORT...}`, `\$@`. Keep the backslashes.
- Line continuations inside the heredoc must be written `\\`.

Consequences: changing `MODEL_REPO`, `MODEL_FILE`, or `ALIAS` needs a re-run of setup.sh. `CTX` and `PORT` can be overridden per launcher run. Both launchers must be started with the same `PORT`.

## Keep README.md in sync with setup.sh

Model repo/file/alias, default `CTX` and `PORT`, model file size, the `llama-server` flags, the harness and its install method, and the reasons for them appear in both files. Change both. Run `/sync-readme` to check.

## Conventions

- Comments on non-obvious flags cite the source (llama.cpp issue, Unsloth guide). Keep that when adding flags.
- Overridable settings use `${VAR:-default}` at the top of setup.sh and are documented in the header comment and README.
