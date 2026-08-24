// Command pi is the entrypoint for pigo, a Go reimplementation of the pi coding agent.
//
// It wires up flag parsing (cobra) and configuration loading (viper via
// internal/config). Interactive mode launches the Phase 0 TUI; print mode streams
// a model reply via a StreamFn (internal/ai). See README.md for the migration plan.
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/ai"
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

			return run(cmd.Context(), cmd.OutOrStdout(), mode, prompt, cfg)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "run a single prompt non-interactively")
	cmd.Flags().StringVar(&mode, "mode", "interactive", "run mode: interactive|print")
	cmd.Flags().StringVar(&configDir, "config-dir", "", "config directory (default ~/.pi)")
	cmd.Flags().BoolVar(&showVer, "version", false, "print version and exit")

	return cmd
}

func run(ctx context.Context, out io.Writer, mode, prompt string, cfg config.Config) error {
	switch mode {
	case "print":
		if prompt == "" {
			fmt.Fprintf(out, "provider=%s model=%s theme=%s\n", cfg.Provider, cfg.Model, cfg.Theme)
			return nil
		}
		return streamPrompt(ctx, out, cfg, prompt)
	case "interactive":
		return tui.Run(cfg)
	default:
		return fmt.Errorf("unknown --mode %q (want: interactive|print)", mode)
	}
}

// streamPrompt sends the prompt through a StreamFn and streams the reply's text
// to out as it arrives (send → stream to screen). Without ANTHROPIC_API_KEY it
// uses a mock provider so the pipeline still runs offline.
func streamPrompt(ctx context.Context, out io.Writer, cfg config.Config, prompt string) error {
	sf, live := ai.DefaultStreamFn()
	provider := "mock"
	if live {
		provider = cfg.Provider
	}
	fmt.Fprintf(out, "[pigo] streaming via %s (model=%s)\n\n", provider, cfg.Model)

	stream, err := sf(ctx, ai.Context{
		Messages: []ai.Message{{Role: ai.RoleUser, Content: prompt}},
	}, ai.Options{Model: cfg.Model})
	if err != nil {
		return err
	}

	for ev := range stream.Events() {
		switch ev.Type {
		case ai.EventTextDelta:
			fmt.Fprint(out, ev.Delta)
		case ai.EventError:
			fmt.Fprintf(out, "\n[error] %s\n", ev.Message.ErrorMessage)
			return nil
		}
	}
	fmt.Fprintln(out)
	return nil
}
