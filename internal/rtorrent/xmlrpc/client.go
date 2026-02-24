package xmlrpc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/thiagokokada/gtorrent/internal/rtorrent/transport"
)

// Caller abstracts XML-RPC method calls and is safe to mock in tests.
type Caller interface {
	Call(ctx context.Context, method string, args ...any) (any, error)
}

// Client is a small XML-RPC client using a pluggable transport.
type Client struct {
	transport transport.Caller
}

func NewClient(c transport.Caller) *Client {
	return &Client{transport: c}
}

func (c *Client) Call(ctx context.Context, method string, args ...any) (any, error) {
	start := time.Now()
	payload, err := EncodeMethodCall(method, args)
	if err != nil {
		wrapped := fmt.Errorf("encode xml-rpc call: %w", err)
		slog.Error("rtorrent rpc failed", "method", method, "stage", "encode", "duration", time.Since(start).Round(time.Millisecond), "error", wrapped)
		return nil, wrapped
	}

	resp, err := c.transport.Do(ctx, payload)
	if err != nil {
		slog.Error("rtorrent rpc failed", "method", method, "stage", "transport", "duration", time.Since(start).Round(time.Millisecond), "error", err)
		return nil, err
	}

	val, fault, err := DecodeMethodResponse(resp)
	if err != nil {
		wrapped := fmt.Errorf("decode xml-rpc response: %w", err)
		slog.Error("rtorrent rpc failed", "method", method, "stage", "decode", "duration", time.Since(start).Round(time.Millisecond), "error", wrapped)
		return nil, wrapped
	}
	if fault != nil {
		slog.Error("rtorrent rpc failed", "method", method, "stage", "fault", "duration", time.Since(start).Round(time.Millisecond), "error", fault)
		return nil, fault
	}
	slog.Debug("rtorrent rpc", "method", method, "duration", time.Since(start).Round(time.Millisecond))
	return val, nil
}
