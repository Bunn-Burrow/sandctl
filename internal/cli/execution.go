package cli

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/client"
	"github.com/Bunn-Burrow/sandctl/internal/config"
	"github.com/Bunn-Burrow/sandctl/internal/output"
)

func newExecutionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execution",
		Short: "Inspect and manage code executions",
		Long: `Inspect and manage code executions in the Bunn Burrow control plane.

Executions are created by sandctl run (or by the agent command) and run
against a sandbox deployment. Use these commands to check status, wait for
completion, fetch logs, or cancel an in-flight execution.

Examples:
  sandctl execution get exec_abc123
  sandctl execution wait exec_abc123 --timeout 300
  sandctl execution logs exec_abc123
  sandctl execution cancel exec_abc123`,
	}

	cmd.AddCommand(newExecutionGetCommand())
	cmd.AddCommand(newExecutionWaitCommand())
	cmd.AddCommand(newExecutionLogsCommand())
	cmd.AddCommand(newExecutionCancelCommand())

	return cmd
}

func newExecutionGetCommand() *cobra.Command {
	var (
		server  string
		outputF string
	)

	cmd := &cobra.Command{
		Use:   "get <execution-id>",
		Short: "Get execution status and result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			status, err := getExecution(cmd.Context(), c, args[0])
			if err != nil {
				return handleError(err)
			}

			w := output.NewWriter(parseFormat(outputF))
			if outputF == "json" || outputF == "yaml" {
				return w.JSON(status)
			}

			rows := [][2]string{
				{"ID", status.ID},
				{"Phase", status.Phase},
				{"Task", status.Task},
				{"Deployment", status.DeploymentRef.Name},
				{"Sandbox", status.SandboxRef.Name},
				{"Submitted at", status.SubmittedAt},
				{"Deadline at", status.DeadlineAt},
			}
			if status.StartedAt != nil {
				rows = append(rows, [2]string{"Started at", *status.StartedAt})
			}
			if status.CompletedAt != nil {
				rows = append(rows, [2]string{"Completed at", *status.CompletedAt})
			}
			if status.Error != "" {
				rows = append(rows, [2]string{"Error", status.Error})
			}

			res, err := parseCodeExecutionResult(status.Result)
			if err != nil {
				return err
			}
			if res.ExitCode != 0 {
				rows = append(rows, [2]string{"Exit code", fmt.Sprintf("%d", res.ExitCode)})
			}
			if res.ExecutionTimeMs > 0 {
				rows = append(rows, [2]string{"Duration", fmt.Sprintf("%dms", res.ExecutionTimeMs)})
			}
			if res.TimedOut {
				rows = append(rows, [2]string{"Timed out", "yes"})
			}
			if res.Cancelled {
				rows = append(rows, [2]string{"Cancelled", "yes"})
			}
			if res.CompileError != "" {
				rows = append(rows, [2]string{"Compile error", res.CompileError})
			}

			return w.Object(rows)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	cmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	return cmd
}

func newExecutionWaitCommand() *cobra.Command {
	var (
		server       string
		outputF      string
		timeoutSec   int
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "wait <execution-id>",
		Short: "Wait for an execution to reach a terminal phase",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

			poll := pollInterval
			if poll <= 0 {
				poll = 500 * time.Millisecond
			}

			execTimeout := timeoutSec
			if execTimeout <= 0 {
				execTimeout = int(cfg.Timeout.Seconds())
			}

			deadline := time.Now().Add(time.Duration(execTimeout) * time.Second)
			ticker := time.NewTicker(poll)
			defer ticker.Stop()

			for {
				if time.Now().After(deadline) {
					return fmt.Errorf("execution %s timed out after %ds waiting for terminal phase", args[0], execTimeout)
				}

				status, err := getExecution(ctx, c, args[0])
				if err != nil {
					if client.StatusCode(err) == http.StatusNotFound {
						return fmt.Errorf("execution %s not found", args[0])
					}
					return handleError(err)
				}

				if terminalPhase(status.Phase) {
					w := output.NewWriter(parseFormat(outputF))
					if outputF == "json" || outputF == "yaml" {
						return w.JSON(status)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Execution %s reached terminal phase: %s\n", args[0], status.Phase)
					if status.Error != "" {
						fmt.Fprintf(cmd.OutOrStdout(), "Error: %s\n", status.Error)
					}
					switch status.Phase {
					case "Succeeded":
						return nil
					case "Failed":
						return fmt.Errorf("execution %s failed", args[0])
					case "TimedOut":
						return fmt.Errorf("execution %s timed out", args[0])
					case "Cancelled":
						return fmt.Errorf("execution %s was cancelled", args[0])
					default:
						return fmt.Errorf("execution %s ended in unexpected phase %s", args[0], status.Phase)
					}
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	cmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 0, "Wait timeout in seconds (default: CLI timeout)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 0, "Status polling interval (default 500ms)")
	return cmd
}

func newExecutionLogsCommand() *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "logs <execution-id>",
		Short: "Fetch execution logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			data, err := getExecutionLogs(cmd.Context(), c, args[0])
			if err != nil {
				return handleError(err)
			}

			fmt.Print(string(data))
			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	return cmd
}

func newExecutionCancelCommand() *cobra.Command {
	var (
		server string
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "cancel <execution-id>",
		Short: "Cancel a running execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := confirmDestructive(yes, fmt.Sprintf("cancel execution %q", args[0])); err != nil {
				return err
			}
			cfg, err := config.Load(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			if err := cancelExecution(cmd.Context(), c, args[0]); err != nil {
				return handleError(err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Execution %s cancelled.\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	return cmd
}
