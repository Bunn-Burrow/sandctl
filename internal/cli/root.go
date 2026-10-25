package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/client"
	"github.com/Bunn-Burrow/sandctl/internal/config"
)

var Version = "dev"

// newClient builds an authenticated HTTP client from the resolved config.
func newClient(cfg config.Config) *client.Client {
	return client.New(cfg.Server, cfg.Token, cfg.Timeout)
}

// handleError converts command errors into concise, user-facing messages
// without leaking tokens or headers.
func handleError(err error) error {
	if err == nil {
		return nil
	}
	if status := client.StatusCode(err); status != 0 {
		switch status {
		case 401:
			return fmt.Errorf("authentication failed: %s\n  Provide a bearer token via the SANDCTL_TOKEN environment variable (or CONTROL_PLANE_API_KEYS).", err)
		case 403:
			return fmt.Errorf("authorization failed: %s", err)
		case 404:
			return fmt.Errorf("resource not found: %s", err)
		case 409:
			return fmt.Errorf("conflict: %s", err)
		case 429:
			return fmt.Errorf("rate limited: %s\n  Retry after a short delay.", err)
		case 503:
			return fmt.Errorf("service unavailable: %s", err)
		default:
			return fmt.Errorf("server error (HTTP %d): %s", status, err)
		}
	}
	return err
}

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "sandctl",
		Short:         "Manage the Bunn Burrow sandbox and agent platform",
		Long:          "sandctl is the command-line interface for the Bunn Burrow control plane. It talks to the control plane's authenticated HTTP API and never implements a sandbox or guest agent directly.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "sandctl version %s\n", Version)
			return err
		},
	})

	rootCmd.AddCommand(newHealthCommand())
	rootCmd.AddCommand(newDoctorCommand())
	rootCmd.AddCommand(newConfigCommand())
	rootCmd.AddCommand(newAgentCommand())
	rootCmd.AddCommand(newRunCommand())
	rootCmd.AddCommand(newExecutionCommand())

	return rootCmd
}
