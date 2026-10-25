package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Bunn-Burrow/sandctl/internal/client"
)

func TestSubmitExecution(t *testing.T) {
	var received SubmitRequestShape
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tok" {
				t.Errorf("Authorization = %q, want Bearer tok", got)
			}
			body, _ := io.ReadAll(r.Body)
			json.Unmarshal(body, &received)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"id":"exec_abc123","task":"python","phase":"Pending"}`)
		},
	))
	defer server.Close()

	c := client.New(server.URL, "tok", time.Second)
	input := map[string]any{"source": "print(1)", "args": []string{"--flag"}}
	result, err := submitExecution(context.Background(), c, "dep-1", "python", input, 30)
	if err != nil {
		t.Fatalf("submitExecution error: %v", err)
	}
	if result.ID != "exec_abc123" || result.Task != "python" || result.Phase != "Pending" {
		t.Errorf("unexpected result: %+v", result)
	}
	if received.DeploymentName != "dep-1" || received.Task != "python" || received.TimeoutSeconds != 30 {
		t.Errorf("unexpected request: %+v", received)
	}
}

func TestGetExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("method = %s, want GET", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"id":"exec_abc123","phase":"Succeeded","task":"python","deploymentRef":{"name":"dep-1"}}`)
		},
	))
	defer server.Close()

	c := client.New(server.URL, "tok", time.Second)
	status, err := getExecution(context.Background(), c, "exec_abc123")
	if err != nil {
		t.Fatalf("getExecution error: %v", err)
	}
	if status.ID != "exec_abc123" || status.Phase != "Succeeded" || status.DeploymentRef.Name != "dep-1" {
		t.Errorf("unexpected status: %+v", status)
	}
}

func TestGetExecutionNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"code":"NOT_FOUND","message":"execution not found"}`)
		},
	))
	defer server.Close()

	c := client.New(server.URL, "tok", time.Second)
	_, err := getExecution(context.Background(), c, "missing")
	if err == nil {
		t.Fatal("expected error for not found")
	}
	if client.StatusCode(err) != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", client.StatusCode(err))
	}
}

func TestCancelExecution(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				t.Errorf("method = %s, want DELETE", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"id":"exec_abc123","phase":"Cancelled"}`)
		},
	))
	defer server.Close()

	c := client.New(server.URL, "tok", time.Second)
	if err := cancelExecution(context.Background(), c, "exec_abc123"); err != nil {
		t.Fatalf("cancelExecution error: %v", err)
	}
}

func TestGetExecutionLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"entries":["line1","line2"]}`)
		},
	))
	defer server.Close()

	c := client.New(server.URL, "tok", time.Second)
	data, err := getExecutionLogs(context.Background(), c, "exec_abc123")
	if err != nil {
		t.Fatalf("getExecutionLogs error: %v", err)
	}
	if !strings.Contains(string(data), "line1") {
		t.Errorf("expected logs to contain 'line1', got %q", string(data))
	}
}

// SubmitRequestShape mirrors the control plane's execution.SubmitRequest for
// test assertions.
type SubmitRequestShape struct {
	DeploymentName string         `json:"deploymentName"`
	Task           string         `json:"task"`
	Input          map[string]any `json:"input,omitempty"`
	TimeoutSeconds int            `json:"timeoutSeconds,omitempty"`
}
