package taskhub

import (
	"fmt"
)

// APIError is a non-2xx response from task-hub.
type APIError struct {
	StatusCode int
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	if e == nil {
		return "task-hub api error"
	}
	return fmt.Sprintf("task-hub %s status %d: %s", e.Path, e.StatusCode, e.Body)
}
