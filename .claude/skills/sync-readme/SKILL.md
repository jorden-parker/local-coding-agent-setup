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
6. **Env vars** exported in `launchers/local-harness` vs those README mentions, per harness (`QWEN_HOME`/`OPENAI_*` for Qwen Code, `PI_CODING_AGENT_DIR` for pi), together with the harness names it dispatches on and the flags it rejects for each.
7. **Install list** (brew formulas in setup.sh, plus the binaries setup.sh installs or builds) vs the README install table.
8. **config.env defaults** in the setup.sh heredoc (every key and value) vs the README key table and the "Configure without re-running setup.sh" section; also the key list, ranges, and `Syncs` flags in `internal/config/catalog.go` vs the README key table and restart rules.
9. **lca commands** (`internal/cli`) vs every `lca ...` invocation README shows; each must exist with those flags.
10. **Qwen settings keys** written by `internal/qwen/settings.go` (`id`, `name`, `baseUrl`, `envKey`, `generationConfig.contextWindowSize`) vs the README description of what `lca sync` writes.
11. **pi configuration keys** written by `internal/pi/models.go` (the `providers.llama-local` fields `name`, `baseUrl`, `api`, `apiKey`, and the model's `id`, `name`, `input`, `contextWindow`, `reasoning`, `cost`, plus `settings.json`'s `defaultProvider`, `defaultModel`, `skills`) vs the README "pi's configuration" section, and the managed directory names in `internal/paths` (`pi`, `pi-<port>`) vs the README paths.

Output a short table: item, setup.sh value, README value, match or mismatch. Then list the fixes needed, one line each. Do not edit files unless the user asks.
