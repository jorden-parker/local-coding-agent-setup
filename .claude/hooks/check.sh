#!/usr/bin/env bash
# PostToolUse hook: static checks for the file just written or edited.
#   *.sh and launchers/*  -> bash -n + shellcheck
#   *.go                  -> gofmt -l must be empty, go vet ./...
# Exit 2 makes Claude Code show the output as feedback.
set -uo pipefail
f=$(jq -r '.tool_input.file_path // .tool_response.filePath // empty')
[[ -n "$f" ]] || exit 0
root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
case "$f" in
  *.sh|*/launchers/*)
    bash -n "$f" && shellcheck "$f" || exit 2
    ;;
  *.go)
    out=$(gofmt -l "$f")
    if [[ -n "$out" ]]; then echo "gofmt: $out needs formatting"; exit 2; fi
    (cd "$root" && go vet ./...) || exit 2
    ;;
esac
exit 0
