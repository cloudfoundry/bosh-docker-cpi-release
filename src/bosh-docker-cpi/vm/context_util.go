package vm

import (
	"context"
	"time"
)

// ContextWithTimeout creates a context with a default timeout for Docker operations
func ContextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// ContextWithCancel creates a cancellable context
func ContextWithCancel() (context.Context, context.CancelFunc) {
	return context.WithCancel(context.Background())
}

// DefaultDockerTimeout is the default timeout for Docker API operations
const DefaultDockerTimeout = 120 * time.Second

// ShortDockerTimeout is for quick operations like inspect
const ShortDockerTimeout = 30 * time.Second

// LongDockerTimeout is for operations that might take longer like pulls
const LongDockerTimeout = 300 * time.Second
