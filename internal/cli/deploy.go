package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Bunn-Burrow/sandctl/internal/client"
	"github.com/Bunn-Burrow/sandctl/internal/config"
)

// waitForDeployment polls a deployment until it reaches the Running phase or
// the timeout expires. It returns an error on failure or timeout.
func waitForDeployment(
	ctx context.Context,
	c *client.Client,
	deployName string,
	timeout time.Duration,
) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		resp, err := c.Get(
			ctx,
			"/v1/agentdeployments/"+deployName,
		)
		if err != nil {
			return fmt.Errorf("poll deployment %q: %w", deployName, err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			var depInfo struct {
				Status *struct {
					Phase   *string `json:"phase"`
					Message string  `json:"message,omitempty"`
					Reason  string  `json:"reason,omitempty"`
				} `json:"status"`
			}

			err := client.DecodeJSON(resp, &depInfo)
			if err != nil {
				return fmt.Errorf("decode deployment %q: %w", deployName, err)
			}

			if depInfo.Status == nil || depInfo.Status.Phase == nil {
				return fmt.Errorf("deployment %q has no lifecycle phase", deployName)
			}

			switch *depInfo.Status.Phase {
			case "Running":
				return nil
			case "Failed":
				return fmt.Errorf(
					"deployment %q failed: %s %s",
					deployName,
					depInfo.Status.Reason,
					depInfo.Status.Message,
				)
			}
		default:
			resp.Body.Close()
			return fmt.Errorf(
				"poll deployment %q: unexpected HTTP status %d",
				deployName,
				resp.StatusCode,
			)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf(
				"timed out after %s waiting for deployment %q to become Running",
				timeout,
				deployName,
			)
		case <-ticker.C:
		}
	}
}

// deploymentConfig holds parameters to ensure an agent and deployment exist
// and are running before an execution can be submitted.
type deploymentConfig struct {
	agentName     string
	deployName    string
	imageRef      string
	provider      string
	model         string
	credentialRef string
	timeoutSec    int
}

