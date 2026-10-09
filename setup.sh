#!/usr/bin/env bash
# Local coding agent setup for Apple Silicon Macs.
# Tested targets: MacBook Pro M4 Pro 48 GB and MacBook Air M1 16 GB.
# Installs llama.cpp + Qwen Code + Go (builds lca), downloads a Qwen GGUF model, seeds
# ~/.config/llama-coder/config.env, and installs three binaries to ~/.local/bin:
#   llama-coder   -> starts the local model server on http://localhost:8080
#   qwen-local    -> runs Qwen Code against that server
#   lca           -> edits config.env, syncs Qwen Code's provider entry, shows response times
#
# The model is picked from detected memory:
#   < 24 GB  -> Qwen3.5-9B UD-Q4_K_XL (6.0 GB), CTX=65536
#   >= 24 GB -> Qwen3.6-35B-A3B UD-Q4_K_XL (22.4 GB), CTX=131072
#
# Re-runnable. config.env is seeded once and never overwritten; after that, change
# settings with `lca config set KEY VALUE` (or `lca` for the UI), not by re-running.
# Env var overrides below only affect the first run (the seeded config.env), e.g.:
#   CTX=65536 ./setup.sh
#   MODEL_REPO=unsloth/Qwen3.8-27B-GGUF MODEL_FILE=Qwen3.8-27B-UD-Q4_K_XL.gguf ALIAS=qwen3.8-27b ./setup.sh
#   MODEL_REPO=unsloth/Qwen3.5-9B-GGUF MODEL_FILE=Qwen3.5-9B-UD-Q4_K_XL.gguf ALIAS=qwen3.5-9b ./setup.sh   # small model on a big machine
set -euo pipefail

log() { printf '\n==> %s\n' "$*"; }

# --- 0. sanity -------------------------------------------------------------
[[ "$(uname -s)" == "Darwin" ]] || { echo "This script is for macOS."; exit 1; }
[[ "$(uname -m)" == "arm64" ]]  || { echo "This script needs an Apple Silicon Mac."; exit 1; }
MEM_GB=$(( $(sysctl -n hw.memsize) / 1024 / 1024 / 1024 ))
log "Detected $(sysctl -n machdep.cpu.brand_string), ${MEM_GB} GB unified memory"

# --- defaults, chosen by memory profile ----------------------------------------
if (( MEM_GB < 24 )); then
  # 16 GB profile (MacBook Air M1). macOS caps Metal's wired GPU memory at ~10.7 GB on a
  # 16 GB machine, so the 22 GB MoE file cannot load (even its 12.3 GB Q2 quant is too big).
  # Qwen3.5-9B UD-Q4_K_XL (6.0 GB) + 64k f16 KV cache (~2 GB) fits.
  MODEL_REPO="${MODEL_REPO:-unsloth/Qwen3.5-9B-GGUF}"
  MODEL_FILE="${MODEL_FILE:-Qwen3.5-9B-UD-Q4_K_XL.gguf}"
  ALIAS="${ALIAS:-qwen3.5-9b}"
  CTX="${CTX:-65536}"           # context window in tokens; 131072 needs ~4 GB KV, right at the Metal limit
  PROFILE="16 GB (Qwen3.5-9B)"
else
  MODEL_REPO="${MODEL_REPO:-unsloth/Qwen3.6-35B-A3B-GGUF}"
  MODEL_FILE="${MODEL_FILE:-Qwen3.6-35B-A3B-UD-Q4_K_XL.gguf}"
  ALIAS="${ALIAS:-qwen3.6-35b-a3b}"
  CTX="${CTX:-131072}"          # context window in tokens; lower to 65536 if memory is tight
  PROFILE="32 GB+ (Qwen3.6-35B-A3B)"
fi
PORT="${PORT:-8080}"
CACHE_RAM="${CACHE_RAM:-8192}"
CTX_CHECKPOINTS="${CTX_CHECKPOINTS:-32}"
MODELS_DIR="${MODELS_DIR:-$HOME/models}"
BIN_DIR="$HOME/.local/bin"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/llama-coder"
CONFIG="$CONFIG_DIR/config.env"
QWEN_SETTINGS="$HOME/.qwen/settings.json"
log "Memory profile: ${PROFILE}"
if (( MEM_GB < 32 )) && [[ "$MODEL_FILE" == Qwen3.6-35B-A3B-* ]]; then
  echo "Warning: ${MODEL_FILE} needs ~23 GB plus context. ${MEM_GB} GB is tight."
