package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/client"
	"github.com/Bunn-Burrow/sandctl/internal/config"
	"github.com/Bunn-Burrow/sandctl/internal/output"
)

type doctorCheck struct {
	name   string
	status string
	detail string
}

func newDoctorCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose control-plane connectivity, readiness, and authentication",
		Long: `Check whether the control plane is reachable, ready to serve requests,
and whether the configured credentials authenticate. Health and readiness
endpoints are public; authenticated routes require a bearer token.`,
		Example: `  sandctl doctor
  sandctl doctor --server https://control-plane.example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}

			w := output.NewWriter(output.FormatTable)

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

			httpClient := &http.Client{Timeout: cfg.Timeout}
			checks := []doctorCheck{}

			getStatus := func(path string) (int, error) {
				resp, err := httpClient.Get(cfg.Server + path)
				if err != nil {
					return 0, err
				}
				defer resp.Body.Close()
				io.Copy(io.Discard, resp.Body)
				return resp.StatusCode, nil
			}

			// 1. Health (public, no auth).
			healthOK := false
			code, err := getStatus("/health")
			if err != nil {
				checks = append(checks, doctorCheck{"health", "unreachable", fmt.Sprintf("cannot reach %s/health: %v", cfg.Server, err)})
			} else if code >= 200 && code < 300 {
				healthOK = true
				checks = append(checks, doctorCheck{"health", "ok", fmt.Sprintf("HTTP %s", http.StatusText(code))})
			} else {
				checks = append(checks, doctorCheck{"health", "unhealthy", fmt.Sprintf("HTTP %s", http.StatusText(code))})
			}

			// 2. Readiness (public, no auth).
			readyOK := false
			code, err = getStatus("/ready")
			if err != nil {
				checks = append(checks, doctorCheck{"readiness", "unreachable", fmt.Sprintf("cannot reach %s/ready: %v", cfg.Server, err)})
			} else if code >= 200 && code < 300 {
				readyOK = true
				checks = append(checks, doctorCheck{"readiness", "ok", fmt.Sprintf("HTTP %s", http.StatusText(code))})
			} else if code == http.StatusServiceUnavailable {
				checks = append(checks, doctorCheck{"readiness", "not-ready", "control plane is not ready (runtime or store unavailable)"})
			} else {
				checks = append(checks, doctorCheck{"readiness", "unhealthy", fmt.Sprintf("HTTP %s", http.StatusText(code))})
			}

			// 3. Authentication (requires bearer token).
			authed := false
			if cfg.Token == "" {
				checks = append(checks, doctorCheck{"authentication", "skipped", "no bearer token configured (set SANDCTL_TOKEN or CONTROL_PLANE_API_KEYS)"})
			} else {
				c := client.New(cfg.Server, cfg.Token, cfg.Timeout)
				resp, err := c.Get(ctx, "/v1/agents")
				if err != nil {
					if client.StatusCode(err) == http.StatusUnauthorized {
						checks = append(checks, doctorCheck{"authentication", "failed", "bearer token rejected by control plane"})
					} else if client.StatusCode(err) == 0 {
						checks = append(checks, doctorCheck{"authentication", "unreachable", fmt.Sprintf("cannot reach control plane: %v", err)})
					} else {
						checks = append(checks, doctorCheck{"authentication", "error", err.Error()})
					}
				} else {
					defer resp.Body.Close()
					io.Copy(io.Discard, resp.Body)
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						authed = true
						checks = append(checks, doctorCheck{"authentication", "ok", "bearer token accepted"})
					} else if resp.StatusCode == http.StatusUnauthorized {
						checks = append(checks, doctorCheck{"authentication", "failed", "bearer token rejected by control plane"})
					} else {
						checks = append(checks, doctorCheck{"authentication", "error", fmt.Sprintf("HTTP %s", http.StatusText(resp.StatusCode))})
					}
				}
			}

			header := []string{"Check", "Status", "Detail"}
			rows := make([][]string, 0, len(checks))
			for _, c := range checks {
				rows = append(rows, []string{c.name, c.status, c.detail})
			}
			if err := w.Table(header, rows); err != nil {
				return err
			}

			allOK := healthOK && readyOK && (authed || cfg.Token == "")
			if !allOK {
				fmt.Fprintln(w.ErrWriter(), "One or more checks failed. See above.")
				return fmt.Errorf("doctor: control plane not fully reachable")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	return cmd
}
