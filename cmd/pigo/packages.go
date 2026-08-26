package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Lowpower/pigo/internal/config"
	"github.com/Lowpower/pigo/internal/pkgmgr"
	"github.com/Lowpower/pigo/internal/trust"
)

type packageFlags struct {
	local     bool
	approve   bool
	noApprove bool
	self      bool
	exts      bool
	models    bool
	all       bool
	force     bool
	extension string
}

func trustOverride(f packageFlags) *bool {
	if f.approve {
		v := true
		return &v
	}
	if f.noApprove {
		v := false
		return &v
	}
	return nil
}

func addPackageFlags(cmd *cobra.Command, f *packageFlags, localOK, updateOK bool) {
	if localOK {
		cmd.Flags().BoolVarP(&f.local, "local", "l", false, "use project-local settings (.pigo/settings.json)")
	}
	cmd.Flags().BoolVarP(&f.approve, "approve", "a", false, "trust project-local files for this command")
	cmd.Flags().BoolVar(&f.noApprove, "no-approve", false, "ignore project-local files for this command")
	if updateOK {
		cmd.Flags().BoolVar(&f.self, "self", false, "update pigo only (not implemented)")
		cmd.Flags().BoolVar(&f.exts, "extensions", false, "update installed packages only")
		cmd.Flags().BoolVar(&f.models, "models", false, "refresh model catalogs only (not implemented)")
		cmd.Flags().BoolVar(&f.all, "all", false, "update pigo and installed packages")
		cmd.Flags().BoolVar(&f.force, "force", false, "reinstall pigo even if current (not implemented)")
		cmd.Flags().StringVar(&f.extension, "extension", "", "update one package only")
	}
}

func openPackageManager(f packageFlags) (*pkgmgr.Manager, error) {
	cwd, _ := os.Getwd()
	agentDir := config.DefaultConfigDir()
	st := trust.Open(agentDir)
	trusted := trust.Resolve(st, cwd, trustOverride(f))
	return pkgmgr.Open(cwd, agentDir, trusted)
}

func newInstallCmd() *cobra.Command {
	var f packageFlags
	cmd := &cobra.Command{
		Use:   "install <source>",
		Short: "install a package and add it to settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := openPackageManager(f)
			if err != nil {
				return err
			}
			if err := m.InstallAndPersist(cmd.Context(), args[0], f.local); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Installed %s\n", args[0])
			return nil
		},
	}
	addPackageFlags(cmd, &f, true, false)
	return cmd
}

func newRemoveCmd() *cobra.Command {
	var f packageFlags
	cmd := &cobra.Command{
		Use:     "remove <source>",
		Aliases: []string{"uninstall"},
		Short:   "remove a package and its source from settings",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			m, err := openPackageManager(f)
			if err != nil {
				return err
			}
			ok, err := m.RemoveAndPersist(cmd.Context(), args[0], f.local)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("no matching package found for %s", args[0])
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", args[0])
			return nil
		},
	}
	addPackageFlags(cmd, &f, true, false)
	return cmd
}

func newListCmd() *cobra.Command {
	var f packageFlags
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list installed packages from user and project settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := openPackageManager(f)
			if err != nil {
				return err
			}
			pkgs := m.ListConfigured()
			if len(pkgs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No packages installed.")
				return nil
			}
			var user, project []pkgmgr.Configured
			for _, p := range pkgs {
				if p.Scope == "project" {
					project = append(project, p)
				} else {
					user = append(user, p)
				}
			}
			out := cmd.OutOrStdout()
			if len(user) > 0 {
				fmt.Fprintln(out, "User packages:")
				for _, p := range user {
					printConfigured(cmd, p)
				}
			}
			if len(project) > 0 {
				if len(user) > 0 {
					fmt.Fprintln(out)
				}
				fmt.Fprintln(out, "Project packages:")
				for _, p := range project {
					printConfigured(cmd, p)
				}
			}
			return nil
		},
	}
	addPackageFlags(cmd, &f, false, false)
	return cmd
}

func printConfigured(cmd *cobra.Command, p pkgmgr.Configured) {
	label := p.Source
	if p.Filtered {
		label += " (filtered)"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", label)
	if p.InstalledPath != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", p.InstalledPath)
	}
}

func newUpdateCmd() *cobra.Command {
	var f packageFlags
	cmd := &cobra.Command{
		Use:   "update [source|self|pigo]",
		Short: "update pigo, installed packages, or model catalogs",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := ""
			if len(args) > 0 {
				source = args[0]
			}
			wantSelf := f.self || f.all || source == "self" || source == "pigo" || source == "pi"
			wantExt := f.exts || f.all || f.extension != "" || (source != "" && source != "self" && source != "pigo" && source != "pi")
			wantModels := f.models
			if !wantSelf && !wantExt && !wantModels {
				fmt.Fprintln(cmd.OutOrStdout(), "Extensions are skipped. Run pigo update --extensions to update extensions.")
				wantSelf = true
			}
			if wantModels {
				return fmt.Errorf("model catalog refresh is not implemented")
			}
			if wantExt {
				m, err := openPackageManager(f)
				if err != nil {
					return err
				}
				upd := f.extension
				if upd == "" && source != "" && source != "self" && source != "pigo" && source != "pi" {
					upd = source
				}
				if err := m.Update(cmd.Context(), upd); err != nil {
					return err
				}
				if upd != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", upd)
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), "Updated packages")
				}
			}
			if wantSelf {
				return fmt.Errorf("pigo cannot self-update this installation")
			}
			return nil
		},
	}
	addPackageFlags(cmd, &f, false, true)
	cmd.Long = `Update pigo, installed packages, or model catalogs.

  pigo update                 Update pigo only (not implemented)
  pigo update --extensions    Update installed packages
  pigo update --models        Refresh model catalogs (not implemented)
  pigo update --self          Same as update with no args (not implemented)
  pigo update --all           Packages then self-update (self not implemented)
  pigo update <source>        Update one package
`
	return cmd
}

func addPackageCommands(root *cobra.Command) {
	root.AddCommand(newInstallCmd(), newRemoveCmd(), newListCmd(), newUpdateCmd())
}