// ensureDeploymentRunning checks that an agent exists (creating it if needed)
// and that a deployment for that agent is Running, waiting up to the given
// timeout. It returns the deployment name.
func ensureDeploymentRunning(
	ctx context.Context,
	c *client.Client,
	out io.Writer,
	cfg deploymentConfig,
) (string, error) {
	// Step 1: Ensure agent exists.
	agentResp, err := c.Get(ctx, "/v1/agents/"+cfg.agentName)
	if err != nil {
		return "", fmt.Errorf("check agent %q: %w", cfg.agentName, err)
	}

	needCreateAgent := false

	switch agentResp.StatusCode {
	case http.StatusNotFound:
		agentResp.Body.Close()
		needCreateAgent = true

	case http.StatusOK:
		var existingAgent struct {
			Spec struct {
				Image struct {
					Reference string `json:"reference"`
				} `json:"image"`
				Model *struct {
					Provider string `json:"provider"`
					Model    string `json:"model"`
				} `json:"model"`
			} `json:"spec"`
		}
		if err := client.DecodeJSON(agentResp, &existingAgent); err != nil {
			return "", fmt.Errorf("decode existing agent %q: %w", cfg.agentName, err)
		}

		if existingAgent.Spec.Image.Reference != cfg.imageRef ||
			existingAgent.Spec.Model == nil ||
			existingAgent.Spec.Model.Provider != cfg.provider ||
			existingAgent.Spec.Model.Model != cfg.model {
			return "", fmt.Errorf(
				"agent %q already exists with a different image or model configuration; use a different --agent name",
				cfg.agentName,
			)
		}

	default:
		agentResp.Body.Close()
		return "", fmt.Errorf("check agent %q: unexpected HTTP status %d", cfg.agentName, agentResp.StatusCode)
	}

	if needCreateAgent {
		fmt.Fprintf(out, "  Registering agent %q...\n", cfg.agentName)
		agentPayload := map[string]any{
			"apiVersion": "agentplatform.io/v1alpha1",
			"kind":       "Agent",
			"metadata": map[string]any{
				"name": cfg.agentName,
			},
			"spec": map[string]any{
				"image": map[string]any{
					"reference": cfg.imageRef,
				},
				"model": map[string]any{
					"provider": cfg.provider,
					"model":    cfg.model,
					"credentialRef": map[string]any{
						"name": cfg.credentialRef,
					},
				},
			},
		}
		createResp, err := c.Post(ctx, "/v1/agents", agentPayload)
		if err != nil {
			return "", fmt.Errorf("create agent %q: %w", cfg.agentName, err)
		}
		createResp.Body.Close()
		if createResp.StatusCode >= 300 && createResp.StatusCode != http.StatusConflict {
			return "", fmt.Errorf("create agent %q returned HTTP %d", cfg.agentName, createResp.StatusCode)
		}
	}

	// Step 2: Ensure deployment exists and is running.
	depResp, err := c.Get(ctx, "/v1/agentdeployments/"+cfg.deployName)
	if err != nil {
		return "", fmt.Errorf("check deployment %q: %w", cfg.deployName, err)
	}
	needCreateDep := false

	switch depResp.StatusCode {
	case http.StatusNotFound:
		depResp.Body.Close()
		needCreateDep = true

	case http.StatusOK:
		var depInfo struct {
			Status *struct {
				Phase   *string `json:"phase"`
				Message string  `json:"message,omitempty"`
				Reason  string  `json:"reason,omitempty"`
			} `json:"status"`
		}
		if err := client.DecodeJSON(depResp, &depInfo); err != nil {
			return "", fmt.Errorf("decode deployment %q: %w", cfg.deployName, err)
		}

		if depInfo.Status == nil || depInfo.Status.Phase == nil {
			return "", fmt.Errorf("deployment %q has no lifecycle phase", cfg.deployName)
		}

		if *depInfo.Status.Phase != "Running" {
			fmt.Fprintf(out, "  Waiting for deployment %q to become Running...\n", cfg.deployName)
			if err := waitForDeployment(ctx, c, cfg.deployName, time.Duration(cfg.timeoutSec)*time.Second); err != nil {
				return "", err
			}
		}

	default:
		depResp.Body.Close()
		return "", fmt.Errorf("check deployment %q: unexpected HTTP status %d", cfg.deployName, depResp.StatusCode)
	}

	if needCreateDep {
		fmt.Fprintf(out, "  Launching sandbox deployment %q...\n", cfg.deployName)
		depPayload := map[string]any{
			"apiVersion": "agentplatform.io/v1alpha1",
			"kind":       "AgentDeployment",
			"metadata": map[string]any{
				"name": cfg.deployName,
			},
			"spec": map[string]any{
				"agentRef": map[string]any{
					"apiVersion": "agentplatform.io/v1alpha1",
					"kind":       "Agent",
					"name":       cfg.agentName,
				},
			},
		}
		startDepResp, err := c.Post(ctx, "/v1/agentdeployments", depPayload)
		if err != nil {
			return "", fmt.Errorf("create deployment %q: %w", cfg.deployName, err)
		}
		startDepResp.Body.Close()
		if startDepResp.StatusCode >= 300 {
			return "", fmt.Errorf("deployment %q failed to start: HTTP %d", cfg.deployName, startDepResp.StatusCode)
		}

		fmt.Fprintf(out, "  Waiting for deployment %q to become Running...\n", cfg.deployName)
		if err := waitForDeployment(ctx, c, cfg.deployName, time.Duration(cfg.timeoutSec)*time.Second); err != nil {
			return "", err
		}
	}

	return cfg.deployName, nil
}

// runtimeFromExtension detects the runtime from a file extension.
func runtimeFromExtension(path string) (string, bool) {
	ext := strings.ToLower(fileExt(path))
	switch ext {
	case "py":
		return "python", true
	case "go":
		return "go", true
	case "java":
		return "java", true
	case "js", "mjs", "cjs":
		return "javascript", true
	case "ts", "mts", "cts":
		return "typescript", true
	case "c":
		return "c", true
	case "cpp", "cc", "cxx", "hpp", "hxx":
		return "cpp", true
	case "rs":
		return "rust", true
	case "cs":
		return "csharp", true
	case "sh":
		return "shell", true
	default:
		return "", false
	}
}

// fileExt returns the extension without the leading dot.
func fileExt(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx <= 0 || idx == len(path)-1 {
		return ""
	}
	return path[idx+1:]
}

// readSource reads source code from a file path, stdin ("-"), or "--code".
func readSource(path string, code string) (string, error) {
	if code != "" {
		return code, nil
	}
	if path == "-" || path == "" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read source file %q: %w", path, err)
	}
	return string(data), nil
}

// loadConfig resolves the sandctl configuration with the given server override.
func loadConfig(serverOverride string) (config.Config, error) {
	return config.Load(serverOverride)
}

