package cli

import (
	"fmt"

	"github.com/jorden-parker/local-coding-agent-setup/internal/backup"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/qwen"
	"github.com/spf13/cobra"
)

func qwenProfileCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "qwen-profile",
		Short: "Apply or restore the lean Qwen Code profile",
		Long: `Turn Qwen Code's background memory work, automatic skill review, workflows
and the agent subagent tool off, and defer every optional tool schema behind
tool search. A local model pays for all of those twice: in extra requests to a
single-slot llama-server, and in prompt tokens it has few of.

There is one Qwen Code configuration, so this applies to qwen-local and plain
qwen alike. lca sync never writes these settings — only this command does, and
only the settings it changed are saved, so restore puts them back.`,
	}
	for _, action := range []string{"lean", "restore"} {
		short := "Disable background memory work and subagents; defer optional tool schemas"
		if action == "restore" {
			short = "Restore only the settings saved when lean was applied"
		}
		c.AddCommand(&cobra.Command{
			Use: action, Short: short, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				settings := paths.QwenSettings()
				snapshot := qwen.SnapshotFor(settings)
				operation := qwen.ApplyLean
				if cmd.Name() == "restore" {
					operation = qwen.RestoreLean
				}
				// The snapshot is a targeted undo of these settings alone;
				// the backup is the whole file as it was before lca first
				// touched it, which applying a profile also counts as.
				if backedUp, err := backup.Once(settings); err != nil {
					return err
				} else if backedUp != "" {
					report(settings, backedUp)
				}
				changed, err := operation(settings, snapshot)
				if err != nil {
					return fmt.Errorf("%s: %w", paths.Tildify(settings), err)
				}
				if !changed {
					fmt.Println("Already up to date.")
					return nil
				}
				what := "lean profile applied"
				if cmd.Name() == "restore" {
					what = "original profile settings restored"
				}
				fmt.Printf("%s: %s. Restart qwen to apply.\n", paths.Tildify(settings), what)
				return nil
			},
		})
	}
	return c
}
