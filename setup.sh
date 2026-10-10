#!/usr/bin/env bash
# Local coding agent setup for Apple Silicon Macs: the one command for installing and updating.
# Tested targets: MacBook Pro M4 Pro 48 GB and MacBook Air M1 16 GB.
#
# Every run, in order:
#   1. fast-forwards this checkout (skipped when it has local changes or no upstream)
#   2. installs Homebrew if missing, then installs or upgrades llama.cpp, hf and Go
#   3. downloads the Qwen GGUF model for the detected memory profile unless config.env already
#      points at a model that exists
#   4. seeds ~/.config/llama-coder/config.env once (never overwritten)
#   5. installs these to ~/.local/bin:
#        llama-coder   -> starts the local model server on http://127.0.0.1:8080
#        local-harness -> runs an agent harness against that server, with two names:
#          qwen-local  -> Qwen Code (brew install qwen-code)
#          pi-local    -> pi (https://pi.dev)
#        lca           -> edits config.env, patches the harness configuration, shows response times
#   6. runs `lca sync`, which merges the local provider entry into the configuration each harness
#      already owns (~/.qwen/settings.json, ~/.pi/agent/models.json), copying each file aside as
#      <name>.lca-backup-<timestamp>.json before the first change; `lca unsync` reverses it.
#      On a first install only, also applies the lean Qwen Code profile. Then `lca doctor`.
#
# Neither harness is installed here: pi has no Homebrew formula and self-updates with
# `pi update`, and Qwen Code is yours to install so this script never replaces a harness, or a
# harness configuration, that it did not create. Install at least one before running an agent.
#
# The model is picked from detected memory:
#   < 24 GB  -> Qwen3.5-9B UD-Q4_K_XL (6.0 GB), CTX=65536
#   >= 24 GB -> Qwen3.6-35B-A3B UD-Q4_K_XL (22.4 GB), CTX=131072
#
# Re-run ./setup.sh to update. config.env is seeded once and never overwritten; after that,
# change settings with `lca config set KEY VALUE` (or `lca` for the UI), not by re-running.
# CTX, PORT, CACHE_RAM and CTX_CHECKPOINTS below only affect the first run (the seeded
# config.env). MODEL_REPO/MODEL_FILE/ALIAS download that model on any run and, on an existing
# install, print the `lca config set` lines that switch to it, e.g.:
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
MODEL_GIVEN="${MODEL_REPO:-}${MODEL_FILE:-}"   # non-empty: the caller chose a model explicitly

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
# lca patches the configuration Qwen Code already owns rather than keeping one.
QWEN_SETTINGS="${QWEN_HOME:-$HOME/.qwen}/settings.json"
log "Memory profile: ${PROFILE}"
if (( MEM_GB < 32 )) && [[ "$MODEL_FILE" == Qwen3.6-35B-A3B-* ]]; then
  echo "Warning: ${MODEL_FILE} needs ~22.4 GB plus context. ${MEM_GB} GB is tight."
fi

# --- 1. this checkout ------------------------------------------------------------
# Fast-forward only, never touching local changes. A changed checkout re-executes itself
# so the rest of this run uses the new launchers, Go sources and script.
if [[ -z "${LCA_SETUP_REEXEC:-}" ]] && git -C "$SCRIPT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  if [[ -n "$(git -C "$SCRIPT_DIR" status --porcelain --untracked-files=no)" ]]; then
    log "Checkout has local changes; not pulling"
  elif ! git -C "$SCRIPT_DIR" rev-parse --abbrev-ref '@{upstream}' >/dev/null 2>&1; then
    log "Checkout has no upstream branch; not pulling"
  else
    log "Updating checkout ${SCRIPT_DIR}"
    BEFORE=$(git -C "$SCRIPT_DIR" rev-parse HEAD)
    if GIT_TERMINAL_PROMPT=0 git -C "$SCRIPT_DIR" pull --ff-only; then
      if [[ "$(git -C "$SCRIPT_DIR" rev-parse HEAD)" != "$BEFORE" ]]; then
        log "Checkout updated; restarting setup.sh"
        LCA_SETUP_REEXEC=1 exec "$SCRIPT_DIR/setup.sh" "$@"
      fi
    else
      echo "Warning: git pull failed (offline or diverged); continuing with the current checkout"
    fi
  fi
fi

# --- 2. Homebrew packages -------------------------------------------------------
if ! command -v brew >/dev/null 2>&1; then
  log "Installing Homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  eval "$(/opt/homebrew/bin/brew shellenv)"
