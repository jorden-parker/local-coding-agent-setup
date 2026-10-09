package cli

import (
	"fmt"

	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
	"github.com/spf13/cobra"
)

func qwenProfileCmd() *cobra.Command {
	c := &cobra.Command{Use: "qwen-profile", Short: "Apply or restore lean for ordinary qwen (qwen-local is always lean)"}
	for _, action := range []string{"lean", "restore"} {
		short := "Disable background memory work and subagents; defer optional tool schemas"
		if action == "restore" {
			short = "Restore only the settings saved when lean was applied"
		}
		c.AddCommand(&cobra.Command{
			Use: action, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				settings := paths.LegacyQwenSettings()
				snapshot := settings + ".lca-lean.json"
				operation := qwen.ApplyLean
				if cmd.Name() == "restore" {
					operation = qwen.RestoreLean
				}
				changed, err := operation(settings, snapshot)
				if err != nil {
					return fmt.Errorf("%s: %w", paths.Tildify(settings), err)
				}
				if !changed {
					fmt.Println("Already up to date.")
				} else if cmd.Name() == "restore" {
					fmt.Printf("%s: original profile settings restored. Restart ordinary qwen to apply; qwen-local has its own lean settings.\n", paths.Tildify(settings))
				} else {
					fmt.Printf("%s: lean profile applied. Restart ordinary qwen to apply; qwen-local has its own lean settings.\n", paths.Tildify(settings))
				}
				return nil
			},
		})
	}
	return c
}
