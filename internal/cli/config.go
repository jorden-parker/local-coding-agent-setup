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
			Use: "set KEY VALUE", Short: "Validate and write one value; ALIAS, PORT and CTX also update Qwen Code",
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
				if _, changed, err := app.SyncQwen(f); err != nil {
					return err
				} else if changed {
					fmt.Println("Updated", paths.Tildify(paths.QwenSettings()))
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

func syncCmd() *cobra.Command {
	var port string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Prepare the local Qwen model, provider and lean settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := app.LoadConfig()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("port") {
				f.Set("PORT", port) // Runtime override only; config.env is not saved.
			}
			p, changed, err := app.SyncQwen(f)
			if err != nil {
				return err
			}
			state := "already up to date"
			if changed {
				state = "updated"
			}
			fmt.Printf("%s: provider %q → %s, contextWindowSize %d (%s)\n", paths.Tildify(paths.QwenSettings()), p.ID, p.BaseURL, p.ContextWindow, state)
			return nil
		},
	}
	c.Flags().StringVar(&port, "port", "", "Use this server port without changing config.env")
	return c
}