fi
# formula:command pairs. A formula Homebrew already manages is upgraded (Qwen3.x needs a
# recent llama.cpp build); a command found outside Homebrew is left alone; anything else
# is installed.
INSTALL=()
UPGRADE=()
BREW_INSTALLED=$(brew list --formula --versions llama.cpp hf go 2>/dev/null | cut -d' ' -f1 || true)
for pair in llama.cpp:llama-server hf:hf go:go; do
  formula="${pair%%:*}"
  binary="${pair##*:}"
  if grep -qx "$formula" <<<"$BREW_INSTALLED"; then
    UPGRADE+=("$formula")
  elif command -v "$binary" >/dev/null 2>&1; then
    echo "${binary} found outside Homebrew ($(command -v "$binary")); not upgrading it"
  else
    INSTALL+=("$formula")
  fi
done
if (( ${#INSTALL[@]} > 0 )); then
  log "Installing ${INSTALL[*]}"
  brew install "${INSTALL[@]}"
fi
if (( ${#UPGRADE[@]} > 0 )); then
  log "Upgrading ${UPGRADE[*]}"
  brew upgrade "${UPGRADE[@]}" || echo "Warning: brew upgrade failed; continuing with the installed versions"
fi

# --- 3. Model ----------------------------------------------------------------------
# Download target: the explicit MODEL_REPO/MODEL_FILE, else the model config.env already
# points at, else the memory profile's default.
DEFAULT_MODEL_PATH="$MODELS_DIR/$(basename "$MODEL_REPO")/$MODEL_FILE"
MODEL_PATH="$DEFAULT_MODEL_PATH"
if [[ -z "$MODEL_GIVEN" && -f "$CONFIG" ]]; then
  # shellcheck source=/dev/null
  CONFIGURED_MODEL=$(source "$CONFIG" >/dev/null 2>&1 || true; printf '%s' "${MODEL_PATH:-}")
  [[ -n "$CONFIGURED_MODEL" ]] && MODEL_PATH="$CONFIGURED_MODEL"
fi
if [[ -f "$MODEL_PATH" ]]; then
  log "Model present: ${MODEL_PATH}"
elif [[ "$MODEL_PATH" != "$DEFAULT_MODEL_PATH" ]]; then
  cat >&2 <<MSG
config.env points at ${MODEL_PATH}, which does not exist.
Put the file back, or download one with
  MODEL_REPO=<owner/repo> MODEL_FILE=<file.gguf> ALIAS=<alias> ./setup.sh
and switch to it with the lca config set lines it prints.
MSG
  exit 1
else
  log "Downloading ${MODEL_REPO} / ${MODEL_FILE} to ${MODELS_DIR}"
  mkdir -p "$MODELS_DIR"
  hf download "$MODEL_REPO" "$MODEL_FILE" --local-dir "$MODELS_DIR/$(basename "$MODEL_REPO")"
  [[ -f "$MODEL_PATH" ]] || { echo "Download failed: $MODEL_PATH not found"; exit 1; }
fi

# --- 4. config.env (seeded once, never overwritten) ---------------------------
mkdir -p "$BIN_DIR" "$CONFIG_DIR"
WANT_CONFIG=$(cat <<CFG
# llama-coder configuration. Created by setup.sh.
# Edit with \`lca config\` (validated) or by hand. CTX= and PORT= in the environment override this file.
# Agent harness lca syncs and reports on by default: qwen (Qwen Code) or pi.
# qwen-local and pi-local each pick their own from the name they are invoked under.
HARNESS=qwen
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
# A first run is also the only time the lean profile is applied, further down.
FRESH_CONFIG=0
if [[ -f "$CONFIG" ]]; then
  if ! diff -u "$CONFIG" <(printf '%s\n' "$WANT_CONFIG") >/dev/null; then
    log "Keeping existing ${CONFIG}; defaults differ:"
    diff -u "$CONFIG" <(printf '%s\n' "$WANT_CONFIG") || true
  fi
else
  log "Writing ${CONFIG}"
  printf '%s\n' "$WANT_CONFIG" > "$CONFIG"
  FRESH_CONFIG=1
fi
# Use the user's actual settings from here on; remember what this run downloaded.
WANT_MODEL_PATH="$MODEL_PATH"
WANT_ALIAS="$ALIAS"
# shellcheck source=/dev/null
source "$CONFIG"

# --- 5. Launchers + lca ---------------------------------------------------------
log "Installing launchers to ${BIN_DIR}"
install -m 755 "$SCRIPT_DIR/launchers/llama-coder" "$BIN_DIR/llama-coder"
install -m 755 "$SCRIPT_DIR/launchers/local-harness" "$BIN_DIR/local-harness"
# The name the launcher is invoked under picks the harness, so these are links,
# not copies. -n keeps an existing link from being followed into a directory.
ln -sfn local-harness "$BIN_DIR/qwen-local"
ln -sfn local-harness "$BIN_DIR/pi-local"

log "Building ${BIN_DIR}/lca"
LCA_VERSION=$(git -C "$SCRIPT_DIR" describe --tags --always 2>/dev/null || echo dev)
(cd "$SCRIPT_DIR" && go build -trimpath -ldflags "-s -w -X main.version=${LCA_VERSION}" -o "$BIN_DIR/lca" ./cmd/lca)

# --- 6. Harnesses, their configuration, then verify ----------------------------------------
# Neither harness is installed from here. pi has no Homebrew formula and updates itself
# with `pi update`; Qwen Code is left to the user so that lca never replaces a harness, or
# its configuration, that it did not install.
HARNESS_FOUND=0
if command -v qwen >/dev/null 2>&1; then
  HARNESS_FOUND=1
  log "Qwen Code $(qwen --version 2>/dev/null) found"
else
  log "Qwen Code not found: install it with 'brew install qwen-code', then re-run this script"
fi
if command -v pi >/dev/null 2>&1; then
  HARNESS_FOUND=1
  log "pi $(pi --version 2>/dev/null) found: run pi-local, or 'lca config set HARNESS pi' to make it lca's default"
else
  log "pi not found (optional): install it with 'curl -fsSL https://pi.dev/install.sh | sh'"
fi
if (( ! HARNESS_FOUND )); then
  echo "Warning: no agent harness is installed, so there is nothing for lca to configure yet." >&2
fi

# Qwen sizes its context from modelProviders.openai[].generationConfig.contextWindowSize;
# without it, compaction triggers early. lca sync merges only that provider entry into the
# harness's own configuration, after copying the file aside.
log "Patching the harness configuration (provider ${ALIAS}, context ${CTX})"
"$BIN_DIR/lca" sync

# Lean is a global Qwen Code setting, so lca sync never writes it: re-running this script
# must not undo a deliberate `lca qwen-profile restore`. A first install has made no such
# choice yet, and a local model is what lean exists for.
if (( FRESH_CONFIG )) && command -v qwen >/dev/null 2>&1; then
  log "Applying the lean Qwen Code profile (reversible: lca qwen-profile restore)"
  "$BIN_DIR/lca" qwen-profile lean || echo "Warning: could not apply the lean profile; continuing" >&2
fi

log "Checking the install (lca doctor)"
DOCTOR_OK=1
"$BIN_DIR/lca" doctor || DOCTOR_OK=0

# --- 7. PATH hint ------------------------------------------------------------
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    log "Add ${BIN_DIR} to your PATH"
    echo "  fish:  fish_add_path $BIN_DIR"
    echo "  zsh:   echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.zshrc"
    ;;
esac

if [[ -n "$MODEL_GIVEN" && "$WANT_MODEL_PATH" != "$MODEL_PATH" ]]; then
  log "Downloaded ${WANT_MODEL_PATH}; config.env still uses ${MODEL_PATH}. Switch with:"
  echo "  lca config set MODEL_PATH \"${WANT_MODEL_PATH}\""
  echo "  lca config set ALIAS ${WANT_ALIAS}"
fi

if (( DOCTOR_OK )); then
  log "Done"
else
  log "Done, but lca doctor reported failures (see above)"
fi
cat <<MSG

Next steps:
  cd <your-project> && qwen-local      (starts llama-server itself and stops it on exit)
  cd <your-project> && pi-local        (the same, running pi instead)
  or run llama-coder first to watch the server in its own terminal

Install a harness yourself; this script configures them but installs neither:
  Qwen Code:  brew install qwen-code
  pi:         curl -fsSL https://pi.dev/install.sh | sh   (then: pi update)

Profile: ${MEM_GB} GB detected (${PROFILE})
Model:   ${MODEL_PATH}
Server:  http://127.0.0.1:${PORT}   (context ${CTX} tokens)
Config:  ${CONFIG}
         change with: lca config set CTX 65536   (or just: lca)
         one-off:     CTX=65536 llama-coder
Qwen:    ${QWEN_SETTINGS} gained provider ${ALIAS} with contextWindowSize ${CTX}
pi:      pi-local adds the same provider to ${HOME}/.pi/agent/models.json
Patched: each file is copied to <name>.lca-backup-<timestamp>.json before the first change
         undo with: lca unsync           (--dry-run first to see what it would remove)
         lean profile: lca qwen-profile lean | restore
Timing:  server logs in ${XDG_STATE_HOME:-$HOME/.local/state}/llama-coder; review with: lca stats
Update:  re-run ./setup.sh (pulls this repo, upgrades packages, rebuilds lca, keeps config.env)
Check:                    lca doctor
Benchmark:                llama-bench -m "${MODEL_PATH}" -ngl 99 -p 512 -n 128
MSG
(( DOCTOR_OK ))
