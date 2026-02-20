package transport

import "context"

// Caller executes one XML-RPC request payload and returns the raw XML response.
type Caller interface {
	Do(ctx context.Context, payload []byte) ([]byte, error)
}
