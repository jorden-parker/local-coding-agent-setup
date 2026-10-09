// Command lca configures the local llama.cpp coding setup and reports its
// response times. Run with no arguments for the TUI.
package main

import "github.com/jorden-parker/local-coding-agent-setup/internal/cli"

// version is set by setup.sh via -ldflags "-X main.version=...".
var version = "dev"

func main() { cli.Main("lca", version) }
