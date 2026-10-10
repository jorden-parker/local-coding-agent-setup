package cli

import (
	"fmt"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/spf13/cobra"
)

func unsyncCmd() *cobra.Command {
	var harness string
	var dryRun bool
	c := &cobra.Command{
		Use:   "unsync",
		Short: "Take lca's provider entry back out of a harness's own configuration",
		Long: `Undo what lca sync wrote, leaving the rest of the harness's configuration
alone. It removes only what it can still recognise as its own and names
anything it decided to keep, so run it with --dry-run first if in doubt.

Qwen Code (~/.qwen/settings.json):
  - the modelProviders.openai[] entry for ALIAS, while every field lca owns
    still matches and nothing has been added to it;
  - env.OPENAI_API_KEY, only while it is still the "local" placeholder;
  - security.auth.selectedType and model.name, only while they are still
    exactly what sync would have written.

pi (~/.pi/agent/models.json):
  - the model whose id is ALIAS, and the llama-local provider once that leaves
    it with no models.

What it cannot undo: the key order and formatting that the first sync replaced
when it rewrote the document — restore the backup it took for that; a selection
you have changed since, or one you had chosen yourself before lca ran, which is
indistinguishable from lca's own write; an entry you have edited, which is kept
and reported; and the lean profile, which lca qwen-profile restore reverts.

A changed ALIAS also matters: unsync looks for the alias config.env names now,
so change it back first if you synced under another one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := app.LoadConfig()
			if err != nil {
				return err
			}
			results, err := app.UnsyncAll(f, harness, dryRun)
			if err != nil {
				return err
			}
			if len(results) == 0 {
				fmt.Println("No harness configuration is managed by lca.")
				return nil
			}
			prefix := ""
			if dryRun {
				prefix = "would be "
			}
			for _, u := range results {
				printUnsynced(u, prefix)
			}
			return nil
		},
	}
	c.Flags().StringVar(&harness, "harness", "", "Undo only this harness: qwen or pi")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would change without writing")
	return c
}

func printUnsynced(u app.Unsynced, prefix string) {
	path := paths.Tildify(u.Path)
	switch {
	case u.Deleted:
		fmt.Printf("%s: %sremoved; lca had created it and nothing else was in it\n", path, prefix)
	case !u.Changed:
		fmt.Printf("%s: nothing of lca's left to remove\n", path)
	default:
		fmt.Printf("%s: %supdated\n", path, prefix)
	}
	for _, r := range u.Removed {
		fmt.Printf("  - %s %sremoved\n", r, prefix)
	}
	for _, l := range u.Left {
		fmt.Printf("  ! %s: kept, it has been edited since lca wrote it\n", l)
	}
	if u.Backup != "" {
		fmt.Printf("  the file as it was before lca first changed it: %s\n", paths.Tildify(u.Backup))
	}
	if u.LeanApplied {
		fmt.Printf("  the lean profile is still applied; revert it with: lca qwen-profile restore\n")
	}
}
