package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Error is a typed error returned by the control plane. It carries the HTTP
// status code and, when the server returns a PlatformError body, the platform
// error code and message so callers can distinguish authentication, not found,
// rate-limited, and service-unavailable failures without parsing bodies.
type Error struct {
	StatusCode int
	Code       string
	Message    string
	Retryable  bool
	RequestID  string
	RawBody    []byte
	underlying error
}

func (e *Error) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("API %s (HTTP %d): %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API returned HTTP %d: %s", e.StatusCode, string(e.RawBody))
}

func (e *Error) Unwrap() error { return e.underlying }

func (e *Error) Is(target error) bool {
	if target == nil {
		return false
	}
	if t, ok := target.(*Error); ok {
		return e.StatusCode == t.StatusCode && e.Code == t.Code
	}
	return false
}

// Status helpers for common classifications.
func (e *Error) IsUnauthorized() bool { return e.StatusCode == http.StatusUnauthorized }
func (e *Error) IsNotFound() bool     { return e.StatusCode == http.StatusNotFound }
func (e *Error) IsConflict() bool     { return e.StatusCode == http.StatusConflict }
func (e *Error) IsRateLimited() bool  { return e.StatusCode == http.StatusTooManyRequests }
func (e *Error) IsUnavailable() bool {
	return e.StatusCode >= 500 || e.StatusCode == http.StatusServiceUnavailable
}

// platformError matches the control plane's PlatformError response shape.
type platformError struct {
	Code      string  `json:"code"`
	Message   string  `json:"message"`
	RequestID *string `json:"requestId,omitempty"`
	Retryable bool    `json:"retryable"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

func New(baseURL, token string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// A nil response with a non-nil error means the request never reached
		// the server (connection refused, timeout, TLS failure, etc.).
		return nil, &Error{
			StatusCode: 0,
			Message:    err.Error(),
			underlying: err,
		}
	}
	return resp, nil
}

func (c *Client) Get(ctx context.Context, path string) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Post(ctx context.Context, path string, body any) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Put(ctx context.Context, path string, body any) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Delete(ctx context.Context, path string) (*http.Response, error) {
	req, err := c.newRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

// DecodeJSON validates the response status and decodes the JSON body into dst.
// Non-2xx responses are surfaced as a typed *Error including the platform
// error code when the server supplies one. The caller must not close resp.Body.
func DecodeJSON(resp *http.Response, dst any) error {
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return &Error{StatusCode: resp.StatusCode, Message: fmt.Sprintf("read response body: %v", err), underlying: err}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errorFromResponse(resp.StatusCode, body)
	}

	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return &Error{StatusCode: resp.StatusCode, Message: fmt.Sprintf("decode API response: %v", err), underlying: err, RawBody: body}
	}
	return nil
}

func errorFromResponse(statusCode int, body []byte) *Error {
	err := &Error{StatusCode: statusCode, RawBody: body}
	var pe platformError
	if json.Unmarshal(body, &pe) == nil && pe.Code != "" {
		err.Code = pe.Code
		err.Message = pe.Message
		if pe.RequestID != nil {
			err.RequestID = *pe.RequestID
		}
		err.Retryable = pe.Retryable
	} else {
		err.Message = strings.TrimSpace(string(body))
		if err.Message == "" {
			err.Message = http.StatusText(statusCode)
		}
	}
	return err
}

// StatusCode extracts the control-plane HTTP status code from an error,
// returning 0 when the error is not an API error (for example a transport
// failure).
func StatusCode(err error) int {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}
