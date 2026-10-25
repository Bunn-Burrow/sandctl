package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/config"
	"github.com/Bunn-Burrow/sandctl/internal/output"
)

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage sandctl connection contexts",
		Long: `Manage named connection contexts for the control plane.

Configuration is stored in a user-level config file (for example
~/.config/sandctl/config.yaml) with restrictive file permissions. Bearer
tokens are never written to the config file; resolve them from the
SANDCTL_TOKEN or CONTROL_PLANE_API_KEYS environment variable instead.`,
	}

	cmd.AddCommand(newConfigShowCommand())
	cmd.AddCommand(newConfigSetContextCommand())
	cmd.AddCommand(newConfigUseContextCommand())
	cmd.AddCommand(newConfigCurrentContextCommand())
	cmd.AddCommand(newConfigDeleteContextCommand())
	cmd.AddCommand(newConfigInitCommand())

	return cmd
}

func newConfigShowCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the resolved connection configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, file, err := config.Show()
			if err != nil {
				return err
			}
			if server != "" {
				cfg.Server = server
			}

			w := output.NewWriter(output.FormatTable)
			rows := [][]string{
				{"Context", cfg.ContextName},
				{"Server", cfg.Server},
				{"Timeout", cfg.Timeout.String()},
				{"Token", func() string {
					if cfg.Token == "" {
						return "not set (set SANDCTL_TOKEN or CONTROL_PLANE_API_KEYS)"
					}
					return "***"
				}()},
			}
			if err := w.Table([]string{"Key", "Value"}, rows); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "")
			fmt.Fprintf(cmd.OutOrStdout(), "Contexts:\n")
			names := config.SortedContextNames(file.Contexts)
			for _, name := range names {
				ctx := file.Contexts[name]
				marker := "  "
				if name == cfg.ContextName {
					marker = "* "
				}
				server := ctx.Server
				if server == "" {
					server = "(default http://localhost:8080)"
				}
				timeout := ctx.Timeout
				if timeout == "" {
					timeout = "(default 10s)"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s%s  server=%s timeout=%s\n", marker, name, server, timeout)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Show this server override instead of the resolved value")
	return cmd
}

func newConfigSetContextCommand() *cobra.Command {
	var server, timeout string

	cmd := &cobra.Command{
		Use:   "set-context <name>",
		Short: "Create or update a named context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.SetContext(args[0], server, timeout); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Context %q configured.\n", args[0])
			if timeout != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  server=%s timeout=%s\n", server, timeout)
			} else if server != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  server=%s\n", server)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane base URL (http:// or https://)")
	cmd.Flags().StringVar(&timeout, "timeout", "", "Request timeout (e.g. 30s)")
	return cmd
}

func newConfigUseContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use-context <name>",
		Short: "Set the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.UseContext(args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Context %q set as current.\n", args[0])
			return nil
		},
	}
	return cmd
}

func newConfigCurrentContextCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "current-context",
		Short: "Print the current context name",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := config.CurrentContext()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), name)
			return nil
		},
	}
	return cmd
}

func newConfigDeleteContextCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete-context <name>",
		Short: "Delete a named context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.DeleteContext(args[0], force); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Context %q deleted.\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Delete the current context")
	return cmd
}

func newConfigInitCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a default configuration file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.Init(force); err != nil {
				return err
			}
			path, err := config.ConfigPath()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Configuration written to %s\n", path)
			fmt.Fprintln(cmd.OutOrStdout(), "Set SANDCTL_TOKEN (or CONTROL_PLANE_API_KEYS) to authenticate.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing config file")
	return cmd
}
