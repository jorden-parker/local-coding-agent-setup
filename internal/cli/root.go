// Package cli defines the lca commands.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/jorden-parker/local-coding-agent-setup/internal/tui"
)

var name = "lca"

// Main runs the command under the given program name and exits non-zero on
// error.
func Main(program, version string) {
	name = program
	plain := func(w io.Writer, _ fang.Styles, err error) { fmt.Fprintf(w, "%s: %v\n", name, err) }
	if err := fang.Execute(context.Background(), Root(), fang.WithVersion(version), fang.WithErrorHandler(plain)); err != nil {
		os.Exit(1)
	}
}

// Root returns the root command.
func Root() *cobra.Command {
	root := &cobra.Command{
		Use:   name,
		Short: "Configure the local llama.cpp coding setup and review its response times",
		Long: name + ` edits ~/.config/llama-coder/config.env (the file the llama-coder,
qwen-local and pi-local launchers read), prepares a dedicated configuration for
the agent harness that config.env selects (Qwen Code or pi), and summarises
response times from that harness's records and llama-server's timing lines.

Run with no arguments to open the interactive UI.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tui.Run()
		},
	}
	root.AddCommand(configCmd(), syncCmd(), statsCmd(), compactCmd(), metricsCmd(), doctorCmd(), qwenProfileCmd())
	return root
}
