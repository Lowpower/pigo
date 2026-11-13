package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/auth"
	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/mcp"
	"github.com/Lowpower/pigo/internal/mcp/oauth"
	"github.com/Lowpower/pigo/internal/trust"
)

const defaultMCPLoginTimeout = 300 * time.Second

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "manage MCP servers",
	}
	cmd.SilenceUsage = true
	cmd.AddCommand(newMCPAddCmd(), newMCPRemoveCmd(), newMCPListCmd(), newMCPLoginCmd(), newMCPLogoutCmd())
	return cmd
}

type mcpAddFlags struct {
	url         string
	description string
	exposure    string
	clientID    string
	secret      string
	clientName  string
	callback    int
	bearer      string
	headers     []string
	env         []string
	local       bool
}

func newMCPAddCmd() *cobra.Command {
	var f mcpAddFlags
	cmd := &cobra.Command{
		Use:   "add <name> (--url <url> | -- <command> [args...])",
		Short: "add or replace an MCP server in mcp.json",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPAdd(cmd.OutOrStdout(), f, args)
		},
	}
	cmd.Flags().StringVar(&f.url, "url", "", "streamable HTTP endpoint")
	cmd.Flags().StringVar(&f.description, "description", "", "server description")
	cmd.Flags().StringVar(&f.exposure, "exposure", "", "direct, model-only, codemode, deferred, or hidden")
	cmd.Flags().StringVar(&f.clientID, "oauth-client-id", "", "OAuth client id")
	cmd.Flags().StringVar(&f.secret, "oauth-client-secret", "", "OAuth client secret")
	cmd.Flags().StringVar(&f.clientName, "oauth-client-name", "", "OAuth client name")
	cmd.Flags().IntVar(&f.callback, "oauth-callback-port", 0, "OAuth callback port")
	cmd.Flags().StringVar(&f.bearer, "bearer-token-env-var", "", "env var holding a bearer token")
	cmd.Flags().StringArrayVar(&f.headers, "header", nil, "HTTP header key=value (repeatable)")
	cmd.Flags().StringArrayVar(&f.env, "env", nil, "stdio environment key=value (repeatable)")
	cmd.Flags().BoolVar(&f.local, "local", false, "write .pigo/mcp.json instead of the user file")
	return cmd
}

func runMCPAdd(out io.Writer, f mcpAddFlags, args []string) error {
	if f.exposure != "" && !validExposure(f.exposure) {
		return fmt.Errorf("unknown exposure %q", f.exposure)
	}
	if f.callback < 0 || f.callback > 65535 {
		return fmt.Errorf("oauth callback port is invalid")
	}
	spec := mcp.UpsertSpec{
		Name:              args[0],
		URL:               strings.TrimSpace(f.url),
		Description:       f.description,
		Exposure:          f.exposure,
		OAuthClientID:     f.clientID,
		OAuthClientSecret: f.secret,
		OAuthClientName:   f.clientName,
		OAuthCallbackPort: f.callback,
		BearerTokenEnvVar: f.bearer,
	}
	httpFlags := f.clientID != "" || f.secret != "" || f.clientName != "" || f.callback != 0 || f.bearer != "" || len(f.headers) > 0
	if spec.URL == "" {
		if httpFlags {
			return fmt.Errorf("HTTP flags require --url")
		}
		if len(args) < 2 {
			return fmt.Errorf("stdio server needs a command after --")
		}
		spec.Command = args[1]
		spec.Args = append([]string(nil), args[2:]...)
		env, err := parsePairs(f.env)
		if err != nil {
			return err
		}
		spec.Env = env
	} else if len(args) != 1 {
		return fmt.Errorf("pass either --url or a command, not both")
	} else if len(f.env) > 0 {
		return fmt.Errorf("--env is only for a stdio server")
	} else {
		headers, err := parsePairs(f.headers)
		if err != nil {
			return err
		}
		spec.Headers = headers
	}
	path, err := mcpConfigPath(f.local)
	if err != nil {
		return err
	}
	if err := mcp.Upsert(path, spec); err != nil {
		return err
	}
	fmt.Fprintf(out, "added %s\n", spec.Name)
	return nil
}

func newMCPRemoveCmd() *cobra.Command {
	var local bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "remove an MCP server from mcp.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := mcpConfigPath(local)
			if err != nil {
				return err
			}
			if err := mcp.Remove(path, args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVar(&local, "local", false, "remove from .pigo/mcp.json")
	return cmd
}

func newMCPListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "connect to enabled MCP servers and print their status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCPList(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func runMCPList(ctx context.Context, out, errw io.Writer, asJSON bool) error {
	agentDir, cwd, trusted := mcpSession()
	servers, diags := mcp.Load(agentDir, cwd, trusted)
	for _, d := range diags {
		fmt.Fprintf(errw, "Warning: %s\n", d.Error())
	}
	statuses, err := mcp.Probe(ctx, servers, cwd)
	if err != nil && ctx.Err() != nil {
		return err
	}
	entries := mcp.ListEntries(statuses)
	if asJSON {
		b, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%s\n", b)
		return nil
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "No MCP servers.")
		return nil
	}
	for _, entry := range entries {
		line := entry.Name + "  " + entry.Status + "  " + entry.Exposure
		if entry.Resources {
			line += "  resources"
		}
		if entry.Error != "" {
			line += "  " + entry.Error
		}
		fmt.Fprintln(out, line)
	}
	return nil
}

func newMCPLoginCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "login <name>",
		Short: "sign in to an HTTP MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPLogin(cmd.Context(), cmd.OutOrStdout(), args[0], timeout)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", defaultMCPLoginTimeout, "limit the whole sign-in")
	return cmd
}

func runMCPLogin(parent context.Context, out io.Writer, name string, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	agentDir, cwd, trusted := mcpSession()
	srv, err := mcp.Find(agentDir, cwd, trusted, name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	return mcp.Login(ctx, agentDir, srv, oauth.Prompt{
		Notify: func(ev auth.Event) {
			if ev.URL != "" {
				fmt.Fprintln(out, ev.URL)
			}
		},
	})
}

func newMCPLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout <name>",
		Short: "delete stored OAuth credentials for an MCP server",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentDir, cwd, trusted := mcpSession()
			srv, err := mcp.Find(agentDir, cwd, trusted, args[0])
			if err != nil {
				return err
			}
			if err := mcp.Logout(agentDir, srv); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "logged out %s\n", args[0])
			return nil
		},
	}
	return cmd
}

func mcpSession() (agentDir, cwd string, trusted bool) {
	agentDir = config.DefaultConfigDir()
	cwd, _ = os.Getwd()
	cfg, err := config.Load(agentDir)
	if err != nil {
		cfg = config.Config{}
	}
	trusted = trust.Decide(trust.Open(agentDir), cwd, trust.Options{Default: cfg.ProjectTrustDefault()})
	return agentDir, cwd, trusted
}

func mcpConfigPath(local bool) (string, error) {
	agentDir, cwd, trusted := mcpSession()
	return mcp.ConfigPath(agentDir, cwd, local, trusted)
}

func validExposure(s string) bool {
	switch s {
	case "direct", "model-only", "codemode", "deferred", "hidden":
		return true
	default:
		return false
	}
}

func parsePairs(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(items))
	for _, item := range items {
		key, val, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("expected key=value, got %q", item)
		}
		out[key] = val
	}
	return out, nil
}