fi

# --- 1. Homebrew -----------------------------------------------------------
if ! command -v brew >/dev/null 2>&1; then
  log "Installing Homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  eval "$(/opt/homebrew/bin/brew shellenv)"
fi

# --- 2. llama.cpp + HuggingFace CLI + Go --------------------------------------
log "Installing llama.cpp and hf (HuggingFace CLI)"
brew install llama.cpp hf
brew upgrade llama.cpp hf 2>/dev/null || true   # Qwen3.x needs a recent build
if ! command -v go >/dev/null 2>&1; then
  log "Installing Go (builds the lca tool)"
  brew install go
fi

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

# --- 5. config.env (seeded once, never overwritten) ---------------------------
mkdir -p "$BIN_DIR" "$CONFIG_DIR"
WANT_CONFIG=$(cat <<CFG
# llama-coder configuration. Created by setup.sh.
# Edit with \`lca config\` (validated) or by hand. CTX= and PORT= in the environment override this file.
MODEL_PATH=${MODEL_PATH}
ALIAS=${ALIAS}
CTX=${CTX}
PORT=${PORT}
# Sampling: Unsloth's non-thinking recommendations for Qwen3.5/3.6 (identical values).
TEMP=0.7
TOP_P=0.8
TOP_K=20
MIN_P=0.0
PRESENCE_PENALTY=1.5
# true enables reasoning via --chat-template-kwargs enable_thinking (slower agent turns).
THINKING=false
# Host-memory cache/checkpoint limits (shared with Metal); tune with lca config.
CACHE_RAM=${CACHE_RAM}
CTX_CHECKPOINTS=${CTX_CHECKPOINTS}
# Extra llama-server flags, whitespace-separated, no quoting (e.g. --jinja).
EXTRA_ARGS=
CFG
)
if [[ -f "$CONFIG" ]]; then
  if ! diff -u "$CONFIG" <(printf '%s\n' "$WANT_CONFIG") >/dev/null; then
    log "Keeping existing ${CONFIG}; defaults differ:"
    diff -u "$CONFIG" <(printf '%s\n' "$WANT_CONFIG") || true
  fi
else
  log "Writing ${CONFIG}"
  printf '%s\n' "$WANT_CONFIG" > "$CONFIG"
fi
# Use the user's actual settings from here on.
# shellcheck source=/dev/null
source "$CONFIG"

# --- 6. Launchers + lca ---------------------------------------------------------
log "Installing launchers to ${BIN_DIR}"
install -m 755 "$SCRIPT_DIR/launchers/llama-coder" "$BIN_DIR/llama-coder"
install -m 755 "$SCRIPT_DIR/launchers/qwen-local" "$BIN_DIR/qwen-local"

log "Building ${BIN_DIR}/lca"
LCA_VERSION=$(git -C "$SCRIPT_DIR" describe --tags --always 2>/dev/null || echo dev)
(cd "$SCRIPT_DIR" && go build -trimpath -ldflags "-s -w -X main.version=${LCA_VERSION}" -o "$BIN_DIR/lca" ./cmd/lca)

# --- 7. Qwen Code provider entry ----------------------------------------------
# Qwen sizes its context from modelProviders.openai[].generationConfig.contextWindowSize;
# without it, compaction triggers early. lca sync merges one entry keyed by ALIAS.
log "Syncing ${QWEN_SETTINGS} (provider ${ALIAS}, contextWindowSize ${CTX})"
"$BIN_DIR/lca" sync

# --- 8. PATH hint ------------------------------------------------------------
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

Profile: ${MEM_GB} GB detected (${PROFILE})
Model:   ${MODEL_PATH}
Server:  http://127.0.0.1:${PORT}   (context ${CTX} tokens)
Config:  ${CONFIG}
         change with: lca config set CTX 65536   (or just: lca)
         one-off:     CTX=65536 llama-coder
Qwen:    ${QWEN_SETTINGS} now has provider ${ALIAS} with contextWindowSize ${CTX}
Timing:  server logs in ${XDG_STATE_HOME:-$HOME/.local/state}/llama-coder; review with: lca stats
Check:                    lca doctor
Benchmark:                llama-bench -m "${MODEL_PATH}" -ngl 99 -p 512 -n 128
MSG
