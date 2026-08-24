// Command pi is the entrypoint for pigo, a Go reimplementation of the pi coding agent.
//
// This is the Phase 0 scaffold: it wires up flag parsing (cobra) and configuration
// loading (viper via internal/config). The interactive TUI and agent loop land in
// later phases; see README.md for the migration plan.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/tui"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "0.0.1-dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		prompt    string
		mode      string
		configDir string
		showVer   bool
	)

	cmd := &cobra.Command{
		Use:           "pi",
		Short:         "pigo — a Go reimplementation of the pi coding agent",
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVer {
				fmt.Fprintf(cmd.OutOrStdout(), "pigo %s\n", version)
				return nil
			}

			if configDir == "" {
				configDir = config.DefaultConfigDir()
			}
			cfg, err := config.Load(configDir)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// A prompt implies a single non-interactive run, matching pi's -p.
			if prompt != "" && mode == "interactive" {
				mode = "print"
			}

			return run(cmd, mode, prompt, cfg)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "run a single prompt non-interactively")
	cmd.Flags().StringVar(&mode, "mode", "interactive", "run mode: interactive|print")
	cmd.Flags().StringVar(&configDir, "config-dir", "", "config directory (default ~/.pi)")
	cmd.Flags().BoolVar(&showVer, "version", false, "print version and exit")

	return cmd
}

func run(cmd *cobra.Command, mode, prompt string, cfg config.Config) error {
	out := cmd.OutOrStdout()
	switch mode {
	case "print":
		fmt.Fprintf(out, "provider=%s model=%s theme=%s\n", cfg.Provider, cfg.Model, cfg.Theme)
		if prompt != "" {
			fmt.Fprintf(out, "prompt=%q\n", prompt)
		}
		return nil
	case "interactive":
		return tui.Run(cfg)
	default:
		return fmt.Errorf("unknown --mode %q (want: interactive|print)", mode)
	}
}
