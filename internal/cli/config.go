package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
	"github.com/jorden-parker/local-coding-agent-setup/internal/config"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
)

func configCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "config",
		Short: "Show or change config.env (model, context, port, sampling)",
		RunE:  func(cmd *cobra.Command, args []string) error { return showConfig() },
	}
	c.AddCommand(
		&cobra.Command{Use: "show", Short: "Print every key with its value", RunE: func(cmd *cobra.Command, args []string) error { return showConfig() }},
		&cobra.Command{Use: "path", Short: "Print the config.env path", Run: func(cmd *cobra.Command, args []string) { fmt.Println(paths.Config()) }},
		&cobra.Command{
			Use: "get KEY", Short: "Print one value", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if _, ok := config.Lookup(args[0]); !ok {
					return fmt.Errorf("unknown key %s (known: %s)", args[0], strings.Join(config.Names(), ", "))
				}
				f, err := app.LoadConfig()
				if err != nil {
					return err
				}
				v, ok := f.Get(args[0])
				if !ok {
					k, _ := config.Lookup(args[0])
					v = k.Default
					if v == "" {
						return fmt.Errorf("%s is not set", args[0])
					}
				}
				fmt.Println(v)
				return nil
			},
		},
		&cobra.Command{
			Use: "set KEY VALUE", Short: "Validate and write one value; ALIAS, PORT and CTX also update the harness settings",
			// Values such as EXTRA_ARGS "--jinja" start with dashes, so flags are not parsed.
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
					return cmd.Help()
				}
				if len(args) != 2 {
					return fmt.Errorf("usage: %s config set KEY VALUE", name)
				}
				f, err := app.LoadConfig()
				if err != nil {
					return err
				}
				warn, hint, err := app.Set(f, args[0], args[1])
				if warn != "" {
					fmt.Fprintln(os.Stderr, "warning:", warn)
				}
				if err != nil {
					return err
				}
				fmt.Printf("%s=%s  →  %s\n%s\n", args[0], args[1], paths.Tildify(f.Path), hint)
				return nil
			},
		},
		&cobra.Command{
			Use: "edit", Short: "Open config.env in $VISUAL or $EDITOR, then validate",
			RunE: func(cmd *cobra.Command, args []string) error {
				f, err := app.LoadConfig()
				if err != nil {
					return err
				}
				editor := os.Getenv("VISUAL")
				if editor == "" {
					editor = os.Getenv("EDITOR")
				}
				if editor == "" {
					editor = "vi"
				}
				parts := strings.Fields(editor)
				ed := exec.Command(parts[0], append(parts[1:], f.Path)...)
				ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
				if err := ed.Run(); err != nil {
					return err
				}
				f, err = app.LoadConfig()
				if err != nil {
					return err
				}
				warns, errs := config.ValidateAll(f.Values())
				for _, w := range warns {
					fmt.Fprintln(os.Stderr, "warning:", w)
				}
				for _, e := range errs {
					fmt.Fprintln(os.Stderr, "error:", e)
				}
				if len(errs) > 0 {
					return fmt.Errorf("%d problem(s) in %s", len(errs), paths.Tildify(f.Path))
				}
				synced, err := app.SyncAll(f)
				if err != nil {
					return err
				}
				for _, path := range synced {
					fmt.Println("Updated", paths.Tildify(path))
				}
				return nil
			},
		},
	)
	return c
}

func showConfig() error {
	f, err := app.LoadConfig()
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n", paths.Tildify(f.Path))
	for _, k := range config.Keys {
		v, ok := f.Get(k.Name)
		if !ok {
			v = k.Default
			if v == "" {
				v = "(unset)"
			} else {
				v += " (default)"
			}
		}
		fmt.Printf("%-17s %s\n", k.Name, v)
	}
	warns, errs := config.ValidateAll(f.Values())
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "error:", e)
	}
	return nil
}

// report names the backup lca just took, so the first write to a file the
// user owns says where the original went.
func report(path, backedUp string) {
	if backedUp == "" {
		return
	}
	fmt.Printf("%s: copied to %s before the first change\n", paths.Tildify(path), paths.Tildify(backedUp))
}

func syncCmd() *cobra.Command {
	var port, ctx, harness string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Patch an agent harness's own configuration to reach the local server",
		Long: `Merge the local llama-server's provider entry into the configuration the
agent harness already owns: Qwen Code's ~/.qwen/settings.json, pi's
~/.pi/agent/models.json, or both. Only the keys lca needs are written; every
other setting is left as it is, and the file is copied aside as
<name>.lca-backup-<timestamp>.json before the first such write.

Without --harness, every harness lca manages is patched: the one config.env
selects, plus any whose configuration already carries the local provider from an
earlier sync. The launchers pass the harness and the port they started with.

lca unsync takes the entry back out.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := app.LoadConfig()
			if err != nil {
				return err
			}
			// Runtime overrides only; config.env is not saved. The launcher
			// passes the PORT= and CTX= it launched with, and the port has to
			// reach the provider entry: pi has no base-URL flag, so
			// models.json is the only way it learns which server to call.
			if cmd.Flags().Changed("port") {
				f.Set("PORT", port)
			}
			if cmd.Flags().Changed("ctx") {
				f.Set("CTX", ctx)
			}
			state := func(changed bool) string {
				if changed {
					return "updated"
				}
				return "already up to date"
			}
			harnesses := []string{harness}
			if !cmd.Flags().Changed("harness") {
				harnesses = app.Harnesses(f)
			}
			for _, h := range harnesses {
				switch h {
				case "pi":
					p, changed, modelsPath, backedUp, err := app.SyncPi(f)
					if err != nil {
						return err
					}
					report(modelsPath, backedUp)
					fmt.Printf("%s: provider %q → %s, model %q, contextWindow %d (%s)\n", paths.Tildify(modelsPath), p.ID, p.BaseURL, p.Model, p.ContextWindow, state(changed))
				case "qwen":
					p, changed, settingsPath, backedUp, err := app.SyncQwen(f)
					if err != nil {
						return err
					}
					report(settingsPath, backedUp)
					fmt.Printf("%s: provider %q → %s, contextWindowSize %d (%s)\n", paths.Tildify(settingsPath), p.ID, p.BaseURL, p.ContextWindow, state(changed))
				default:
					return fmt.Errorf("--harness must be qwen or pi, not %q", h)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&port, "port", "", "Use this server port without changing config.env")
	c.Flags().StringVar(&ctx, "ctx", "", "Use this context size without changing config.env")
	c.Flags().StringVar(&harness, "harness", "", "Prepare only this harness: qwen or pi")
	return c
}
