package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/jorden-parker/local-coding-agent-setup/internal/app"
	"github.com/jorden-parker/local-coding-agent-setup/internal/paths"
	"github.com/jorden-parker/local-coding-agent-setup/internal/server"
	"github.com/jorden-parker/local-coding-agent-setup/internal/stats"
)

func statsCmd() *cobra.Command {
	var days int
	var model, source string
	var asJSON, raw bool
	c := &cobra.Command{
		Use:   "stats",
		Short: "Response times per day from Qwen Code's usage log and llama-server's timings",
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := app.ParseSource(source)
			if err != nil {
				return err
			}
			s, err := app.CollectStats(days, model, src)
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if raw {
					return enc.Encode(map[string]any{"qwen": s.QwenRaw, "server": s.ServerRaw})
				}
				return enc.Encode(map[string]any{"qwen": s.Qwen, "server": s.Server})
			}
			if raw {
				printRaw(s)
				return nil
			}
			if src != app.SourceServer {
				fmt.Printf("Qwen Code API calls (apiDurationMs), last %d days, from %s\n", days, paths.Tildify(paths.QwenUsageDir())+" and "+paths.Tildify(paths.LegacyQwenUsageDir()))
				printBuckets(s.Qwen, false)
				if s.Skipped > 0 {
					fmt.Printf("(%d malformed or unsupported lines skipped)\n", s.Skipped)
				}
				fmt.Println()
			}
			if src != app.SourceQwen {
				fmt.Printf("llama-server requests (total time), last %d days, from %s\n", days, paths.Tildify(paths.Timings()))
				printBuckets(s.Server, true)
				fmt.Println()
			}
			for _, n := range s.Notes {
				fmt.Fprintln(os.Stderr, "note:", n)
			}
			return nil
		},
	}
	c.Flags().IntVar(&days, "days", 14, "how many days back to look")
	c.Flags().StringVar(&model, "model", "", "only this model / alias")
	c.Flags().StringVar(&source, "source", "both", "qwen | server | both")
	c.Flags().BoolVar(&asJSON, "json", false, "print JSON instead of tables")
	c.Flags().BoolVar(&raw, "raw", false, "print every request instead of daily buckets")
	return c
}

func printBuckets(bs []stats.Bucket, tps bool) {
	if len(bs) == 0 {
		fmt.Println("  no data")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 2, 2, ' ', 0)
	if tps {
		fmt.Fprintln(w, "  day\tmodel\tn\tp50\tp95\tmean\tin tok\tout tok\tprompt t/s\tgen t/s")
	} else {
		fmt.Fprintln(w, "  day\tmodel\tn\tp50\tp95\tmean\tin tok\tout tok")
	}
	for _, b := range bs {
		line := fmt.Sprintf("  %s\t%s\t%d\t%s\t%s\t%s\t%d\t%d", b.Day, b.Model, b.N, app.FormatMs(b.P50), app.FormatMs(b.P95), app.FormatMs(b.Mean), b.InTok, b.OutTok)
		if tps {
			line += fmt.Sprintf("\t%.1f\t%.1f", b.PromptTPS, b.GenTPS)
		}
		fmt.Fprintln(w, line)
	}
	_ = w.Flush()
	days, vals := stats.DailyP50(bs)
	fmt.Printf("  daily p50 %s  (%s .. %s)\n", stats.Sparkline(vals, 40), days[0], days[len(days)-1])
}

func printRaw(s app.Stats) {
	w := tabwriter.NewWriter(os.Stdout, 2, 2, 2, ' ', 0)
	if len(s.QwenRaw) > 0 {
		fmt.Fprintln(w, "source\ttime\tmodel\tcaller\tin\tout\tcached\tduration")
		for _, r := range s.QwenRaw {
			fmt.Fprintf(w, "qwen\t%s\t%s\t%s\t%d\t%d\t%d\t%s\n", r.Timestamp.Local().Format("2006-01-02 15:04:05"), r.Model, r.Source, r.InputTokens, r.OutputTokens, r.CachedTokens, app.FormatMs(r.APIDurationMs))
		}
	}
	if len(s.ServerRaw) > 0 {
		fmt.Fprintln(w, "source\ttime\tmodel\ttask\tprompt tok\tprompt t/s\tgen tok\tgen t/s\ttotal")
		for _, t := range s.ServerRaw {
			fmt.Fprintf(w, "server\t%s\t%s\t%d\t%d\t%.1f\t%d\t%.1f\t%s\n", t.Time.Local().Format("2006-01-02 15:04:05"), t.Model, t.Task, t.PromptN, t.PromptTPS, t.GenN, t.GenTPS, app.FormatMs(t.TotalMs))
		}
	}
	_ = w.Flush()
}

func compactCmd() *cobra.Command {
	var quiet bool
	c := &cobra.Command{
		Use:   "compact",
		Short: "Fold server-*.log timing lines into timings.jsonl and prune logs older than 14 days",
		RunE: func(cmd *cobra.Command, args []string) error {
			added, pruned, err := server.Compact(paths.StateDir())
			if err != nil {
				return err
			}
			if !quiet {
				fmt.Printf("%d new timing(s) added to %s, %d old log(s) removed\n", added, paths.Tildify(paths.Timings()), pruned)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&quiet, "quiet", false, "print nothing on success")
	return c
}

func metricsCmd() *cobra.Command {
	var port int
	c := &cobra.Command{
		Use:   "metrics",
		Short: "Scrape the running llama-server's /metrics once",
		RunE: func(cmd *cobra.Command, args []string) error {
			if port == 0 {
				f, err := app.LoadConfig()
				if err != nil {
					return err
				}
				p, err := app.Provider(f)
				if err != nil {
					return err
				}
				fmt.Sscanf(strings.TrimPrefix(p.BaseURL, "http://127.0.0.1:"), "%d", &port)
			}
			m, err := server.Scrape(port)
			if err != nil {
				return err
			}
			fmt.Println(m.Summary())
			keys := make([]string, 0, len(m))
			for k := range m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Printf("  %-32s %g\n", k, m[k])
			}
			return nil
		},
	}
	c.Flags().IntVar(&port, "port", 0, "server port (default: PORT from config.env)")
	return c
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check config, launchers, Qwen settings, state dir and the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := app.Doctor()
			for _, c := range checks {
				mark := "ok  "
				switch {
				case c.Warn:
					mark = "warn"
				case !c.OK:
					mark = "FAIL"
				}
				fmt.Printf("%s  %-18s %s\n", mark, c.Name, c.Detail)
			}
			if app.Failed(checks) {
				return fmt.Errorf("some checks failed")
			}
			return nil
		},
	}
}
