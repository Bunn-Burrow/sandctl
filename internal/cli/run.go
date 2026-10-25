package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Bunn-Burrow/sandctl/internal/client"
	"github.com/Bunn-Burrow/sandctl/internal/output"
)

func newRunCommand() *cobra.Command {
	var (
		server        string
		outputF       string
		runtime       string
		deployment    string
		agent         string
		image         string
		code          string
		timeoutSec    int
		pollInterval  time.Duration
		args          []string
		noWait        bool
		credentialRef string
	)

	cmd := &cobra.Command{
		Use:   "run [file]",
		Short: "Run source code in an isolated sandbox",
		Long: `Run source code in an isolated Bunn Burrow sandbox.

The source is read from a file, stdin ("-"), or --code. The runtime is
detected from the file extension or set explicitly with --runtime. The
execution runs against a running agent deployment (auto-created if needed).

Supported runtimes: python, go, java, javascript, typescript, c, cpp, rust, csharp, shell.

Examples:
  sandctl run main.py
  sandctl run --runtime go main.go
  sandctl run --runtime python --code 'print("hello")'
  echo 'print("hello")' | sandctl run --runtime python -
  sandctl run --runtime rust main.rs --timeout 120 --output json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var path string
			if len(args) > 0 {
				path = args[0]
			}

			source, err := readSource(path, code)
			if err != nil {
				return err
			}
			if strings.TrimSpace(source) == "" {
				return fmt.Errorf("source code is empty")
			}

			rt := runtime
			if rt == "" && path != "" && path != "-" {
				detected, ok := runtimeFromExtension(path)
				if !ok {
					return fmt.Errorf("cannot detect runtime from extension of %q; use --runtime", path)
				}
				rt = detected
			}
			if rt == "" {
				return fmt.Errorf("--runtime is required when reading from stdin or --code")
			}

			// Normalize aliases.
			rt = normalizeRuntime(rt)

			// Enforce source-size limits before submission.
			if len(source) > maxSourceBytes {
				return fmt.Errorf("source code exceeds maximum size of %d bytes", maxSourceBytes)
			}

			// Build input payload. Code execution tasks use the "source" key;
			// the guest agent parses it per-runtime.
			input := map[string]any{"source": source}
			if len(args) > 0 {
				input["args"] = args
			}

			cfg, err := loadConfig(server)
			if err != nil {
				return err
			}
			c := newClient(cfg)

			// Health check.
			healthResp, err := c.Get(cmd.Context(), "/health")
			if err != nil {
				return fmt.Errorf("cannot contact control plane at %s: %w\nEnsure the control plane is running.", cfg.Server, err)
			}
			healthResp.Body.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

			// Determine agent/deployment names.
			agentName := agent
			if agentName == "" {
				agentName = "sandctl-run-" + shortID()
			}
			deployName := deployment
			if deployName == "" {
				deployName = agentName + "-deployment"
			}

			dcfg := deploymentConfig{
				agentName:     agentName,
				deployName:    deployName,
				imageRef:      image,
				provider:      "openrouter",
				model:         "openai/gpt-4o-mini",
				credentialRef: credentialRef,
				timeoutSec:    timeoutSec,
			}
			if dcfg.imageRef == "" {
				dcfg.imageRef = defaultAgentImage
			}
			if dcfg.timeoutSec <= 0 {
				dcfg.timeoutSec = 120
			}

			deployNameOut, err := ensureDeploymentRunning(cmd.Context(), c, cmd.OutOrStdout(), dcfg)
			if err != nil {
				return err
			}

			// Step 4: Submit execution.
			fmt.Fprintf(cmd.OutOrStdout(), "  Submitting execution to deployment %q...\n", deployNameOut)
			execTimeout := timeoutSec
			if execTimeout <= 0 {
				execTimeout = dcfg.timeoutSec
			}

			result, err := submitExecution(ctx, c, deployNameOut, rt, input, execTimeout)
			if err != nil {
				return handleError(err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "  Execution %s submitted (phase: %s)\n", result.ID, result.Phase)

			// Step 5: Poll for result (unless --no-wait).
			if noWait {
				w := output.NewWriter(parseFormat(outputF))
				if outputF == "json" || outputF == "yaml" {
					return w.JSON(result)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\nPhase: %s\n", result.ID, result.Phase)
				return nil
			}

			poll := pollInterval
			if poll <= 0 {
				poll = 500 * time.Millisecond
			}

			deadline := time.Now().Add(time.Duration(execTimeout) * time.Second)
			ticker := time.NewTicker(poll)
			defer ticker.Stop()

			var final *executionStatus
			for {
				if time.Now().After(deadline) {
					return fmt.Errorf("execution %s timed out after %ds waiting for terminal phase", result.ID, execTimeout)
				}

				status, err := getExecution(ctx, c, result.ID)
				if err != nil {
					if client.StatusCode(err) == http.StatusNotFound {
						return fmt.Errorf("execution %s not found", result.ID)
					}
					return handleError(err)
				}
				final = status

				if terminalPhase(status.Phase) {
					break
				}

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				}
			}

			// Step 6: Display results.
			w := output.NewWriter(parseFormat(outputF))
			if outputF == "json" || outputF == "yaml" {
				return w.JSON(final)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "\nExecution %s\n", final.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "  Phase:     %s\n", final.Phase)
			fmt.Fprintf(cmd.OutOrStdout(), "  Task:      %s\n", final.Task)
			fmt.Fprintf(cmd.OutOrStdout(), "  Runtime:   %s\n", rt)
			if final.StartedAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  Started:   %s\n", *final.StartedAt)
			}
			if final.CompletedAt != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "  Completed: %s\n", *final.CompletedAt)
			}

			res, err := parseCodeExecutionResult(final.Result)
			if err != nil {
				return err
			}

			if res.ExitCode != 0 || res.Error != "" || res.CompileError != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Exit code: %d\n", res.ExitCode)
			}
			if res.ExecutionTimeMs > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "  Duration:  %dms\n", res.ExecutionTimeMs)
			}
			if res.TimedOut {
				fmt.Fprintf(cmd.OutOrStdout(), "  Timed out: yes\n")
			}
			if res.Cancelled {
				fmt.Fprintf(cmd.OutOrStdout(), "  Cancelled: yes\n")
			}
			if res.CompileError != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Compile error: %s\n", res.CompileError)
			}
			if res.Error != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  Error: %s\n", res.Error)
			}

			if res.Stdout != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\n--- stdout ---\n%s", res.Stdout)
				if !strings.HasSuffix(res.Stdout, "\n") {
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}
			if res.Stderr != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\n--- stderr ---\n%s", res.Stderr)
				if !strings.HasSuffix(res.Stderr, "\n") {
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}

			// Nonzero exit code on failure.
			switch final.Phase {
			case "Succeeded":
				if res.ExitCode != 0 {
					return fmt.Errorf("execution %s failed with exit code %d", result.ID, res.ExitCode)
				}
				return nil
			case "Failed":
				return fmt.Errorf("execution %s failed: %s", result.ID, final.Error)
			case "TimedOut":
				return fmt.Errorf("execution %s timed out", result.ID)
			case "Cancelled":
				return fmt.Errorf("execution %s was cancelled", result.ID)
			default:
				return fmt.Errorf("execution %s ended in unexpected phase %s", result.ID, final.Phase)
			}
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "Control plane URL (overrides configuration)")
	cmd.Flags().StringVarP(&outputF, "output", "o", "table", "Output format: table, json, yaml")
	cmd.Flags().StringVar(&runtime, "runtime", "", "Runtime (python, go, java, javascript, typescript, c, cpp, rust, csharp, shell)")
	cmd.Flags().StringVar(&deployment, "deployment", "", "Target deployment name (auto-created if omitted)")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent name (auto-generated if omitted)")
	cmd.Flags().StringVar(&image, "image", "", "Agent sandbox image reference")
	cmd.Flags().StringVar(&code, "code", "", "Inline source code (alternative to file/stdin)")
	cmd.Flags().IntVar(&timeoutSec, "timeout", 0, "Execution timeout in seconds")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 0, "Status polling interval (default 500ms)")
	cmd.Flags().StringSliceVar(&args, "arg", nil, "Argument to pass to the program (repeatable)")
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "Submit and exit without waiting for completion")
	cmd.Flags().StringVar(&credentialRef, "credential-ref", "bootstrap", "Model credential reference")

	return cmd
}

const (
	defaultAgentImage = "sandbox-llm-agent:local"
	maxSourceBytes    = 256 << 10 // 256 KiB, matches guest agent limits
)

// shortID returns a short random identifier for auto-generated names.
func shortID() string {
	b := make([]byte, 4)
	// Use os.ReadFile to read random bytes without importing crypto/rand
	// in this helper; fall back to a timestamp-based ID on failure.
	if f, err := os.Open("/dev/urandom"); err == nil {
		if _, err := f.Read(b); err == nil {
			f.Close()
			return fmt.Sprintf("%x", b)
		}
		f.Close()
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// normalizeRuntime maps common aliases to canonical runtime names.
func normalizeRuntime(rt string) string {
	switch strings.ToLower(strings.TrimSpace(rt)) {
	case "python", "python3":
		return "python"
	case "go", "golang":
		return "go"
	case "java":
		return "java"
	case "js", "node", "nodejs", "javascript":
		return "javascript"
	case "ts", "typescript":
		return "typescript"
	case "c":
		return "c"
	case "cpp", "c++", "cc", "cxx":
		return "cpp"
	case "rs", "rust":
		return "rust"
	case "cs", "csharp", "dotnet":
		return "csharp"
	case "sh", "bash", "shell":
		return "shell"
	default:
		return rt
	}
}
