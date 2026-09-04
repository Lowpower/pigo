package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/runtime"
	"github.com/Lowpower/pigo/internal/sesssrv"
)

func newServerCmd() *cobra.Command {
	var listen string
	cmd := &cobra.Command{
		Use:   "server",
		Short: "serve JSONL RPC sessions on a Unix socket",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if listen == "" {
				listen = os.Getenv("PIGO_SERVER_LISTEN")
			}
			path, err := sesssrv.ParseUnixPath(listen)
			if err != nil {
				return err
			}
			ln, err := sesssrv.ListenUnix(path)
			if err != nil {
				return err
			}
			defer func() { _ = ln.Close() }()
			fmt.Fprintf(cmd.ErrOrStderr(), "pigo server listening on unix://%s\n", path)
			return sesssrv.Serve(cmd.Context(), ln, func() (sesssrv.Session, error) {
				return newServerEngine(cmd.Context())
			})
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "unix:///path.sock (or PIGO_SERVER_LISTEN)")
	return cmd
}

func newClientCmd() *cobra.Command {
	var connect string
	cmd := &cobra.Command{
		Use:   "client",
		Short: "connect to a pigo server Unix socket (JSONL stdio bridge)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if connect == "" {
				connect = os.Getenv("PIGO_SERVER_CONNECT")
			}
			path, err := sesssrv.ParseUnixPath(connect)
			if err != nil {
				return err
			}
			conn, err := sesssrv.DialUnix(path)
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			return sesssrv.Bridge(cmd.Context(), conn, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&connect, "connect", "", "unix:///path.sock (or PIGO_SERVER_CONNECT)")
	return cmd
}

func newServerEngine(ctx context.Context) (*runtime.Engine, error) {
	agentDir := config.DefaultConfigDir()
	cfg, err := config.Load(agentDir)
	if err != nil {
		cfg = config.Config{}
	}
	cwd, _ := os.Getwd()
	return runtime.New(ctx, runtime.Options{
		Config:       cfg,
		UserConfig:   &cfg,
		Cwd:          cwd,
		AgentDir:     agentDir,
		NoExtensions: true,
		Offline:      os.Getenv("PIGO_OFFLINE") != "",
	})
}
