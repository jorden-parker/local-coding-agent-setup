#!/usr/bin/env bash
# Update only lca and its launchers; no downloads, Homebrew, or server restart.
# Existing config.env is preserved. Missing config is seeded for a downloaded
# model; MODEL_PATH, ALIAS, CTX and PORT can override that initial seed.
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/llama-coder"
config="$config_dir/config.env"
bin_dir="$HOME/.local/bin"

[[ "$(uname -s)" == Darwin && "$(uname -m)" == arm64 ]] || { echo "Apple Silicon macOS required." >&2; exit 1; }
command -v go >/dev/null || { echo "Go is required; this updater does not install dependencies." >&2; exit 1; }
for dependency in python3 rg; do
  command -v "$dependency" >/dev/null || { echo "$dependency is required; this updater does not install dependencies." >&2; exit 1; }
done
if [[ ! -f "$config" ]]; then
  mem_gb=$(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
  if (( mem_gb < 24 )); then
    MODEL_PATH="${MODEL_PATH:-$HOME/models/Qwen3.5-9B-GGUF/Qwen3.5-9B-UD-Q4_K_XL.gguf}"
    ALIAS="${ALIAS:-qwen3.5-9b}"
    CTX="${CTX:-65536}"
  else
    MODEL_PATH="${MODEL_PATH:-$HOME/models/Qwen3.6-35B-A3B-GGUF/Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf}"
    ALIAS="${ALIAS:-qwen3.6-35b-a3b}"
    CTX="${CTX:-131072}"
  fi
  [[ -f "$MODEL_PATH" ]] || { echo "Model missing: $MODEL_PATH. Set MODEL_PATH, ALIAS and CTX to your downloaded model." >&2; exit 1; }
fi

build_dir=$(mktemp -d)
trap 'rm -rf "$build_dir"' EXIT
(cd "$root" && go build -trimpath -o "$build_dir/lca" ./cmd/lca)
mkdir -p "$config_dir" "$bin_dir"
if [[ ! -f "$config" ]]; then
  # Quote shell values safely, including paths with spaces.
  # The temporary seed is validated before it is copied into the user's config.
  python3 - "$build_dir/config.env" "$MODEL_PATH" "$ALIAS" "$CTX" "${PORT:-8080}" <<'PY'
import shlex
import sys
from pathlib import Path
values = dict(zip(("MODEL_PATH", "ALIAS", "CTX", "PORT"), sys.argv[2:]))
values.update(TEMP="0.7", TOP_P="0.8", TOP_K="20", MIN_P="0.0",
              PRESENCE_PENALTY="1.5", THINKING="false", CACHE_RAM="8192",
              CTX_CHECKPOINTS="32", EXTRA_ARGS="")
Path(sys.argv[1]).write_text("# llama-coder configuration. Created by install-tools.sh.\n" +
                           "".join(f"{key}={shlex.quote(value)}\n" for key, value in values.items()))
PY
  # Validate the seed using isolated XDG_CONFIG_HOME; do not touch Qwen settings.
  mkdir -p "$build_dir/llama-coder"
  mv "$build_dir/config.env" "$build_dir/llama-coder/config.env"
  XDG_CONFIG_HOME="$build_dir" "$build_dir/lca" config show > "$build_dir/validation.txt" 2>&1
  if rg '^error:' "$build_dir/validation.txt"; then
    echo "Initial config is invalid; no installed files changed." >&2
    exit 1
  fi
  install -m 600 "$build_dir/llama-coder/config.env" "$config"
fi
install -m 755 "$build_dir/lca" "$bin_dir/lca"
install -m 755 "$root/launchers/llama-coder" "$bin_dir/llama-coder"
install -m 755 "$root/launchers/qwen-local" "$bin_dir/qwen-local"
"$bin_dir/lca" sync
printf 'Updated tools. Start llama-coder, then qwen-local. qwen-local automatically uses its own lean profile\n'