// submissionResult is the decoded execution response from the control plane.
type submissionResult struct {
	ID     string          `json:"id"`
	Task   string          `json:"task"`
	Phase  string          `json:"phase"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// submitExecution submits a code execution task and returns the decoded result.
func submitExecution(
	ctx context.Context,
	c *client.Client,
	deploymentName string,
	task string,
	input map[string]any,
	timeoutSec int,
) (*submissionResult, error) {
	execPayload := map[string]any{
		"deploymentName": deploymentName,
		"task":           task,
		"input":          input,
		"timeoutSeconds": timeoutSec,
	}
	resp, err := c.Post(ctx, "/internal/v1/executions", execPayload)
	if err != nil {
		return nil, fmt.Errorf("submit execution: %w", err)
	}
	defer resp.Body.Close()

	var result submissionResult
	if err := client.DecodeJSON(resp, &result); err != nil {
		return nil, fmt.Errorf("decode execution response: %w", err)
	}
	return &result, nil
}

// executionStatus is the decoded execution status from GET /internal/v1/executions/{id}.
type executionStatus struct {
	ID            string          `json:"id"`
	DeploymentRef deploymentRef   `json:"deploymentRef"`
	SandboxRef    deploymentRef   `json:"sandboxRef"`
	Task          string          `json:"task"`
	Input         map[string]any  `json:"input,omitempty"`
	Phase         string          `json:"phase"`
	SubmittedAt   string          `json:"submittedAt"`
	DeadlineAt    string          `json:"deadlineAt"`
	StartedAt     *string         `json:"startedAt,omitempty"`
	CompletedAt   *string         `json:"completedAt,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	Error         string          `json:"error,omitempty"`
}

type deploymentRef struct {
	ApiVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// codeExecutionResult is the structured result carried in TaskStatus.Result.
type codeExecutionResult struct {
	ExitCode        int    `json:"exitCode"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	ExecutionTimeMs int64  `json:"executionTimeMs"`
	TimedOut        bool   `json:"timedOut,omitempty"`
	Cancelled       bool   `json:"cancelled,omitempty"`
	StdoutTruncated bool   `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool   `json:"stderrTruncated,omitempty"`
	CompileError    string `json:"compileError,omitempty"`
	Error           string `json:"error,omitempty"`
}

// llmAgentResult is the LLM agent's result wrapper, which carries the code
// execution result inside an "execution" field alongside code/model metadata.
type llmAgentResult struct {
	Code      string               `json:"code,omitempty"`
	Model     string               `json:"model,omitempty"`
	Content   string               `json:"content,omitempty"`
	Execution *codeExecutionResult `json:"execution,omitempty"`
}

// getExecution fetches the current status of an execution.
func getExecution(ctx context.Context, c *client.Client, id string) (*executionStatus, error) {
	resp, err := c.Get(ctx, "/internal/v1/executions/"+id)
	if err != nil {
		return nil, fmt.Errorf("get execution %q: %w", id, err)
	}
	defer resp.Body.Close()

	var status executionStatus
	if err := client.DecodeJSON(resp, &status); err != nil {
		return nil, fmt.Errorf("decode execution status: %w", err)
	}
	return &status, nil
}

// cancelExecution requests cancellation of an execution.
func cancelExecution(ctx context.Context, c *client.Client, id string) error {
	resp, err := c.Delete(ctx, "/internal/v1/executions/"+id)
	if err != nil {
		return fmt.Errorf("cancel execution %q: %w", id, err)
	}
	defer resp.Body.Close()
	if err := client.DecodeJSON(resp, nil); err != nil {
		return fmt.Errorf("decode cancel response: %w", err)
	}
	return nil
}

// getExecutionLogs fetches logs for an execution.
func getExecutionLogs(ctx context.Context, c *client.Client, id string) ([]byte, error) {
	resp, err := c.Get(ctx, "/internal/v1/executions/"+id+"/logs")
	if err != nil {
		return nil, fmt.Errorf("get execution logs %q: %w", id, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read execution logs: %w", err)
	}
	return data, nil
}

// terminalPhase reports whether an execution phase is terminal.
func terminalPhase(phase string) bool {
	switch phase {
	case "Succeeded", "Failed", "TimedOut", "Cancelled":
		return true
	}
	return false
}

// parseCodeExecutionResult decodes a CodeExecutionResult from JSON. The
// control plane may return the result directly or wrapped in an LLM agent
// result envelope with an "execution" field.
func parseCodeExecutionResult(data json.RawMessage) (*codeExecutionResult, error) {
	if len(data) == 0 {
		return &codeExecutionResult{}, nil
	}
	// First try the LLM agent wrapper format.
	var wrapped llmAgentResult
	if err := json.Unmarshal(data, &wrapped); err == nil && wrapped.Execution != nil {
		return wrapped.Execution, nil
	}
	// Fall back to direct codeExecutionResult parsing.
	var res codeExecutionResult
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("decode execution result: %w", err)
	}
	return &res, nil
}
