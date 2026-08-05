package taskhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// CreateTask enqueues a task (POST /v1/tasks). Requires API key when auth is enabled.
func (c *Client) CreateTask(ctx context.Context, in CreateTaskInput) (*Task, error) {
	raw, err := c.doJSON(ctx, http.MethodPost, "/v1/tasks", in, false)
	if err != nil {
		return nil, err
	}
	return decodeTaskData(raw)
}

// GetTask fetches a task (GET /v1/tasks/:id).
func (c *Client) GetTask(ctx context.Context, taskID string) (*Task, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task-hub client: taskID is required")
	}
	path := "/v1/tasks/" + url.PathEscape(taskID)
	raw, err := c.doJSON(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return nil, err
	}
	return decodeTaskData(raw)
}

// CancelTask cancels a task (POST /v1/tasks/:id/cancel).
func (c *Client) CancelTask(ctx context.Context, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("task-hub client: taskID is required")
	}
	path := "/v1/tasks/" + url.PathEscape(taskID) + "/cancel"
	_, err := c.doJSON(ctx, http.MethodPost, path, nil, false)
	return err
}

// Heartbeat renews an async lease (POST /v1/tasks/:id/heartbeat). Uses owner token when set.
func (c *Client) Heartbeat(ctx context.Context, taskID string, in HeartbeatInput) error {
	if taskID == "" {
		return fmt.Errorf("task-hub client: taskID is required")
	}
	path := "/v1/tasks/" + url.PathEscape(taskID) + "/heartbeat"
	var payload any
	if in.ProgressMessage != "" || in.ProgressCurrent != 0 || in.ProgressTotal != 0 {
		payload = in
	} else {
		payload = map[string]any{}
	}
	return c.postJSON(ctx, path, payload, true)
}

// Complete reports async success (POST /v1/tasks/:id/complete). Uses owner token when set.
func (c *Client) Complete(ctx context.Context, taskID string, result json.RawMessage) error {
	if taskID == "" {
		return fmt.Errorf("task-hub client: taskID is required")
	}
	path := "/v1/tasks/" + url.PathEscape(taskID) + "/complete"
	body := CompleteInput{}
	if len(result) > 0 {
		body.Result = result
	}
	return c.postJSON(ctx, path, body, true)
}

// Fail reports async failure (POST /v1/tasks/:id/fail). Uses owner token when set.
func (c *Client) Fail(ctx context.Context, taskID string, message string, retryable bool) error {
	if taskID == "" {
		return fmt.Errorf("task-hub client: taskID is required")
	}
	path := "/v1/tasks/" + url.PathEscape(taskID) + "/fail"
	return c.postJSON(ctx, path, FailInput{Error: message, Retryable: retryable}, true)
}

func decodeTaskData(raw []byte) (*Task, error) {
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("task-hub client: decode envelope: %w", err)
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return nil, fmt.Errorf("task-hub client: empty data in response")
	}
	var task Task
	if err := json.Unmarshal(env.Data, &task); err != nil {
		return nil, fmt.Errorf("task-hub client: decode task: %w", err)
	}
	return &task, nil
}
