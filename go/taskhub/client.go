// Package taskhub is the shared HTTP + gRPC helper surface for owner services
// that integrate with task-hub (create/cancel tasks, owner report, TaskCallback listen).
package taskhub

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	headerOwnerToken = "X-Task-Hub-Owner-Token"
	headerAPIKey     = "X-API-Key"
)

// Client talks to task-hub HTTP APIs.
type Client struct {
	baseURL    string
	apiKey     string
	ownerToken string
	httpClient *http.Client
}

// Option configures Client.
type Option func(*Client)

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) {
		if c != nil {
			cl.httpClient = c
		}
	}
}

// WithTimeout sets the HTTP client timeout (ignored if WithHTTPClient was used first with a custom client that already has Timeout set; applied to the default client).
func WithTimeout(d time.Duration) Option {
	return func(cl *Client) {
		if cl.httpClient == nil {
			cl.httpClient = &http.Client{Timeout: d}
			return
		}
		cl.httpClient.Timeout = d
	}
}

// WithAPIKey sets X-API-Key for consumer routes (and optional owner routes).
func WithAPIKey(key string) Option {
	return func(cl *Client) {
		cl.apiKey = strings.TrimSpace(key)
	}
}

// WithOwnerToken sets X-Task-Hub-Owner-Token for heartbeat/complete/fail.
func WithOwnerToken(token string) Option {
	return func(cl *Client) {
		cl.ownerToken = strings.TrimSpace(token)
	}
}

// New builds a Client. baseURL is required (e.g. http://task-hub:8080).
func New(baseURL string, opts ...Option) (*Client, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("task-hub client: baseURL is required")
	}
	cl := &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	for _, opt := range opts {
		opt(cl)
	}
	return cl, nil
}

// postJSON POSTs JSON to path and requires 2xx. Body may be nil.
func (c *Client) postJSON(ctx context.Context, path string, payload any, useOwnerToken bool) error {
	_, err := c.doJSON(ctx, http.MethodPost, path, payload, useOwnerToken)
	return err
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, useOwnerToken bool) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if useOwnerToken && c.ownerToken != "" {
		req.Header.Set(headerOwnerToken, c.ownerToken)
	}
	if c.apiKey != "" {
		req.Header.Set(headerAPIKey, c.apiKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return respBody, &APIError{StatusCode: resp.StatusCode, Path: path, Body: msg}
	}
	return respBody, nil
}
