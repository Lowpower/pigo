package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/eval"
)

func newEvalCmd() *cobra.Command {
	var (
		outDir   string
		provider string
		model    string
		apiKey   string
	)
	cmd := &cobra.Command{
		Use:   "eval [dir]",
		Short: "run JSON scenarios in isolated temp directories",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "evals"
			if len(args) == 1 {
				dir = args[0]
			}
			if outDir == "" {
				outDir = ".eval"
			}
			rep, err := eval.RunDir(cmd.Context(), eval.Options{
				Dir:      dir,
				OutDir:   outDir,
				Provider: provider,
				Model:    model,
				APIKey:   apiKey,
			})
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), eval.FormatReport(rep))
			if rep.Failed > 0 {
				return fmt.Errorf("%d scenario(s) failed", rep.Failed)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "out", "", "report directory (default .eval)")
	cmd.Flags().StringVar(&provider, "provider", "", "provider id (env PIGO_PROVIDER)")
	cmd.Flags().StringVar(&model, "model", "", "model id (env PIGO_MODEL)")
	cmd.Flags().StringVar(&apiKey, "api-key", "", "API key for this process (does not persist)")
	return cmd
}
