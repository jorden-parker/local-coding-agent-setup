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

func syncCmd() *cobra.Command {
	var port, ctx, harness string
	c := &cobra.Command{
		Use:   "sync",
		Short: "Prepare the local model, provider and settings of an agent harness",
		Long: `Write the configuration an agent harness needs to reach the local
llama-server: Qwen Code's provider entry and lean settings, pi's models.json
and default model, or both.

Without --harness, Qwen Code is prepared, and pi too when config.env selects it
or its managed directory already exists. The launchers pass the harness and the
port they actually started with.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := app.LoadConfig()
			if err != nil {
				return err
			}
			cfgPort, _ := f.Get("PORT")
			activePort := cfgPort
			// Runtime overrides only; config.env is not saved. The launcher
			// passes the PORT= and CTX= it launched with.
			if cmd.Flags().Changed("port") {
				f.Set("PORT", port)
				activePort = port
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
				// Qwen Code is always installed; pi only once it has been used.
				harnesses = []string{"qwen"}
				if app.SyncsPi(f) {
					harnesses = append(harnesses, "pi")
				}
			}
			for _, h := range harnesses {
				switch h {
				case "pi":
					p, changed, modelsPath, err := app.SyncPiAt(f, activePort, cfgPort)
					if err != nil {
						return err
					}
					fmt.Printf("%s: provider %q → %s, model %q, contextWindow %d (%s)\n", paths.Tildify(modelsPath), p.ID, p.BaseURL, p.Model, p.ContextWindow, state(changed))
				case "qwen":
					p, changed, shared, settingsPath, err := app.SyncQwenAt(f, activePort, cfgPort)
					if err != nil {
						return err
					}
					fmt.Printf("%s: provider %q → %s, contextWindowSize %d (%s)\n", paths.Tildify(settingsPath), p.ID, p.BaseURL, p.ContextWindow, state(changed))
					if activePort == cfgPort {
						fmt.Printf("%s: provider %q shared with ordinary qwen and the VS Code companion (%s)\n", paths.Tildify(paths.LegacyQwenSettings()), p.ID, state(shared))
					}
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
