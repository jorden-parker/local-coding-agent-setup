#!/usr/bin/env bash
# Local coding agent setup for Apple Silicon Macs (tested target: MacBook Pro M4 Pro, 48 GB).
# Installs llama.cpp + Qwen Code, downloads Qwen3.6-35B-A3B (fast MoE), and creates
# two launchers:
#   llama-coder   -> starts the local model server on http://localhost:8080
#   qwen-local    -> runs Qwen Code against that server
#
# Re-runnable. Override defaults with env vars, e.g.:
#   CTX=65536 ./setup.sh
#   MODEL_REPO=unsloth/Qwen3.8-27B-GGUF MODEL_FILE=Qwen3.8-27B-UD-Q4_K_XL.gguf ALIAS=qwen3.8-27b ./setup.sh
set -euo pipefail

MODEL_REPO="${MODEL_REPO:-unsloth/Qwen3.6-35B-A3B-GGUF}"
MODEL_FILE="${MODEL_FILE:-Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf}"
ALIAS="${ALIAS:-qwen3.6-35b-a3b}"
CTX="${CTX:-131072}"          # context window in tokens; lower to 65536 if memory is tight
PORT="${PORT:-8080}"
MODELS_DIR="${MODELS_DIR:-$HOME/models}"
BIN_DIR="$HOME/.local/bin"

log() { printf '\n==> %s\n' "$*"; }

# --- 0. sanity -------------------------------------------------------------
[[ "$(uname -s)" == "Darwin" ]] || { echo "This script is for macOS."; exit 1; }
[[ "$(uname -m)" == "arm64" ]]  || { echo "This script needs an Apple Silicon Mac."; exit 1; }
MEM_GB=$(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
log "Detected $(sysctl -n machdep.cpu.brand_string), ${MEM_GB} GB unified memory"
if (( MEM_GB < 32 )); then
  echo "Warning: ${MODEL_FILE} needs ~23 GB plus context. ${MEM_GB} GB is tight."
fi

# --- 1. Homebrew -----------------------------------------------------------
if ! command -v brew >/dev/null 2>&1; then
  log "Installing Homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  eval "$(/opt/homebrew/bin/brew shellenv)"
fi

# --- 2. llama.cpp + HuggingFace CLI ------------------------------------------
log "Installing llama.cpp and hf (HuggingFace CLI)"
brew install llama.cpp hf
brew upgrade llama.cpp hf 2>/dev/null || true   # Qwen3.x needs a recent build

# --- 3. Qwen Code ------------------------------------------------------------
if ! command -v qwen >/dev/null 2>&1; then
  log "Installing Qwen Code"
  brew install qwen-code
fi

# --- 4. Model download -------------------------------------------------------
log "Downloading ${MODEL_REPO} / ${MODEL_FILE} to ${MODELS_DIR} (skips if present)"
mkdir -p "$MODELS_DIR"
hf download "$MODEL_REPO" "$MODEL_FILE" --local-dir "$MODELS_DIR/$(basename "$MODEL_REPO")"
MODEL_PATH="$MODELS_DIR/$(basename "$MODEL_REPO")/$MODEL_FILE"
[[ -f "$MODEL_PATH" ]] || { echo "Download failed: $MODEL_PATH not found"; exit 1; }

# --- 5. Launchers ------------------------------------------------------------
mkdir -p "$BIN_DIR"

log "Writing ${BIN_DIR}/llama-coder"
cat > "$BIN_DIR/llama-coder" <<LAUNCHER
#!/usr/bin/env bash
# Starts the local model server. Ctrl-C to stop.
# Notes for Apple Metal (from llama.cpp maintainers, issue #23011):
#   - no --cache-type-k/v: quantised KV cache is not optimised for Metal
#   - no MTP speculative decoding: slower than baseline on Metal for this model
# Thinking is disabled for fast agent turns. Sampling params are Unsloth's
# non-thinking recommendations for Qwen3.6.
exec llama-server \\
  -m "${MODEL_PATH}" \\
  --alias "${ALIAS}" \\
  -c "\${CTX:-${CTX}}" -fa on -ngl 99 -np 1 \\
  --temp 0.7 --top-p 0.8 --top-k 20 --min-p 0.0 --presence-penalty 1.5 \\
  --chat-template-kwargs '{"enable_thinking":false}' \\
  --host 127.0.0.1 --port "\${PORT:-${PORT}}" \\
  "\$@"
LAUNCHER
chmod +x "$BIN_DIR/llama-coder"

log "Writing ${BIN_DIR}/qwen-local"
cat > "$BIN_DIR/qwen-local" <<LAUNCHER
#!/usr/bin/env bash
# Runs Qwen Code against the local llama-server (OpenAI-compatible /v1 API).
# OPENAI_API_KEY must be set (any value) or Qwen Code will not select the
# OpenAI-compatible auth type (Qwen Code docs: users/configuration/auth.md).
PORT="\${PORT:-${PORT}}"
if ! curl -sf "http://127.0.0.1:\${PORT}/health" >/dev/null 2>&1; then
  echo "llama-server is not running on port \${PORT}. Start it with: llama-coder" >&2
  exit 1
fi
export OPENAI_BASE_URL="http://127.0.0.1:\${PORT}/v1"
export OPENAI_API_KEY="local"
export OPENAI_MODEL="${ALIAS}"
exec qwen "\$@"
LAUNCHER
chmod +x "$BIN_DIR/qwen-local"

# --- 6. PATH hint ------------------------------------------------------------
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    log "Add ${BIN_DIR} to your PATH"
    echo "  fish:  fish_add_path $BIN_DIR"
    echo "  zsh:   echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc"
    ;;
esac

log "Done"
cat <<MSG

Next steps:
  1. In one terminal:   llama-coder
  2. In another:        cd <your-project> && qwen-local

Model:   ${MODEL_PATH}
Server:  http://127.0.0.1:${PORT}   (context ${CTX} tokens)
Change context per run:   CTX=65536 llama-coder
Benchmark:                llama-bench -m "${MODEL_PATH}" -ngl 99 -p 512 -n 128
MSG
