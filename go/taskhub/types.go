package taskhub

import (
	"encoding/json"
	"time"
)

// CreateTaskInput mirrors task-hub POST /v1/tasks body.
type CreateTaskInput struct {
	Type          string          `json:"type"`
	Queue         string          `json:"queue,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	MaxRetry      int             `json:"max_retry,omitempty"`
	OwnerService  string          `json:"owner_service"`
	OwnerUserID   *uint           `json:"owner_user_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	ProcessAt     *time.Time      `json:"process_at,omitempty"`
}

// Task is a task DTO from task-hub responses.
type Task struct {
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Queue           string          `json:"queue"`
	Payload         []byte          `json:"payload"`
	State           string          `json:"state"`
	MaxRetry        int             `json:"max_retry"`
	RetryCount      int             `json:"retry_count"`
	ProcessAt       *time.Time      `json:"process_at,omitempty"`
	OwnerService    string          `json:"owner_service"`
	OwnerUserID     *uint           `json:"owner_user_id,omitempty"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
	RequestID       string          `json:"request_id,omitempty"`
	LastError       string          `json:"last_error,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	CompletedAt     *time.Time      `json:"completed_at,omitempty"`
	LeaseUntil      *time.Time      `json:"lease_until,omitempty"`
	ProgressMessage string          `json:"progress_message,omitempty"`
	ProgressCurrent int             `json:"progress_current,omitempty"`
	ProgressTotal   int             `json:"progress_total,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
}

// HeartbeatInput is optional progress for owner heartbeat.
type HeartbeatInput struct {
	ProgressMessage string `json:"progress_message,omitempty"`
	ProgressCurrent int    `json:"progress_current,omitempty"`
	ProgressTotal   int    `json:"progress_total,omitempty"`
}

// CompleteInput is optional result for owner complete.
type CompleteInput struct {
	Result json.RawMessage `json:"result,omitempty"`
}

// FailInput is required for owner fail.
type FailInput struct {
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
}

// envelope matches task-hub response.Response.
type envelope struct {
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Code    string          `json:"code"`
}
