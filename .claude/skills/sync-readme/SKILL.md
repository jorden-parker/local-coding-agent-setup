---
name: sync-readme
description: Cross-check README.md against setup.sh for drift (model names, defaults, flags, file size, rationale). Use after editing either file, or when asked whether the docs match the script.
---

Compare `setup.sh` and `README.md` and report every mismatch. Read both files in full first. Do not run setup.sh.

Check each of these, citing the line in each file:

1. **Model defaults** in setup.sh (`MODEL_REPO`, `MODEL_FILE`, `ALIAS`) vs the README install table and the `llama-bench` path under Tuning.
2. **Alternative model** in the setup.sh header comment vs the README "Swap" section (repo, file, alias).
3. **Other defaults** (`CTX`, `PORT`, `MODELS_DIR`) vs every place README mentions a context size, port, or `~/models`.
4. **Model file size** stated in README vs the setup.sh memory warning. If the model MCP tools are available, confirm the actual size of `MODEL_FILE` in `MODEL_REPO` on Hugging Face and report it.
5. **llama-server flags** in the `llama-coder` heredoc vs the README "Mac-specific choices" list. Every flag the README explains must exist in the heredoc, and every non-default flag in the heredoc should be explained somewhere.
6. **Env vars** exported in the `qwen-local` heredoc vs those README mentions.
7. **Install list** (brew formulas and casks in setup.sh) vs the README install table.

Output a short table: item, setup.sh value, README value, match or mismatch. Then list the fixes needed, one line each. Do not edit files unless the user asks.
