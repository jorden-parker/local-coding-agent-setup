---
name: sync-readme
description: Cross-check README.md against setup.sh for drift (model names, defaults, flags, file size, rationale). Use after editing either file, or when asked whether the docs match the script.
---

Compare `setup.sh` (plus `launchers/llama-coder`, `launchers/local-harness`, and `internal/config/catalog.go`) against `README.md` and report every mismatch. Read the files in full first. Do not run setup.sh or the launchers.

Check each of these, citing the line in each file:

1. **Model defaults** in setup.sh (`MODEL_REPO`, `MODEL_FILE`, `ALIAS`) vs the README install table and the `llama-bench` path under Tuning.
2. **Alternative model** in the setup.sh header comment vs the README "Swap" section (repo, file, alias).
3. **Other defaults** (`CTX`, `PORT`, `MODELS_DIR`) vs every place README mentions a context size, port, or `~/models`.
4. **Model file size** stated in README vs the setup.sh memory warning. If the model MCP tools are available, confirm the actual size of `MODEL_FILE` in `MODEL_REPO` on Hugging Face and report it.
5. **llama-server flags** in `launchers/llama-coder` vs the README "Mac-specific choices" list, the "Response times" section (`--log-file`, `--log-timestamps`, `--metrics`), and the key table's flag column. Every flag the README explains must exist in the launcher, and every non-default flag in the launcher should be explained somewhere.
6. **Env vars** in `launchers/local-harness` vs those README mentions, per harness: the `OPENAI_*` trio it exports for Qwen Code, and the variables it deliberately *leaves alone* (`QWEN_HOME`, `PI_CODING_AGENT_DIR`) or clears (`QWEN_CODE_ENABLE_WORKFLOWS`, `QWEN_CODE_DISABLE_WORKFLOWS`, `QWEN_RUNTIME_DIR`, `PI_CODING_AGENT_SESSION_DIR`). Also the harness names it dispatches on, the flags it rejects for each, and that its two install hints match `qwenInstallHint`/`piInstallHint` in `internal/app` and the README.
7. **Install list** (brew formulas in setup.sh, plus the binaries setup.sh installs or builds) vs the README install table. Neither harness may appear as installed; both must be listed as the user's own, and the harness configuration files must be described as *patched*, not created.
8. **config.env defaults** in the setup.sh heredoc (every key and value) vs the README key table and the "Configure without re-running setup.sh" section; also the key list, ranges, and `Syncs` flags in `internal/config/catalog.go` vs the README key table and restart rules.
9. **lca commands** (`internal/cli`) vs every `lca ...` invocation README shows; each must exist with those flags.
10. **Qwen settings keys** written by `internal/qwen/settings.go` and `sync.go` (the provider entry's `id`, `name`, `baseUrl`, `envKey`, `generationConfig.contextWindowSize`, plus `env.<EnvKey>` and the `security.auth.selectedType`/`model.name` selection) vs the README description of what `lca sync` writes — including the conditions on the last two, since the README promises they are written only when nothing has been chosen yet. Confirm the lean leaves in `internal/qwen/profile.go` are *not* among them.
11. **pi configuration keys** written by `internal/pi/models.go` (the `providers.llama-local` fields `name`, `baseUrl`, `api`, `apiKey`, and the model's `id`, `name`, `input`, `contextWindow`, `reasoning`, `cost`) vs the README "pi's configuration" section. Nothing may write pi's `settings.json`. Check the file paths `internal/paths` resolves (`~/.qwen/settings.json`, `~/.pi/agent/models.json`, and the `QWEN_HOME`/`PI_CODING_AGENT_DIR` overrides) against every path the README names.
12. **Backup and unsync**: the filename `internal/backup.Name` builds vs the one setup.sh's banner and the README quote, and the removals `qwen.Unprepare`/`pi.UnprepareModels` actually perform vs the README's list of what `lca unsync` can and cannot undo.
13. **Lean profile**: that `lca sync` writes none of `internal/qwen/profile.go`'s `leanSettings`, that setup.sh applies lean only on a fresh config.env, and that `lca doctor` reports it as a warning at most — against what the README's "Lean Qwen Code profile" section claims.

Output a short table: item, setup.sh value, README value, match or mismatch. Then list the fixes needed, one line each. Do not edit files unless the user asks.
