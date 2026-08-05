package taskhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
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

// ListTasks lists tasks (GET /v1/tasks). Requires API key when auth is enabled.
func (c *Client) ListTasks(ctx context.Context, in ListTasksInput) (*ListTasksResult, error) {
	q := url.Values{}
	if in.Queue != "" {
		q.Set("queue", in.Queue)
	}
	if in.State != "" {
		q.Set("state", in.State)
	}
	if in.OwnerService != "" {
		q.Set("owner_service", in.OwnerService)
	}
	if in.OwnerUserID != nil {
		q.Set("owner_user_id", fmt.Sprintf("%d", *in.OwnerUserID))
	}
	if in.CreatedSince != nil {
		q.Set("created_since", in.CreatedSince.UTC().Format(time.RFC3339))
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	q.Set("limit", fmt.Sprintf("%d", limit))
	page := 1
	if in.Offset > 0 {
		page = (in.Offset / limit) + 1
	}
	q.Set("page", fmt.Sprintf("%d", page))

	path := "/v1/tasks?" + q.Encode()
	raw, err := c.doJSON(ctx, http.MethodGet, path, nil, false)
	if err != nil {
		return nil, err
	}
	var env struct {
		Message  string          `json:"message"`
		Data     json.RawMessage `json:"data"`
		Metadata *struct {
			Pagination struct {
				Page  int   `json:"page"`
				Limit int   `json:"limit"`
				Total int64 `json:"total"`
			} `json:"pagination"`
		} `json:"metadata"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("task-hub client: decode list envelope: %w", err)
	}
	var tasks []Task
	if len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, &tasks); err != nil {
			return nil, fmt.Errorf("task-hub client: decode tasks: %w", err)
		}
	}
	out := &ListTasksResult{Tasks: tasks, Page: page, Limit: limit}
	if env.Metadata != nil {
		out.Total = env.Metadata.Pagination.Total
		if env.Metadata.Pagination.Page > 0 {
			out.Page = env.Metadata.Pagination.Page
		}
		if env.Metadata.Pagination.Limit > 0 {
			out.Limit = env.Metadata.Pagination.Limit
		}
	} else {
		out.Total = int64(len(tasks))
	}
	return out, nil
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

// envelope matches task-hub response.Response.
type envelope struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Code    string          `json:"code"`
}
