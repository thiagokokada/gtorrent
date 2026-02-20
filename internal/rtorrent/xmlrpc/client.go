package xmlrpc

import (
	"context"
	"fmt"

	"gtorrent/internal/rtorrent/transport"
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
	payload, err := EncodeMethodCall(method, args)
	if err != nil {
		return nil, fmt.Errorf("encode xml-rpc call: %w", err)
	}

	resp, err := c.transport.Do(ctx, payload)
	if err != nil {
		return nil, err
	}

	val, fault, err := DecodeMethodResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("decode xml-rpc response: %w", err)
	}
	if fault != nil {
		return nil, fault
	}
	return val, nil
}
