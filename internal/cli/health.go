package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/config"
)

func newHealthCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "health",
		Short: "Check control plane health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(
				cmd.Context(),
				cfg.Timeout,
			)
			defer cancel()

			req, err := http.NewRequestWithContext(
				ctx, http.MethodGet, cfg.Server+"/health", nil,
			)
			if err != nil {
				return err
			}

			client := &http.Client{Timeout: cfg.Timeout}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("contact control plane: %w", err)
			}
			defer resp.Body.Close()

			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				return fmt.Errorf(
					"control plane returned HTTP %d: %s",
					resp.StatusCode, string(body),
				)
			}

			success := lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("42"))

			fmt.Fprintf(cmd.OutOrStdout(), "%s Control plane is healthy\n",
				success.Render("✓"))
			fmt.Fprintf(cmd.OutOrStdout(), "  Server: %s\n", cfg.Server)
			fmt.Fprintf(cmd.OutOrStdout(), "  Status: %s\n", resp.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "  Checked: %s\n",
				time.Now().Format(time.RFC3339))

			return nil
		},
	}

	cmd.Flags().StringVar(
		&server, "server", "",
		"Control plane URL (overrides configuration)",
	)

	return cmd
}
