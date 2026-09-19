package ratelimit

import (
	"github.com/ulule/limiter/v3/drivers/store/memory"
)

// NewMemoryStore returns an in-memory ulule store.
func NewMemoryStore() Store {
	return memory.NewStore()
}
