package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetSendsBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
				t.Errorf("Authorization = %q, want Bearer test-token", got)
			}
			if r.URL.Path != "/health" {
				t.Errorf("path = %q, want /health", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		},
	))
	defer server.Close()

	c := New(server.URL, "test-token", time.Second)

	resp, err := c.Get(context.Background(), "/health")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestGetRejectsUnreachableServer(t *testing.T) {
	c := New("http://127.0.0.1:1", "", 200*time.Millisecond)

	resp, err := c.Get(context.Background(), "/health")
	if err == nil {
		if resp != nil {
			resp.Body.Close()
		}
		t.Fatal("expected connection error")
	}
	if StatusCode(err) != 0 {
		t.Errorf("StatusCode(transport error) = %d, want 0", StatusCode(err))
	}
}

func TestDecodeJSONServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"code":"UNAUTHENTICATED","message":"a valid bearer token is required","retryable":false}`)
		},
	))
	defer server.Close()

	c := New(server.URL, "bad-token", time.Second)
	resp, err := c.Get(context.Background(), "/v1/agents")
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}

	var dst any
	err = DecodeJSON(resp, &dst)
	if err == nil {
		t.Fatal("expected DecodeJSON error for 401")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.Code != "UNAUTHENTICATED" {
		t.Errorf("Code = %q, want UNAUTHENTICATED", apiErr.Code)
	}
	if apiErr.IsUnauthorized() != true {
		t.Error("IsUnauthorized() = false, want true")
	}
}

func TestDecodeJSONNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"code":"NOT_FOUND","message":"resource not found"}`)
		},
	))
	defer server.Close()

	c := New(server.URL, "token", time.Second)
	resp, err := c.Get(context.Background(), "/v1/agents/missing")
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}

	var dst any
	err = DecodeJSON(resp, &dst)
	if err == nil {
		t.Fatal("expected DecodeJSON error for 404")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if !apiErr.IsNotFound() {
		t.Error("IsNotFound() = false, want true")
	}
	if apiErr.Code != "NOT_FOUND" {
		t.Errorf("Code = %q, want NOT_FOUND", apiErr.Code)
	}
}

func TestDecodeJSONSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"id":"exec_abc123","phase":"Succeeded","task":"python"}`)
		},
	))
	defer server.Close()

	c := New(server.URL, "token", time.Second)
	resp, err := c.Post(context.Background(), "/internal/v1/executions", map[string]any{"task": "python"})
	if err != nil {
		t.Fatalf("unexpected transport error: %v", err)
	}

	var result struct {
		ID    string `json:"id"`
		Phase string `json:"phase"`
		Task  string `json:"task"`
	}
	if err := DecodeJSON(resp, &result); err != nil {
		t.Fatalf("DecodeJSON error: %v", err)
	}
	if result.ID != "exec_abc123" || result.Phase != "Succeeded" || result.Task != "python" {
		t.Errorf("unexpected result: %+v", result)
	}
}

func TestPostSendsJSONBody(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			if got := r.Header.Get("Authorization"); got != "Bearer my-token" {
				t.Errorf("Authorization = %q, want Bearer my-token", got)
			}
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &received); err != nil {
				t.Errorf("failed to read request body: %v", err)
			}
			w.WriteHeader(http.StatusAccepted)
			io.WriteString(w, `{"id":"exec_1"}`)
		},
	))
	defer server.Close()

	c := New(server.URL, "my-token", time.Second)
	resp, err := c.Post(context.Background(), "/internal/v1/executions", map[string]any{
		"deploymentName": "dep-1",
		"task":           "python",
		"input":          map[string]any{"source": "print(1)"},
		"timeoutSeconds": 30,
	})
	if err != nil {
		t.Fatalf("Post error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
	if received["deploymentName"] != "dep-1" || received["task"] != "python" {
		t.Errorf("unexpected received body: %+v", received)
	}
}

func TestStatusCodeHelpers(t *testing.T) {
	tests := []struct {
		code int
		want bool
		fn   func(*Error) bool
	}{
		{401, true, func(e *Error) bool { return e.IsUnauthorized() }},
		{404, true, func(e *Error) bool { return e.IsNotFound() }},
		{409, true, func(e *Error) bool { return e.IsConflict() }},
		{429, true, func(e *Error) bool { return e.IsRateLimited() }},
		{503, true, func(e *Error) bool { return e.IsUnavailable() }},
		{200, false, func(e *Error) bool { return e.IsUnauthorized() }},
	}
	for _, tt := range tests {
		e := &Error{StatusCode: tt.code}
		if got := tt.fn(e); got != tt.want {
			t.Errorf("status %d: helper = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestErrorMessage(t *testing.T) {
	e := &Error{StatusCode: 500, Code: "INTERNAL", Message: "something broke"}
	got := e.Error()
	if !strings.Contains(got, "INTERNAL") || !strings.Contains(got, "500") || !strings.Contains(got, "something broke") {
		t.Errorf("unexpected error message: %q", got)
	}
}
