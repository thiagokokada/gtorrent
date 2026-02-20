package rtorrent

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"gtorrent/internal/domain"
	"gtorrent/internal/rtorrent/xmlrpc"
)

// Service describes torrent operations used by the HTTP handlers.
type Service interface {
	List(ctx context.Context) ([]domain.Torrent, error)
	AddMagnet(ctx context.Context, magnet string) error
	AddTorrent(ctx context.Context, data []byte, filename string) error
	Remove(ctx context.Context, hash string, deleteData bool) error
}

// Client is an rTorrent service backed by XML-RPC methods.
type Client struct {
	rpc xmlrpc.Caller
}

func NewClient(rpc xmlrpc.Caller) *Client {
	return &Client{rpc: rpc}
}

func (c *Client) List(ctx context.Context) ([]domain.Torrent, error) {
	result, err := c.rpc.Call(ctx, "d.multicall2",
		"",
		"main",
		"d.hash=",
		"d.name=",
		"d.size_bytes=",
		"d.completed_bytes=",
		"d.down.rate=",
		"d.up.rate=",
		"d.is_active=",
		"d.complete=",
	)
	if err != nil {
		return nil, err
	}

	outer, ok := result.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected multicall result type %T", result)
	}

	out := make([]domain.Torrent, 0, len(outer))
	for _, row := range outer {
		cols, ok := row.([]any)
		if !ok || len(cols) < 8 {
			continue
		}

		size := asInt64(cols[2])
		done := asInt64(cols[3])
		progress := 0.0
		if size > 0 {
			progress = float64(done) / float64(size)
		}
		if progress < 0 {
			progress = 0
		}
		if progress > 1 {
			progress = 1
		}
		progress = math.Round(progress*1000) / 1000

		active := asBool(cols[6])
		complete := asBool(cols[7])
		state := "stopped"
		switch {
		case complete && active:
			state = "seeding"
		case complete:
			state = "complete"
		case active:
			state = "downloading"
		}

		out = append(out, domain.Torrent{
			Hash:      asString(cols[0]),
			Name:      asString(cols[1]),
			SizeBytes: size,
			DoneBytes: done,
			Progress:  progress,
			State:     state,
			DownRate:  asInt64(cols[4]),
			UpRate:    asInt64(cols[5]),
		})
	}
	return out, nil
}

func (c *Client) AddMagnet(ctx context.Context, magnet string) error {
	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		return errors.New("magnet is required")
	}
	if !strings.HasPrefix(magnet, "magnet:") {
		return errors.New("invalid magnet URI")
	}

	methods := []string{"load.start", "load.normal"}
	var lastErr error
	for _, method := range methods {
		_, err := c.rpc.Call(ctx, method, "", magnet)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("add magnet failed: %w", lastErr)
}

func (c *Client) AddTorrent(ctx context.Context, data []byte, _ string) error {
	if len(data) == 0 {
		return errors.New("torrent payload is empty")
	}

	methods := []string{"load.raw_start", "load.raw", "load.start"}
	var lastErr error
	for _, method := range methods {
		_, err := c.rpc.Call(ctx, method, "", data)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("add torrent failed: %w", lastErr)
}

func (c *Client) Remove(ctx context.Context, hash string, deleteData bool) error {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return errors.New("hash is required")
	}

	if deleteData {
		_, _ = c.rpc.Call(ctx, "d.stop", hash)
		_, _ = c.rpc.Call(ctx, "d.close", hash)
	}
	_, err := c.rpc.Call(ctx, "d.erase", hash)
	if err != nil {
		return fmt.Errorf("remove torrent: %w", err)
	}
	return nil
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case bool:
		if t {
			return 1
		}
		return 0
	case string:
		if t == "" {
			return 0
		}
		var n int64
		_, _ = fmt.Sscan(t, &n)
		return n
	default:
		return 0
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "1" || s == "true" || s == "yes"
	default:
		return false
	}
}
